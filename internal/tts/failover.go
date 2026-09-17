package tts

import (
	"context"
	"log"
)

// FailoverSynthesizer tries Primary; on ANY error it retries the same call
// once against Fallback. No transient/permanent error classification —
// see the identical design note on dialog.FailoverGenerator /
// stt.FailoverTranscriber; the same trade-off applies here.
//
// The intended pairing is a paid primary (elevenlabs/azure) with the local
// espeakSynth as Fallback — espeakSynth ignores voiceID entirely, so a
// voice id meant for the primary backend passed through unchanged on
// failover is harmless (see NewEspeak's doc comment).
type FailoverSynthesizer struct {
	Primary, Fallback Synthesizer
}

// Speak implements Synthesizer.
func (f *FailoverSynthesizer) Speak(ctx context.Context, text, voiceID, lang string) ([]byte, error) {
	data, err := f.Primary.Speak(ctx, text, voiceID, lang)
	if err == nil {
		return data, nil
	}
	log.Printf("tts: primary synthesizer failed, falling back: %v", err)
	return f.Fallback.Speak(ctx, text, voiceID, lang)
}
