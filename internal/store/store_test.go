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

// 2.1 — logs hold user messages: private to the service user.
func TestLogFilesAndDirArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := AppendTurn(dir, TurnRecord{Time: time.Now(), ChatID: 1}); err != nil {
		t.Fatal(err)
	}
	// a pre-existing, too-open file is tightened on the next append
	lead := filepath.Join(dir, "leads.jsonl")
	if err := os.WriteFile(lead, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendLead(dir, LeadRecord{Time: time.Now(), ChatID: 1}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "turns.jsonl"): 0o600, lead: 0o600} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", path, got, want)
		}
	}
}

// 2.1 — retention: records older than N days are pruned, recent and
// unparsable lines are kept, 0 days keeps everything.
func TestPruneLogs(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	old := TurnRecord{Time: now.AddDate(0, 0, -91), ChatID: 1, UserText: "old"}
	fresh := TurnRecord{Time: now.AddDate(0, 0, -89), ChatID: 2, UserText: "fresh"}
	for _, r := range []TurnRecord{old, fresh} {
		if err := AppendTurn(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := AppendLead(dir, LeadRecord{Time: now.AddDate(0, 0, -200), ChatID: 1}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(filepath.Join(dir, "turns.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("not json\n")
	f.Close()

	if err := PruneLogs(dir, 0, now); err != nil || countLines(t, filepath.Join(dir, "turns.jsonl")) != 3 {
		t.Fatalf("0 days must keep everything: %v", err)
	}
	if err := PruneLogs(dir, 90, now); err != nil {
		t.Fatal(err)
	}
	if n := countLines(t, filepath.Join(dir, "turns.jsonl")); n != 2 {
		t.Fatalf("turns.jsonl lines = %d, want 2 (fresh + unparsable)", n)
	}
	if n := countLines(t, filepath.Join(dir, "leads.jsonl")); n != 0 {
		t.Fatalf("leads.jsonl lines = %d, want 0", n)
	}
	if err := PruneLogs(filepath.Join(dir, "missing"), 90, now); err != nil {
		t.Fatalf("missing dir is not an error: %v", err)
	}
}

// A prune rewrites the file; an append racing it must not be lost. The
// appender interleaves stale records (so every prune really rewrites) with
// fresh ones; every fresh one must survive.
func TestPruneDoesNotLoseConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	const n = 300
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			ts, id := now, int64(i)
			if i%2 == 0 {
				ts, id = now.AddDate(0, 0, -400), -1 // stale
			}
			if err := AppendTurn(dir, TurnRecord{Time: ts, ChatID: id}); err != nil {
				t.Error(err)
			}
		}
	}()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
			if err := PruneLogs(dir, 90, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := PruneLogs(dir, 90, now); err != nil {
		t.Fatal(err)
	}
	if got := countLines(t, filepath.Join(dir, "turns.jsonl")); got != n/2 {
		t.Fatalf("fresh lines = %d, want %d (a concurrent append was lost)", got, n/2)
	}
}
