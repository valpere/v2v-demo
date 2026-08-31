package stt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// localWhisper shells out to the openai-whisper CLI. It ingests the OGG
// directly (Whisper decodes it via its own ffmpeg call — no manual
// conversion). The model name auto-downloads to ~/.cache/whisper on first
// use, so the first transcription blocks for a while (~1.5 GB for "turbo").
type localWhisper struct {
	bin   string // CLI name, default "whisper"
	model string // model NAME, default "turbo" (faster than medium on CPU + better Ukrainian)
	lang  string // "auto" | "uk" | "en" — the config default, overridden by a call's langHint
}

// NewLocal builds the dev-default Transcriber.
func NewLocal(bin, model, lang string) Transcriber {
	if bin == "" {
		bin = "whisper"
	}
	if model == "" {
		model = "turbo"
	}
	if lang == "" {
		lang = "auto"
	}
	return &localWhisper{bin: bin, model: model, lang: lang}
}

func (w *localWhisper) Transcribe(ctx context.Context, oggPath, langHint string) (string, error) {
	lang := langHint
	if lang == "" || lang == "auto" {
		lang = w.lang
	}

	outDir, err := os.MkdirTemp("", "v2v-stt-*")
	if err != nil {
		return "", fmt.Errorf("stt: temp dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	cmd := exec.CommandContext(ctx, w.bin, whisperArgs(oggPath, outDir, w.model, lang)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("stt: %s: %w: %s", w.bin, err, tail(stderr.String(), 500))
	}

	stem := strings.TrimSuffix(filepath.Base(oggPath), filepath.Ext(oggPath))
	data, err := os.ReadFile(filepath.Join(outDir, stem+".txt"))
	if err != nil {
		return "", fmt.Errorf("stt: reading transcript: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// whisperArgs builds the CLI invocation. --language is passed only for a
// concrete "uk"/"en"; anything else lets Whisper auto-detect.
func whisperArgs(oggPath, outDir, model, lang string) []string {
	args := []string{
		oggPath,
		"--model", model,
		"--task", "transcribe",
		"--output_format", "txt",
		"--output_dir", outDir,
		"--fp16", "False",
		"--verbose", "False",
	}
	if lang == "uk" || lang == "en" {
		args = append(args, "--language", lang)
	}
	return args
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
