package tts

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeEspeakNG writes a script at dir/espeak-ng that mimics the CLI: it
// finds the -w output path and writes a canned WAV-ish placeholder there
// (or exits 1 with failMsg on stderr). It also records the invocation's
// args (space-joined) to dir/espeak-ng.args for assertions on voice
// selection.
func fakeEspeakNG(t *testing.T, dir, failMsg string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script fake")
	}
	script := `#!/bin/bash
echo "$@" > "` + dir + `/espeak-ng.args"
out=""
while [ $# -gt 0 ]; do
  case "$1" in -w) out="$2"; shift 2 ;; *) shift ;; esac
done
`
	if failMsg != "" {
		script += "echo '" + failMsg + "' >&2\nexit 1\n"
	} else {
		script += `printf 'fake-wav-bytes' > "$out"
`
	}
	p := filepath.Join(dir, "espeak-ng")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeFfmpeg writes a script at dir/ffmpeg that mimics transcoding: it
// finds the trailing output path (last arg) and writes canned ogg bytes
// there (or exits 1 with failMsg on stderr).
func fakeFfmpeg(t *testing.T, dir, failMsg string) string {
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
		script += `printf 'fake-ogg-bytes' > "$out"
`
	}
	p := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestEspeak(espeakBin, ffmpegBin string) *espeakSynth {
	return &espeakSynth{bin: espeakBin, ffmpegBin: ffmpegBin}
}

func TestEspeakSpeak(t *testing.T) {
	dir := t.TempDir()
	espeakBin := fakeEspeakNG(t, dir, "")
	ffmpegBin := fakeFfmpeg(t, dir, "")

	e := newTestEspeak(espeakBin, ffmpegBin)
	got, err := e.Speak(context.Background(), "text", "some-voice-id", "uk")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if string(got) != "fake-ogg-bytes" {
		t.Errorf("got %q, want fake-ogg-bytes", got)
	}
}

func TestEspeakSpeakLangSelectsVoice(t *testing.T) {
	dir := t.TempDir()
	espeakBin := fakeEspeakNG(t, dir, "")
	ffmpegBin := fakeFfmpeg(t, dir, "")
	e := newTestEspeak(espeakBin, ffmpegBin)

	cases := []struct{ lang, wantVoice string }{
		{"uk", "uk"},
		{"en", "en-us"},
		{"", "uk"}, // "" defaults to uk, matching sessLang's "en, or else Ukrainian" idiom
	}
	for _, c := range cases {
		if _, err := e.Speak(context.Background(), "text", "", c.lang); err != nil {
			t.Fatalf("lang %q: Speak: %v", c.lang, err)
		}
		args, err := os.ReadFile(filepath.Join(dir, "espeak-ng.args"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(args), "-v "+c.wantVoice) {
			t.Errorf("lang %q: args %q missing -v %s", c.lang, args, c.wantVoice)
		}
	}
}

func TestEspeakSpeakVoiceIDIgnored(t *testing.T) {
	dir := t.TempDir()
	espeakBin := fakeEspeakNG(t, dir, "")
	ffmpegBin := fakeFfmpeg(t, dir, "")
	e := newTestEspeak(espeakBin, ffmpegBin)

	// A voiceID that would mean something to ElevenLabs/Azure must not
	// change espeak's output — it's simply not read.
	got1, err := e.Speak(context.Background(), "text", "some-elevenlabs-hash-id", "uk")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	got2, err := e.Speak(context.Background(), "text", "uk-UA-PolinaNeural", "uk")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if string(got1) != string(got2) {
		t.Errorf("voiceID changed the output: %q vs %q", got1, got2)
	}
}

func TestEspeakSpeakEspeakFails(t *testing.T) {
	dir := t.TempDir()
	espeakBin := fakeEspeakNG(t, dir, "voice not found")
	ffmpegBin := fakeFfmpeg(t, dir, "")
	e := newTestEspeak(espeakBin, ffmpegBin)

	_, err := e.Speak(context.Background(), "text", "", "uk")
	if err == nil {
		t.Fatal("want error when espeak-ng exits non-zero")
	}
	if !strings.Contains(err.Error(), "voice not found") {
		t.Errorf("error should carry stderr tail: %v", err)
	}
}

func TestEspeakSpeakFfmpegFails(t *testing.T) {
	dir := t.TempDir()
	espeakBin := fakeEspeakNG(t, dir, "")
	ffmpegBin := fakeFfmpeg(t, dir, "unsupported codec")
	e := newTestEspeak(espeakBin, ffmpegBin)

	_, err := e.Speak(context.Background(), "text", "", "uk")
	if err == nil {
		t.Fatal("want error when ffmpeg exits non-zero")
	}
	if !strings.Contains(err.Error(), "unsupported codec") {
		t.Errorf("error should carry stderr tail: %v", err)
	}
}

func TestNewEspeakDefault(t *testing.T) {
	e := NewEspeak("").(*espeakSynth)
	if e.bin != "espeak-ng" {
		t.Errorf("bin = %q, want espeak-ng", e.bin)
	}
	if e.ffmpegBin != "ffmpeg" {
		t.Errorf("ffmpegBin = %q, want ffmpeg", e.ffmpegBin)
	}
}
