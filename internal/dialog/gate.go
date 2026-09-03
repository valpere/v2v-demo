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
	// legal threats only — a bare "суд"/"court" also matched "для суду" /
	// "for a court" (a court as the document's recipient — a normal
	// certification case). Keep the phrasings that are unambiguously threats.
	"до суду", "в суд ", "позов", "судитися", "судовий позов",
	"see you in court", "you to court", "sue you", "sue the", "lawsuit", "legal action",
	"дайте людину", "з людиною", "справжн", "real person",
	"talk to a person", "з менеджером", "менеджера напряму", "напряму з", "speak to a manager", "human agent",
	"хочу менедж", "потрібен менедж", "потрібно менедж", "дайте менедж", "покличте менедж",
	"переведіть на менедж", "переключіть на менедж", "менеджера прошу", "давайте менедж",
	"want a manager", "need a manager", "get me a manager", "connect me to a manager",
	// services the bureau definitively does not offer / methods not in the KB —
	// distinctive enough to catch before the LLM (which is inconsistent at
	// coupling "we don't do that" to signal:escalate)
	"усний переклад", "усного переклад", "усним переклад", "interpreting", "interpreter",
	"субтитр", "subtitle", "переклад відео", "translate a video", "translate the video",
	"paypal", "пейпал",
	"факсом", "по факсу", "by fax", "send it by fax",
}

// pleasantryMarkers are greeting / thanks / farewell / acknowledgement
// fragments (lowercase, punctuation-free) that mark a conversation boundary,
// not a content question. Matched against a space-padded query, so a leading
// space means "as its own word".
var pleasantryMarkers = []string{
	"добрий день", "доброго дня", "добрий вечір", "доброго вечора", "доброго ранку",
	"добридень", "привіт", "вітаю", "доброго здоров", "радий вас",
	"дякую", "дякуємо", "вдячн", "спасибі", "щиро дяк",
	"до побачення", "на все добре", "гарного дня", "всього найкращого", "бувайте",
	" ок", "добре", "гаразд", "зрозуміл", " ясно", "згод", "чудово", "супер",
	"hello", "hi ", "hey", "good morning", "good afternoon", "good evening",
	"thank you", "thanks", "thank u", "appreciate",
	"goodbye", "bye", "have a good", "have a nice", "see you", "cheers",
	" ok", "okay", "got it", "understood", "noted", "alright", "all right", " sure",
}

// looksLikeInjection reports whether userText is shaped like the model's own
// structured output rather than a client sentence — a JSON object carrying
// slots/signal, or a ```json fence. A real translation client never sends
// that; a probe does, and gpt-4.1-mini will read slots out of it and fire
// lead_ready. dialog.Handle answers it with the clarify line, pre-LLM.
func looksLikeInjection(userText string) bool {
	t := strings.ToLower(userText)
	if strings.Contains(t, "```") && strings.Contains(t, "{") {
		return true
	}
	// a JSON-ish object mentioning our own field names
	if strings.Contains(t, "{") && strings.Contains(t, "}") &&
		(strings.Contains(t, `"slots"`) || strings.Contains(t, `"signal"`) ||
			strings.Contains(t, `"language_pair"`) || strings.Contains(t, `"doc_type"`) ||
			(strings.Contains(t, `"reply"`) && strings.Contains(t, `":`))) {
		return true
	}
	return false
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

// kbOverlap is the fraction of the query's meaningful terms that appear in
// the KB, matched by shared word-stem rather than exact substring — Ukrainian
// inflection is suffixal, so "університету" hits "університети",
// "засвідчений" hits "засвідчення" ("контейнер"-style prefix match, ≥5 runes;
// this is the B1 stemmer fallback, done without a dependency). The и/і/ї/е/є
// vowel alternations that inflection also produces conveniently break spurious
// matches ("Києві" ≠ "київського"). Deterministic.
func kbOverlap(query string, sections []kb.Section) float64 {
	qterms := meaningfulTerms(query)
	if len(qterms) == 0 {
		return 0
	}
	kbTokens := kbTokenSet(sections)

	hit := 0
	for _, t := range qterms {
		if stemMatch(t, kbTokens) {
			hit++
		}
	}
	return float64(hit) / float64(len(qterms))
}

// kbTokenSet is the distinct lowercased word set of the whole KB.
func kbTokenSet(sections []kb.Section) map[string]bool {
	set := make(map[string]bool, 512)
	for _, s := range sections {
		for _, t := range tokens(s.Title + " " + s.Body) {
			set[t] = true
		}
	}
	return set
}

// stemMatch reports whether q shares a word-stem with any KB token: an exact
// hit, or a common prefix of ≥5 runes, or the shorter token (≥4 runes) being
// a prefix of the other.
func stemMatch(q string, kbTokens map[string]bool) bool {
	if kbTokens[q] {
		return true
	}
	qr := []rune(q)
	for t := range kbTokens {
		tr := []rune(t)
		n := commonPrefix(qr, tr)
		if n >= 5 {
			return true
		}
		if short := min(len(qr), len(tr)); n == short && short >= 4 {
			return true
		}
	}
	return false
}

func commonPrefix(a, b []rune) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
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
// the bot just asked: short, the most recent assistant message contains a
// "?", and a quote slot is still nil (B3 — the "bot just asked" clause stops
// a bare "apostille?" from bypassing the gate). "contains" not "ends with":
// the model routinely asks "…?" and then adds a clarifying sentence, e.g.
// "…для якої установи? Це потрібно, щоб підібрати тип засвідчення." (found in
// live testing 2026-08-31 — "для університету в Берліні" was escalating).
func isSlotAnswer(sess *Session, userText string) bool {
	if sess.gateStrike {
		return false // the last reply was "I didn't understand", not a slot question
	}
	if !strings.Contains(lastAssistant(sess.History), "?") {
		return false
	}
	if sess.Slots.Complete() {
		return false
	}
	// Once a quote is clearly under way (2+ slots filled) any reply to the
	// bot's question is a slot answer — a full sentence like "диплом на одну
	// сторінку і додаток на дві, бажано за тиждень" must not hit the grounding
	// gate. The LLM handles a mid-quote off-topic itself. Before that, keep the
	// terse-answer check so an opening ramble still gets grounded.
	if filledSlots(sess.Slots) >= 2 {
		return true
	}
	return len(tokens(userText)) <= SlotAnswerMaxTok
}

func filledSlots(s QuoteSlots) int {
	n := 0
	for _, p := range []*string{s.LanguagePair, s.DocType, s.Volume, s.Deadline, s.Certification, s.Delivery} {
		if p != nil {
			n++
		}
	}
	return n
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

// matchedTitles are the KB section titles that share a stem with a meaningful
// query term. Log-only, informational.
func matchedTitles(query string, sections []kb.Section) []string {
	terms := meaningfulTerms(query)
	if len(terms) == 0 {
		return nil
	}
	var out []string
	for _, s := range sections {
		secTokens := kbTokenSet([]kb.Section{s})
		for _, t := range terms {
			if stemMatch(t, secTokens) {
				out = append(out, s.Title)
				break
			}
		}
	}
	return out
}
