package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/kb"
	"github.com/valpere/v2v-demo/internal/telegram"
)

// ── fakes ───────────────────────────────────────────────────────

type fakeTG struct {
	mu        sync.Mutex
	texts     []string           // all chats, in send order — single-chat tests
	byChat    map[int64][]string // per-chat text, for isolation tests
	voices    int
	downloads int // DownloadVoice call count
}

func (f *fakeTG) Updates(context.Context) (<-chan telegram.Update, error) { return nil, nil }
func (f *fakeTG) DownloadVoice(context.Context, string) (string, error) {
	f.mu.Lock()
	f.downloads++
	f.mu.Unlock()
	return "", nil
}
func (f *fakeTG) SendVoice(context.Context, int64, []byte) error {
	f.mu.Lock()
	f.voices++
	f.mu.Unlock()
	return nil
}
func (f *fakeTG) SendRecordingAction(context.Context, int64) error { return nil }
func (f *fakeTG) SendText(_ context.Context, chatID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	if f.byChat == nil {
		f.byChat = map[int64][]string{}
	}
	f.byChat[chatID] = append(f.byChat[chatID], text)
	return nil
}
func (f *fakeTG) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}
func (f *fakeTG) sentTo(chatID int64) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.byChat[chatID]...)
}

type fakeGen struct {
	mu    sync.Mutex
	seen  []string // user texts, in call order
	delay time.Duration
	err   error // when set, Generate returns it (simulates the LLM backend down)
}

func (g *fakeGen) Generate(_ context.Context, _ string, hist []dialog.Msg) (string, error) {
	if g.delay > 0 {
		time.Sleep(g.delay)
	}
	g.mu.Lock()
	g.seen = append(g.seen, hist[len(hist)-1].Text)
	err := g.err
	g.mu.Unlock()
	if err != nil {
		return "", err
	}
	return `{"reply":"ok","slots":{"language_pair":null,"doc_type":null,"volume":null,"deadline":null,"certification":null,"delivery":null},"signal":"continue"}`, nil
}

func (g *fakeGen) setErr(err error) {
	g.mu.Lock()
	g.err = err
	g.mu.Unlock()
}

type fakeTTS struct{ err error }

func (t fakeTTS) Speak(context.Context, string, string, string) ([]byte, error) {
	if t.err != nil {
		return nil, t.err
	}
	return []byte("OggS"), nil
}

type fakeSTT struct {
	text string
	err  error
}

func (s fakeSTT) Transcribe(context.Context, string, string) (string, error) {
	return s.text, s.err
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
		loc:      time.UTC,
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
	if last := tg.sent()[len(tg.sent())-1]; !contains(last, "другий") {
		t.Fatalf("/voice b line = %q, want the 'second voice' one", last)
	}

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "/voice a"})
	if last := tg.sent()[len(tg.sent())-1]; !contains(last, "перший") {
		t.Fatalf("/voice a line = %q, want the 'first voice' one", last)
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

func TestVoiceMessageWhenNoSTT(t *testing.T) {
	gen := &fakeGen{}
	a, tg := newTestApp(t, gen)
	a.stt = nil // STT_BACKEND=none
	a.seen[7] = true

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, VoiceFileID: "vf"})

	if tg.downloads != 0 {
		t.Fatalf("DownloadVoice called %d times, want 0 when stt is nil", tg.downloads)
	}
	if len(gen.seen) != 0 {
		t.Fatalf("generator called on a voice message with stt disabled: %v", gen.seen)
	}
	last := tg.sent()
	if len(last) == 0 || !contains(last[len(last)-1], "текстом") {
		t.Fatalf("want the voice-unavailable line, got %q", last)
	}
	if b, _ := os.ReadFile(filepath.Join(a.cfg.DataDir, "turns.jsonl")); len(b) != 0 {
		t.Fatalf("a declined voice message wrote a turn record:\n%s", b)
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

	greetings := func() (n int) {
		for _, s := range tg.sent() {
			if s == "GREETING" {
				n++
			}
		}
		return
	}
	before := greetings()
	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "hi again"})
	if greetings() != before {
		t.Fatalf("greeting replayed after /reset (%d → %d); it should not", before, greetings())
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

// 16b — two chats at once: one escalates, the other is untouched.
func TestPerChatIsolation(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	a.seen[3], a.seen[9] = true, true
	ctx := context.Background()

	// chat 3 is mid-quote — 4 of 6 slots filled
	s3 := a.session(3)
	s3.Slots.LanguagePair = strptr("uk->pl")
	s3.Slots.DocType = strptr("диплом")
	s3.Slots.Volume = strptr("2 pages")
	s3.Slots.Deadline = strptr("Friday")

	// chat 9 sends a refund demand -> hard escalate
	a.handleUpdate(ctx, telegram.Update{ChatID: 9, Text: "Поверніть гроші за переклад, це скарга"})

	if !a.session(9).Escalated {
		t.Fatal("chat 9 should be escalated")
	}
	if a.session(3).Escalated {
		t.Fatal("chat 9's escalation leaked into chat 3")
	}
	if got := a.session(3).Slots; got.LanguagePair == nil || got.DocType == nil ||
		got.Volume == nil || got.Deadline == nil {
		t.Fatalf("chat 3 lost its slots: %+v", got)
	}
	if len(tg.sentTo(3)) != 0 {
		t.Fatalf("chat 3 received messages it should not have: %q", tg.sentTo(3))
	}
	if to9 := tg.sentTo(9); len(to9) == 0 || !strings.Contains(to9[len(to9)-1], "менеджер") {
		t.Fatalf("chat 9 handoff line missing: %q", to9)
	}
}

// 16c — the LLM backend is down (unreachable URL): degrade, don't crash;
// recover on the next message once the backend is back.
func TestGeneratorErrorNoCrashThenRecovers(t *testing.T) {
	gen := &fakeGen{err: errors.New("dial tcp 127.0.0.1:1: connection refused")}
	a, tg := newTestApp(t, gen)
	a.kb = []kb.Section{{Title: "Ціни", Body: "переклад диплома вартість сторінка"}}
	a.seen[5] = true
	ctx := context.Background()

	a.handleUpdate(ctx, telegram.Update{ChatID: 5, Text: "Скільки коштує переклад диплома?"})

	last := tg.sentTo(5)
	if len(last) == 0 || !strings.Contains(last[len(last)-1], "менеджер") {
		t.Fatalf("expected the apology+handoff line, got %q", last)
	}
	// a TurnRecord is still written on a degraded turn
	if b, _ := os.ReadFile(filepath.Join(a.cfg.DataDir, "turns.jsonl")); bytes.Count(b, []byte("\n")) != 1 {
		t.Fatalf("want 1 turn record after the degraded turn, got %q", b)
	}

	// backend recovers -> next message is a normal reply
	gen.setErr(nil)
	a.handleUpdate(ctx, telegram.Update{ChatID: 5, Text: "А скільки це коштує за сторінку?"})
	if got := tg.sentTo(5); got[len(got)-1] != "ok" {
		t.Fatalf("after recovery want the normal reply %q, got %q", "ok", got[len(got)-1])
	}
}

// 16d — TTS credential is bad: text still goes out, TurnRecord still written,
// the loop stays alive.
func TestTTSErrorFallsBackToText(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	a.tts = fakeTTS{err: errors.New("401 Unauthorized")}
	a.kb = []kb.Section{{Title: "Ціни", Body: "переклад диплома вартість сторінка"}}
	a.seen[5] = true

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 5, Text: "Скільки коштує переклад диплома?"})

	if tg.voices != 0 {
		t.Fatalf("SendVoice succeeded %d times despite the TTS error", tg.voices)
	}
	if got := tg.sentTo(5); len(got) == 0 || got[len(got)-1] != "ok" {
		t.Fatalf("text reply missing on a TTS failure: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(a.cfg.DataDir, "turns.jsonl")); bytes.Count(b, []byte("\n")) != 1 {
		t.Fatalf("want 1 turn record, got %q", b)
	}
}

