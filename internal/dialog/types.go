// Package dialog turns one user turn into one reply: the grounding gate, the
// LLM call, response parsing and the slot merge. The behaviour is spelled out
// step-for-step in .agents/plan.md §"Behavioural spec" — gate.go and
// dialog.go implement it verbatim.
package dialog

import "github.com/valpere/v2v-demo/internal/kb"

// SlotSpec declares one thing a topic collects from the client. Key is the
// JSON key the model returns it under (and the map key in Session.Slots);
// AskUK/AskEN are the plain-words phrasings the clarify line uses when the
// slot is still missing ("з якої мови на яку" / "which languages"); Rule is
// an optional one-line constraint hint injected into the response-format
// block (e.g. "e.g. uk->de", "none|certified|notarized|sworn").
type SlotSpec struct {
	Key   string `json:"key"`
	AskUK string `json:"ask_uk"`
	AskEN string `json:"ask_en"`
	Rule  string `json:"rule"`
}

// TopicSpec is everything Handle needs that varies per topic: the whole KB,
// the persona/playbook system prompt, the slot schema, and the one sentence
// the clarify line uses to state what this assistant is for
// ("Я допомагаю лише з перекладами документів." / "I only help with …").
type TopicSpec struct {
	KB      []kb.Section
	System  string
	Slots   []SlotSpec
	ScopeUK string
	ScopeEN string
}

// Complete reports whether every slot the topic declares has a non-empty
// value — the condition for a lead_ready signal (REQ-DLG-04).
func Complete(slots map[string]string, spec []SlotSpec) bool {
	for _, s := range spec {
		if slots[s.Key] == "" {
			return false
		}
	}
	return true
}

// filledSlots counts the topic's slots that currently have a non-empty value.
func filledSlots(slots map[string]string, spec []SlotSpec) int {
	n := 0
	for _, s := range spec {
		if slots[s.Key] != "" {
			n++
		}
	}
	return n
}

// slotKeySet is the set of keys the topic declares — the merge filters
// incoming slots to it so a model-invented key can't pollute Session.Slots
// (and therefore the compactSlots lead-dedupe hash and the turn log).
func slotKeySet(spec []SlotSpec) map[string]bool {
	m := make(map[string]bool, len(spec))
	for _, s := range spec {
		m[s.Key] = true
	}
	return m
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
	// Slots is the collected state, keyed by the topic's SlotSpec.Key. An
	// absent key or an empty value means "still unknown"; the state of the
	// intake *is* which keys are unset. May be nil (a fresh session, a
	// /reset); Handle lazily initialises it before the first write.
	Slots     map[string]string `json:"slots"`
	History   []Msg             `json:"history"` // trimmed to the last HistoryLimit entries
	Voice     string            `json:"voice"`   // "a" | "b"; default "a"
	Lang      string            `json:"lang"`    // "uk" | "en"; locked from the first non-empty user turn
	Escalated bool              `json:"escalated"`
	LeadDone  bool              `json:"lead_done"` // a lead_ready already fired this session

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
	// resolves which TopicSpec to pass in based on this.
	Topic string `json:"topic"`
}

// Reply is the outcome of one turn.
type Reply struct {
	Text    string   // spoken text (mr.Reply) (or a fixed handoff/apology line)
	Signal  Signal   // continue | lead_ready | escalate
	Matched []string // log-only: KB section titles with a query-term hit; nil on an early escalate
}
