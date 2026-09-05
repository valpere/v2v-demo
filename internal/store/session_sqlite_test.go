package store

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/valpere/v2v-demo/internal/dialog"
)

func sp(s string) *string { return &s }

func TestSQLiteSessionsRoundTrip(t *testing.T) {
	db, err := NewSQLiteSessions(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	want := &dialog.Session{
		Slots:      dialog.QuoteSlots{DocType: sp("diploma"), Volume: sp("2 pages")},
		History:    []dialog.Msg{{Role: "user", Text: "hi"}, {Role: "assistant", Text: "hello"}},
		Voice:      "b",
		Lang:       "uk",
		Escalated:  true,
		LeadDone:   true,
		LeadSlots:  `{"doc_type":"diploma"}`,
		GateStrike: true,
		Topic:      "translations",
	}
	if err := db.Save(42, want); err != nil {
		t.Fatal(err)
	}

	got, found, err := db.Load(42)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("found = false, want true after Save")
	}
	if got.Voice != want.Voice || got.Lang != want.Lang || got.Escalated != want.Escalated ||
		got.LeadDone != want.LeadDone || got.LeadSlots != want.LeadSlots ||
		got.GateStrike != want.GateStrike || got.Topic != want.Topic {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, want)
	}
	if got.Slots.DocType == nil || *got.Slots.DocType != "diploma" {
		t.Fatalf("Slots not round-tripped: %+v", got.Slots)
	}
	if len(got.History) != 2 || got.History[0].Text != "hi" {
		t.Fatalf("History not round-tripped: %+v", got.History)
	}
}

func TestSQLiteSessionsLoadNotFound(t *testing.T) {
	db, err := NewSQLiteSessions(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, found, err := db.Load(1); err != nil || found {
		t.Fatalf("Load on an empty DB: found=%v err=%v, want false/nil", found, err)
	}
}

func TestSQLiteSessionsDeleteThenLoad(t *testing.T) {
	db, err := NewSQLiteSessions(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Save(7, &dialog.Session{Voice: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(7); err != nil {
		t.Fatal(err)
	}
	if _, found, err := db.Load(7); err != nil || found {
		t.Fatalf("Load after Delete: found=%v err=%v, want false/nil", found, err)
	}
	if err := db.Delete(7); err != nil { // deleting again is not an error
		t.Fatalf("second Delete: %v", err)
	}
}

// TestSQLiteSessionsConcurrentSave is the regression test for the
// concurrency fix: SetMaxOpenConns(1) + WAL/busy-timeout must let N chats
// save at once without SQLITE_BUSY. Run with -race (make check does).
func TestSQLiteSessionsConcurrentSave(t *testing.T) {
	db, err := NewSQLiteSessions(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := int64(0); i < n; i++ {
		wg.Add(1)
		go func(chatID int64) {
			defer wg.Done()
			errs <- db.Save(chatID, &dialog.Session{Voice: "a", Topic: "t"})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Save: %v", err)
		}
	}
	for i := int64(0); i < n; i++ {
		if _, found, err := db.Load(i); err != nil || !found {
			t.Errorf("Load(%d): found=%v err=%v", i, found, err)
		}
	}
}
