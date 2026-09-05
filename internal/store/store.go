// Package store is persistence: the append-only JSONL log (REQ-LOG-01, one
// TurnRecord per turn in turns.jsonl, one LeadRecord per lead_ready turn in
// leads.jsonl, written under DATA_DIR and sent nowhere) plus, optionally, a
// SQLite-backed session store (SESSION_STORE=sqlite — see session.go /
// session_sqlite.go) that lets dialog.Session survive a bot restart.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
)

// TurnRecord is one dialogue turn: what the user said, what the bot replied,
// the resolved signal, the KB section titles consulted, the slot snapshot
// after this turn's merge, and the turn latency.
type TurnRecord struct {
	Time      time.Time         `json:"time"`
	ChatID    int64             `json:"chat_id"`
	UserText  string            `json:"user_text"`
	ReplyText string            `json:"reply_text"`
	Signal    string            `json:"signal"`
	Matched   []string          `json:"matched"` // empty on a pre-LLM escalate
	Slots     dialog.QuoteSlots `json:"slots"`
	LatencyMS int64             `json:"latency_ms"`
}

// LeadRecord is the shape a Zoho lead would take — written to leads.jsonl,
// sent nowhere. Unset slots become "".
type LeadRecord struct {
	Time          time.Time `json:"time"`
	ChatID        int64     `json:"chat_id"`
	LanguagePair  string    `json:"language_pair"`
	DocType       string    `json:"doc_type"`
	Volume        string    `json:"volume"`
	Deadline      string    `json:"deadline"`
	Certification string    `json:"certification"`
	Delivery      string    `json:"delivery"`
}

// AppendTurn appends r to dir/turns.jsonl, creating dir if needed.
func AppendTurn(dir string, r TurnRecord) error {
	return appendJSONL(dir, "turns.jsonl", r)
}

// AppendLead appends r to dir/leads.jsonl, creating dir if needed.
func AppendLead(dir string, r LeadRecord) error {
	return appendJSONL(dir, "leads.jsonl", r)
}

func appendJSONL(dir, name string, v any) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("store: marshal %s: %w", name, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("store: open %s: %w", name, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("store: write %s: %w", name, err)
	}
	return nil
}
