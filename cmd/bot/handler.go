package main

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/store"
	"github.com/valpere/v2v-demo/internal/telegram"
	"github.com/valpere/v2v-demo/internal/tts"
)

// RecordingTick is how often the "recording voice" chat action is re-sent
// while a turn runs (server-side it lasts ~5s). See @schema GateParams.
const RecordingTick = 4 * time.Second

func (a *app) session(id int64) *dialog.Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[id]
	if s == nil {
		s = &dialog.Session{Voice: "a"}
		a.sessions[id] = s
	}
	return s
}

// firstContact marks the chat seen and reports whether this is the first time
// (REQ-UX-02 — greet once per chat).
func (a *app) firstContact(id int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen[id] {
		return false
	}
	a.seen[id] = true
	return true
}

func (a *app) send(ctx context.Context, chatID int64, text string) {
	if err := a.tg.SendText(ctx, chatID, text); err != nil {
		log.Printf("send (chat %d): %v", chatID, err)
	}
}

// startRecordingTicker shows the "recording voice" action immediately and
// re-sends it every RecordingTick until the returned stop() is called or ctx
// is cancelled.
func (a *app) startRecordingTicker(ctx context.Context, chatID int64) (stop func()) {
	action := func() {
		if err := a.tg.SendRecordingAction(ctx, chatID); err != nil {
			log.Printf("recording action (chat %d): %v", chatID, err)
		}
	}
	action()

	done := make(chan struct{})
	go func() {
		t := time.NewTicker(RecordingTick)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				action()
			}
		}
	}()

	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// handleUpdate processes one inbound update. The per-chat worker calls this
// serially, so no locking is needed for sess. A panic is recovered and
// logged — it never stops the worker (REQ-NFR-03).
func (a *app) handleUpdate(ctx context.Context, u telegram.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered panic (chat %d): %v", u.ChatID, r)
		}
	}()

	sess := a.session(u.ChatID)

	if first := a.firstContact(u.ChatID); u.IsStart || first {
		a.send(ctx, u.ChatID, a.greeting)
		if u.IsStart {
			return // /start carries no other content
		}
	}

	text, ok := a.resolveText(ctx, sess, u)
	if !ok {
		return
	}

	// slash commands are handled here, never reach dialog.Handle
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		a.handleCommand(ctx, sess, u.ChatID, text)
		return
	}

	start := time.Now()
	stop := a.startRecordingTicker(ctx, u.ChatID)
	reply, _ := dialog.Handle(ctx, sess, a.kb, a.gen, a.sys, text) // never returns a non-nil error
	ogg, terr := a.tts.Speak(ctx, tts.Spoken(reply.Text, sess.Lang), voiceID(a.cfg, sess.Voice), sess.Lang)
	stop()

	if terr != nil {
		log.Printf("tts (chat %d): %v", u.ChatID, terr)
	} else if err := a.tg.SendVoice(ctx, u.ChatID, ogg); err != nil {
		log.Printf("send voice (chat %d): %v", u.ChatID, err)
	}
	a.send(ctx, u.ChatID, reply.Text) // text always goes out once

	if err := store.AppendTurn(a.cfg.DataDir, store.TurnRecord{
		Time:      start,
		ChatID:    u.ChatID,
		UserText:  text,
		ReplyText: reply.Text,
		Signal:    string(reply.Signal),
		Matched:   reply.Matched,
		Slots:     sess.Slots,
		LatencyMS: time.Since(start).Milliseconds(),
	}); err != nil {
		log.Printf("store turn (chat %d): %v", u.ChatID, err)
	}

	if reply.Signal == dialog.SignalLeadReady {
		if err := store.AppendLead(a.cfg.DataDir, leadFrom(u.ChatID, sess.Slots)); err != nil {
			log.Printf("store lead (chat %d): %v", u.ChatID, err)
		}
	}
}

// resolveText yields the turn's text: Update.Text for a text message, or the
// STT transcript for a voice message. On an STT failure or an empty
// transcript it replies with the fixed sttFail line and returns ok=false —
// no dialogue turn, no turn record (REQ-BOT-03).
func (a *app) resolveText(ctx context.Context, sess *dialog.Session, u telegram.Update) (string, bool) {
	if u.VoiceFileID == "" {
		return u.Text, true
	}

	stop := a.startRecordingTicker(ctx, u.ChatID)
	defer stop()

	// pin STT to the conversation's language once it's known (lingua-detected
	// from earlier turns); fall back to the config default on first contact.
	langHint := sess.Lang
	if langHint == "" {
		langHint = a.cfg.WhisperLang
	}

	ogg, err := a.tg.DownloadVoice(ctx, u.VoiceFileID)
	var text string
	if err == nil {
		text, err = a.stt.Transcribe(ctx, ogg, langHint)
		os.Remove(ogg)
	}
	if err != nil || strings.TrimSpace(text) == "" {
		if err != nil {
			log.Printf("stt (chat %d): %v", u.ChatID, err)
		}
		a.send(ctx, u.ChatID, dialog.SttFailLine(sess))
		return "", false
	}
	return text, true
}

// handleCommand handles /voice (and swallows any other slash command with a
// short hint, so a stray "/foo" never becomes a dialogue turn).
func (a *app) handleCommand(ctx context.Context, sess *dialog.Session, chatID int64, text string) {
	switch cmd := strings.ToLower(strings.TrimSpace(text)); cmd {
	case "/voice a", "/voice b":
		sess.Voice = cmd[len(cmd)-1:]
		a.send(ctx, chatID, dialog.VoiceSwitchedLine(sess))
	default: // "/voice", "/start" after greeting, "/help", anything unknown
		a.send(ctx, chatID, voiceHelp(sess))
	}
}

func voiceHelp(sess *dialog.Session) string {
	cur := "A"
	if sess.Voice == "b" {
		cur = "B"
	}
	if sess.Lang == "en" {
		return "Voice commands: /voice a, /voice b (current: " + cur + ")."
	}
	return "Команди голосу: /voice a, /voice b (зараз: " + cur + ")."
}

func leadFrom(chatID int64, s dialog.QuoteSlots) store.LeadRecord {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return store.LeadRecord{
		Time:          time.Now(),
		ChatID:        chatID,
		LanguagePair:  deref(s.LanguagePair),
		DocType:       deref(s.DocType),
		Volume:        deref(s.Volume),
		Deadline:      deref(s.Deadline),
		Certification: deref(s.Certification),
		Delivery:      deref(s.Delivery),
	}
}
