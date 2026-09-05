package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	return n
}

func TestAppendTurn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data") // not pre-created

	rec := TurnRecord{
		Time:      time.Now(),
		ChatID:    42,
		UserText:  "скільки коштує переклад диплома?",
		ReplyText: "Уточню кілька деталей…",
		Signal:    "continue",
		Matched:   []string{"Services"},
		Slots:     map[string]string{"doc_type": "diploma"},
		LatencyMS: 1234,
	}
	if err := AppendTurn(dir, rec); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	if err := AppendTurn(dir, rec); err != nil {
		t.Fatalf("AppendTurn 2: %v", err)
	}

	path := filepath.Join(dir, "turns.jsonl")
	if got := countLines(t, path); got != 2 {
		t.Fatalf("turns.jsonl has %d lines, want 2", got)
	}

	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan()
	var back TurnRecord
	if err := json.Unmarshal(sc.Bytes(), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ChatID != 42 || back.Signal != "continue" || back.Slots["doc_type"] != "diploma" {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

func TestAppendLead(t *testing.T) {
	dir := t.TempDir()
	if err := AppendLead(dir, LeadRecord{ChatID: 7, Topic: "translation", Fields: map[string]string{"language_pair": "uk->de"}}); err != nil {
		t.Fatalf("AppendLead: %v", err)
	}
	if got := countLines(t, filepath.Join(dir, "leads.jsonl")); got != 1 {
		t.Fatalf("leads.jsonl has %d lines, want 1", got)
	}
}
