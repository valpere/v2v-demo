package dialog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeGen struct {
	reply   string
	err     error
	gotSys  string
	gotHist []Msg
	calls   int
}

func (f *fakeGen) Generate(_ context.Context, sys string, hist []Msg) (string, error) {
	f.calls++
	f.gotSys = sys
	f.gotHist = hist
	return f.reply, f.err
}

// reply builds a model response in the current contract: a single JSON object
// {"reply","slots","signal"}. kv is the slots the model "learned" this turn
// (an empty/nil kv → "slots":{}, i.e. nothing new).
func reply(text, sig string, kv map[string]string) string {
	b, _ := json.Marshal(text)
	slots := "{}"
	if len(kv) > 0 {
		s, _ := json.Marshal(kv)
		slots = string(s)
	}
	return `{"reply":` + string(b) + `,"slots":` + slots + `,"signal":"` + sig + `"}`
}

// ── parseResponse ──────────────────────────────────────────────

func TestParseResponse(t *testing.T) {
	t.Run("bare object", func(t *testing.T) {
		mr := parseResponse(`{"reply":"Hello there.","slots":{"language_pair":"uk->de","doc_type":null,"volume":null,"deadline":null,"certification":null,"delivery":null},"signal":"continue"}`)
		if mr == nil {
			t.Fatal("mr = nil, want parsed")
		}
		if mr.Reply != "Hello there." {
			t.Fatalf("reply = %q", mr.Reply)
		}
		if mr.Signal != SignalContinue || mr.Slots["language_pair"] != "uk->de" {
			t.Fatalf("mr = %+v", mr)
		}
	})

	t.Run("wrapped in a json fence", func(t *testing.T) {
		mr := parseResponse("```json\n{\"reply\":\"Hi.\",\"slots\":{},\"signal\":\"escalate\"}\n```")
		if mr == nil || mr.Signal != SignalEscalate || mr.Reply != "Hi." {
			t.Fatalf("mr = %+v", mr)
		}
	})

	t.Run("prose around the object", func(t *testing.T) {
		mr := parseResponse("Here you go:\n{\"reply\":\"Done.\",\"slots\":{},\"signal\":\"lead_ready\"}\nHope that helps")
		if mr == nil || mr.Signal != SignalLeadReady || mr.Reply != "Done." {
			t.Fatalf("mr = %+v", mr)
		}
	})

	t.Run("no object at all", func(t *testing.T) {
		if parseResponse("  Just a sentence.  ") != nil {
			t.Fatal("want nil for plain prose (no JSON object)")
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		if parseResponse(`{"reply":"x", "slots": {not valid`) != nil {
			t.Fatal("want nil for bad json")
		}
	})

	t.Run("empty reply", func(t *testing.T) {
		if parseResponse(`{"reply":"  ","slots":{},"signal":"continue"}`) != nil {
			t.Fatal("want nil for an empty reply string")
		}
	})

	t.Run("unknown signal", func(t *testing.T) {
		if parseResponse(`{"reply":"x","slots":{},"signal":"halt"}`) != nil {
			t.Fatal("want nil for an unknown signal")
		}
	})

	t.Run("missing signal", func(t *testing.T) {
		if parseResponse(`{"reply":"x","slots":{}}`) != nil {
			t.Fatal("want nil when signal is absent")
		}
	})
}

// ── Handle ──────────────────────────────────────────────────────

func TestHandleContinueAndMerge(t *testing.T) {
	gen := &fakeGen{reply: reply("What volume are we looking at?", "continue", map[string]string{
		"language_pair": "uk->de", "doc_type": "diploma",
	})}
	sess := &Session{}
	r, err := dialogHandle(t, sess, gen, "I need a certified translation of a diploma, delivery by email")
	if err != nil {
		t.Fatal(err)
	}
	if r.Signal != SignalContinue {
		t.Fatalf("signal = %q", r.Signal)
	}
	if sess.Slots["language_pair"] != "uk->de" || sess.Slots["doc_type"] == "" {
		t.Fatalf("slots not merged: %s", compactSlots(sess.Slots))
	}
	if sess.Lang != "en" {
		t.Fatalf("Lang = %q, want en", sess.Lang)
	}
	if len(sess.History) != 2 || sess.History[0].Role != "user" || sess.History[1].Role != "assistant" {
		t.Fatalf("history = %+v", sess.History)
	}
	if !strings.Contains(gen.gotSys, "--- KNOWLEDGE BASE ---") || !strings.Contains(gen.gotSys, "## Services") {
		t.Fatal("system prompt missing KB")
	}
}

func TestHandleMergeNeverClears(t *testing.T) {
	sess := &Session{
		Slots:   map[string]string{"doc_type": "contract"},
		History: []Msg{{Role: "assistant", Text: "How many pages is it?"}},
	}
	gen := &fakeGen{reply: reply("ok?", "continue", map[string]string{"volume": "10 pages"})}
	if _, err := dialogHandle(t, sess, gen, "about ten pages"); err != nil { // slot answer -> bypasses the gate
		t.Fatal(err)
	}
	if sess.Slots["doc_type"] != "contract" {
		t.Fatal("existing slot was cleared by an omitted key in the response")
	}
	if sess.Slots["volume"] != "10 pages" {
		t.Fatal("new slot not merged")
	}
}

func TestHandleHardEscalate(t *testing.T) {
	gen := &fakeGen{reply: reply("should not be used", "continue", nil)}
	sess := &Session{}
	r, _ := dialogHandle(t, sess, gen, "Поверніть гроші, це скарга")
	if r.Signal != SignalEscalate || r.Text != handoffUK {
		t.Fatalf("r = %+v", r)
	}
	if gen.calls != 0 {
		t.Fatal("generator called on a hardEscalate turn")
	}
	if r.Matched != nil {
		t.Fatal("Matched should be nil on an early escalate")
	}
	if !sess.Escalated {
		t.Fatal("sess.Escalated not set")
	}
}

func TestHandleGroundingGate(t *testing.T) {
	gen := &fakeGen{reply: reply("unused", "continue", nil)}
	sess := &Session{}
	offTopic := "What is the capital of Australia and how tall is Everest"

	// first hit — the clarification line, no handoff, no generator call
	r, _ := dialogHandle(t, sess, gen, offTopic)
	if r.Signal != SignalContinue {
		t.Fatalf("first gate hit: signal = %q, want continue", r.Signal)
	}
	if sess.Escalated {
		t.Fatal("first gate hit marked the session escalated")
	}
	if !strings.Contains(r.Text, "only help with document translations") {
		t.Fatalf("first gate hit text = %q, want the clarification line", r.Text)
	}
	if gen.calls != 0 {
		t.Fatal("generator called despite the gate firing")
	}

	// second message, still short: isSlotAnswer now accepts a reply to the
	// clarify line's own "?" the same as any other question it asked
	// (2026-09-05 — see the GateStrike note on isSlotAnswer in gate.go).
	// That means this reaches the model rather than pre-LLM escalating: the
	// heuristic (length + "?" + nil slot) can't tell a short off-topic
	// message from a short legit answer, so the model itself decides from
	// here via its own signal.
	r2, _ := dialogHandle(t, sess, gen, "qwerty asdf")
	if gen.calls != 1 {
		t.Fatalf("generator calls = %d, want 1 — a short reply to the clarify line's own question should reach the model", gen.calls)
	}
	if r2.Signal != SignalContinue {
		t.Fatalf("second short reply: signal = %q, want continue (the fake generator's own signal)", r2.Signal)
	}
}

// TestHandleSlotAnswerAcceptedAfterGateStrike is the regression test for the
// 2026-09-05 fix: a legitimate short answer to the bot's own question,
// arriving right after a first gate strike, must reach the model instead of
// being pre-LLM escalated. Before the fix, isSlotAnswer checked GateStrike
// first and always returned false for the turn right after any strike —
// "5 сторінок" answering "Скільки сторінок?" escalated to a human instead
// of being accepted, purely because an off-topic message happened earlier
// in the same conversation.
func TestHandleSlotAnswerAcceptedAfterGateStrike(t *testing.T) {
	gen := &fakeGen{reply: reply("Дякую, записала.", "continue", map[string]string{"volume": "5 pages"})}
	sess := &Session{History: []Msg{{Role: "assistant", Text: "Скільки сторінок потрібно перекласти?"}}}

	dialogHandle(t, sess, gen, "What is the capital of Australia and how tall is Everest") // strike 1
	if !sess.GateStrike {
		t.Fatal("expected a gate strike after the off-topic message")
	}

	r, _ := dialogHandle(t, sess, gen, "5 сторінок")
	if r.Signal == SignalEscalate {
		t.Fatal("a legit slot answer right after a gate strike must not escalate")
	}
	if gen.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", gen.calls)
	}
}

