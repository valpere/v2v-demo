// Command bot is the v2v-demo Telegram voice assistant: the full loop on
// text and voice — STT (local Whisper / whisper-1) → grounding gate +
// generator (ollama / openai / gemini) → TTS (ElevenLabs / Azure) → voice +
// text reply, plus the greeting and the /voice command. Backends are picked
// by the *_BACKEND env vars.
package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/stt"
	"github.com/valpere/v2v-demo/internal/telegram"
	"github.com/valpere/v2v-demo/internal/tts"
)

type app struct {
	cfg Config
	tg  telegram.Client
	gen dialog.Generator
	stt stt.Transcriber
	tts tts.Synthesizer
	loc *time.Location // bureau timezone (BOT_TIMEZONE) — for the prompt's CURRENT TIME block

	topics   map[string]topicBundle // >1 entry -> a picker is shown after /start
	topicIDs []string               // display order (map iteration isn't stable)

	sessions sessionStore

	mu    sync.Mutex
	inbox map[int64]chan telegram.Update // per-chat FIFO queue (one serial worker each)
	wg    sync.WaitGroup                 // outstanding chatWorker goroutines — see main()'s shutdown order
}

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	topics, topicIDs, err := loadTopics(cfg)
	if err != nil {
		log.Fatal(err)
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Fatalf("BOT_TIMEZONE %q: %v", cfg.Timezone, err)
	}
	gen, err := newGenerator(cfg)
	if err != nil {
		log.Fatal(err)
	}
	transcriber, err := newTranscriber(cfg)
	if err != nil {
		log.Fatal(err)
	}
	synth, err := newSynthesizer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	sessions, err := newSessionStore(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer sessions.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tg, err := telegram.New(cfg.TelegramToken)
	if err != nil {
		log.Fatal(err)
	}

	a := &app{
		cfg:      cfg,
		tg:       tg,
		gen:      gen,
		stt:      transcriber,
		tts:      synth,
		loc:      loc,
		topics:   topics,
		topicIDs: topicIDs,
		sessions: sessions,
		inbox:    make(map[int64]chan telegram.Update),
	}

	updates, err := tg.Updates(ctx)
	if err != nil {
		log.Fatal(err)
	}

	dialogModel := cfg.DialogModel
	if dialogModel == "" {
		dialogModel = "(backend default)"
	}
	dialogDesc := fmt.Sprintf("%s/%s", cfg.DialogBackend, dialogModel)
	if cfg.DialogFallbackBackend != "" {
		dialogDesc += "→" + cfg.DialogFallbackBackend
	}
	sttDesc := cfg.STTBackend
	if cfg.STTFallbackBackend != "" {
		sttDesc += "→" + cfg.STTFallbackBackend
	}
	ttsDesc := cfg.TTSBackend
	if cfg.TTSFallbackBackend != "" {
		ttsDesc += "→" + cfg.TTSFallbackBackend
	}
	log.Printf("v2v-demo: listening — dialog=%s tts=%s stt=%s sessions=%s tz=%s; %d topic(s)",
		dialogDesc, ttsDesc, sttDesc, cfg.SessionStore, cfg.Timezone, len(topics))

	for u := range updates {
		a.dispatch(ctx, u)
	}
	// every chatWorker exits once ctx is cancelled (the same ctx that closed
	// the updates channel above), but one may still be mid-handleUpdate —
	// wait for it before the deferred sessions.Close() runs, or a save can
	// race a session-store shutdown.
	a.wg.Wait()
	log.Print("v2v-demo: shut down")
}

// dispatch routes an update to its chat's serial worker, spawning the worker
// on first contact. This keeps one chat's turns in strict arrival order (a
// per-chat lock would serialise them but not order them) while different
// chats run concurrently (B5).
func (a *app) dispatch(ctx context.Context, u telegram.Update) {
	a.mu.Lock()
	ch := a.inbox[u.ChatID]
	if ch == nil {
		ch = make(chan telegram.Update, 64)
		a.inbox[u.ChatID] = ch
		a.wg.Add(1)
		go a.chatWorker(ctx, ch)
	}
	a.mu.Unlock()

	select {
	case ch <- u:
	case <-ctx.Done():
	}
}

func (a *app) chatWorker(ctx context.Context, ch <-chan telegram.Update) {
	defer a.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-ch:
			a.handleUpdate(ctx, u)
		}
	}
}

// buildGenerator resolves one dialogue backend by name — the step
// newGenerator needs twice when a fallback is configured (once for
// DIALOG_BACKEND, once for DIALOG_FALLBACK_BACKEND). An empty
// DIALOG_MODEL lets each impl pick its own default.
func buildGenerator(name string, cfg Config) (dialog.Generator, error) {
	switch name {
	case "ollama":
		return dialog.NewOllama(cfg.OllamaBaseURL, cfg.DialogModel), nil
	case "openai":
		return dialog.NewOpenAI(cfg.OpenAIKey, cfg.DialogModel), nil
	case "gemini":
		return dialog.NewGemini(cfg.GeminiKey, cfg.DialogModel), nil
	default:
		return nil, fmt.Errorf("unknown DIALOG_BACKEND %q", name)
	}
}

