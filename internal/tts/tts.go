// Package tts synthesises the reply text to OGG/Opus mono, ready for
// Telegram sendVoice (REQ-TTS-01). Three impls, selected by TTS_BACKEND in
// cmd/bot: elevenlabs (eleven_multilingual_v2, the demo default), azure
// (uk-UA-*Neural, the rollback), and espeak (espeak-ng formant synthesis —
// free, local, robotic, intended as a TTS_FALLBACK_BACKEND "spare tire"
// behind the other two, not a primary voice). Google is a documented
// fourth impl, not built. A failure degrades to a text-only reply, unless
// TTS_FALLBACK_BACKEND is set — then FailoverSynthesizer retries once
// against a second backend before giving up.
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