func TestHandleGateStrikeResetsOnARealTurn(t *testing.T) {
	gen := &fakeGen{reply: reply("Записала.", "continue", map[string]string{"doc_type": "diploma"})}
	sess := &Session{}

	dialogHandle(t, sess, gen, "абракадабра нісенітниця")                      // strike 1
	dialogHandle(t, sess, gen, "переклад диплома з української на англійську") // real turn -> reset
	if sess.GateStrike {
		t.Fatal("gateStrike not cleared after a turn that reached the model")
	}
	r, _ := dialogHandle(t, sess, gen, "qwqw zzz nonsense") // strike 1 again, not escalate
	if r.Signal != SignalContinue {
		t.Fatalf("signal = %q after the strike was reset, want continue", r.Signal)
	}
}

func TestHandleSlotAnswerBypassesGate(t *testing.T) {
	gen := &fakeGen{reply: reply("Записала.", "continue", map[string]string{"volume": "5 pages"})}
	sess := &Session{
		History: []Msg{{Role: "assistant", Text: "Скільки сторінок у документі?"}},
		Slots:   map[string]string{"doc_type": "diploma"},
	}
	r, _ := dialogHandle(t, sess, gen, "п'ять сторінок")
	if gen.calls != 1 {
		t.Fatalf("generator calls = %d, want 1 (slot answer should bypass the gate)", gen.calls)
	}
	if r.Signal != SignalContinue {
		t.Fatalf("signal = %q", r.Signal)
	}
}

