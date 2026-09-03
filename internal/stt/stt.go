// Package stt transcribes a voice message to text (REQ-STT-01). Two impls,
// selected by STT_BACKEND in cmd/bot: local (the openai-whisper CLI, the dev
// default) and openai (whisper-1 API, the mandatory flip for the I-10 client
// recording). A failure never auto-switches backends.
package stt

import (
	"context"
	"strings"
)

// Transcriber turns a local OGG file into text.
type Transcriber interface {
	// Transcribe reads oggPath and returns the transcript. langHint is
	// "" | "auto" | "uk" | "en" — a concrete hint pins Whisper's language,
	// otherwise it auto-detects.
	Transcribe(ctx context.Context, oggPath, langHint string) (string, error)
}

// hallucinations are the boilerplate phrases Whisper (both the CLI and
// whisper-1) emits on silence / breathing / a cough — it was trained on
// YouTube captions. If the whole transcript is just one of these, the turn
// had no speech.
var hallucinations = []string{
	"дякую за перегляд", "дякую за увагу", "дякуємо за перегляд",
	"продовження буде", "субтитрував не будеш", "субтитри від",
	"thanks for watching", "thank you for watching", "please subscribe",
	"subtitles by the amara.org community",
}

// IsNonSpeech reports whether a transcript is empty or just a known Whisper
// hallucination — the caller should treat it like a failed transcription.
func IsNonSpeech(transcript string) bool {
	t := strings.ToLower(strings.TrimSpace(transcript))
	t = strings.Trim(t, ".!?…- ")
	if t == "" {
		return true
	}
	for _, h := range hallucinations {
		if t == h {
			return true
		}
	}
	return false
}
