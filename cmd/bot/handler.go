package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/store"
	"github.com/valpere/v2v-demo/internal/telegram"
)

// chatLock returns the per-chat mutex, creating it under a.mu. Holding it
// serialises one chat's turns while different chats run concurrently (B5).
func (a *app) chatLock(id int64) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	l := a.chatLocks[id]
	if l == nil {
		l = &sync.Mutex{}
		a.chatLocks[id] = l
	}
	return l
}

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

func (a *app) send(ctx context.Context, chatID int64, text string) {
	if err := a.tg.SendText(ctx, chatID, text); err != nil {
		log.Printf("send (chat %d): %v", chatID, err)
	}
}

// handleUpdate processes one inbound update. A panic here is recovered and
// logged — it never stops the loop (REQ-NFR-03).
func (a *app) handleUpdate(ctx context.Context, u telegram.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered panic (chat %d): %v", u.ChatID, r)
		}
	}()

	lock := a.chatLock(u.ChatID)
	lock.Lock()
	defer lock.Unlock()

	sess := a.session(u.ChatID)

	if u.VoiceFileID != "" {
		// transcription lands in build-order step 4
		a.send(ctx, u.ChatID, "(voice received — transcription lands in a later build step)")
		return
	}

	text := u.Text
	start := time.Now()

	reply, _ := dialog.Handle(ctx, sess, a.kb, a.gen, a.sys, text) // never returns a non-nil error
	a.send(ctx, u.ChatID, reply.Text)

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
