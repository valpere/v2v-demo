package stt

import (
	"context"
	"errors"
	"testing"
)

type fakeTranscriber struct {
	text  string
	err   error
	calls int
}

func (f *fakeTranscriber) Transcribe(_ context.Context, _, _ string) (string, error) {
	f.calls++
	return f.text, f.err
}

func TestFailoverTranscriberPrimarySucceeds(t *testing.T) {
	primary := &fakeTranscriber{text: "primary text"}
	fallback := &fakeTranscriber{text: "fallback text"}
	f := &FailoverTranscriber{Primary: primary, Fallback: fallback}

	got, err := f.Transcribe(context.Background(), "in.ogg", "uk")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "primary text" {
		t.Errorf("got %q, want primary text", got)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback.calls = %d, want 0 (primary succeeded)", fallback.calls)
	}
}

func TestFailoverTranscriberPrimaryErrors(t *testing.T) {
	primary := &fakeTranscriber{err: errors.New("primary down")}
	fallback := &fakeTranscriber{text: "fallback text"}
	f := &FailoverTranscriber{Primary: primary, Fallback: fallback}

	got, err := f.Transcribe(context.Background(), "in.ogg", "uk")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "fallback text" {
		t.Errorf("got %q, want fallback text", got)
	}
	if fallback.calls != 1 {
		t.Errorf("fallback.calls = %d, want 1", fallback.calls)
	}
}

func TestFailoverTranscriberBothError(t *testing.T) {
	primary := &fakeTranscriber{err: errors.New("primary down")}
	fallbackErr := errors.New("fallback down too")
	fallback := &fakeTranscriber{err: fallbackErr}
	f := &FailoverTranscriber{Primary: primary, Fallback: fallback}

	_, err := f.Transcribe(context.Background(), "in.ogg", "uk")
	if !errors.Is(err, fallbackErr) {
		t.Errorf("err = %v, want fallback's error to bubble unwrapped", err)
	}
}
