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
	"github.com/valpere/v2v-demo/internal/stt"
	"github.com/valpere/v2v-demo/internal/telegram"
	"github.com/valpere/v2v-demo/internal/tts"
)

// RecordingTick is how often the "recording voice" chat action is re-sent
// while a turn runs (server-side it lasts ~5s). See @schema GateParams.
const RecordingTick = 4 * time.Second

// topicCallbackPrefix is the inline-keyboard payload prefix for a topic
// pick: "topic:<id>".
const topicCallbackPrefix = "topic:"

// sendTopicPicker shows one inline button per configured topic, in
// a.topicIDs order (map iteration isn't stable).
func (a *app) sendTopicPicker(ctx context.Context, chatID int64) {
	buttons := make([]telegram.Button, len(a.topicIDs))
	for i, id := range a.topicIDs {
		buttons[i] = telegram.Button{Label: a.topics[id].Title, Data: topicCallbackPrefix + id}
	}
	text := "Оберіть тему розмови:"
	if err := a.tg.SendButtons(ctx, chatID, text, buttons); err != nil {
		log.Printf("send topic picker (chat %d): %v", chatID, err)
	}
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
//
// Session state has exactly one load point and one save point: Load here,
// and the deferred Save below (skipped when the turn deleted the session,
// e.g. /reset, or when Load itself failed — see skipSave below). Every
// mutation in between — slot fills, /voice, the gate strike, etc. — relies
// on that deferred Save to persist; nothing else in this file calls
// a.sessions.Save directly. See sessionStore's doc comment in sessions.go
// for why that discipline matters.
func (a *app) handleUpdate(ctx context.Context, u telegram.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered panic (chat %d): %v", u.ChatID, r)
		}
	}()

	sess, found, loadErr := a.sessions.Load(u.ChatID)
	if loadErr != nil {
		log.Printf("session load (chat %d): %v", u.ChatID, loadErr)
	}
	if sess == nil {
		sess = &dialog.Session{Voice: "a"}
	}

	// skipSave covers two cases: the turn deleted the session (/reset — a
	// deliberate skip) and a failed Load (an involuntary one — sess above is
	// a blank stand-in, not a decoded row, so saving it would clobber
	// whatever is actually on disk with an empty session the moment the
	// store recovers).
	skipSave := loadErr != nil
	defer func() {
		if skipSave {
			return
		}
		if err := a.sessions.Save(u.ChatID, sess); err != nil {
			log.Printf("session save (chat %d): %v", u.ChatID, err)
		}
	}()

	// an inline-keyboard tap always resolves before anything else — it may
	// be the very first message this chat ever sends (a picker button
	// tapped right after /start's picker went out).
	if u.CallbackID != "" {
		id, isTopicPick := strings.CutPrefix(u.CallbackData, topicCallbackPrefix)
		topic, valid := a.topics[id]
		valid = valid && isTopicPick
		if valid {
			resetConversationState(sess) // a topic switch starts that assistant fresh
			sess.Topic = id              // the deferred Save persists it
		} else {
			log.Printf("unknown topic callback (chat %d): %q", u.ChatID, u.CallbackData)
		}
		if err := a.tg.AnswerCallback(ctx, u.CallbackID); err != nil {
			log.Printf("answer callback (chat %d): %v", u.ChatID, err)
		}
		if valid {
			a.send(ctx, u.ChatID, topic.Greeting)
		}
		return
	}

	if isFirst := !found; u.IsStart || isFirst {
		if len(a.topics) > 1 {
			// can't answer this message without a topic pick — the later
			// sess.Topic=="" gate would just re-show the same picker, so
			// stop here instead of showing it twice for one update.
			a.sendTopicPicker(ctx, u.ChatID)
			return
		}
		for id, t := range a.topics { // exactly one entry — auto-assign, no picker
			sess.Topic = id
			a.send(ctx, u.ChatID, t.Greeting)
		}
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
		if a.handleCommand(ctx, sess, u.ChatID, text) {
			skipSave = true
		}
		return
	}

	// topic, ok is re-checked (not just sess.Topic == "") because a
	// persisted Topic id can go stale — e.g. topics.json lost that entry
	// across a restart — and a zero-value topicBundle would otherwise run
	// dialog.Handle with an empty KB and system prompt.
	topic, ok := a.topics[sess.Topic]
	if !ok {
		sess.Topic = ""
		if len(a.topics) > 1 {
			// a message arrived before a topic was picked (e.g. the
			// picker's message was dismissed, or its id no longer exists)
			// — re-show it rather than guessing which assistant should answer.
			a.sendTopicPicker(ctx, u.ChatID)
			return
		}
		// single-topic mode: silently auto-assign the one topic. Reached
		// whenever a session's Topic wasn't set by the first-contact path
		// above (e.g. a pre-existing session from before this feature).
		for id, t := range a.topics {
			sess.Topic = id
			topic = t
		}
	}

	start := time.Now()
	// TTS_BACKEND=none → text-only: no synthesizer, no "recording voice" action.
	stopTicker := func() {}
	if a.tts != nil {
		stopTicker = a.startRecordingTicker(ctx, u.ChatID)
	}
	reply, _ := dialog.Handle(ctx, sess, topic.Spec, a.gen, text, time.Now().In(a.loc)) // never returns a non-nil error

	if a.tts != nil {
		ogg, terr := a.tts.Speak(ctx, tts.Spoken(reply.Text, sess.Lang), voiceID(a.cfg, sess.Voice), sess.Lang)
		if terr != nil {
			log.Printf("tts (chat %d): %v", u.ChatID, terr)
		} else if err := a.tg.SendVoice(ctx, u.ChatID, ogg); err != nil {
			log.Printf("send voice (chat %d): %v", u.ChatID, err)
		}
	}
	stopTicker()

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
		if err := store.AppendLead(a.cfg.DataDir, leadFrom(u.ChatID, sess.Topic, sess.Slots)); err != nil {
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
	if a.stt == nil { // STT_BACKEND=none — never attempt a download/transcribe
		a.send(ctx, u.ChatID, dialog.VoiceUnavailableLine(sess))
		return "", false
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
	if err != nil || stt.IsNonSpeech(text) {
		if err != nil {
			log.Printf("stt (chat %d): %v", u.ChatID, err)
		} else if strings.TrimSpace(text) != "" {
			log.Printf("stt (chat %d): non-speech transcript %q, treating as no speech", u.ChatID, text)
		}
		a.send(ctx, u.ChatID, dialog.SttFailLine(sess))
		return "", false
	}
	return text, true
}

// handleCommand handles /voice and /reset (and swallows any other slash
// command with a short hint, so a stray "/foo" never becomes a dialogue
// turn). It reports whether the session was deleted, so handleUpdate's
// deferred Save can skip re-persisting a session that no longer exists —
// true only when /reset's Delete actually succeeded.
func (a *app) handleCommand(ctx context.Context, sess *dialog.Session, chatID int64, text string) bool {
	switch cmd := strings.ToLower(strings.TrimSpace(text)); cmd {
	case "/voice a", "/voice b":
		sess.Voice = cmd[len(cmd)-1:]
		a.send(ctx, chatID, dialog.VoiceSwitchedLine(sess))
	case "/reset", "/clean":
		// drop this chat's session (slots, history, language, escalated
		// flag) — a smoke-test aid. First-contact is now derived from
		// whether Load finds a row, so /reset also makes the next message
		// replay the greeting (unlike the old in-memory-only "seen" map).
		if err := a.sessions.Delete(chatID); err != nil {
			log.Printf("session delete (chat %d): %v", chatID, err)
			a.send(ctx, chatID, "Не вдалося очистити сесію, спробуйте ще раз. / Could not clear the session, please try again.")
			return false // Delete failed — the deferred Save keeps the session as it was
		}
		a.send(ctx, chatID, "Сесію очищено. / Session cleared.")
		return true
	default: // "/voice", "/start" after greeting, "/help", anything unknown
		a.send(ctx, chatID, voiceHelp(sess))
	}
	return false
}

// resetConversationState clears everything that belongs to one topic's
// conversation (slots, history, lead/escalation flags) when a callback picks
// a topic — each topic is a genuinely different assistant, so switching
// (or first-picking) one must not carry a quote-in-progress or an
// escalated flag from whatever came before. Voice and Lang are left alone —
// they're a per-chat preference/detection, not part of any one topic's
// conversation.
func resetConversationState(sess *dialog.Session) {
	sess.Slots = nil
	sess.History = nil
	sess.Escalated = false
	sess.LeadDone = false
	sess.LeadSlots = ""
	sess.GateStrike = false
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

func leadFrom(chatID int64, topic string, fields map[string]string) store.LeadRecord {
	return store.LeadRecord{
		Time:   time.Now(),
		ChatID: chatID,
		Topic:  topic,
		Fields: fields,
	}
}
