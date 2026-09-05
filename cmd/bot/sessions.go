package main

import (
	"fmt"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/store"
)

// sessionStore is how cmd/bot persists per-chat dialog.Session state.
// Both implementations are copy-semantics — Load hands back a value decoded
// fresh from storage, never a pointer aliased to anything the store still
// holds — so a caller that mutates the returned *Session and forgets to
// Save loses the change under *either* backend. That symmetry is
// deliberate: it means the same bug is visible (and testable) in the cheap
// in-memory default, not just after switching to SESSION_STORE=sqlite.
type sessionStore interface {
	// Load returns the session for chatID, or found=false for a chat seen
	// for the first time (or whose stored row failed to decode).
	Load(chatID int64) (sess *dialog.Session, found bool, err error)
	Save(chatID int64, sess *dialog.Session) error
	Delete(chatID int64) error
	Close() error
}

// newSessionStore builds the configured backend (SESSION_STORE).
func newSessionStore(cfg Config) (sessionStore, error) {
	switch cfg.SessionStore {
	case "memory":
		return newMemSessions(), nil
	case "sqlite":
		return store.NewSQLiteSessions(cfg.SessionDBPath)
	default:
		return nil, fmt.Errorf("unknown SESSION_STORE %q", cfg.SessionStore)
	}
}
