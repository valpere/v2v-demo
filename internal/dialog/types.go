// Package dialog turns one user turn into one reply: the grounding gate, the
// LLM call, response parsing and the slot merge. The behaviour is spelled out
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
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

// Signal is the model's per-turn control signal (or one the gate forces).
type Signal string

const (
	SignalContinue  Signal = "continue"
	SignalLeadReady Signal = "lead_ready"
	SignalEscalate  Signal = "escalate"
)

// Session is the per-chat state. By default it lives in memory only and is
// dropped on restart; SESSION_STORE=sqlite persists it (cmd/bot + internal/store).
// All fields are exported (and json-tagged) so the whole struct round-trips
// through encoding/json for storage — encoding/json silently drops
// unexported fields, which would otherwise reset LeadSlots/GateStrike on
// every reload.
type Session struct {
	Slots     QuoteSlots `json:"slots"`
	History   []Msg      `json:"history"` // trimmed to the last HistoryLimit entries
	Voice     string     `json:"voice"`   // "a" | "b"; default "a"
	Lang      string     `json:"lang"`    // "uk" | "en"; locked from the first non-empty user turn
	Escalated bool       `json:"escalated"`
	LeadDone  bool       `json:"lead_done"` // a lead_ready already fired this session

	// LeadSlots is the slot JSON at the last lead_ready that was allowed
	// through. A later lead_ready with the *same* slots is a spurious
	// re-trigger and is downgraded to continue; a later one with *different*
	// slots is a real post-summary correction and is let through so a
	// corrected LeadRecord gets written (consumers take the newest row per
	// chat). Empty until the first lead.
	LeadSlots string `json:"lead_slots"`

	// GateStrike is set when the grounding gate fired on the previous turn
	// and the bot answered with the fixed clarification line instead of
	// escalating. A second consecutive gate hit escalates. Cleared on any
	// turn that reaches the model, is small talk, or is a slot answer.
	GateStrike bool `json:"gate_strike"`

	// Topic is the chosen assistant/KB id (cmd/bot topics.go); empty means
	// no topic has been picked yet. Ignored by Handle itself — cmd/bot
	// resolves which KB/system prompt to pass in based on this.
	Topic string `json:"topic"`
}

// Reply is the outcome of one turn.
type Reply struct {
	Text    string   // spoken text (mr.Reply) (or a fixed handoff/apology line)
	Signal  Signal   // continue | lead_ready | escalate
	Matched []string // log-only: KB section titles with a query-term hit; nil on an early escalate
}
