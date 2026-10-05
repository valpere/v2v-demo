package tts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type countingSynth struct {
	calls atomic.Int32
	audio []byte
	err   error
}

func (c *countingSynth) Speak(context.Context, string, string, string) ([]byte, error) {
	c.calls.Add(1)
	return c.audio, c.err
}

func TestCachedSynthHitsOnlyWhenCacheable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "c")
	inner := &countingSynth{audio: []byte("OggS-1")}
	c := NewCached(inner, "elevenlabs/m1", dir, 1<<20)
	ctx := Cacheable(context.Background())

	for i := 0; i < 3; i++ {
		got, err := c.Speak(ctx, "fixed line", "voiceA", "uk")
		if err != nil || string(got) != "OggS-1" {
			t.Fatalf("speak %d: %q %v", i, got, err)
		}
	}
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("backend calls = %d, want 1 (the rest are cache hits)", n)
	}

	// a reply that is not a fixed line is never stored or served from the cache
	c.Speak(context.Background(), "personal reply", "voiceA", "uk")
	c.Speak(context.Background(), "personal reply", "voiceA", "uk")
	if n := inner.calls.Load(); n != 3 {
		t.Fatalf("non-cacheable ctx must always hit the backend, calls = %d", n)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("cache holds %d files, want only the fixed line", len(entries))
	}
}

// The same text must never be served across a different backend/model, voice
// or language — that would mix voices within one conversation.
func TestCachedSynthKeyCoversBackendVoiceLangText(t *testing.T) {
	dir := t.TempDir()
	ctx := Cacheable(context.Background())
	base := &countingSynth{audio: []byte("a")}
	NewCached(base, "elevenlabs/m1", dir, 1<<20).Speak(ctx, "t", "A", "uk")

	for name, call := range map[string]func(){
		"other model":   func() { NewCached(base, "elevenlabs/m2", dir, 1<<20).Speak(ctx, "t", "A", "uk") },
		"other backend": func() { NewCached(base, "azure/ogg", dir, 1<<20).Speak(ctx, "t", "A", "uk") },
		"other voice":   func() { NewCached(base, "elevenlabs/m1", dir, 1<<20).Speak(ctx, "t", "B", "uk") },
		"other lang":    func() { NewCached(base, "elevenlabs/m1", dir, 1<<20).Speak(ctx, "t", "A", "en") },
		"other text":    func() { NewCached(base, "elevenlabs/m1", dir, 1<<20).Speak(ctx, "t2", "A", "uk") },
	} {
		before := base.calls.Load()
		call()
		if base.calls.Load() != before+1 {
			t.Errorf("%s: served from another key's cache entry", name)
		}
	}
	before := base.calls.Load()
	NewCached(base, "elevenlabs/m1", dir, 1<<20).Speak(ctx, "t", "A", "uk")
	if base.calls.Load() != before {
		t.Error("identical key must hit (also across instances: the cache is on disk)")
	}
}

func TestCachedSynthDoesNotCacheErrorsOrEmpty(t *testing.T) {
	dir := t.TempDir()
	ctx := Cacheable(context.Background())
	bad := &countingSynth{err: errors.New("boom")}
	NewCached(bad, "x", dir, 1<<20).Speak(ctx, "t", "A", "uk")
	empty := &countingSynth{audio: nil}
	NewCached(empty, "x", dir, 1<<20).Speak(ctx, "t", "A", "uk")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("failed/empty synthesis was cached: %d files", len(entries))
	}
}

func TestCachedSynthEvictsOldestOverBudget(t *testing.T) {
	dir := t.TempDir()
	ctx := Cacheable(context.Background())
	inner := &countingSynth{audio: make([]byte, 400)}
	c := NewCached(inner, "x", dir, 1000) // room for two 400-byte clips
	for _, txt := range []string{"one", "two", "three"} {
		c.Speak(ctx, txt, "A", "uk")
		time.Sleep(10 * time.Millisecond) // distinct mtimes
	}
	var total int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		fi, _ := e.Info()
		total += fi.Size()
	}
	if total > 1000 || len(entries) != 2 {
		t.Fatalf("cache = %d files / %d bytes, want 2 files within the 1000-byte budget", len(entries), total)
	}
	before := inner.calls.Load()
	c.Speak(ctx, "one", "A", "uk") // the oldest was evicted -> synthesised again
	if inner.calls.Load() != before+1 {
		t.Error("oldest entry should have been evicted")
	}
}

func TestCachedSynthPrivateFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "c")
	c := NewCached(&countingSynth{audio: []byte("a")}, "x", dir, 1<<20)
	c.Speak(Cacheable(context.Background()), "t", "A", "uk")
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %o", di.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	fi, _ := entries[0].Info()
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %o", fi.Mode().Perm())
	}
}
