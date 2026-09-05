package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// SQLiteSessions persists dialog.Session state across restarts
// (SESSION_STORE=sqlite). One row per chat, the whole session as a JSON
// blob (see EncodeSession/DecodeSession) — no per-slot columns, since
// nothing ever queries by slot value.
type SQLiteSessions struct {
	db *sql.DB
}

// NewSQLiteSessions opens (creating if needed) the session database at path
// and ensures its schema. WAL + a busy-timeout are set via the DSN (modernc's
// pragma syntax — mattn's `_busy_timeout=`/`_journal_mode=` query params are
// silently ignored by this driver). SetMaxOpenConns(1): database/sql pools
// connections but does not serialize writes between them, and at this
// traffic volume serializing every access through one connection is free —
// without it, two chats saving at once can still hit SQLITE_BUSY under WAL
// (WAL removes reader/writer contention, not writer/writer).
func NewSQLiteSessions(path string) (*SQLiteSessions, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: mkdir %s: %w", dir, err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		chat_id    INTEGER PRIMARY KEY,
		data       TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: create sessions table: %w", err)
	}
	return &SQLiteSessions{db: db}, nil
}

// Load returns the stored session for chatID. found=false (nil error) means
// no row exists yet — a fresh chat. A row that fails to decode (schema drift,
// corruption) is treated the same as not-found rather than surfaced as an
// error the caller must special-case.
func (s *SQLiteSessions) Load(chatID int64) (sess *dialog.Session, found bool, err error) {
	var data string
	err = s.db.QueryRow(`SELECT data FROM sessions WHERE chat_id = ?`, chatID).Scan(&data)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("store: load session %d: %w", chatID, err)
	}
	sess, decErr := DecodeSession([]byte(data))
	if decErr != nil {
		return nil, false, nil // corrupt/old row — start fresh rather than fail the turn
	}
	return sess, true, nil
}

// Save upserts the session for chatID.
func (s *SQLiteSessions) Save(chatID int64, sess *dialog.Session) error {
	data, err := EncodeSession(sess)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO sessions (chat_id, data, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at
	`, chatID, string(data), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store: save session %d: %w", chatID, err)
	}
	return nil
}

// Delete drops the row for chatID (e.g. /reset). Deleting a non-existent row
// is not an error.
func (s *SQLiteSessions) Delete(chatID int64) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE chat_id = ?`, chatID); err != nil {
		return fmt.Errorf("store: delete session %d: %w", chatID, err)
	}
	return nil
}

// Close closes the database handle (checkpoints the WAL file cleanly).
func (s *SQLiteSessions) Close() error {
	return s.db.Close()
}
