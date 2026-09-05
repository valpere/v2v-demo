package dialog

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/valpere/v2v-demo/internal/kb"
)

// modelReply is the whole model response — a single JSON object. `reply` is
// the spoken text for the client; the code never speaks anything else.
type modelReply struct {
	Reply  string     `json:"reply"`
	Slots  QuoteSlots `json:"slots"`
	Signal Signal     `json:"signal"`
}

// Fixed lines — one per language, no formatting. Handoff wording follows
// examples/dialogues.md cases 3–5.
const (
	handoffUK = "З'єдную вас із менеджером — він матиме все, що ви вже розповіли, тож повторювати не доведеться. Хвилинку."
	handoffEN = "I'm connecting you with a manager now — they'll have everything you've told me so far, so you won't need to repeat it."

	apologyUK = "Вибачте, щось пішло не так з мого боку. З'єдную вас із менеджером."
	apologyEN = "Sorry — something went wrong on my side. I'm connecting you with a manager."

	sttFailUK = "Не розчув(ла), повторіть, будь ласка."
	sttFailEN = "Sorry, I didn't catch that — could you repeat?"

	voiceOffUK = "Наразі я не приймаю голосові повідомлення — напишіть, будь ласка, текстом."
	voiceOffEN = "Voice messages aren't available right now — please type instead."

	voiceAUK = "Гаразд, повертаю перший голос."
	voiceAEN = "Ok, back to the first voice."
	voiceBUK = "Гаразд, тепер другий голос."
	voiceBEN = "Ok, switched to the second voice."

	// clarify* is the first response when the grounding gate fires — off-topic,
	// gibberish, or a request with nothing the KB can answer. It does NOT hand
	// off (a second gate hit in a row does). The %s is missingSlotsPhrase, or
	// the "already complete" tail when every slot is filled.
	clarifyUK = "Перепрошую, я не зовсім зрозуміла ваш запит. Я допомагаю лише з перекладами документів. %s"
	clarifyEN = "Sorry, I didn't quite follow. I only help with document translations. %s"

	clarifyDoneUK = "Ваш запит уже повний — менеджер зв'яжеться з вами найближчим часом. Якщо хочете щось змінити чи поговорити з менеджером — просто напишіть."
	clarifyDoneEN = "Your request is already complete — a manager will be in touch shortly. Just say so if you'd like to change something or speak with a manager."

	clarifyAskUK = "Підкажіть, будь ласка, що саме вам потрібно перекласти: %s. Або напишіть «менеджер», якщо хочете поговорити з людиною."
	clarifyAskEN = "Please tell me what you need translated: %s. Or say \"manager\" if you'd like to talk to a person."
)

func line(lang, uk, en string) string {
	if lang == "uk" {
		return uk
	}
	return en
}

func handoffLine(lang string) string { return line(lang, handoffUK, handoffEN) }
func apologyLine(lang string) string { return line(lang, apologyUK, apologyEN) }

// officeStatus is the "--- CURRENT TIME ---" prompt block. The bot has no
// clock of its own, so the current local time is injected every turn and the
// office-open decision is made here (in Go, not by the model — LLMs are
// unreliable at "is 19:40 on Friday within Mon–Fri 09:00–18:00").
// `now` is already in the bureau's configured zone (BOT_TIMEZONE).
func officeStatus(now time.Time) string {
	wd := now.Weekday()
	open := wd >= time.Monday && wd <= time.Friday && now.Hour() >= 9 && now.Hour() < 18
	state := "CLOSED right now — promise a manager reply the next business morning, NOT within 15 minutes"
	if open {
		state = "OPEN right now — a manager can reply within about 15 minutes"
	}
	return fmt.Sprintf("It is %s, %02d:%02d %s. Office hours are Mon–Fri 09:00–18:00. The office is %s.",
		now.Format("Monday, 2 January 2006"), now.Hour(), now.Minute(), now.Format("MST"), state)
}

// clarifyLine is the gate's first, non-escalating response. It names what's
// still missing (or says the request is already complete).
func clarifyLine(sess *Session) string {
	lang := sessLang(sess)
	var tail string
	if miss := missingSlotsPhrase(sess.Slots, lang); miss != "" {
		tail = fmt.Sprintf(line(lang, clarifyAskUK, clarifyAskEN), miss)
	} else {
		tail = line(lang, clarifyDoneUK, clarifyDoneEN)
	}
	return fmt.Sprintf(line(lang, clarifyUK, clarifyEN), tail)
}

