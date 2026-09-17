package stt

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWhisperCPPArgs(t *testing.T) {
	// -l is ALWAYS present, never omitted — regression test for the
	// hazard found 2026-09-17: whisper-cli's own -l default is "en", not
	// auto-detect, the opposite of openai-whisper's CLI.
	args := whisperCPPArgs("/tmp/in.wav", "/tmp/out", "/models/m.bin", "auto", 0)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-m /models/m.bin", "-f /tmp/in.wav", "-l auto", "-otxt", "-of /tmp/out"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	if strings.Contains(joined, "-t ") {
		t.Errorf("threads=0 should omit -t: %v", args)
	}

	withThreads := whisperCPPArgs("/tmp/in.wav", "/tmp/out", "/models/m.bin", "uk", 8)
	if !strings.Contains(strings.Join(withThreads, " "), "-t 8") {
		t.Errorf("threads=8 should pass -t 8: %v", withThreads)
	}
}

// fakeWhisperCLI writes a script at dir/whisper-cli that mimics the CLI:
// finds -of and writes "<prefix>.txt" (or exits 1 with failMsg on stderr).
// Records the invocation's args to dir/whisper-cli.args for assertions.
func fakeWhisperCLI(t *testing.T, dir, body, failMsg string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script fake")
	}
	script := `#!/bin/bash
echo "$@" > "` + dir + `/whisper-cli.args"
prefix=""
while [ $# -gt 0 ]; do
  case "$1" in -of) prefix="$2"; shift 2 ;; *) shift ;; esac
done
`
	if failMsg != "" {
		script += "echo '" + failMsg + "' >&2\nexit 1\n"
	} else {
		script += `printf '%s' '` + body + `' > "$prefix.txt"
`
	}
	p := filepath.Join(dir, "whisper-cli")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeFfmpegForSTT writes a script at dir/ffmpeg that mimics the OGG->WAV
// conversion step: finds the trailing output path (last arg) and writes a
// placeholder there (or exits 1 with failMsg on stderr).
func fakeFfmpegForSTT(t *testing.T, dir, failMsg string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script fake")
	}
	script := `#!/bin/bash
out="${@: -1}"
`
	if failMsg != "" {
		script += "echo '" + failMsg + "' >&2\nexit 1\n"
	} else {
		script += `printf 'fake-wav-bytes' > "$out"
`
	}
	p := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestWhisperCPP(bin, ffmpegBin, modelPath string, threads int, lang string) *whisperCPP {
	return &whisperCPP{bin: bin, ffmpegBin: ffmpegBin, modelPath: modelPath, threads: threads, lang: lang}
}

func TestWhisperCPPTranscribe(t *testing.T) {
	dir := t.TempDir()
	ffmpegBin := fakeFfmpegForSTT(t, dir, "")
	cliBin := fakeWhisperCLI(t, dir, "привіт зі світу", "")

	ogg := filepath.Join(dir, "voice-42.ogg")
	if err := os.WriteFile(ogg, []byte("fake ogg"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := newTestWhisperCPP(cliBin, ffmpegBin, "/models/m.bin", 0, "auto")
	got, err := w.Transcribe(context.Background(), ogg, "uk")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "привіт зі світу" {
		t.Fatalf("got %q, want trimmed transcript", got)
	}
}

func TestWhisperCPPTranscribeEmptyHintPassesAuto(t *testing.T) {
	// Regression test for the hazard: an empty/"" langHint must resolve to
	// an EXPLICIT "-l auto" in the whisper-cli invocation, never an
	// omitted flag (whisper-cli's own default is "en").
	dir := t.TempDir()
	ffmpegBin := fakeFfmpegForSTT(t, dir, "")
	cliBin := fakeWhisperCLI(t, dir, "text", "")
	ogg := filepath.Join(dir, "v.ogg")
	os.WriteFile(ogg, []byte("x"), 0o644)

	w := newTestWhisperCPP(cliBin, ffmpegBin, "/models/m.bin", 0, "auto")
	if _, err := w.Transcribe(context.Background(), ogg, ""); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "whisper-cli.args"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "-l auto") {
		t.Errorf("empty langHint: args %q missing -l auto", args)
	}
}

func TestWhisperCPPTranscribeFfmpegFails(t *testing.T) {
	dir := t.TempDir()
	ffmpegBin := fakeFfmpegForSTT(t, dir, "unsupported codec")
	cliBin := fakeWhisperCLI(t, dir, "", "")
	ogg := filepath.Join(dir, "v.ogg")
	os.WriteFile(ogg, []byte("x"), 0o644)

	_, err := newTestWhisperCPP(cliBin, ffmpegBin, "/models/m.bin", 0, "auto").
		Transcribe(context.Background(), ogg, "uk")
	if err == nil {
		t.Fatal("want error when ffmpeg exits non-zero")
	}
	if !strings.Contains(err.Error(), "unsupported codec") {
		t.Errorf("error should carry stderr tail: %v", err)
	}
}

func TestWhisperCPPTranscribeCLIFails(t *testing.T) {
	dir := t.TempDir()
	ffmpegBin := fakeFfmpegForSTT(t, dir, "")
	cliBin := fakeWhisperCLI(t, dir, "", "model load failed")
	ogg := filepath.Join(dir, "v.ogg")
	os.WriteFile(ogg, []byte("x"), 0o644)

	_, err := newTestWhisperCPP(cliBin, ffmpegBin, "/models/m.bin", 0, "auto").
		Transcribe(context.Background(), ogg, "uk")
	if err == nil {
		t.Fatal("want error when whisper-cli exits non-zero")
	}
	if !strings.Contains(err.Error(), "model load failed") {
		t.Errorf("error should carry stderr tail: %v", err)
	}
}

func TestNewWhisperCPPDefaults(t *testing.T) {
	w := NewWhisperCPP("", "", "/models/m.bin", 0, "").(*whisperCPP)
	if w.bin != "whisper-cli" {
		t.Errorf("bin = %q, want whisper-cli", w.bin)
	}
	if w.ffmpegBin != "ffmpeg" {
		t.Errorf("ffmpegBin = %q, want ffmpeg", w.ffmpegBin)
	}
	if w.lang != "auto" {
		t.Errorf("lang = %q, want auto (not en, whisper-cli's own default)", w.lang)
	}
}
