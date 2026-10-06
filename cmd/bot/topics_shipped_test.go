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

// An electric vehicle is served for tyres and alignment only (auto KB): any
// other job is a deterministic handoff, whatever the model would have done.
func TestShippedAutoElectricVehicleRule(t *testing.T) {
	t.Chdir("../..")
	topics, _, err := loadTopics(Config{TopicsPath: "topics/topics.json"})
	if err != nil {
		t.Fatal(err)
	}
	spec := topics["auto"].Spec
	for _, text := range []string{
		"Tesla Model 3, треба замінити гальмівні колодки",
		"Nissan Leaf потрібна діагностика",
		"у мене електромобіль, стукає підвіска",
		"my EV needs an oil check", // not a real EV job but the rule is deliberately conservative
		"Hyundai Ioniq 5, ремонт кондиціонера",
	} {
		gen := &fakeGen{}
		reply, _ := dialog.Handle(context.Background(), &dialog.Session{}, spec, gen, text, time.Now())
		if reply.Signal != dialog.SignalEscalate || len(gen.seen) != 0 {
			t.Errorf("%q: want a handoff without the model, got %s (model calls %d)", text, reply.Signal, len(gen.seen))
		}
	}
	for _, text := range []string{
		"Tesla Model 3, потрібен шиномонтаж",
		"Nissan Leaf, розвал-сходження",
		"Skoda Octavia, замінити гальмівні колодки",
		"Toyota Prius гібрид, діагностика", // a hybrid is not an EV
	} {
		gen := &fakeGen{}
		dialog.Handle(context.Background(), &dialog.Session{}, spec, gen, text, time.Now())
		if len(gen.seen) != 1 {
			t.Errorf("%q: the EV rule must not fire — the model should be consulted once, got %d calls", text, len(gen.seen))
		}
	}
}

// Pregnancy and pacemakers are medical questions: a person answers, never the
// model (lyapko guardrail), whatever the model would have said.
func TestShippedLyapkoMedicalHandoff(t *testing.T) {
	t.Chdir("../..")
	topics, _, err := loadTopics(Config{TopicsPath: "topics/topics.json"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Підійде вагітній?", "I am pregnant, can I use the mat?", "в мене кардіостимулятор"} {
		gen := &fakeGen{}
		reply, _ := dialog.Handle(context.Background(), &dialog.Session{}, topics["lyapko"].Spec, gen, text, time.Now())
		if reply.Signal != dialog.SignalEscalate || len(gen.seen) != 0 {
			t.Errorf("%q: want a handoff without the model, got %s (%d model calls)", text, reply.Signal, len(gen.seen))
		}
	}
}
