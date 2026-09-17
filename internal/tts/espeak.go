package tts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// espeakSynth shells out to the espeak-ng CLI (formant synthesis) plus
// ffmpeg to transcode to OGG/Opus for Telegram sendVoice. Robotic-sounding
// but covers both uk and en with no key, no ML weights, and no per-request
// cost — the intended role is a TTS_FALLBACK_BACKEND "spare tire" behind
// elevenlabs/azure, not a primary voice.
type espeakSynth struct {
	bin       string // espeak-ng binary, default "espeak-ng"
	ffmpegBin string // default "ffmpeg" — no config knob, matches
	// minions/i10-sample's existing hardcoded "ffmpeg"
}

// NewEspeak builds the espeak-ng Synthesizer.
func NewEspeak(bin string) Synthesizer {
	if bin == "" {
		bin = "espeak-ng"
	}
	return &espeakSynth{bin: bin, ffmpegBin: "ffmpeg"}
}

// Speak ignores voiceID — eSpeak's "voice" concept is the language, not a
// distinct speaker identity like ElevenLabs/Azure voice ids. This backend
// is only ever reached as a fallback behind elevenlabs/azure, whose voice
// ids are meaningless to espeak-ng anyway. lang "" defaults to "uk",
// matching this codebase's established "en, or else Ukrainian" idiom.
func (e *espeakSynth) Speak(ctx context.Context, text, _, lang string) ([]byte, error) {
	voice := "uk"
	if lang == "en" {
		voice = "en-us"
	}

	dir, err := os.MkdirTemp("", "v2v-tts-*")
	if err != nil {
		return nil, fmt.Errorf("tts: espeak: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	wavPath := filepath.Join(dir, "out.wav")
	oggPath := filepath.Join(dir, "out.ogg")

	espeakCmd := exec.CommandContext(ctx, e.bin, "-v", voice, "-w", wavPath, text)
	var espeakErr strings.Builder
	espeakCmd.Stderr = &espeakErr
	if err := espeakCmd.Run(); err != nil {
		return nil, fmt.Errorf("tts: espeak: %s: %w: %s", e.bin, err, tail(espeakErr.String(), 500))
	}

	// 48kHz mono opus matches azure.go's ogg-48khz-16bit-mono-opus — not a
	// quality requirement for espeak's source audio, just keeps the same
	// container/codec Telegram already gets from both other backends.
	ffmpegCmd := exec.CommandContext(ctx, e.ffmpegBin,
		"-y", "-i", wavPath, "-ac", "1", "-ar", "48000", "-c:a", "libopus", "-f", "ogg", oggPath)
	var ffmpegErr strings.Builder
	ffmpegCmd.Stderr = &ffmpegErr
	if err := ffmpegCmd.Run(); err != nil {
		return nil, fmt.Errorf("tts: espeak: %s: %w: %s", e.ffmpegBin, err, tail(ffmpegErr.String(), 500))
	}

	data, err := os.ReadFile(oggPath)
	if err != nil {
		return nil, fmt.Errorf("tts: espeak: reading ogg: %w", err)
	}
	return data, nil
}
