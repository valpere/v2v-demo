// Package stt transcribes a voice message to text (REQ-STT-01). Two impls,
// selected by STT_BACKEND in cmd/bot: local (the openai-whisper CLI, the dev
// default) and openai (whisper-1 API, the mandatory flip for the I-10 client
// recording). A failure never auto-switches backends.
package stt

import "context"

// Transcriber turns a local OGG file into text.
type Transcriber interface {
	// Transcribe reads oggPath and returns the transcript. langHint is
	// "" | "auto" | "uk" | "en" — a concrete hint pins Whisper's language,
	// otherwise it auto-detects.
	Transcribe(ctx context.Context, oggPath, langHint string) (string, error)
}
