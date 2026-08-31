// Package tts synthesises the reply text to OGG/Opus mono, ready for
// Telegram sendVoice (REQ-TTS-01). Two impls, selected by TTS_BACKEND in
// cmd/bot: elevenlabs (eleven_multilingual_v2, the demo default) and azure
// (uk-UA-*Neural, the rollback). Google is a documented third impl, not built.
package tts

import (
	"context"
	"strings"
)

// Synthesizer turns text into a voice message.
type Synthesizer interface {
	// Speak returns OGG/Opus mono audio for voiceID. lang is "uk" | "en" | ""
	// — used by backends that need an explicit language (Azure SSML);
	// ignored by ElevenLabs multilingual_v2, which auto-detects.
	Speak(ctx context.Context, text, voiceID, lang string) ([]byte, error)
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
