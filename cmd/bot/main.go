// Command bot is the v2v-demo Telegram voice assistant. Build-order step 3:
// the dialogue core runs on text messages (grounding gate + Ollama generator
// + trailer parse + slot merge + JSONL logging). STT / TTS land in later steps.
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
	"github.com/valpere/v2v-demo/internal/telegram"
)

type app struct {
	cfg Config
	tg  telegram.Client
	gen dialog.Generator
	kb  []kb.Section
	sys string

	mu        sync.Mutex
	sessions  map[int64]*dialog.Session
	chatLocks map[int64]*sync.Mutex
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
	gen, err := newGenerator(cfg)
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
		cfg:       cfg,
		tg:        tg,
		gen:       gen,
		kb:        sections,
		sys:       string(sys),
		sessions:  make(map[int64]*dialog.Session),
		chatLocks: make(map[int64]*sync.Mutex),
	}

	updates, err := tg.Updates(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("v2v-demo: listening — dialog=%s/%s tts=%s stt=%s; %d KB sections",
		cfg.DialogBackend, cfg.DialogModel, cfg.TTSBackend, cfg.STTBackend, len(sections))

	for u := range updates {
		go a.handleUpdate(ctx, u)
	}
	log.Print("v2v-demo: shut down")
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
