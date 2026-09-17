package dialog

import (
	"context"
	"errors"
	"testing"
)

func TestFailoverGeneratorPrimarySucceeds(t *testing.T) {
	primary := &fakeGen{reply: "primary reply"}
	fallback := &fakeGen{reply: "fallback reply"}
	f := &FailoverGenerator{Primary: primary, Fallback: fallback}

	got, err := f.Generate(context.Background(), "sys", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "primary reply" {
		t.Errorf("got %q, want primary reply", got)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback.calls = %d, want 0 (primary succeeded)", fallback.calls)
	}
}

func TestFailoverGeneratorPrimaryErrors(t *testing.T) {
	primary := &fakeGen{err: errors.New("primary down")}
	fallback := &fakeGen{reply: "fallback reply"}
	f := &FailoverGenerator{Primary: primary, Fallback: fallback}

	got, err := f.Generate(context.Background(), "sys", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "fallback reply" {
		t.Errorf("got %q, want fallback reply", got)
	}
	if fallback.calls != 1 {
		t.Errorf("fallback.calls = %d, want 1", fallback.calls)
	}
}

func TestFailoverGeneratorBothError(t *testing.T) {
	primary := &fakeGen{err: errors.New("primary down")}
	fallbackErr := errors.New("fallback down too")
	fallback := &fakeGen{err: fallbackErr}
	f := &FailoverGenerator{Primary: primary, Fallback: fallback}

	_, err := f.Generate(context.Background(), "sys", nil)
	if !errors.Is(err, fallbackErr) {
		t.Errorf("err = %v, want fallback's error to bubble unwrapped", err)
	}
}
