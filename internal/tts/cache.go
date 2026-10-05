package tts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Identities of the shipped backends for NewCached: the synthesis engine plus
// the model/format that shapes the audio. Changing a model or output format
// MUST change its identity, so old clips are never served under the new one.
const (
	ElevenLabsID = "elevenlabs/" + elevenModel + "/" + elevenFormat
	AzureID      = "azure/ogg-48khz-16bit-mono-opus"
	EspeakID     = "espeak"
)

type cacheableKey struct{}

// Cacheable marks ctx's Speak call as a fixed, user-independent line (a
// handoff, emergency or clarify text) that may be stored and replayed. Every
// other reply may carry a client's name or phone number and is never cached.
func Cacheable(ctx context.Context) context.Context {
	return context.WithValue(ctx, cacheableKey{}, true)
}

func cacheable(ctx context.Context) bool { v, _ := ctx.Value(cacheableKey{}).(bool); return v }

// CachedSynth decorates ONE concrete backend with an on-disk cache of its
// audio for Cacheable calls. Wrap each backend individually (below any
// failover wrapper): the key carries that backend's identity, voice id and
// language, so a conversation never gets a clip made by a different
// engine/model/voice than the one that would have spoken it live.
type CachedSynth struct {
	inner    Synthesizer
	id       string
	dir      string
	maxBytes int64
	mu       sync.Mutex
	hits     atomic.Int64 // cacheable calls served from disk
	misses   atomic.Int64 // cacheable calls that had to synthesise
}

// NewCached caches inner's audio under dir (0700, files 0600), evicting the
// oldest clips beyond maxBytes. id is the backend identity (see the *ID consts).
func NewCached(inner Synthesizer, id, dir string, maxBytes int64) *CachedSynth {
	return &CachedSynth{inner: inner, id: id, dir: dir, maxBytes: maxBytes}
}

func (c *CachedSynth) path(text, voiceID, lang string) string {
	h := sha256.New()
	for _, part := range []string{c.id, voiceID, lang, text} {
		h.Write([]byte(part))
		h.Write([]byte{0}) // unambiguous field boundary
	}
	return filepath.Join(c.dir, hex.EncodeToString(h.Sum(nil))+".ogg")
}

func (c *CachedSynth) Speak(ctx context.Context, text, voiceID, lang string) ([]byte, error) {
	if !cacheable(ctx) {
		return c.inner.Speak(ctx, text, voiceID, lang)
	}
	p := c.path(text, voiceID, lang)
	if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
		now := time.Now()
		_ = os.Chtimes(p, now, now) // touch: recently used survives eviction
		c.hits.Add(1)
		log.Printf("tts cache: hit (%s) hits=%d misses=%d", c.id, c.hits.Load(), c.misses.Load())
		return b, nil
	}
	c.misses.Add(1)
	audio, err := c.inner.Speak(ctx, text, voiceID, lang)
	if err != nil || len(audio) == 0 {
		return audio, err
	}
	c.store(p, audio) // a failed store only costs the next hit
	return audio, nil
}

func (c *CachedSynth) store(path string, audio []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, audio, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return
	}
	c.evict()
}

// evict removes the least recently used clips until the cache fits maxBytes.
func (c *CachedSynth) evict() {
	if c.maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type clip struct {
		path string
		size int64
		mod  int64
	}
	var clips []clip
	var total int64
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".ogg" {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		clips = append(clips, clip{filepath.Join(c.dir, e.Name()), fi.Size(), fi.ModTime().UnixNano()})
		total += fi.Size()
	}
	sort.Slice(clips, func(i, j int) bool { return clips[i].mod < clips[j].mod })
	for _, cl := range clips {
		if total <= c.maxBytes {
			return
		}
		if os.Remove(cl.path) == nil {
			total -= cl.size
		}
	}
}