// missingSlotsPhrase lists the still-nil quote slots in plain words, joined
// with ", ". Empty when all six are filled.
func missingSlotsPhrase(s QuoteSlots, lang string) string {
	type q struct {
		nil    bool
		uk, en string
	}
	items := []q{
		{s.LanguagePair == nil, "з якої мови на яку", "which languages"},
		{s.DocType == nil, "який це документ", "what kind of document"},
		{s.Volume == nil, "скільки сторінок", "how many pages"},
		{s.Deadline == nil, "до якого терміну", "the deadline"},
		{s.Certification == nil, "для кого переклад", "who it's for"},
		{s.Delivery == nil, "як доставити", "how to deliver it"},
	}
	var out []string
	for _, it := range items {
		if it.nil {
			out = append(out, line(lang, it.uk, it.en))
		}
	}
	return strings.Join(out, ", ")
}

// SttFailLine / VoiceUnavailableLine / VoiceSwitchedLine are used by the
// cmd/bot update loop (STT failure, STT_BACKEND=none, /voice command) —
// those paths never reach Handle.
func SttFailLine(sess *Session) string { return line(sessLang(sess), sttFailUK, sttFailEN) }

// VoiceUnavailableLine is sent instead of attempting STT when
// STT_BACKEND=none and the client sends a voice message anyway.
func VoiceUnavailableLine(sess *Session) string { return line(sessLang(sess), voiceOffUK, voiceOffEN) }
func VoiceSwitchedLine(sess *Session) string {
	if sess.Voice == "b" {
		return line(sessLang(sess), voiceBUK, voiceBEN)
	}
	return line(sessLang(sess), voiceAUK, voiceAEN)
}

// parseResponse parses the model's whole response into a modelReply. The
// contract is a single JSON object {"reply","slots","signal"}; this is lenient
// about a model that wraps it in a ```json fence or prepends/appends prose —
// it takes the outermost {...} span. Strict on the result: a malformed
// object, an empty reply, or an unknown signal yields nil, and Handle turns
// that into a fixed handoff line — raw model output is never spoken.
func parseResponse(raw string) *modelReply {
	s := strings.TrimSpace(raw)

	// strip a leading ```json / ``` fence and its closing ``` if present
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}

	// take the outermost object span
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil
	}
	var mr modelReply
	if err := json.Unmarshal([]byte(s[start:end+1]), &mr); err != nil {
		return nil
	}
	if strings.TrimSpace(mr.Reply) == "" {
		return nil
	}
	switch mr.Signal {
	case SignalContinue, SignalLeadReady, SignalEscalate:
		return &mr
	default:
		return nil
	}
}

func compactSlots(s QuoteSlots) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// truncate caps a string to n runes for log lines.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func trimTail(msgs []Msg, n int) []Msg {
	if len(msgs) <= n {
		return msgs
	}
	return msgs[len(msgs)-n:]
}