func TestHandleNilTrailerEscalates(t *testing.T) {
	gen := &fakeGen{reply: "Just talking, no trailer at all."}
	sess := &Session{Lang: "en"}
	r, _ := dialogHandle(t, sess, gen, "certified translation please, how does it work")
	if r.Signal != SignalEscalate || r.Text != handoffEN {
		t.Fatalf("r = %+v", r)
	}
	if !sess.Escalated {
		t.Fatal("Escalated not set")
	}
}

func TestOfficeStatus(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		open bool
	}{
		{"wed midday", time.Date(2026, 9, 9, 14, 30, 0, 0, time.UTC), true},
		{"wed after 18:00", time.Date(2026, 9, 9, 19, 40, 0, 0, time.UTC), false},
		{"wed before 09:00", time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC), false},
		{"saturday", time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC), false},
		{"sunday", time.Date(2026, 9, 13, 11, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := officeStatus(tc.when)
			isOpen := strings.Contains(got, "OPEN right now")
			isClosed := strings.Contains(got, "CLOSED right now")
			if isOpen == isClosed {
				t.Fatalf("ambiguous: %q", got)
			}
			if isOpen != tc.open {
				t.Fatalf("open = %v, want %v (%q)", isOpen, tc.open, got)
			}
		})
	}
}

func TestHandleInjectsCurrentTime(t *testing.T) {
	run := func(when time.Time) string {
		gen := &fakeGen{reply: reply("ok", "continue", nil)}
		_, err := Handle(context.Background(), &Session{}, testTopic(), gen,
			"certified translation of a diploma, tell me about delivery", when)
		if err != nil {
			t.Fatal(err)
		}
		return gen.gotSys
	}

	sys := run(time.Date(2026, 9, 9, 14, 30, 0, 0, time.UTC))
	if !strings.Contains(sys, "--- CURRENT TIME ---") || !strings.Contains(sys, "OPEN right now") {
		t.Fatalf("weekday-midday prompt missing an open-office block:\n%s", sys)
	}

	sys = run(time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)) // Saturday
	if !strings.Contains(sys, "CLOSED right now") || !strings.Contains(sys, "next business morning") {
		t.Fatalf("weekend prompt missing a closed-office block:\n%s", sys)
	}

	// zero time -> no block at all (the shared test helper's default)
	gen := &fakeGen{reply: reply("ok", "continue", nil)}
	_, _ = Handle(context.Background(), &Session{}, testTopic(), gen,
		"certified translation of a diploma, tell me about delivery", time.Time{})
	if strings.Contains(gen.gotSys, "CURRENT TIME") {
		t.Fatalf("zero now should omit the block:\n%s", gen.gotSys)
	}
}

func TestHandleHistoryTrimKeepsSlots(t *testing.T) {
	// Slot state lives in sess.Slots, not sess.History — a long conversation
	// that trims history past HistoryLimit must not lose an early slot value
	// (docs/smoke-test.md §18, the ">20 turns" row).
	gen := &fakeGen{reply: reply("ok", "continue", map[string]string{"language_pair": "uk->de"})}
	sess := &Session{}

	dialogHandle(t, sess, gen, "certified translation of a diploma into German, tell me about delivery")
	gen.reply = reply("ok", "continue", nil) // later turns add no new slot
	for i := 0; i < 30; i++ {
		dialogHandle(t, sess, gen, "and one more question about certified translation delivery options please")
	}

	if len(sess.History) > HistoryLimit {
		t.Fatalf("history not trimmed: %d entries, limit %d", len(sess.History), HistoryLimit)
	}
	if sess.Slots["language_pair"] != "uk->de" {
		t.Fatalf("early slot lost after the history trim: %+v", sess.Slots)
	}
}

