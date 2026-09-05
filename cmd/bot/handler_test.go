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
	downloads int                 // DownloadVoice call count
	buttons   [][]telegram.Button // one entry per SendButtons call
	acked     []string            // callback IDs passed to AnswerCallback, in order
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
func (f *fakeTG) SendButtons(_ context.Context, _ int64, _ string, buttons []telegram.Button) error {
	f.mu.Lock()
	f.buttons = append(f.buttons, buttons)
	f.mu.Unlock()
	return nil
}
func (f *fakeTG) AnswerCallback(_ context.Context, callbackID string) error {
	f.mu.Lock()
	f.acked = append(f.acked, callbackID)
	f.mu.Unlock()
	return nil
}
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

// faultySessions wraps a real sessionStore and injects one-shot errors —
// used to exercise handleUpdate's error paths without a real SQLite failure.
type faultySessions struct {
	sessionStore
	failNextLoad   bool
	failNextDelete bool
}

func (f *faultySessions) Load(chatID int64) (*dialog.Session, bool, error) {
	if f.failNextLoad {
		f.failNextLoad = false
		return nil, false, errors.New("injected load failure")
	}
	return f.sessionStore.Load(chatID)
}

func (f *faultySessions) Delete(chatID int64) error {
	if f.failNextDelete {
		f.failNextDelete = false
		return errors.New("injected delete failure")
	}
	return f.sessionStore.Delete(chatID)
}

// defaultTopic is the sole topic newTestApp seeds — single-topic mode, so
// every existing test keeps its old text/greeting/kb behavior unchanged and
// never sees a picker. Tests exercising the multi-topic picker build their
// own app.topics/topicIDs instead of using this helper.
const defaultTopicID = "default"

func newTestApp(t *testing.T, gen dialog.Generator) (*app, *fakeTG) {
	t.Helper()
	tg := &fakeTG{}
	topic := topicBundle{
		ID:       defaultTopicID,
		Title:    "Default",
		KB:       []kb.Section{{Title: "Services", Body: "translation"}},
		Sys:      "SYS",
		Greeting: "GREETING",
	}
	return &app{
		cfg:      Config{TTSBackend: "elevenlabs", ElevenVoiceA: "A", ElevenVoiceB: "B", DataDir: t.TempDir()},
		tg:       tg,
		gen:      gen,
		tts:      fakeTTS{},
		topics:   map[string]topicBundle{defaultTopicID: topic},
		topicIDs: []string{defaultTopicID},
		loc:      time.UTC,
		sessions: newMemSessions(),
		inbox:    make(map[int64]chan telegram.Update),
	}, tg
}

// seed marks chatID as already contacted (so handleUpdate skips the
// greeting) and returns a fresh *dialog.Session for the test to mutate —
// call save(t, a, chatID, sess) to persist any change before the next
// handleUpdate, since sessionStore is copy-semantics (see sessions.go).
func seed(t *testing.T, a *app, chatID int64) *dialog.Session {
	t.Helper()
	sess := &dialog.Session{Voice: "a"}
	if err := a.sessions.Save(chatID, sess); err != nil {
		t.Fatalf("seed session %d: %v", chatID, err)
	}
	return sess
}

// loadSession is the test-side equivalent of handleUpdate's load point —
// used to peek at a session's state after a turn.
func loadSession(t *testing.T, a *app, chatID int64) *dialog.Session {
	t.Helper()
	sess, _, err := a.sessions.Load(chatID)
	if err != nil {
		t.Fatalf("load session %d: %v", chatID, err)
	}
	if sess == nil {
		sess = &dialog.Session{Voice: "a"}
	}
	return sess
}

func save(t *testing.T, a *app, chatID int64, sess *dialog.Session) {
	t.Helper()
	if err := a.sessions.Save(chatID, sess); err != nil {
		t.Fatalf("save session %d: %v", chatID, err)
	}
}

// ── tests ───────────────────────────────────────────────────────