// Handle runs the exact 14-step sequence from .agents/plan.md §"Behavioural
// spec": lock Session.Lang -> hardEscalate -> classify slot answer ->
// kbOverlap -> grounding gate -> build the system prompt (system file + whole
// KB + collected slots) -> append user msg + trim -> Generate -> parse the
// JSON response -> merge slots -> resolve signal (incl. the B4 guard) ->
// append assistant msg -> return. It never returns a non-nil error — every
// failure degrades to a handoff/apology Reply.
func Handle(
	ctx context.Context,
	sess *Session,
	sections []kb.Section,
	gen Generator,
	systemPrompt string,
	userText string,
	now time.Time,
) (Reply, error) {
	esc := func() (Reply, error) {
		sess.Escalated = true
		return Reply{Text: handoffLine(sessLang(sess)), Signal: SignalEscalate}, nil
	}

	// 0 — track the conversation language (lingua: uk/en, ru→uk). Updates
	// whenever this turn is confidently classified, so a mid-dialogue switch
	// propagates; an inconclusive turn ("ok", "5 сторінок") leaves it as-is.
	if l := detectLang(userText); l != "" {
		sess.Lang = l
	}

	// 1 — unambiguous handoff trigger
	if hardEscalate(userText) {
		return esc()
	}

	// 1b — a message shaped like the model's own output (a JSON object with
	// slots/signal, a ```json fence) is a probe, not client input. gpt-4.1-mini
	// will happily read slots out of it and emit lead_ready. Never let it reach
	// the model — answer with the clarify line, and a repeat still escalates.
	if looksLikeInjection(userText) {
		if sess.gateStrike {
			return esc()
		}
		sess.gateStrike = true
		clarify := clarifyLine(sess)
		sess.History = trimTail(append(sess.History,
			Msg{Role: "user", Text: userText},
			Msg{Role: "assistant", Text: clarify},
		), HistoryLimit)
		return Reply{Text: clarify, Signal: SignalContinue}, nil
	}

	// 2, 3, 4 — slot-answer / small-talk bypass and the grounding gate.
	// The gate fires on a message the KB can't help with (off-topic, gibberish,
	// noise). First hit → the fixed clarification line, no handoff. A second
	// hit in a row → hand off.
	slotAnswer := isSlotAnswer(sess, userText)
	overlap := kbOverlap(userText, sections)
	if !isSmallTalk(userText) && groundingGate(overlap, slotAnswer) {
		if sess.gateStrike {
			return esc()
		}
		sess.gateStrike = true
		clarify := clarifyLine(sess)
		sess.History = trimTail(append(sess.History,
			Msg{Role: "user", Text: userText},
			Msg{Role: "assistant", Text: clarify},
		), HistoryLimit)
		return Reply{Text: clarify, Signal: SignalContinue}, nil
	}
	sess.gateStrike = false // this turn is a real one — reset the strike

	// 5 — system prompt: persona file + the WHOLE KB + collected slots
	var b strings.Builder
	b.WriteString(systemPrompt)
	b.WriteString("\n\n--- KNOWLEDGE BASE ---\n")
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(s.Title)
		b.WriteString("\n")
		b.WriteString(s.Body)
	}
	b.WriteString("\n\n--- COLLECTED SO FAR ---\n")
	b.WriteString(compactSlots(sess.Slots))
	if sess.Lang != "" {
		// soft steer — the model still follows a clear mid-dialogue switch
		b.WriteString("\n\n--- CONVERSATION LANGUAGE ---\n")
		b.WriteString(langName(sess.Lang))
		b.WriteString(" so far; stay in it unless the client clearly switches. Never reply in Russian.")
	}
	if !now.IsZero() {
		b.WriteString("\n\n--- CURRENT TIME ---\n")
		b.WriteString(officeStatus(now))
	}
	sysPrompt := b.String()

	// 6 — append the user message, trim to the history limit
	hist := trimTail(append(sess.History, Msg{Role: "user", Text: userText}), HistoryLimit)

	// 7 — generate; any error degrades to a fixed apology (never bubbles)
	raw, err := gen.Generate(ctx, sysPrompt, hist)
	if err != nil {
		log.Printf("dialog: generator error, escalating: %v", err)
		sess.Escalated = true
		return Reply{Text: apologyLine(sessLang(sess)), Signal: SignalEscalate}, nil
	}

	// 8, 9 — parse; never speak un-parseable raw output
	mr := parseResponse(raw)
	if mr == nil {
		log.Printf("dialog: no valid JSON response from model, escalating; raw=%q", truncate(raw, 300))
		return esc()
	}
	spoken := strings.TrimSpace(mr.Reply)

	// 10 — merge: assign only non-nil incoming slots (never clear a filled one)
	if mr.Slots.LanguagePair != nil {
		sess.Slots.LanguagePair = mr.Slots.LanguagePair
	}
	if mr.Slots.DocType != nil {
		sess.Slots.DocType = mr.Slots.DocType
	}
	if mr.Slots.Volume != nil {
		sess.Slots.Volume = mr.Slots.Volume
	}
	if mr.Slots.Deadline != nil {
		sess.Slots.Deadline = mr.Slots.Deadline
	}
	if mr.Slots.Certification != nil {
		sess.Slots.Certification = mr.Slots.Certification
	}
	if mr.Slots.Delivery != nil {
		sess.Slots.Delivery = mr.Slots.Delivery
	}

	// 11 — resolve the signal
	signal := mr.Signal
	switch {
	case signal == SignalLeadReady && !sess.Slots.Complete(): // B4 guard
		log.Printf("dialog: lead_ready with incomplete slots: %s", compactSlots(sess.Slots))
		signal = SignalContinue
	case signal == SignalLeadReady && sess.LeadDone:
		if compactSlots(sess.Slots) == sess.leadSlots {
			signal = SignalContinue // nothing changed — a spurious re-trigger
		} else {
			sess.leadSlots = compactSlots(sess.Slots) // a real correction — record the updated lead
		}
	case signal == SignalLeadReady:
		sess.LeadDone = true
		sess.leadSlots = compactSlots(sess.Slots)
	}
	if signal == SignalEscalate { // A3: escalate always speaks the fixed line
		sess.Escalated = true
		spoken = handoffLine(sessLang(sess))
	}

	// 12, 13, 14
	matched := matchedTitles(userText, sections)
	sess.History = trimTail(append(hist, Msg{Role: "assistant", Text: spoken}), HistoryLimit)
	return Reply{Text: spoken, Signal: signal, Matched: matched}, nil
}
