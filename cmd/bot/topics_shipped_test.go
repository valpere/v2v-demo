package main

import (
	"strings"
	"testing"
)

// The other topics tests build throwaway manifests; this one loads the
// manifest the repo actually ships, so a dropped-in topic that breaks
// loadTopics (bad path, empty slots, missing scope) fails `make check`
// instead of the bot at startup.
func TestShippedTopicsManifestLoads(t *testing.T) {
	t.Chdir("../..") // topics.json paths are repo-root relative

	topics, ids, err := loadTopics(Config{TopicsPath: "topics/topics.json"})
	if err != nil {
		t.Fatalf("loadTopics(shipped manifest): %v", err)
	}

	want := []string{"translation", "dental", "auto", "realestate", "cleaning", "lyapko"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("shipped topic ids = %v, want %v", ids, want)
	}

	for _, id := range ids {
		b := topics[id]
		if b.Title == "" || b.TitleEN == "" {
			t.Errorf("%s: picker title / title_en empty (%q / %q)", id, b.Title, b.TitleEN)
		}
		if strings.TrimSpace(b.Greeting) == "" {
			t.Errorf("%s: empty greeting", id)
		}
		if len(b.Spec.KB) == 0 || strings.TrimSpace(b.Spec.System) == "" {
			t.Errorf("%s: empty KB or system prompt", id)
		}
		if len(b.Spec.Slots) == 0 || b.Spec.ScopeUK == "" || b.Spec.ScopeEN == "" {
			t.Errorf("%s: missing slots or scope lines", id)
		}
	}
}

// lyapko is a public demo of a real, third-party shop and is pitched at the
// US market, so its shape is pinned: slot keys drive the lead record, the
// greeting is EN-first and must carry the unofficial-demo disclaimer in both
// languages (Val, 2026-09-23: "the disclaimer stays, the demo is public").
func TestShippedLyapkoTopic(t *testing.T) {
	t.Chdir("../..")

	topics, _, err := loadTopics(Config{TopicsPath: "topics/topics.json"})
	if err != nil {
		t.Fatalf("loadTopics: %v", err)
	}
	b, ok := topics["lyapko"]
	if !ok {
		t.Fatal("lyapko missing from the shipped manifest")
	}

	var keys []string
	for _, s := range b.Spec.Slots {
		keys = append(keys, s.Key)
	}
	if got, want := strings.Join(keys, ","), "symptom,skin_type,format,contact"; got != want {
		t.Errorf("lyapko slot keys = %s, want %s", got, want)
	}

	en, uk := strings.Index(b.Greeting, "Hi!"), strings.Index(b.Greeting, "Вітаю!")
	if en < 0 || uk < 0 || en > uk {
		t.Errorf("greeting must be bilingual, EN first (Hi! at %d, Вітаю! at %d)", en, uk)
	}
	for _, need := range []string{"unofficial", "неофіційн"} {
		if !strings.Contains(b.Greeting, need) {
			t.Errorf("greeting lacks the %q disclaimer", need)
		}
	}
}
