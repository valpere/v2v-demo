package dialog

import (
	"context"
	"errors"
	"strings"
	"testing"
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

func trailerJSON(sig string, kv map[string]string) string {
	slots := map[string]any{
		"language_pair": nil, "doc_type": nil, "volume": nil,
		"deadline": nil, "certification": nil, "delivery": nil,
	}
	for k, v := range kv {
		slots[k] = v
	}
	var pairs []string
	for _, k := range []string{"language_pair", "doc_type", "volume", "deadline", "certification", "delivery"} {
		if slots[k] == nil {
			pairs = append(pairs, `"`+k+`":null`)
		} else {
			pairs = append(pairs, `"`+k+`":"`+slots[k].(string)+`"`)
		}
	}
	return "{\"slots\":{" + strings.Join(pairs, ",") + "},\"signal\":\"" + sig + "\"}"
}

func reply(text, sig string, kv map[string]string) string {
	return text + "\n\n```json\n" + trailerJSON(sig, kv) + "\n```"
}

// ── parseTrailer ────────────────────────────────────────────────

func TestParseTrailer(t *testing.T) {
	t.Run("json fence", func(t *testing.T) {
		spoken, tr := parseTrailer("Hello there.\n```json\n{\"slots\":{\"language_pair\":\"uk->de\",\"doc_type\":null,\"volume\":null,\"deadline\":null,\"certification\":null,\"delivery\":null},\"signal\":\"continue\"}\n```")
		if tr == nil {
			t.Fatal("tr = nil, want parsed")
		}
		if spoken != "Hello there." {
			t.Fatalf("spoken = %q", spoken)
		}
		if tr.Signal != SignalContinue || tr.Slots.LanguagePair == nil || *tr.Slots.LanguagePair != "uk->de" {
			t.Fatalf("tr = %+v", tr)
		}
	})

	t.Run("uppercase JSON fence", func(t *testing.T) {
		_, tr := parseTrailer("Hi.\n```JSON\n{\"slots\":{},\"signal\":\"escalate\"}\n```")
		if tr == nil || tr.Signal != SignalEscalate {
			t.Fatalf("tr = %+v", tr)
		}
	})

	t.Run("bare fence", func(t *testing.T) {
		_, tr := parseTrailer("Hi.\n```\n{\"slots\":{},\"signal\":\"lead_ready\"}\n```")
		if tr == nil || tr.Signal != SignalLeadReady {
			t.Fatalf("tr = %+v", tr)
		}
	})

	t.Run("last block wins", func(t *testing.T) {
		raw := "text\n```json\n{\"slots\":{},\"signal\":\"continue\"}\n```\nmore\n```json\n{\"slots\":{},\"signal\":\"escalate\"}\n```"
		spoken, tr := parseTrailer(raw)
		if tr == nil || tr.Signal != SignalEscalate {
			t.Fatalf("tr = %+v", tr)
		}
		if !strings.Contains(spoken, "text") || !strings.Contains(spoken, "more") {
			t.Fatalf("spoken = %q", spoken)
		}
	})

	t.Run("no trailer", func(t *testing.T) {
		spoken, tr := parseTrailer("  Just a sentence.  ")
		if tr != nil {
			t.Fatal("tr != nil")
		}
		if spoken != "Just a sentence." {
			t.Fatalf("spoken = %q", spoken)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		spoken, tr := parseTrailer("Reply.\n```json\n{not valid\n```")
		if tr != nil {
			t.Fatal("tr != nil for bad json")
		}
		if spoken != "Reply." {
			t.Fatalf("spoken = %q (fenced block should be stripped)", spoken)
		}
	})

	t.Run("unknown signal", func(t *testing.T) {
		_, tr := parseTrailer("Reply.\n```json\n{\"slots\":{},\"signal\":\"halt\"}\n```")
		if tr != nil {
			t.Fatal("tr != nil for unknown signal")
		}
	})

	t.Run("fence without json object is ignored", func(t *testing.T) {
		spoken, tr := parseTrailer("Reply.\n```\nnot an object\n```")
		if tr != nil {
			t.Fatal("tr != nil")
		}
		_ = spoken
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
	if sess.Slots.LanguagePair == nil || *sess.Slots.LanguagePair != "uk->de" || sess.Slots.DocType == nil {
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
		Slots:   QuoteSlots{DocType: sp("contract")},
		History: []Msg{{Role: "assistant", Text: "How many pages is it?"}},
	}
	gen := &fakeGen{reply: reply("ok?", "continue", map[string]string{"volume": "10 pages"})}
	if _, err := dialogHandle(t, sess, gen, "about ten pages"); err != nil { // slot answer -> bypasses the gate
		t.Fatal(err)
	}
	if sess.Slots.DocType == nil || *sess.Slots.DocType != "contract" {
		t.Fatal("existing slot was cleared by a null in the trailer")
	}
	if sess.Slots.Volume == nil || *sess.Slots.Volume != "10 pages" {
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

	// second hit in a row — hand off
	r2, _ := dialogHandle(t, sess, gen, offTopic)
	if r2.Signal != SignalEscalate {
		t.Fatalf("second gate hit: signal = %q, want escalate", r2.Signal)
	}
	if gen.calls != 0 {
		t.Fatal("generator called on the second gate hit")
	}
}

func TestHandleGateStrikeResetsOnARealTurn(t *testing.T) {
	gen := &fakeGen{reply: reply("Записала.", "continue", map[string]string{"doc_type": "diploma"})}
	sess := &Session{}

	dialogHandle(t, sess, gen, "абракадабра нісенітниця")                      // strike 1
	dialogHandle(t, sess, gen, "переклад диплома з української на англійську") // real turn -> reset
	if sess.gateStrike {
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
		Slots:   QuoteSlots{DocType: sp("diploma")},
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
	if !sess.Slots.Complete() {
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
	if got := deref(sess.Slots.DocType); got != "birth certificate" {
		t.Fatalf("doc_type = %q, want the correction", got)
	}

	// a third lead_ready with nothing changed is still a spurious re-trigger
	gen3 := &fakeGen{reply: reply("Дякую.", "lead_ready", nil)}
	r3, _ := dialogHandle(t, sess, gen3, "certified translation delivery by email")
	if r3.Signal != SignalContinue {
		t.Fatalf("unchanged re-trigger after the correction should downgrade, got %s", r3.Signal)
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// dialogHandle runs Handle with the shared test KB and a stub system prompt.
func dialogHandle(t *testing.T, sess *Session, gen Generator, text string) (Reply, error) {
	t.Helper()
	return Handle(context.Background(), sess, testKB(), gen, "SYSTEM PROMPT", text)
}
