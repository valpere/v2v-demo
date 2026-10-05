package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

// pickerIntro sits above the topic buttons — a bilingual set of links to
// Val's site (this bot is a portfolio / pitch artifact), one resource per
// row (UK · EN), then the prompt. Sent with HTML parse mode by SendButtons.
const pickerIntro = `<a href="https://valpere.github.io/uk/portfolio/">Мій портфоліо</a> · <a href="https://valpere.github.io/portfolio/">My Portfolio</a>
<a href="https://valpere.github.io/uk/projects/">Мої проєкти</a> · <a href="https://valpere.github.io/projects/">My Projects</a>
<a href="https://valpere.github.io/uk/mvb/">Мої брифи</a> · <a href="https://valpere.github.io/mvb/">My Briefs</a>
<a href="https://valpere.github.io/uk/about/">Про мене</a> · <a href="https://valpere.github.io/about/">About Me</a>

Оберіть тему розмови · Choose a topic:`

// sendTopicPicker shows one inline button per configured topic, in
// a.topicIDs order (map iteration isn't stable). The picker is the first
// interaction — before any language is known — so both the prompt and the
// button labels are bilingual.
func (a *app) sendTopicPicker(ctx context.Context, chatID int64) {
	buttons := make([]telegram.Button, len(a.topicIDs))
	for i, id := range a.topicIDs {
		buttons[i] = telegram.Button{Label: topicButtonLabel(a.topics[id]), Data: topicCallbackPrefix + id}
	}
	if err := a.tg.SendButtons(ctx, chatID, pickerIntro, buttons); err != nil {
		log.Printf("send topic picker (chat %d): %v", chatID, err)
	}
}

// topicButtonLabel is "<Title> · <TitleEN>" when a topic has an English
// label, else just the Title.
func topicButtonLabel(t topicBundle) string {
	if t.TitleEN != "" && t.TitleEN != t.Title {
		return t.Title + " · " + t.TitleEN
	}
	return t.Title
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

	if !a.admit(ctx, u) {
		return
	}

	// work bounds the slow backends (STT, LLM, TTS); replies, saves and the
	// apology still go out on the root ctx after it expires.
	work, cancelWork := a.turnContext(ctx)
	defer cancelWork()

	text, ok := a.resolveText(work, ctx, sess, u)
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
		stopTicker = a.startRecordingTicker(work, u.ChatID)
	}
	defer stopTicker() // idempotent; also covers a panic between here and the explicit stop below
	prevLeadDone, prevLeadSlots := sess.LeadDone, sess.LeadSlots
	reply, _ := dialog.Handle(work, sess, topic.Spec, a.gen, text, time.Now().In(a.loc)) // never returns a non-nil error

	if a.tts != nil && (a.cfg.SpeakMaxChars <= 0 || utf8.RuneCountInString(reply.Text) <= a.cfg.SpeakMaxChars) {
		speakCtx := work
		if reply.Fixed {
			speakCtx = tts.Cacheable(work) // canned lines only; see tts.CachedSynth
		}
		ogg, terr := a.tts.Speak(speakCtx, tts.Spoken(reply.Text, sess.Lang), voiceID(a.cfg, sess.Voice), sess.Lang)
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
			// not recorded -> not done: a repeat lead_ready must be able to record it
			sess.LeadDone, sess.LeadSlots = prevLeadDone, prevLeadSlots
		}
	}
}

const (
	throttleLine = "Забагато повідомлень — зачекайте хвилинку, будь ласка. / Too many messages — please wait a minute."
	dayCapLine   = "На сьогодні ліміт розмов вичерпано — спробуйте, будь ласка, завтра. / Today's conversation limit has been reached — please try again tomorrow."
)

// admit applies the spend limits to a message that would start a turn. It
// replies (once, via the limiter) and returns false when the message must not
// be processed. Slash commands are free; they never reach a paid backend.
func (a *app) admit(ctx context.Context, u telegram.Update) bool {
	if u.VoiceFileID == "" && strings.HasPrefix(strings.TrimSpace(u.Text), "/") {
		return true
	}
	if max := a.cfg.MaxTextChars; max > 0 && utf8.RuneCountInString(u.Text) > max {
		a.send(ctx, u.ChatID, fmt.Sprintf("Повідомлення задовге — скоротіть його, будь ласка (до %d символів). / Message too long — please shorten it (up to %d characters).", max, max))
		return false
	}
	if u.VoiceFileID != "" && a.voiceTooBig(u) {
		a.send(ctx, u.ChatID, fmt.Sprintf("Голосове задовге або завелике — надішліть, будь ласка, коротше (до %d с). / That voice message is too long or too large — please send a shorter one (up to %d s).", a.cfg.VoiceMaxSeconds, a.cfg.VoiceMaxSeconds))
		return false
	}
	if a.lim == nil {
		return true
	}
	switch a.lim.admit(u.ChatID) {
	case admitThrottleNotify:
		a.send(ctx, u.ChatID, throttleLine)
		return false
	case admitDayCapNotify:
		a.send(ctx, u.ChatID, dayCapLine)
		return false
	case admitDrop:
		return false
	}
	return true
}

// voiceTooBig reports a voice note over the configured duration/size caps,
// judged from the update's metadata before anything is downloaded.
func (a *app) voiceTooBig(u telegram.Update) bool {
	return (a.cfg.VoiceMaxSeconds > 0 && u.VoiceSeconds > a.cfg.VoiceMaxSeconds) ||
		(a.cfg.VoiceMaxBytes > 0 && u.VoiceBytes > int64(a.cfg.VoiceMaxBytes))
}

// turnContext derives the budgeted context for one turn's slow work.
func (a *app) turnContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if a.cfg.TurnTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, a.cfg.TurnTimeout)
}

// resolveText yields the turn's text: Update.Text for a text message, or the
// STT transcript for a voice message. On an STT failure or an empty
// transcript it replies with the fixed sttFail line and returns ok=false —
// no dialogue turn, no turn record (REQ-BOT-03).
func (a *app) resolveText(work, ctx context.Context, sess *dialog.Session, u telegram.Update) (string, bool) {
	if u.VoiceFileID == "" {
		return u.Text, true
	}
	if a.stt == nil { // STT_BACKEND=none — never attempt a download/transcribe
		a.send(ctx, u.ChatID, dialog.VoiceUnavailableLine(sess))
		return "", false
	}

	stop := a.startRecordingTicker(work, u.ChatID)
	defer stop()

	// pin STT to the conversation's language once it's known (lingua-detected
	// from earlier turns). On first contact the hint is empty and each backend
	// applies its own configured default (WHISPER_LANG / WHISPER_CPP_LANG).
	langHint := sess.Lang

	ogg, err := a.tg.DownloadVoice(work, u.VoiceFileID)
	var text string
	if err == nil {
		defer os.Remove(ogg) // also on a panic inside Transcribe
		text, err = a.stt.Transcribe(work, ogg, langHint)
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
