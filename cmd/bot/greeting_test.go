package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGreetingBody(t *testing.T) {
	const doc = `# Header line — not sent

Some header prose, also not sent.

---

Вітаю! Це демо.

---

Hi! This is a demo.

What do you need?`

	p := filepath.Join(t.TempDir(), "greeting.md")
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := greetingBody(p)
	if err != nil {
		t.Fatalf("greetingBody: %v", err)
	}
	if strings.Contains(got, "Header line") || strings.Contains(got, "header prose") {
		t.Fatalf("header leaked into body: %q", got)
	}
	if strings.Contains(got, "---") {
		t.Fatalf("separator leaked: %q", got)
	}
	if !strings.HasPrefix(got, "Вітаю!") || !strings.Contains(got, "Hi! This is a demo.") || !strings.HasSuffix(got, "What do you need?") {
		t.Fatalf("body = %q", got)
	}
}

func TestGreetingBodyRealFile(t *testing.T) {
	got, err := greetingBody("../../topics/translation/greeting.md")
	if err != nil {
		t.Fatalf("greetingBody(real): %v", err)
	}
	if got == "" || strings.Contains(got, "NOT sent") || strings.Contains(got, "\n---\n") {
		t.Fatalf("real greeting body looks wrong: %q", got)
	}
}

func TestGreetingBodyErrors(t *testing.T) {
	if _, err := greetingBody(filepath.Join(t.TempDir(), "nope.md")); err == nil {
		t.Error("missing file: want error")
	}
	p := filepath.Join(t.TempDir(), "no-sep.md")
	os.WriteFile(p, []byte("just text, no separator"), 0o644)
	if _, err := greetingBody(p); err == nil {
		t.Error("no '---': want error")
	}
}