func TestVoiceCommand(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	seed(t, a, 7) // skip greeting

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "/voice b"})
	if v := loadSession(t, a, 7).Voice; v != "b" {
		t.Fatalf("voice = %q, want b", v)
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
	seed(t, a, 7)

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
	seed(t, a, 7)

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
	sess := loadSession(t, a, 7)
	sess.Slots.DocType = strptr("диплом")
	save(t, a, 7, sess)

	a.handleUpdate(ctx, telegram.Update{ChatID: 7, Text: "/reset"})
	if loadSession(t, a, 7).Slots.DocType != nil {
		t.Fatal("/reset should drop the session's slots")
	}

	// first-contact is derived from sessionStore.Load's found flag, so a
	// deleted session (via /reset) is indistinguishable from a chat that
	// never wrote — the greeting correctly replays.
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
	if greetings() != before+1 {
		t.Fatalf("greeting count after /reset+message = %d, want %d (should replay once)", greetings(), before+1)
	}
}

// A transient Load failure must not clobber whatever is actually on disk:
// handleUpdate falls back to a blank in-memory session for that one turn,
// but the deferred Save must be skipped so a real persisted row survives.
func TestSessionLoadErrorSkipsSave(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	sess := seed(t, a, 7)
	sess.Slots.DocType = strptr("диплом")
	save(t, a, 7, sess)

	faulty := &faultySessions{sessionStore: a.sessions, failNextLoad: true}
	a.sessions = faulty

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "hi"})

	if faulty.failNextLoad {
		t.Fatal("faultySessions.Load was never called")
	}
	if got := loadSession(t, a, 7).Slots.DocType; got == nil || *got != "диплом" {
		t.Fatalf("a failed Load must not overwrite the persisted session; DocType = %v", got)
	}
	if len(tg.sentTo(7)) == 0 {
		t.Fatal("the turn should still get a best-effort reply despite the Load error")
	}
}

// /reset must not claim success (or skip the deferred Save) when Delete
// itself failed — otherwise the user is told "cleared" while the old
// session row is untouched on disk.
func TestResetDeleteFailureKeepsSessionAndReportsError(t *testing.T) {
	a, tg := newTestApp(t, &fakeGen{})
	sess := seed(t, a, 7)
	sess.Slots.DocType = strptr("диплом")
	save(t, a, 7, sess)

	faulty := &faultySessions{sessionStore: a.sessions, failNextDelete: true}
	a.sessions = faulty

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "/reset"})

	if got := loadSession(t, a, 7).Slots.DocType; got == nil || *got != "диплом" {
		t.Fatalf("a failed Delete must leave the session as it was; DocType = %v", got)
	}
	last := tg.sentTo(7)
	if len(last) == 0 || contains(last[len(last)-1], "очищено") {
		t.Fatalf("want an error reply, not the success line, got %q", last)
	}
}

func strptr(s string) *string { return &s }

// newMultiTopicTestApp builds an app with two distinct topic bundles — own
// KB, persona and greeting each — to exercise the picker/callback flow.
// newTestApp's single-topic app never shows a picker, so these tests need
// their own setup.
func newMultiTopicTestApp(t *testing.T, gen dialog.Generator) (*app, *fakeTG) {
	t.Helper()
	a, tg := newTestApp(t, gen)
	a.topics = map[string]topicBundle{
		"translations": {ID: "translations", Title: "Переклад", KB: []kb.Section{{Title: "T", Body: "translation"}}, Sys: "SYS-T", Greeting: "GREETING-T"},
		"notary":       {ID: "notary", Title: "Нотаріус", KB: []kb.Section{{Title: "N", Body: "notary"}}, Sys: "SYS-N", Greeting: "GREETING-N"},
	}
	a.topicIDs = []string{"translations", "notary"}
	return a, tg
}

// Feature 3 — multi-topic picker + inline-keyboard callback.

func TestFirstContactShowsPickerInMultiTopicMode(t *testing.T) {
	a, tg := newMultiTopicTestApp(t, &fakeGen{})

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, IsStart: true})

	if len(tg.buttons) != 1 {
		t.Fatalf("SendButtons called %d times, want 1", len(tg.buttons))
	}
	if got := tg.buttons[0]; len(got) != 2 || got[0].Data != "topic:translations" || got[1].Data != "topic:notary" {
		t.Fatalf("buttons = %+v, want one per topic in a.topicIDs order", got)
	}
	if loadSession(t, a, 7).Topic != "" {
		t.Fatal("Topic should stay empty until a button is tapped")
	}
}

