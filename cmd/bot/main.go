// Command bot is the v2v-demo Telegram voice assistant. Build-order step 2:
// the long-poll loop echoes text back. STT / dialogue / TTS land in later
// steps, replacing handleUpdate.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/valpere/v2v-demo/internal/kb"
	"github.com/valpere/v2v-demo/internal/telegram"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	sections, err := kb.Load(cfg.KBPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tg, err := telegram.New(cfg.TelegramToken)
	if err != nil {
		log.Fatal(err)
	}
	updates, err := tg.Updates(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("v2v-demo: listening — dialog=%s/%s tts=%s stt=%s; %d KB sections",
		cfg.DialogBackend, cfg.DialogModel, cfg.TTSBackend, cfg.STTBackend, len(sections))

	for u := range updates {
		go handleUpdate(ctx, tg, u)
	}
	log.Print("v2v-demo: shut down")
}

func handleUpdate(ctx context.Context, tg telegram.Client, u telegram.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered panic (chat %d): %v", u.ChatID, r)
		}
	}()

	switch {
	case u.VoiceFileID != "":
		reply(ctx, tg, u.ChatID, "(voice received — transcription lands in a later build step)")
	case u.Text != "":
		reply(ctx, tg, u.ChatID, u.Text) // echo
	}
}

func reply(ctx context.Context, tg telegram.Client, chatID int64, text string) {
	if err := tg.SendText(ctx, chatID, text); err != nil {
		log.Printf("send (chat %d): %v", chatID, err)
	}
}