// newGenerator selects the dialogue backend (DIALOG_BACKEND), wrapping it
// in a dialog.FailoverGenerator when DIALOG_FALLBACK_BACKEND is set.
func newGenerator(cfg Config) (dialog.Generator, error) {
	primary, err := buildGenerator(cfg.DialogBackend, cfg)
	if err != nil || cfg.DialogFallbackBackend == "" {
		return primary, err
	}
	fallback, err := buildGenerator(cfg.DialogFallbackBackend, cfg)
	if err != nil {
		return nil, fmt.Errorf("dialog fallback: %w", err)
	}
	return &dialog.FailoverGenerator{Primary: primary, Fallback: fallback}, nil
}

// buildTranscriber resolves one STT backend by name — the step
// newTranscriber needs twice when a fallback is configured. "none" is only
// valid as the primary (resolveText then declines voice messages outright);
// it makes no sense as a fallback target, so callers building a fallback
// reject it themselves (see newTranscriber).
func buildTranscriber(name string, cfg Config) (stt.Transcriber, error) {
	switch name {
	case "none":
		return nil, nil
	case "local":
		return stt.NewLocal(cfg.WhisperBin, cfg.WhisperModel, cfg.WhisperLang), nil
	case "openai":
		return stt.NewOpenAI(cfg.OpenAIKey, ""), nil
	case "whispercpp":
		return stt.NewWhisperCPP(cfg.WhisperCPPBin, cfg.FfmpegBin, cfg.WhisperCPPModelPath,
			cfg.WhisperCPPThreads, cfg.WhisperCPPLang), nil
	default:
		return nil, fmt.Errorf("unknown STT_BACKEND %q", name)
	}
}

// newTranscriber selects the STT backend (STT_BACKEND), wrapping it in a
// stt.FailoverTranscriber when STT_FALLBACK_BACKEND is set. "none" returns
// nil — resolveText then declines any voice message with a fixed line
// instead of attempting STT.
func newTranscriber(cfg Config) (stt.Transcriber, error) {
	primary, err := buildTranscriber(cfg.STTBackend, cfg)
	if err != nil || cfg.STTFallbackBackend == "" {
		return primary, err
	}
	if cfg.STTFallbackBackend == "none" {
		return nil, fmt.Errorf("STT_FALLBACK_BACKEND cannot be %q", "none")
	}
	fallback, err := buildTranscriber(cfg.STTFallbackBackend, cfg)
	if err != nil {
		return nil, fmt.Errorf("stt fallback: %w", err)
	}
	return &stt.FailoverTranscriber{Primary: primary, Fallback: fallback}, nil
}

// buildSynthesizer resolves one TTS backend by name — the step
// newSynthesizer needs twice when a fallback is configured. "none" is only
// valid as the primary (the update loop then skips SendVoice and replies
// with text only); it makes no sense as a fallback target, so callers
// building a fallback reject it themselves (see newSynthesizer).
func buildSynthesizer(name string, cfg Config) (tts.Synthesizer, error) {
	switch name {
	case "none":
		return nil, nil
	case "elevenlabs":
		return tts.NewElevenLabs(cfg.ElevenKey), nil
	case "azure":
		return tts.NewAzure(cfg.AzureKey, cfg.AzureRegion), nil
	case "espeak":
		return tts.NewEspeak(cfg.EspeakBin, cfg.FfmpegBin), nil
	default:
		return nil, fmt.Errorf("unknown TTS_BACKEND %q", name)
	}
}

// newSynthesizer selects the TTS backend (TTS_BACKEND), wrapping it in a
// tts.FailoverSynthesizer when TTS_FALLBACK_BACKEND is set. "none" returns
// nil — the update loop then skips SendVoice and replies with text only.
func newSynthesizer(cfg Config) (tts.Synthesizer, error) {
	primary, err := buildSynthesizer(cfg.TTSBackend, cfg)
	if err != nil || cfg.TTSFallbackBackend == "" {
		return primary, err
	}
	if cfg.TTSFallbackBackend == "none" {
		return nil, fmt.Errorf("TTS_FALLBACK_BACKEND cannot be %q", "none")
	}
	fallback, err := buildSynthesizer(cfg.TTSFallbackBackend, cfg)
	if err != nil {
		return nil, fmt.Errorf("tts fallback: %w", err)
	}
	return &tts.FailoverSynthesizer{Primary: primary, Fallback: fallback}, nil
}

// voiceID resolves the active backend's voice id for "a" | "b".
func voiceID(cfg Config, voice string) string {
	b := voice == "b"
	if cfg.TTSBackend == "azure" {
		if b {
			return cfg.AzureVoiceB
		}
		return cfg.AzureVoiceA
	}
	if b {
		return cfg.ElevenVoiceB
	}
	return cfg.ElevenVoiceA
}
