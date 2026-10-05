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

// EscalateRule is a per-topic deterministic handoff: when the conversation is
// about X (If — tested on the message plus the named slot, so "Tesla" said a
// turn earlier still counts), the message asks for Y (And — tested on the
// message only) and is not the permitted exception (Unless — message only),
// the bot hands off with the plain handoff line, before the model. It exists
// for rules a prompt cannot be trusted to follow (e.g. auto: an electric
// vehicle is only served for tyres and alignment).
type EscalateRule struct {
	If     string `json:"if"`
	And    string `json:"and"`
	Unless string `json:"unless"`
	Slot   string `json:"slot"` // optional slot key whose value also feeds If

	ifRe, andRe, unlessRe *regexp.Regexp
}

// Compile validates and compiles the patterns; call once when the topic loads.
func (r *EscalateRule) Compile() error {
	var err error
	if r.If == "" || r.And == "" {
		return fmt.Errorf("escalate rule needs both \"if\" and \"and\"")
	}
	if r.ifRe, err = regexp.Compile("(?i)" + r.If); err != nil {
		return fmt.Errorf("escalate rule if %q: %w", r.If, err)
	}
	if r.andRe, err = regexp.Compile("(?i)" + r.And); err != nil {
		return fmt.Errorf("escalate rule and %q: %w", r.And, err)
	}
	if r.Unless != "" {
		if r.unlessRe, err = regexp.Compile("(?i)" + r.Unless); err != nil {
			return fmt.Errorf("escalate rule unless %q: %w", r.Unless, err)
		}
	}
	return nil
}

func (r EscalateRule) matches(text string, slots map[string]string) bool {
	if r.ifRe == nil || r.andRe == nil { // not compiled
		return false
	}
	subject := text
	if r.Slot != "" {
		subject += " " + slots[r.Slot]
	}
	if !r.ifRe.MatchString(subject) || !r.andRe.MatchString(text) {
		return false
	}
	return r.unlessRe == nil || !r.unlessRe.MatchString(text)
}