// 16g — whitespace-only input never reaches the generator.
func TestWhitespaceInputNotADialogueTurn(t *testing.T) {
	gen := &fakeGen{}
	a, _ := newTestApp(t, gen)
	a.seen[5] = true

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 5, Text: "    "})

	if len(gen.seen) != 0 {
		t.Fatalf("generator called with blank input: %q", gen.seen)
	}
}

// 16i — degenerate short inputs: no crash, each gets some reply.
func TestDegenerateShortInputsNoCrash(t *testing.T) {
	gen := &fakeGen{}
	a, tg := newTestApp(t, gen)
	a.seen[5] = true
	ctx := context.Background()

	for _, msg := range []string{"5", "👍", "."} {
		a.handleUpdate(ctx, telegram.Update{ChatID: 5, Text: msg})
	}
	if len(tg.sentTo(5)) == 0 {
		t.Fatal("no replies to the degenerate inputs")
	}
}

// 18 — /voice, /reset and an sttFail write NO TurnRecord; a normal turn
// writes exactly one, with its key fields populated.
func TestTurnRecordOnlyForDialogueTurns(t *testing.T) {
	a, _ := newTestApp(t, &fakeGen{})
	a.stt = fakeSTT{err: errors.New("whisper exploded")}
	a.kb = []kb.Section{{Title: "Ціни", Body: "переклад диплома вартість сторінка"}}
	a.seen[7] = true
	ctx := context.Background()
	turns := filepath.Join(a.cfg.DataDir, "turns.jsonl")

	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "/voice b"})
	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "/reset"})
	a.handleUpdate(ctx, telegram.Update{ChatID: 7, VoiceFileID: "vf"}) // sttFail

	if b, _ := os.ReadFile(turns); len(b) != 0 {
		t.Fatalf("a command or sttFail wrote a turn record:\n%s", b)
	}

	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "Скільки коштує переклад диплома?"})
	b, err := os.ReadFile(turns)
	if err != nil {
		t.Fatalf("a normal turn wrote no record: %v", err)
	}
	if n := bytes.Count(b, []byte("\n")); n != 1 {
		t.Fatalf("want exactly 1 turn record, got %d:\n%s", n, b)
	}
	var rec struct {
		Time      time.Time `json:"time"`
		ChatID    int64     `json:"chat_id"`
		Signal    string    `json:"signal"`
		LatencyMS int64     `json:"latency_ms"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(b), &rec); err != nil {
		t.Fatalf("turn record not valid JSON: %v", err)
	}
	if rec.Time.IsZero() || rec.ChatID != 7 || rec.Signal == "" {
		t.Fatalf("turn record missing fields: %+v", rec)
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
