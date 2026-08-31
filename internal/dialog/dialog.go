package dialog

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/valpere/v2v-demo/internal/kb"
)

// trailer is the fenced JSON block the model appends after the spoken reply.
type trailer struct {
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

	voiceSwitchedUK = "Гаразд, тепер інший голос."
	voiceSwitchedEN = "Ok, switched voice."
)

func line(lang, uk, en string) string {
	if lang == "uk" {
		return uk
	}
	return en
}

func handoffLine(lang string) string { return line(lang, handoffUK, handoffEN) }
func apologyLine(lang string) string { return line(lang, apologyUK, apologyEN) }

// SttFailLine / VoiceSwitchedLine are used by the cmd/bot update loop (STT
// failure, /voice command) — those paths never reach Handle.
func SttFailLine(sess *Session) string { return line(sessLang(sess), sttFailUK, sttFailEN) }
func VoiceSwitchedLine(sess *Session) string {
	return line(sessLang(sess), voiceSwitchedUK, voiceSwitchedEN)
}

// parseTrailer splits raw into the spoken text and the parsed trailer. It
// takes the LAST fenced block whose opening fence is ``` / ```json / ```JSON
// and whose first body line starts with "{". Lenient on the fence spelling,
// strict on the JSON: a malformed or unknown-signal trailer yields nil, and
// Handle turns that into a fixed handoff line — raw model output is never
// spoken.
func parseTrailer(raw string) (string, *trailer) {
	lines := strings.Split(raw, "\n")

	type block struct{ open, close int }
	var blocks []block
	openIdx := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if openIdx < 0 {
			if t == "```" || strings.EqualFold(t, "```json") {
				openIdx = i
			}
			continue
		}
		if t == "```" {
			blocks = append(blocks, block{openIdx, i})
			openIdx = -1
		}
	}

	chosen := -1
	for bi := len(blocks) - 1; bi >= 0; bi-- {
		for _, bl := range lines[blocks[bi].open+1 : blocks[bi].close] {
			if s := strings.TrimSpace(bl); s != "" {
				if strings.HasPrefix(s, "{") {
					chosen = bi
				}
				break
			}
		}
		if chosen >= 0 {
			break
		}
	}
	if chosen < 0 {
		return strings.TrimSpace(raw), nil
	}

	b := blocks[chosen]
	body := strings.Join(lines[b.open+1:b.close], "\n")
	before := strings.Join(lines[:b.open], "\n")
	after := strings.Join(lines[b.close+1:], "\n")
	spoken := strings.TrimSpace(strings.TrimSpace(before) + "\n" + strings.TrimSpace(after))

	var tr trailer
	if err := json.Unmarshal([]byte(body), &tr); err != nil {
		return spoken, nil
	}
	switch tr.Signal {
	case SignalContinue, SignalLeadReady, SignalEscalate:
		return spoken, &tr
	default:
		return spoken, nil
	}
}

func compactSlots(s QuoteSlots) string {
	b, _ := json.Marshal(s)
	return string(b)
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
// KB + collected slots) -> append user msg + trim -> Generate -> parse
// trailer -> merge slots -> resolve signal (incl. the B4 guard) -> append
// assistant msg -> return. It never returns a non-nil error — every failure
// degrades to a handoff/apology Reply.
func Handle(
	ctx context.Context,
	sess *Session,
	sections []kb.Section,
	gen Generator,
	systemPrompt string,
	userText string,
) (Reply, error) {
	esc := func() (Reply, error) {
		sess.Escalated = true
		return Reply{Text: handoffLine(sessLang(sess)), Signal: SignalEscalate}, nil
	}

	// 0 — lock the session language from the first turn it can be detected
	if l := langOf(userText); l != "" && sess.Lang == "" {
		sess.Lang = l
	}

	// 1 — unambiguous handoff trigger
	if hardEscalate(userText) {
		return esc()
	}

	// 2, 3, 4 — slot-answer bypass and the grounding gate
	slotAnswer := isSlotAnswer(sess, userText)
	overlap := kbOverlap(userText, sections)
	if groundingGate(overlap, slotAnswer) {
		return esc()
	}

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
	sysPrompt := b.String()

	// 6 — append the user message, trim to the history limit
	hist := trimTail(append(sess.History, Msg{Role: "user", Text: userText}), HistoryLimit)

	// 7 — generate; any error degrades to a fixed apology (never bubbles)
	raw, err := gen.Generate(ctx, sysPrompt, hist)
	if err != nil {
		sess.Escalated = true
		return Reply{Text: apologyLine(sessLang(sess)), Signal: SignalEscalate}, nil
	}

	// 8, 9 — parse; never speak untrailered raw output
	spoken, tr := parseTrailer(raw)
	if tr == nil {
		return esc()
	}

	// 10 — merge: assign only non-nil incoming slots (never clear a filled one)
	if tr.Slots.LanguagePair != nil {
		sess.Slots.LanguagePair = tr.Slots.LanguagePair
	}
	if tr.Slots.DocType != nil {
		sess.Slots.DocType = tr.Slots.DocType
	}
	if tr.Slots.Volume != nil {
		sess.Slots.Volume = tr.Slots.Volume
	}
	if tr.Slots.Deadline != nil {
		sess.Slots.Deadline = tr.Slots.Deadline
	}
	if tr.Slots.Certification != nil {
		sess.Slots.Certification = tr.Slots.Certification
	}
	if tr.Slots.Delivery != nil {
		sess.Slots.Delivery = tr.Slots.Delivery
	}

	// 11 — resolve the signal
	signal := tr.Signal
	if signal == SignalLeadReady && !sess.Slots.Complete() { // B4 guard
		log.Printf("dialog: lead_ready with incomplete slots: %s", compactSlots(sess.Slots))
		signal = SignalContinue
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
