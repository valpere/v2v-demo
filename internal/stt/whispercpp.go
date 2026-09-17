package stt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// whisperCPP shells out to whisper.cpp's whisper-cli binary. Unlike
// localWhisper (openai-whisper), it cannot decode OGG/Opus directly — its
// built-in decoder (miniaudio) handles WAV/MP3/FLAC only, confirmed against
// a real ElevenLabs opus_48000_128 file returning "failed to read audio
// data" (2026-09-17). So this backend converts OGG→WAV via the ffmpeg CLI
// first, mirroring internal/tts/espeak.go's two-subprocess shape exactly.
//
// Needs a pre-built binary and a downloaded ggml model file — neither
// happens for free on a fresh checkout (no pip-install equivalent), so this
// is opt-in via STT_BACKEND=whispercpp, not the default.
type whisperCPP struct {
	bin       string // whisper-cli binary path, default "whisper-cli"
	ffmpegBin string // default "ffmpeg" — shared with espeak.go's dependency
	modelPath string // path to a downloaded ggml .bin — required, no default
	threads   int    // 0 = omit -t, let whisper-cli use its own default.
	// Host-sensitive, not "more is better": measured 4 threads=69.7s,
	// 8 threads=39.7s (best on that host), 16 threads=3m31s (severe
	// contention) on the same 102.6s clip — benchmark per host, never
	// auto-guess.
	lang string // config default (WHISPER_CPP_LANG), overridden by a call's langHint
}

// NewWhisperCPP builds the whisper.cpp Transcriber. lang == "" defaults to
// "auto" (see whisperCPPArgs — whisper-cli's own -l default is "en", not
// auto-detect, so this package must never rely on omitting the flag).
func NewWhisperCPP(bin, ffmpegBin, modelPath string, threads int, lang string) Transcriber {
	if bin == "" {
		bin = "whisper-cli"
	}
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}
	if lang == "" {
		lang = "auto"
	}
	return &whisperCPP{bin: bin, ffmpegBin: ffmpegBin, modelPath: modelPath, threads: threads, lang: lang}
}

func (w *whisperCPP) Transcribe(ctx context.Context, oggPath, langHint string) (string, error) {
	lang := langHint
	if lang == "" || lang == "auto" {
		lang = w.lang
	}

	dir, err := os.MkdirTemp("", "v2v-stt-*")
	if err != nil {
		return "", fmt.Errorf("stt: whispercpp: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	wavPath := filepath.Join(dir, "in.wav")
	ffmpegCmd := exec.CommandContext(ctx, w.ffmpegBin,
		"-y", "-i", oggPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wavPath)
	var ffmpegErr strings.Builder
	ffmpegCmd.Stderr = &ffmpegErr
	if err := ffmpegCmd.Run(); err != nil {
		return "", fmt.Errorf("stt: whispercpp: %s: %w: %s", w.ffmpegBin, err, tail(ffmpegErr.String(), 500))
	}

	outPrefix := filepath.Join(dir, "out")
	cliCmd := exec.CommandContext(ctx, w.bin, whisperCPPArgs(wavPath, outPrefix, w.modelPath, lang, w.threads)...)
	var cliErr strings.Builder
	cliCmd.Stderr = &cliErr
	if err := cliCmd.Run(); err != nil {
		return "", fmt.Errorf("stt: whispercpp: %s: %w: %s", w.bin, err, tail(cliErr.String(), 500))
	}

	data, err := os.ReadFile(outPrefix + ".txt")
	if err != nil {
		return "", fmt.Errorf("stt: whispercpp: reading transcript: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// whisperCPPArgs builds the whisper-cli invocation. -l is ALWAYS passed
// (never omitted) — whisper-cli's own default is "en" if -l is absent,
// the opposite of openai-whisper's CLI (see NewWhisperCPP's doc comment).
// -t is passed only when threads > 0; 0 means "let whisper-cli pick its
// own default", not "auto-tune" — see the threads field's doc comment.
func whisperCPPArgs(wavPath, outPrefix, modelPath, lang string, threads int) []string {
	args := []string{
		"-m", modelPath,
		"-f", wavPath,
		"-l", lang,
	}
	if threads > 0 {
		args = append(args, "-t", strconv.Itoa(threads))
	}
	args = append(args, "-otxt", "-of", outPrefix)
	return args
}
