package stt

import (
	"context"
	"log"
)

// FailoverTranscriber tries Primary; on ANY error it retries the same
// oggPath once against Fallback. No transient/permanent error
// classification — see the identical design note on
// dialog.FailoverGenerator; the same trade-off applies here, and unlike
// the dialog/TTS backends, neither Transcriber impl even computes a
// transient flag internally today.
//
// oggPath is only read, never deleted, by Transcribe — the caller
// (cmd/bot/handler.go's resolveText) removes the temp file after this
// whole call returns, so trying two backends against the same path in
// sequence is safe.
type FailoverTranscriber struct {
	Primary, Fallback Transcriber
}

// Transcribe implements Transcriber.
func (f *FailoverTranscriber) Transcribe(ctx context.Context, oggPath, langHint string) (string, error) {
	text, err := f.Primary.Transcribe(ctx, oggPath, langHint)
	if err == nil {
		return text, nil
	}
	log.Printf("stt: primary transcriber failed, falling back: %v", err)
	return f.Fallback.Transcribe(ctx, oggPath, langHint)
}
