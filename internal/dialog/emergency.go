package dialog

import (
	"fmt"
	"regexp"
)

// EmergencySpec is a topic's deterministic emergency detection (e.g. a dental
// clinic: heavy bleeding, trouble breathing). A user message matching any
// pattern gets the fixed emergency text — before the model is consulted — plus
// the normal handoff, whether or not the office is open: the model's own
// reply is never trusted with safety advice. Both reply texts empty (or no
// patterns) = the topic has no emergency handling.
type EmergencySpec struct {
	Patterns []string `json:"patterns"` // case-insensitive regexps, matched against the user text
	ReplyUK  string   `json:"reply_uk"`
	ReplyEN  string   `json:"reply_en"`

	re []*regexp.Regexp
}

// Compile validates and compiles Patterns; call it once when the topic loads.
func (e *EmergencySpec) Compile() error {
	e.re = e.re[:0]
	for _, p := range e.Patterns {
		r, err := regexp.Compile("(?i)" + p)
		if err != nil {
			return fmt.Errorf("emergency pattern %q: %w", p, err)
		}
		e.re = append(e.re, r)
	}
	if len(e.re) > 0 && (e.ReplyUK == "" || e.ReplyEN == "") {
		return fmt.Errorf("emergency: reply_uk and reply_en are both required when patterns are set")
	}
	return nil
}

func (e EmergencySpec) matches(text string) bool {
	for _, r := range e.re {
		if r.MatchString(text) {
			return true
		}
	}
	return false
}

func (e EmergencySpec) reply(lang string) string {
	if lang == "uk" {
		return e.ReplyUK
	}
	return e.ReplyEN
}
