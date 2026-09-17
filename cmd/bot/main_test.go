package main

import (
	"testing"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/stt"
	"github.com/valpere/v2v-demo/internal/tts"
)

// These check the newGenerator/newTranscriber *wiring* only — that a
// configured fallback actually produces a FailoverGenerator/
// FailoverTranscriber, and that leaving it unset returns the primary
// unwrapped (today's behavior, unchanged). No network/process calls: the
// underlying backends aren't invoked here, only constructed.

func TestNewGeneratorNoFallback(t *testing.T) {
	cfg := Config{DialogBackend: "ollama", OllamaBaseURL: "http://localhost:11434"}
	gen, err := newGenerator(cfg)
	if err != nil {
		t.Fatalf("newGenerator: %v", err)
	}
	if _, wrapped := gen.(*dialog.FailoverGenerator); wrapped {
		t.Error("no DialogFallbackBackend set — got a FailoverGenerator, want the bare primary")
	}
}

func TestNewGeneratorWithFallback(t *testing.T) {
	cfg := Config{
		DialogBackend:         "openai",
		OpenAIKey:             "sk-test",
		DialogFallbackBackend: "ollama",
		OllamaBaseURL:         "http://localhost:11434",
	}
	gen, err := newGenerator(cfg)
	if err != nil {
		t.Fatalf("newGenerator: %v", err)
	}
	fo, wrapped := gen.(*dialog.FailoverGenerator)
	if !wrapped {
		t.Fatal("DialogFallbackBackend set — want a FailoverGenerator")
	}
	if fo.Primary == nil || fo.Fallback == nil {
		t.Errorf("FailoverGenerator has a nil Primary/Fallback: %+v", fo)
	}
}

func TestNewTranscriberNoFallback(t *testing.T) {
	cfg := Config{STTBackend: "local", WhisperBin: "whisper", WhisperModel: "turbo", WhisperLang: "uk"}
	tr, err := newTranscriber(cfg)
	if err != nil {
		t.Fatalf("newTranscriber: %v", err)
	}
	if _, wrapped := tr.(*stt.FailoverTranscriber); wrapped {
		t.Error("no STTFallbackBackend set — got a FailoverTranscriber, want the bare primary")
	}
}

func TestNewTranscriberWithFallback(t *testing.T) {
	cfg := Config{
		STTBackend:         "openai",
		OpenAIKey:          "sk-test",
		STTFallbackBackend: "local",
		WhisperBin:         "whisper",
		WhisperModel:       "turbo",
		WhisperLang:        "uk",
	}
	tr, err := newTranscriber(cfg)
	if err != nil {
		t.Fatalf("newTranscriber: %v", err)
	}
	fo, wrapped := tr.(*stt.FailoverTranscriber)
	if !wrapped {
		t.Fatal("STTFallbackBackend set — want a FailoverTranscriber")
	}
	if fo.Primary == nil || fo.Fallback == nil {
		t.Errorf("FailoverTranscriber has a nil Primary/Fallback: %+v", fo)
	}
}

func TestNewTranscriberFallbackNoneRejected(t *testing.T) {
	cfg := Config{
		STTBackend:         "openai",
		OpenAIKey:          "sk-test",
		STTFallbackBackend: "none",
	}
	if _, err := newTranscriber(cfg); err == nil {
		t.Fatal("STTFallbackBackend=none — want an error, got nil")
	}
}

func TestNewSynthesizerNoFallback(t *testing.T) {
	cfg := Config{
		TTSBackend:   "elevenlabs",
		ElevenKey:    "k",
		ElevenVoiceA: "a",
		ElevenVoiceB: "b",
	}
	synth, err := newSynthesizer(cfg)
	if err != nil {
		t.Fatalf("newSynthesizer: %v", err)
	}
	if _, wrapped := synth.(*tts.FailoverSynthesizer); wrapped {
		t.Error("no TTSFallbackBackend set — got a FailoverSynthesizer, want the bare primary")
	}
}

func TestNewSynthesizerWithFallback(t *testing.T) {
	cfg := Config{
		TTSBackend:         "elevenlabs",
		ElevenKey:          "k",
		ElevenVoiceA:       "a",
		ElevenVoiceB:       "b",
		TTSFallbackBackend: "espeak",
		EspeakBin:          "espeak-ng",
	}
	synth, err := newSynthesizer(cfg)
	if err != nil {
		t.Fatalf("newSynthesizer: %v", err)
	}
	fo, wrapped := synth.(*tts.FailoverSynthesizer)
	if !wrapped {
		t.Fatal("TTSFallbackBackend set — want a FailoverSynthesizer")
	}
	if fo.Primary == nil || fo.Fallback == nil {
		t.Errorf("FailoverSynthesizer has a nil Primary/Fallback: %+v", fo)
	}
}

func TestNewSynthesizerFallbackNoneRejected(t *testing.T) {
	cfg := Config{
		TTSBackend:         "elevenlabs",
		ElevenKey:          "k",
		ElevenVoiceA:       "a",
		ElevenVoiceB:       "b",
		TTSFallbackBackend: "none",
	}
	if _, err := newSynthesizer(cfg); err == nil {
		t.Fatal("TTSFallbackBackend=none — want an error, got nil")
	}
}
