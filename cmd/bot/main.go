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
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/kb"
	"github.com/valpere/v2v-demo/internal/stt"
	"github.com/valpere/v2v-demo/internal/telegram"
	"github.com/valpere/v2v-demo/internal/tts"
)

type app struct {
	cfg      Config
	tg       telegram.Client
	gen      dialog.Generator
	stt      stt.Transcriber
	tts      tts.Synthesizer
	kb       []kb.Section
	sys      string
	greeting string
	loc      *time.Location // bureau timezone (BOT_TIMEZONE) — for the prompt's CURRENT TIME block

	mu       sync.Mutex
	sessions map[int64]*dialog.Session
	seen     map[int64]bool
	inbox    map[int64]chan telegram.Update // per-chat FIFO queue (one serial worker each)
}

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	sections, err := kb.Load(cfg.KBPath)
	if err != nil {
		log.Fatal(err)
	}
	sys, err := os.ReadFile(cfg.SystemPromptPath)
	if err != nil {
		log.Fatalf("system prompt: %v", err)
	}
	greeting, err := greetingBody(cfg.GreetingPath)
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
		kb:       sections,
		sys:      string(sys),
		greeting: greeting,
		loc:      loc,
		sessions: make(map[int64]*dialog.Session),
		seen:     make(map[int64]bool),
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
	log.Printf("v2v-demo: listening — dialog=%s/%s tts=%s stt=%s tz=%s; %d KB sections",
		cfg.DialogBackend, dialogModel, cfg.TTSBackend, cfg.STTBackend, cfg.Timezone, len(sections))

	for u := range updates {
		a.dispatch(ctx, u)
	}
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
		go a.chatWorker(ctx, ch)
	}
	a.mu.Unlock()

	select {
	case ch <- u:
	case <-ctx.Done():
	}
}

func (a *app) chatWorker(ctx context.Context, ch <-chan telegram.Update) {
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-ch:
			a.handleUpdate(ctx, u)
		}
	}
}

// newGenerator selects the dialogue backend (DIALOG_BACKEND). An empty
// DIALOG_MODEL lets each impl pick its own default.
func newGenerator(cfg Config) (dialog.Generator, error) {
	switch cfg.DialogBackend {
	case "ollama":
		return dialog.NewOllama(cfg.OllamaBaseURL, cfg.DialogModel), nil
	case "openai":
		return dialog.NewOpenAI(cfg.OpenAIKey, cfg.DialogModel), nil
	case "gemini":
		return dialog.NewGemini(cfg.GeminiKey, cfg.DialogModel), nil
	default:
		return nil, fmt.Errorf("unknown DIALOG_BACKEND %q", cfg.DialogBackend)
	}
}

// newTranscriber selects the STT backend (STT_BACKEND).
func newTranscriber(cfg Config) (stt.Transcriber, error) {
	switch cfg.STTBackend {
	case "local":
		return stt.NewLocal(cfg.WhisperBin, cfg.WhisperModel, cfg.WhisperLang), nil
	case "openai":
		return stt.NewOpenAI(cfg.OpenAIKey, ""), nil
	default:
		return nil, fmt.Errorf("unknown STT_BACKEND %q", cfg.STTBackend)
	}
}

// newSynthesizer selects the TTS backend (TTS_BACKEND). "none" returns nil —
// the update loop then skips SendVoice and replies with text only.
func newSynthesizer(cfg Config) (tts.Synthesizer, error) {
	switch cfg.TTSBackend {
	case "none":
		return nil, nil
	case "elevenlabs":
		return tts.NewElevenLabs(cfg.ElevenKey), nil
	case "azure":
		return tts.NewAzure(cfg.AzureKey, cfg.AzureRegion), nil
	default:
		return nil, fmt.Errorf("unknown TTS_BACKEND %q", cfg.TTSBackend)
	}
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
