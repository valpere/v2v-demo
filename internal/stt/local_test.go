package stt

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWhisperArgs(t *testing.T) {
	base := whisperArgs("/tmp/v.ogg", "/out", "medium", "auto")
	joined := strings.Join(base, " ")
	for _, want := range []string{"/tmp/v.ogg", "--model medium", "--task transcribe", "--output_format txt", "--output_dir /out", "--fp16 False"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, base)
		}
	}
	if strings.Contains(joined, "--language") {
		t.Errorf("auto should not pass --language: %v", base)
	}

	for _, lang := range []string{"uk", "en"} {
		got := strings.Join(whisperArgs("/tmp/v.ogg", "/out", "small", lang), " ")
		if !strings.Contains(got, "--language "+lang) {
			t.Errorf("lang %q not passed: %s", lang, got)
		}
	}
	if strings.Contains(strings.Join(whisperArgs("/tmp/v.ogg", "/out", "small", "de"), " "), "--language") {
		t.Error("unsupported lang should be dropped, not passed")
	}
}

// fakeWhisper writes a script at dir/whisper that mimics the CLI: it finds
// --output_dir and the input file, and writes "<stem>.txt" with body (or
// exits 1 if body is empty and failMsg is set).
func fakeWhisper(t *testing.T, dir, body, failMsg string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script fake")
	}
	script := `#!/bin/bash
set -e
ogg="$1"; outdir=""
while [ $# -gt 0 ]; do
  case "$1" in --output_dir) outdir="$2"; shift 2 ;; *) shift ;; esac
done
`
	if failMsg != "" {
		script += "echo '" + failMsg + "' >&2\nexit 1\n"
	} else {
		script += `stem=$(basename "$ogg"); stem="${stem%.*}"
printf '%s' '` + body + `' > "$outdir/$stem.txt"
`
	}
	p := filepath.Join(dir, "whisper")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLocalTranscribe(t *testing.T) {
	dir := t.TempDir()
	bin := fakeWhisper(t, dir, "  привіт зі світу  ", "")

	ogg := filepath.Join(dir, "voice-42.ogg")
	if err := os.WriteFile(ogg, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewLocal(bin, "small", "uk")
	got, err := tr.Transcribe(context.Background(), ogg, "")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "привіт зі світу" {
		t.Fatalf("got %q, want trimmed transcript", got)
	}
}

func TestLocalTranscribeCommandFails(t *testing.T) {
	dir := t.TempDir()
	bin := fakeWhisper(t, dir, "", "model download failed")
	ogg := filepath.Join(dir, "v.ogg")
	os.WriteFile(ogg, []byte("x"), 0o644)

	_, err := NewLocal(bin, "small", "auto").Transcribe(context.Background(), ogg, "")
	if err == nil {
		t.Fatal("want error when the CLI exits non-zero")
	}
	if !strings.Contains(err.Error(), "model download failed") {
		t.Errorf("error should carry stderr tail: %v", err)
	}
}

func TestLocalTranscribeNoOutput(t *testing.T) {
	dir := t.TempDir()
	// a script that succeeds but writes nothing
	p := filepath.Join(dir, "whisper")
	os.WriteFile(p, []byte("#!/bin/bash\nexit 0\n"), 0o755)
	ogg := filepath.Join(dir, "v.ogg")
	os.WriteFile(ogg, []byte("x"), 0o644)

	_, err := NewLocal(p, "small", "auto").Transcribe(context.Background(), ogg, "")
	if err == nil {
		t.Fatal("want error when the transcript file is missing")
	}
}

func TestNewLocalDefaults(t *testing.T) {
	w := NewLocal("", "", "").(*localWhisper)
	if w.bin != "whisper" || w.model != "medium" || w.lang != "auto" {
		t.Fatalf("defaults not applied: %+v", w)
	}
}