// A brand-new chat's very first message (not /start) in multi-topic mode
// must show the picker exactly once, not once from the first-contact path
// and again from the sess.Topic=="" gate.
func TestFirstContactNonStartShowsPickerOnce(t *testing.T) {
	gen := &fakeGen{}
	a, tg := newMultiTopicTestApp(t, gen)

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "Привіт"})

	if len(tg.buttons) != 1 {
		t.Fatalf("SendButtons called %d times, want 1", len(tg.buttons))
	}
	if len(gen.seen) != 0 {
		t.Fatalf("dialog.Handle must not run before a topic is chosen: %v", gen.seen)
	}
}

func TestTopicCallbackSetsTopicAndGreets(t *testing.T) {
	a, tg := newMultiTopicTestApp(t, &fakeGen{})
	seed(t, a, 7) // already past first contact — picker already shown once

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, CallbackID: "cb1", CallbackData: "topic:notary"})

	if got := loadSession(t, a, 7).Topic; got != "notary" {
		t.Fatalf("Topic = %q, want notary", got)
	}
	if len(tg.acked) != 1 || tg.acked[0] != "cb1" {
		t.Fatalf("AnswerCallback calls = %v, want [cb1]", tg.acked)
	}
	if last := tg.sentTo(7); len(last) == 0 || last[len(last)-1] != "GREETING-N" {
		t.Fatalf("want the notary topic's greeting, got %q", last)
	}
}

func TestTopicCallbackUnknownIDStillAcks(t *testing.T) {
	a, tg := newMultiTopicTestApp(t, &fakeGen{})
	seed(t, a, 7)

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, CallbackID: "cb2", CallbackData: "topic:does-not-exist"})

	if len(tg.acked) != 1 || tg.acked[0] != "cb2" {
		t.Fatalf("an unknown topic id must still ack the callback: %v", tg.acked)
	}
	if loadSession(t, a, 7).Topic != "" {
		t.Fatal("an unknown topic id must not set Topic")
	}
	if len(tg.sentTo(7)) != 0 {
		t.Fatalf("no greeting should be sent for an unknown topic id: %q", tg.sentTo(7))
	}
}

func TestTextBeforeTopicChosenRepromptsPicker(t *testing.T) {
	gen := &fakeGen{}
	a, tg := newMultiTopicTestApp(t, gen)
	seed(t, a, 7) // past first contact, but no topic picked yet

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "Скільки коштує?"})

	if len(gen.seen) != 0 {
		t.Fatalf("dialog.Handle must not run before a topic is chosen: %v", gen.seen)
	}
	if len(tg.buttons) != 1 {
		t.Fatalf("SendButtons called %d times, want 1 (re-prompt)", len(tg.buttons))
	}
}

func TestVoiceCommandStillWorksBeforeTopicChosen(t *testing.T) {
	a, _ := newMultiTopicTestApp(t, &fakeGen{})
	seed(t, a, 7)

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "/voice b"})

	if v := loadSession(t, a, 7).Voice; v != "b" {
		t.Fatalf("voice = %q, want b — slash commands must not be gated by topic choice", v)
	}
}

// A callback payload that doesn't carry the "topic:" prefix must never be
// treated as a topic pick — even if, after stripping nothing, it happens to
// coincidentally match an existing topic id.
func TestTopicCallbackWithoutPrefixIsRejected(t *testing.T) {
	a, tg := newMultiTopicTestApp(t, &fakeGen{})
	seed(t, a, 7)

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, CallbackID: "cb3", CallbackData: "notary"})

	if got := loadSession(t, a, 7).Topic; got != "" {
		t.Fatalf("Topic = %q, want empty — \"notary\" has no \"topic:\" prefix", got)
	}
	if len(tg.acked) != 1 || tg.acked[0] != "cb3" {
		t.Fatalf("the callback must still be acked: %v", tg.acked)
	}
	if len(tg.sentTo(7)) != 0 {
		t.Fatalf("no greeting should be sent: %q", tg.sentTo(7))
	}
}

