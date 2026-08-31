// Command bot is the v2v-demo Telegram voice assistant. Build-order step 5:
// the full loop on text and voice — STT (local Whisper) → grounding gate +
// Ollama generator → ElevenLabs TTS → voice + text reply, plus the greeting
// and the /voice command. Config-flag alternates (openai/gemini/azure,
// whisper-1) land in step 6.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

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
		sessions: make(map[int64]*dialog.Session),
		seen:     make(map[int64]bool),
		inbox:    make(map[int64]chan telegram.Update),
	}

	updates, err := tg.Updates(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("v2v-demo: listening — dialog=%s/%s tts=%s stt=%s; %d KB sections",
		cfg.DialogBackend, cfg.DialogModel, cfg.TTSBackend, cfg.STTBackend, len(sections))

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

// newGenerator selects the dialogue backend. openai / gemini land in step 6.
func newGenerator(cfg Config) (dialog.Generator, error) {
	switch cfg.DialogBackend {
	case "ollama":
		return dialog.NewOllama(cfg.OllamaBaseURL, cfg.DialogModel), nil
	case "openai", "gemini":
		return nil, fmt.Errorf("DIALOG_BACKEND=%s lands in build-order step 6", cfg.DialogBackend)
	default:
		return nil, fmt.Errorf("unknown DIALOG_BACKEND %q", cfg.DialogBackend)
	}
}

// newTranscriber selects the STT backend. openai (whisper-1) lands in step 6.
func newTranscriber(cfg Config) (stt.Transcriber, error) {
	switch cfg.STTBackend {
	case "local":
		return stt.NewLocal(cfg.WhisperBin, cfg.WhisperModel, cfg.WhisperLang), nil
	case "openai":
		return nil, fmt.Errorf("STT_BACKEND=openai lands in build-order step 6")
	default:
		return nil, fmt.Errorf("unknown STT_BACKEND %q", cfg.STTBackend)
	}
}

// newSynthesizer selects the TTS backend. azure lands in step 6.
func newSynthesizer(cfg Config) (tts.Synthesizer, error) {
	switch cfg.TTSBackend {
	case "elevenlabs":
		return tts.NewElevenLabs(cfg.ElevenKey), nil
	case "azure":
		return nil, fmt.Errorf("TTS_BACKEND=azure lands in build-order step 6")
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
