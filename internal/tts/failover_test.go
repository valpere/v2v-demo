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
