// Package dialog turns one user turn into one reply: the grounding gate, the
// LLM call, trailer parsing and the slot merge. The behaviour is spelled out
// step-for-step in .agents/plan.md §"Behavioural spec" — gate.go and
// dialog.go implement it verbatim.
package dialog

// QuoteSlots holds the six quote parameters (FR-7). A nil pointer means the
// slot is still unknown; the state of the intake *is* which fields are nil.
type QuoteSlots struct {
	LanguagePair  *string `json:"language_pair"`
	DocType       *string `json:"doc_type"`
	Volume        *string `json:"volume"`
	Deadline      *string `json:"deadline"`
	Certification *string `json:"certification"`
	Delivery      *string `json:"delivery"`
}

// Complete reports whether all six slots are set — the condition for a
// lead_ready signal (REQ-DLG-04).
func (s QuoteSlots) Complete() bool {
	return s.LanguagePair != nil &&
		s.DocType != nil &&
		s.Volume != nil &&
		s.Deadline != nil &&
		s.Certification != nil &&
		s.Delivery != nil
}

// Msg is one entry of the rolling conversation history.
type Msg struct {
	Role string // "user" | "assistant"
	Text string
}

// Signal is the model's per-turn control signal (or one the gate forces).
type Signal string

const (
	SignalContinue  Signal = "continue"
	SignalLeadReady Signal = "lead_ready"
	SignalEscalate  Signal = "escalate"
)

// Session is the per-chat state, held in memory and dropped on restart.
type Session struct {
	Slots     QuoteSlots
	History   []Msg  // trimmed to the last HistoryLimit entries
	Voice     string // "a" | "b"; default "a"
	Lang      string // "uk" | "en"; locked from the first non-empty user turn
	Escalated bool
}

// Reply is the outcome of one turn.
type Reply struct {
	Text    string   // spoken text, trailer stripped (or a fixed handoff/apology line)
	Signal  Signal   // continue | lead_ready | escalate
	Matched []string // log-only: KB section titles with a query-term hit; nil on an early escalate
}
