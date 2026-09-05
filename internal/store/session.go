package store

import (
	"encoding/json"
	"fmt"

	"github.com/valpere/v2v-demo/internal/dialog"
)

// sessionSchemaVersion guards the on-disk/DB shape of a stored session. Bump
// it (and add a migration in DecodeSession) if Session's fields ever change
// incompatibly; today an old/unknown version is just treated as "not found"
// — losing a demo session is cheap, silently misreading one is not.
//
// v2 (2026-09-06): Session.Slots went from the fixed QuoteSlots struct to a
// per-topic map[string]string.
const sessionSchemaVersion = 2

type sessionRow struct {
	Version int            `json:"version"`
	Session dialog.Session `json:"session"`
}

// EncodeSession serializes sess for storage. Used by both session stores
// (memory and SQLite) so switching SESSION_STORE round-trips identically.
func EncodeSession(sess *dialog.Session) ([]byte, error) {
	b, err := json.Marshal(sessionRow{Version: sessionSchemaVersion, Session: *sess})
	if err != nil {
		return nil, fmt.Errorf("store: encode session: %w", err)
	}
	return b, nil
}

// DecodeSession deserializes a row written by EncodeSession. Malformed JSON
// or an unexpected schema version is an error — the caller treats that the
// same as "not found" and starts the chat fresh rather than risk serving a
// half-decoded session.
func DecodeSession(data []byte) (*dialog.Session, error) {
	var row sessionRow
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, fmt.Errorf("store: decode session: %w", err)
	}
	if row.Version != sessionSchemaVersion {
		return nil, fmt.Errorf("store: session schema version %d, want %d", row.Version, sessionSchemaVersion)
	}
	sess := row.Session
	return &sess, nil
}
