package main

import (
	"context"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
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

	want := []string{"translation", "dental", "auto", "realestate", "cleaning"}
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

// 3.1 — the dental emergency patterns, run against the shipped manifest:
// real emergencies get the fixed 103/112 text without consulting the model;
// ordinary dental questions do not.
func TestShippedDentalEmergency(t *testing.T) {
	t.Chdir("../..")
	topics, _, err := loadTopics(Config{TopicsPath: "topics/topics.json"})
	if err != nil {
		t.Fatal(err)
	}
	spec := topics["dental"].Spec
	for _, text := range []string{
		"У мене сильно набрякла щока і мені важко дихати",
		"Після видалення зуба не зупиняється кровотеча вже три години",
		"після видалення тече кров і не зупиняється",
		"набряк шиї збільшується",
		"не можу ковтати",
		"травма обличчя, вдарили по щелепі",
		"I can't breathe properly and my face is swelling",
		"the bleeding won't stop after my extraction",
		"heavy bleeding since the morning",
		"I have trouble swallowing",
		"my son was hit in the face playing football",
	} {
		gen := &fakeGen{}
		sess := &dialog.Session{}
		reply, _ := dialog.Handle(context.Background(), sess, spec, gen, text, time.Now())
		if !strings.Contains(reply.Text, "103") || len(gen.seen) != 0 || reply.Signal != dialog.SignalEscalate {
			t.Errorf("%q: want the emergency text without the model, got signal=%s model-calls=%d reply=%q", text, reply.Signal, len(gen.seen), reply.Text)
		}
	}
	for _, text := range []string{
		"Скільки коштує чистка зубів?",
		"Хочу записатися на консультацію у суботу",
		"Чи боляче ставити пломбу?",
		"how much is teeth whitening",
	} {
		reply, _ := dialog.Handle(context.Background(), &dialog.Session{}, spec, &fakeGen{}, text, time.Now())
		if strings.Contains(reply.Text, "Якщо у вас сильна кровотеча") || strings.Contains(reply.Text, "heavy bleeding, swelling") {
			t.Errorf("%q: false emergency alarm", text)
		}
	}
}