func TestHandleGeneratorError(t *testing.T) {
	gen := &fakeGen{err: errors.New("boom")}
	sess := &Session{}
	r, err := dialogHandle(t, sess, gen, "certified translation, tell me about delivery options")
	if err != nil {
		t.Fatalf("Handle returned error: %v (must degrade, never bubble)", err)
	}
	if r.Signal != SignalEscalate || r.Text != apologyEN { // text is English -> English apology
		t.Fatalf("r = %+v", r)
	}
}

func TestHandleGeneratorErrorUkrainian(t *testing.T) {
	gen := &fakeGen{err: errors.New("boom")}
	sess := &Session{}
	r, _ := dialogHandle(t, sess, gen, "розкажіть, будь ласка, про засвідчення печаткою бюро та доставку")
	if r.Signal != SignalEscalate || r.Text != apologyUK {
		t.Fatalf("uk turn should degrade to the uk apology: %+v", r)
	}
}

func TestHandleLeadReady(t *testing.T) {
	full := map[string]string{
		"language_pair": "uk->de", "doc_type": "diploma", "volume": "2 pages",
		"deadline": "1 week", "certification": "certified", "delivery": "email",
	}
	gen := &fakeGen{reply: reply("Записала все, менеджер надішле вартість.", "lead_ready", full)}
	sess := &Session{}
	r, _ := dialogHandle(t, sess, gen, "certified translation delivery by email")
	if r.Signal != SignalLeadReady {
		t.Fatalf("signal = %q, want lead_ready", r.Signal)
	}
	if !Complete(sess.Slots, testSlots()) {
		t.Fatal("slots not complete")
	}
}

func TestHandleLeadReadyGuardedWhenIncomplete(t *testing.T) {
	gen := &fakeGen{reply: reply("Дякую!", "lead_ready", map[string]string{"language_pair": "uk->de"})}
	sess := &Session{}
	r, _ := dialogHandle(t, sess, gen, "certified translation and delivery please")
	if r.Signal != SignalContinue {
		t.Fatalf("signal = %q, want continue (B4 guard downgrades lead_ready with incomplete slots)", r.Signal)
	}
}

func TestHandleEscalateSignalSpeaksFixedLine(t *testing.T) {
	gen := &fakeGen{reply: reply("Це вам краще підкаже менеджер, з'єдную.", "escalate", nil)}
	sess := &Session{}
	r, _ := dialogHandle(t, sess, gen, "розкажіть про засвідчення печаткою бюро та доставку документів")
	if r.Signal != SignalEscalate || r.Text != handoffUK {
		t.Fatalf("r = %+v (escalate must replace spoken text with the fixed handoff line)", r)
	}
	if len(sess.History) == 0 || sess.History[len(sess.History)-1].Text != handoffUK {
		t.Fatal("history should record the handoff line, not the model text")
	}
}

// TestHandleUkrainianContentQuestionPassesGate — with a bilingual KB a
// Ukrainian content question whose terms appear in the Ukrainian body clears
// the grounding gate and reaches the LLM (the cross-language wall is gone;
// within-Ukrainian inflection is the remaining B1-stemmer concern).
func TestHandleUkrainianContentQuestionPassesGate(t *testing.T) {
	gen := &fakeGen{reply: reply("Так, робимо.", "continue", nil)}
	sess := &Session{}
	r, _ := dialogHandle(t, sess, gen, "Ви робите присяжний переклад?")
	if gen.calls != 1 {
		t.Fatalf("generator calls = %d, want 1 (uk terms match the uk KB body)", gen.calls)
	}
	if r.Signal != SignalContinue {
		t.Fatalf("signal = %q", r.Signal)
	}
	if sess.Lang != "uk" {
		t.Fatalf("Lang = %q, want uk", sess.Lang)
	}
}

// TestHandleUkrainianOffTopicClarifiesThenEscalates — a Ukrainian question the
// KB doesn't cover scores below the floor: the first hit gets the fixed
// clarification line (in Ukrainian), the second in a row hands off.
func TestHandleUkrainianOffTopicClarifiesThenEscalates(t *testing.T) {
	gen := &fakeGen{reply: reply("unused", "continue", nil)}
	sess := &Session{}
	offTopic := "Яка столиця Австралії і скільки років президенту?"

	r, _ := dialogHandle(t, sess, gen, offTopic)
	if r.Signal != SignalContinue || gen.calls != 0 {
		t.Fatalf("first hit: r = %+v, calls = %d", r, gen.calls)
	}
	if !strings.Contains(r.Text, "лише з перекладами документів") {
		t.Fatalf("first hit text not the uk clarification line: %q", r.Text)
	}

	r2, _ := dialogHandle(t, sess, gen, offTopic)
	if r2.Signal != SignalEscalate || r2.Text != handoffUK {
		t.Fatalf("second hit: r = %+v", r2)
	}
}

