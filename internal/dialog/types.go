// Package dialog turns one user turn into one reply: the grounding gate, the
// LLM call, trailer parsing and the slot merge. The behaviour is spelled out
// step-for-step in .agents/plan.md §"Behavioural spec"; gate.go and dialog.go
// (build-order step 3) implement it. This file holds the type surface that
// other packages depend on.
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
