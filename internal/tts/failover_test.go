package tts

import (
	"context"
	"errors"
	"testing"
)

type fakeSynthesizer struct {
	data  []byte
	err   error
	calls int
}

func (f *fakeSynthesizer) Speak(_ context.Context, _, _, _ string) ([]byte, error) {
	f.calls++
	return f.data, f.err
}

func TestFailoverSynthesizerPrimarySucceeds(t *testing.T) {
	primary := &fakeSynthesizer{data: []byte("primary audio")}
	fallback := &fakeSynthesizer{data: []byte("fallback audio")}
	f := &FailoverSynthesizer{Primary: primary, Fallback: fallback}

	got, err := f.Speak(context.Background(), "text", "voice", "uk")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if string(got) != "primary audio" {
		t.Errorf("got %q, want primary audio", got)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback.calls = %d, want 0 (primary succeeded)", fallback.calls)
	}
}

func TestFailoverSynthesizerPrimaryErrors(t *testing.T) {
	primary := &fakeSynthesizer{err: errors.New("primary down")}
	fallback := &fakeSynthesizer{data: []byte("fallback audio")}
	f := &FailoverSynthesizer{Primary: primary, Fallback: fallback}

	got, err := f.Speak(context.Background(), "text", "voice", "uk")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if string(got) != "fallback audio" {
		t.Errorf("got %q, want fallback audio", got)
	}
	if fallback.calls != 1 {
		t.Errorf("fallback.calls = %d, want 1", fallback.calls)
	}
}

func TestFailoverSynthesizerBothError(t *testing.T) {
	primary := &fakeSynthesizer{err: errors.New("primary down")}
	fallbackErr := errors.New("fallback down too")
	fallback := &fakeSynthesizer{err: fallbackErr}
	f := &FailoverSynthesizer{Primary: primary, Fallback: fallback}

	_, err := f.Speak(context.Background(), "text", "voice", "uk")
	if !errors.Is(err, fallbackErr) {
		t.Errorf("err = %v, want fallback's error to bubble unwrapped", err)
	}
}

type voiceRecorder struct {
	err   error
	voice string
}

func (v *voiceRecorder) Speak(_ context.Context, _, voiceID, _ string) ([]byte, error) {
	v.voice = voiceID
	if v.err != nil {
		return nil, v.err
	}
	return []byte("ogg"), nil
}

// SF-2 — a paid→paid failover must speak with the FALLBACK backend's own
// voice, not the primary's voice id (ElevenLabs would reject an Azure id).
func TestVoiceMapPerBackendSurvivesFailover(t *testing.T) {
	azure := &voiceRecorder{err: errors.New("401")}
	eleven := &voiceRecorder{}
	f := &FailoverSynthesizer{
		Primary:  WithVoices(azure, "az-a", "az-b"),
		Fallback: WithVoices(eleven, "el-a", "el-b"),
	}
	if _, err := f.Speak(context.Background(), "t", "b", "uk"); err != nil {
		t.Fatal(err)
	}
	if azure.voice != "az-b" || eleven.voice != "el-b" {
		t.Fatalf("voices: azure=%q eleven=%q, want az-b / el-b", azure.voice, eleven.voice)
	}
	// "a", "" and anything else map to voice A
	for _, in := range []string{"a", "", "x"} {
		WithVoices(eleven, "el-a", "el-b").Speak(context.Background(), "t", in, "uk")
		if eleven.voice != "el-a" {
			t.Errorf("voice %q mapped to %q, want el-a", in, eleven.voice)
		}
	}
}