func TestHandleGreetingReachesGenerator(t *testing.T) {
	gen := &fakeGen{reply: reply("Вітаю! Чим можу допомогти?", "continue", nil)}
	sess := &Session{}
	r, _ := dialogHandle(t, sess, gen, "Привіт!")
	if gen.calls != 1 {
		t.Fatalf("greeting should reach the generator, calls = %d", gen.calls)
	}
	if r.Signal == SignalEscalate {
		t.Fatal("a greeting must not pre-escalate")
	}
}

func TestHandleFarewellReachesGenerator(t *testing.T) {
	gen := &fakeGen{reply: reply("Дякую, гарного дня!", "continue", nil)}
	sess := &Session{Lang: "uk"}
	r, _ := dialogHandle(t, sess, gen, "Дякую. Ви були цілком люб'язні.")
	if gen.calls != 1 || r.Signal == SignalEscalate {
		t.Fatalf("farewell escalated: calls=%d sig=%s", gen.calls, r.Signal)
	}
}

func TestHandleNoDuplicateLead(t *testing.T) {
	full := map[string]string{
		"language_pair": "uk->de", "doc_type": "diploma", "volume": "2 pages",
		"deadline": "1 week", "certification": "certified", "delivery": "email",
	}
	sess := &Session{}

	gen := &fakeGen{reply: reply("Записала все.", "lead_ready", full)}
	r, _ := dialogHandle(t, sess, gen, "certified translation delivery by email")
	if r.Signal != SignalLeadReady || !sess.LeadDone {
		t.Fatalf("first: sig=%s leadDone=%v", r.Signal, sess.LeadDone)
	}

	// a later turn that also emits lead_ready (e.g. the user says "hello again")
	gen2 := &fakeGen{reply: reply("Радий знову вас чути.", "lead_ready", full)}
	r2, _ := dialogHandle(t, sess, gen2, "Добрий день ще раз")
	if r2.Signal != SignalContinue {
		t.Fatalf("second lead_ready should downgrade to continue, got %s", r2.Signal)
	}
}

func TestHandleCorrectionAfterLeadRecordsUpdatedLead(t *testing.T) {
	full := map[string]string{
		"language_pair": "uk->pl", "doc_type": "passport", "volume": "1 page",
		"deadline": "Friday", "certification": "certified", "delivery": "email",
	}
	sess := &Session{}

	gen := &fakeGen{reply: reply("Записала все.", "lead_ready", full)}
	r, _ := dialogHandle(t, sess, gen, "certified translation delivery by email")
	if r.Signal != SignalLeadReady {
		t.Fatalf("first: sig=%s, want lead_ready", r.Signal)
	}

	// the client corrects the doc type after the summary — a real change,
	// so the corrected lead_ready must go through (a fresh LeadRecord).
	corrected := map[string]string{"doc_type": "birth certificate"}
	gen2 := &fakeGen{reply: reply("Виправляю: свідоцтво про народження.", "lead_ready", corrected)}
	r2, _ := dialogHandle(t, sess, gen2, "the certified translation is of a birth certificate not a passport")
	if r2.Signal != SignalLeadReady {
		t.Fatalf("correction lead_ready downgraded to %s — a real change must be recorded", r2.Signal)
	}
	if got := sess.Slots["doc_type"]; got != "birth certificate" {
		t.Fatalf("doc_type = %q, want the correction", got)
	}

	// a third lead_ready with nothing changed is still a spurious re-trigger
	gen3 := &fakeGen{reply: reply("Дякую.", "lead_ready", nil)}
	r3, _ := dialogHandle(t, sess, gen3, "certified translation delivery by email")
	if r3.Signal != SignalContinue {
		t.Fatalf("unchanged re-trigger after the correction should downgrade, got %s", r3.Signal)
	}
}

// dialogHandle runs Handle with the shared test topic (testKB + testSlots +
// translation scope). A zero `now` omits the CURRENT TIME block — tests that
// care about it call Handle directly with a fixed timestamp.
func dialogHandle(t *testing.T, sess *Session, gen Generator, text string) (Reply, error) {
	t.Helper()
	return Handle(context.Background(), sess, testTopic(), gen, text, time.Time{})
}
