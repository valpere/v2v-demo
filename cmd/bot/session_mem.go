package main

import (
	"sync"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/store"
)

// memSessions is the default sessionStore — in-memory, lost on restart
// (SESSION_STORE=memory). It stores the same JSON encoding SQLiteSessions
// does (store.EncodeSession/DecodeSession) rather than a live *Session
// pointer, so it has the same copy-semantics — see sessionStore's doc
// comment for why that matters.
type memSessions struct {
	mu   sync.Mutex
	rows map[int64][]byte
}

func newMemSessions() *memSessions {
	return &memSessions{rows: make(map[int64][]byte)}
}

func (m *memSessions) Load(chatID int64) (*dialog.Session, bool, error) {
	m.mu.Lock()
	data, ok := m.rows[chatID]
	m.mu.Unlock()
	if !ok {
		return nil, false, nil
	}
	sess, err := store.DecodeSession(data)
	if err != nil {
		return nil, false, nil // corrupt row (shouldn't happen in-process) — start fresh
	}
	return sess, true, nil
}

func (m *memSessions) Save(chatID int64, sess *dialog.Session) error {
	data, err := store.EncodeSession(sess)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.rows[chatID] = data
	m.mu.Unlock()
	return nil
}

func (m *memSessions) Delete(chatID int64) error {
	m.mu.Lock()
	delete(m.rows, chatID)
	m.mu.Unlock()
	return nil
}

func (m *memSessions) Close() error { return nil }
