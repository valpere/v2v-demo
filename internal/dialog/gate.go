package dialog

import (
	"strings"
	"unicode"

	"github.com/valpere/v2v-demo/internal/kb"
)

// Behavioural constants — see @schema GateParams in docs/requirements.md §1.
const (
	GateFloor        = 0.25 // kbOverlap below this on a content question -> escalate pre-LLM
	HistoryLimit     = 20   // Session.History cap, in Msg entries (~10 turns)
	SlotAnswerMaxTok = 6    // isSlotAnswer: max tokens for a plausible slot answer
)

// stopwords is a fixed uk+en function-word list. Without it, "the"/"of"/"і"/"на"
// inflate kbOverlap and the grounding gate never fires (ragline's tsquery lesson).
var stopwords = func() map[string]bool {
	list := strings.Fields(`
		а і й та це то бо як що чи не ні но але або де коли там тут вже ще
		я ти ви ми він вона воно вони мене тебе вас нас
		в у на до з із зі за під над про від о об по при для без
		the a an and or but if of to in on at by for from with as is are was
		were be been it its this that these those you your i we they he she
		do does did has have had will would can could my me our
	`)
	m := make(map[string]bool, len(list))
	for _, w := range list {
		m[w] = true
	}
	return m
}()

// escalateKeywords are unambiguous handoff triggers (B3), checked as
// lowercase substrings before the slot-answer bypass and the gate. The
// two-term cases (sworn + non-listed language; interpreting + booking) stay
// with the model's own escalate signal and prompt/system.md hard rules.
var escalateKeywords = []string{
	"повернути гро", "повернення кош", "поверніть гро", "refund",
	"скарг", "complaint",
	"суд", "позов", "court",
	"дайте людину", "з людиною", "справжн", "real person",
	"talk to a person", "менеджера напряму",
}

// pleasantryMarkers are greeting / thanks / farewell fragments (lowercase,
// punctuation-free) that mark a conversation boundary, not a content question.
var pleasantryMarkers = []string{
	"добрий день", "доброго дня", "добрий вечір", "доброго вечора", "доброго ранку",
	"добридень", "привіт", "вітаю", "доброго здоров", "радий вас",
	"дякую", "дякуємо", "вдячн", "спасибі", "щиро дяк",
	"до побачення", "на все добре", "гарного дня", "всього найкращого", "бувайте",
	"hello", "hi ", "hey", "good morning", "good afternoon", "good evening",
	"thank you", "thanks", "thank u", "appreciate",
	"goodbye", "bye", "have a good", "have a nice", "see you", "cheers",
}

// isSmallTalk reports whether userText is a short greeting / thanks / farewell
// with no embedded question — it should reach the assistant, never pre-escalate
// (a greeting is the start of a dialogue, not grounds for a handoff).
func isSmallTalk(userText string) bool {
	if len(tokens(userText)) > 8 || strings.Contains(userText, "?") {
		return false
	}
	q := " " + lowerStripPunct(userText) + " "
	for _, m := range pleasantryMarkers {
		if strings.Contains(q, m) {
			return true
		}
	}
	return false
}

func lowerStripPunct(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return b.String()
}

func tokens(s string) []string {
	return strings.Fields(lowerStripPunct(s))
}

// meaningfulTerms is the distinct, stopword-filtered token set of s.
func meaningfulTerms(s string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, t := range tokens(s) {
		if stopwords[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// kbOverlap is the fraction of the query's meaningful terms that appear
// anywhere in the concatenated KB. Exact substring match, no stemming
// (B1 fallback: add a Ukrainian stemmer if inflected forms are missed —
// do not reintroduce per-section retrieval). Deterministic.
func kbOverlap(query string, sections []kb.Section) float64 {
	qterms := meaningfulTerms(query)
	if len(qterms) == 0 {
		return 0
	}
	var h strings.Builder
	for _, s := range sections {
		h.WriteString(lowerStripPunct(s.Title + " " + s.Body))
		h.WriteByte(' ')
	}
	haystack := h.String()

	hit := 0
	for _, t := range qterms {
		if strings.Contains(haystack, t) {
			hit++
		}
	}
	return float64(hit) / float64(len(qterms))
}

// hardEscalate reports whether the message contains an unambiguous handoff
// trigger (B3). A true result forces a handoff before the slot-answer bypass
// and the gate.
func hardEscalate(query string) bool {
	q := strings.ToLower(query)
	for _, kw := range escalateKeywords {
		if strings.Contains(q, kw) {
			return true
		}
	}
	return false
}

// isSlotAnswer reports whether userText is plausibly an answer to a question
// the bot just asked: short, the most recent assistant message ends with "?",
// and a quote slot is still nil (B3 — the "bot just asked" clause stops a
// bare "apostille?" from bypassing the gate).
func isSlotAnswer(sess *Session, userText string) bool {
	if len(tokens(userText)) > SlotAnswerMaxTok {
		return false
	}
	last := lastAssistant(sess.History)
	if !strings.HasSuffix(strings.TrimSpace(last), "?") {
		return false
	}
	return !sess.Slots.Complete()
}

func lastAssistant(history []Msg) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			return history[i].Text
		}
	}
	return ""
}

// groundingGate decides, from (kbOverlap, slotAnswer) only, whether to
// escalate before any Generator call. Pure function of its two arguments.
func groundingGate(overlap float64, slotAnswer bool) (forceEscalate bool) {
	if slotAnswer {
		return false
	}
	if overlap < GateFloor {
		return true
	}
	return false
}

// matchedTitles are the KB section titles whose title+body contains a
// meaningful query term. Log-only, informational.
func matchedTitles(query string, sections []kb.Section) []string {
	terms := meaningfulTerms(query)
	if len(terms) == 0 {
		return nil
	}
	var out []string
	for _, s := range sections {
		hay := lowerStripPunct(s.Title + " " + s.Body)
		for _, t := range terms {
			if strings.Contains(hay, t) {
				out = append(out, s.Title)
				break
			}
		}
	}
	return out
}