// Picking a topic is a fresh start for that assistant: any slots/history/
// escalation left over from a previous topic (or from before any topic was
// picked) must not bleed into the new one. Voice is a per-chat preference,
// not part of a topic's conversation, so it survives.
func TestTopicSwitchResetsConversationState(t *testing.T) {
	a, _ := newMultiTopicTestApp(t, &fakeGen{})
	sess := seed(t, a, 7)
	sess.Topic = "translations"
	sess.Voice = "b"
	sess.Slots.DocType = strptr("диплом")
	sess.History = []dialog.Msg{{Role: "user", Text: "hi"}, {Role: "assistant", Text: "hello"}}
	sess.Escalated = true
	sess.LeadDone = true
	sess.LeadSlots = `{"doc_type":"диплом"}`
	sess.GateStrike = true
	save(t, a, 7, sess)

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, CallbackID: "cb4", CallbackData: "topic:notary"})

	got := loadSession(t, a, 7)
	if got.Topic != "notary" {
		t.Fatalf("Topic = %q, want notary", got.Topic)
	}
	if got.Voice != "b" {
		t.Fatalf("Voice = %q, want b (a per-chat preference, not part of the conversation)", got.Voice)
	}
	if got.Slots.DocType != nil || len(got.History) != 0 || got.Escalated || got.LeadDone ||
		got.LeadSlots != "" || got.GateStrike {
		t.Fatalf("switching topics should reset the conversation state, got %+v", got)
	}
}

// A session's persisted Topic id can go stale if topics.json drops that
// entry across a restart. handleUpdate must not run dialog.Handle with the
// resulting zero-value topicBundle (empty KB/system prompt) — it re-shows
// the picker instead, same as Topic=="".
func TestStaleTopicIDRepromptsPickerInsteadOfEmptyBundle(t *testing.T) {
	gen := &fakeGen{}
	a, tg := newMultiTopicTestApp(t, gen)
	sess := seed(t, a, 7)
	sess.Topic = "ghost" // not in a.topics
	save(t, a, 7, sess)

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 7, Text: "Скільки коштує?"})

	if len(gen.seen) != 0 {
		t.Fatalf("dialog.Handle must not run with a stale topic id: %v", gen.seen)
	}
	if len(tg.buttons) != 1 {
		t.Fatalf("SendButtons called %d times, want 1 (re-prompt)", len(tg.buttons))
	}
	if got := loadSession(t, a, 7).Topic; got != "" {
		t.Fatalf("the stale Topic id should be cleared, got %q", got)
	}
}

func TestUnknownCommandNotADialogueTurn(t *testing.T) {
	gen := &fakeGen{}
	a, _ := newTestApp(t, gen)
	seed(t, a, 7)
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
	seed(t, a, 7)

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
	seed(t, a, 9)
	ctx := context.Background()

	// chat 3 is mid-quote — 4 of 6 slots filled
	s3 := seed(t, a, 3)
	s3.Slots.LanguagePair = strptr("uk->pl")
	s3.Slots.DocType = strptr("диплом")
	s3.Slots.Volume = strptr("2 pages")
	s3.Slots.Deadline = strptr("Friday")
	save(t, a, 3, s3)

	// chat 9 sends a refund demand -> hard escalate
	a.handleUpdate(ctx, telegram.Update{ChatID: 9, Text: "Поверніть гроші за переклад, це скарга"})

	if !loadSession(t, a, 9).Escalated {
		t.Fatal("chat 9 should be escalated")
	}
	if loadSession(t, a, 3).Escalated {
		t.Fatal("chat 9's escalation leaked into chat 3")
	}
	if got := loadSession(t, a, 3).Slots; got.LanguagePair == nil || got.DocType == nil ||
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
	a.topics[defaultTopicID] = topicBundle{ID: defaultTopicID, Title: "Default", KB: []kb.Section{{Title: "Ціни", Body: "переклад диплома вартість сторінка"}}, Sys: "SYS", Greeting: "GREETING"}
	seed(t, a, 5)
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
	a.topics[defaultTopicID] = topicBundle{ID: defaultTopicID, Title: "Default", KB: []kb.Section{{Title: "Ціни", Body: "переклад диплома вартість сторінка"}}, Sys: "SYS", Greeting: "GREETING"}
	seed(t, a, 5)

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
	seed(t, a, 5)

	a.handleUpdate(context.Background(), telegram.Update{ChatID: 5, Text: "    "})

	if len(gen.seen) != 0 {
		t.Fatalf("generator called with blank input: %q", gen.seen)
	}
}

// 16i — degenerate short inputs: no crash, each gets some reply.
func TestDegenerateShortInputsNoCrash(t *testing.T) {
	gen := &fakeGen{}
	a, tg := newTestApp(t, gen)
	seed(t, a, 5)
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
	a.topics[defaultTopicID] = topicBundle{ID: defaultTopicID, Title: "Default", KB: []kb.Section{{Title: "Ціни", Body: "переклад диплома вартість сторінка"}}, Sys: "SYS", Greeting: "GREETING"}
	seed(t, a, 7)
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
