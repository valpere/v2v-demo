package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/kb"
	"github.com/valpere/v2v-demo/internal/telegram"
)

// ── fakes ───────────────────────────────────────────────────────

type fakeTG struct {
	mu     sync.Mutex
	texts  []string // chatID ignored — single-chat tests
	voices int
}

func (f *fakeTG) Updates(context.Context) (<-chan telegram.Update, error) { return nil, nil }
func (f *fakeTG) DownloadVoice(context.Context, string) (string, error)   { return "", nil }
func (f *fakeTG) SendVoice(context.Context, int64, []byte) error {
	f.mu.Lock()
	f.voices++
	f.mu.Unlock()
	return nil
}
func (f *fakeTG) SendRecordingAction(context.Context, int64) error { return nil }
func (f *fakeTG) SendText(_ context.Context, _ int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	return nil
}
func (f *fakeTG) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}

type fakeGen struct {
	mu    sync.Mutex
	seen  []string // user texts, in call order
	delay time.Duration
}

func (g *fakeGen) Generate(_ context.Context, _ string, hist []dialog.Msg) (string, error) {
	if g.delay > 0 {
		time.Sleep(g.delay)
	}
	g.mu.Lock()
	g.seen = append(g.seen, hist[len(hist)-1].Text)
	g.mu.Unlock()
	return "ok\n\n```json\n{\"slots\":{\"language_pair\":null,\"doc_type\":null,\"volume\":null,\"deadline\":null,\"certification\":null,\"delivery\":null},\"signal\":\"continue\"}\n```", nil
}

type fakeTTS struct{}

func (fakeTTS) Speak(context.Context, string, string, string) ([]byte, error) {
	return []byte("OggS"), nil
}

func newTestApp(t *testing.T, gen dialog.Generator) (*app, *fakeTG) {
	t.Helper()
	tg := &fakeTG{}
	return &app{
		cfg:      Config{TTSBackend: "elevenlabs", ElevenVoiceA: "A", ElevenVoiceB: "B", DataDir: t.TempDir()},
		tg:       tg,
		gen:      gen,
		tts:      fakeTTS{},
		kb:       []kb.Section{{Title: "Services", Body: "translation"}},
		sys:      "SYS",
		greeting: "GREETING",
		sessions: make(map[int64]*dialog.Session),
		seen:     make(map[int64]bool),
		inbox:    make(map[int64]chan telegram.Update),
	}, tg
}

// ── tests ───────────────────────────────────────────────────────

func TestVoiceCommand(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	a.seen[7] = true // skip greeting

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "/voice b"})
	if a.session(7).Voice != "b" {
		t.Fatalf("voice = %q, want b", a.session(7).Voice)
	}

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "/voice"})
	last := tg.sent()[len(tg.sent())-1]
	if last == "" || !contains(last, "/voice a") {
		t.Fatalf("bare /voice should give help, got %q", last)
	}
}

func TestTextOnlyWhenNoTTS(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	a.tts = nil // TTS_BACKEND=none
	a.seen[7] = true

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "Скільки коштує?"})

	if tg.voices != 0 {
		t.Fatalf("SendVoice called %d times, want 0 when tts is nil", tg.voices)
	}
	if len(tg.sent()) == 0 {
		t.Fatal("no text reply sent")
	}
}

func TestResetClearsSession(t *testing.T) {
	gen := &fakeGen{}
	a, tg := newTestApp(t, gen)

	ctx := context.Background()
	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "Треба перекласти диплом"}) // greeting + turn
	a.session(7).Slots.DocType = strptr("диплом")

	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "/reset"})
	if a.session(7).Slots.DocType != nil {
		t.Fatal("/reset should drop the session's slots")
	}

	before := 0
	for _, s := range tg.sent() {
		if s == "GREETING" {
			before++
		}
	}
	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "hi again"})
	after := 0
	for _, s := range tg.sent() {
		if s == "GREETING" {
			after++
		}
	}
	if after != before+1 {
		t.Fatalf("greeting re-armed count: before=%d after=%d, want +1", before, after)
	}
}

func strptr(s string) *string { return &s }

func TestUnknownCommandNotADialogueTurn(t *testing.T) {
	gen := &fakeGen{}
	a, _ := newTestApp(t, gen)
	a.seen[7] = true
	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "/wat"})
	if len(gen.seen) != 0 {
		t.Fatalf("slash command reached the generator: %v", gen.seen)
	}
}

func TestGreetingOncePerChat(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "hello", IsStart: false})
	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "again"})
	n := 0
	for _, s := range tg.sent() {
		if s == "GREETING" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("greeting sent %d times, want 1", n)
	}
}

func TestPerChatFIFOOrdering(t *testing.T) {
	gen := &fakeGen{delay: 20 * time.Millisecond}
	a, _ := newTestApp(t, gen)
	a.seen[7] = true

	ctx := context.Background()
	want := []string{"translation 1", "translation 2", "translation 3", "translation 4", "translation 5"}
	for _, msg := range want {
		a.dispatch(ctx, telegram.Update{ChatID: 7, Text: msg})
	}

	// Wait for the turn records — the *last* thing handleUpdate does — so no
	// worker is still writing under t.TempDir() when Cleanup runs RemoveAll.
	turnsPath := filepath.Join(a.cfg.DataDir, "turns.jsonl")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(turnsPath)
		if bytes.Count(b, []byte("\n")) >= 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	gen.mu.Lock()
	defer gen.mu.Unlock()
	if len(gen.seen) != 5 {
		t.Fatalf("processed %d/5: %v", len(gen.seen), gen.seen)
	}
	for i := range want {
		if gen.seen[i] != want[i] {
			t.Fatalf("order = %v, want %v", gen.seen, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
