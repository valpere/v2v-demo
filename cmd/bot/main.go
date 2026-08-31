// Command bot is the v2v-demo Telegram voice assistant. Build-order step 1
// wires config + KB loading; the long-poll update loop (step 2 onward) is
// added next.
package main

import (
	"log"

	"github.com/valpere/v2v-demo/internal/kb"
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

	log.Printf("v2v-demo: config ok — dialog=%s/%s tts=%s stt=%s; %d KB sections from %s",
		cfg.DialogBackend, cfg.DialogModel, cfg.TTSBackend, cfg.STTBackend, len(sections), cfg.KBPath)
}
