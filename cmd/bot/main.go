// Command bot is the v2v-demo Telegram voice assistant: the full loop on
// text and voice — STT (local Whisper / whisper-1) → grounding gate +
// generator (ollama / openai / gemini) → TTS (ElevenLabs / Azure) → voice +
// text reply, plus the greeting and the /voice command. Backends are picked
// by the *_BACKEND env vars.
package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/store"
	"github.com/valpere/v2v-demo/internal/stt"
	"github.com/valpere/v2v-demo/internal/telegram"
	"github.com/valpere/v2v-demo/internal/tts"
)

type app struct {
	cfg Config
	tg  telegram.Client
	gen dialog.Generator
	stt stt.Transcriber
	tts tts.Synthesizer
	loc *time.Location // bureau timezone (BOT_TIMEZONE) — for the prompt's CURRENT TIME block

	topics   map[string]topicBundle // >1 entry -> a picker is shown after /start
	topicIDs []string               // display order (map iteration isn't stable)

	sessions sessionStore

	lim *limiter // nil = no limits

	mu        sync.Mutex
	inbox     map[int64]chan telegram.Update // per-chat FIFO queue (one serial worker each)
	idleEvict time.Duration                  // 0 = defaultIdleEvict
	slow      map[int64]time.Time            // last "slow down" notice per chat — one per slowDownWindow
	wg        sync.WaitGroup                 // outstanding chatWorker goroutines — see main()'s shutdown order
}

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	topics, topicIDs, err := loadTopics(cfg)
	if err != nil {
		log.Fatal(err)
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Fatalf("BOT_TIMEZONE %q: %v", cfg.Timezone, err)
	}
	gen, err := newGenerator(cfg)
	if err != nil {
		log.Fatal(err)
	}
	transcriber, err := newTranscriber(cfg)
	if err != nil {
		log.Fatal(err)
	}
	synth, err := newSynthesizer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := store.PruneLogs(cfg.DataDir, cfg.LogRetentionDays, time.Now()); err != nil {
		log.Printf("prune logs: %v", err) // not fatal: retention must not stop the bot
	}
	sessions, err := newSessionStore(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer sessions.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go pruneLogsDaily(ctx, cfg) // a long-running bot never restarts, so the startup prune is not enough

	tg, err := telegram.New(cfg.TelegramToken)
	if err != nil {
		log.Fatal(err)
	}

	lim := newLimiter(cfg)
	lim.loc = loc
	a := &app{
		lim:      lim,
		cfg:      cfg,
		tg:       tg,
		gen:      gen,
		stt:      transcriber,
		tts:      synth,
		loc:      loc,
		topics:   topics,
		topicIDs: topicIDs,
		sessions: sessions,
		inbox:    make(map[int64]chan telegram.Update),
	}

	updates, err := tg.Updates(ctx)
	if err != nil {
		log.Fatal(err)
	}

	dialogModel := cfg.DialogModel
	if dialogModel == "" {
		dialogModel = "(backend default)"
	}
	dialogDesc := fmt.Sprintf("%s/%s", cfg.DialogBackend, dialogModel)
	if cfg.DialogFallbackBackend != "" {
		dialogDesc += "→" + cfg.DialogFallbackBackend
	}
	sttDesc := cfg.STTBackend
	if cfg.STTFallbackBackend != "" {
		sttDesc += "→" + cfg.STTFallbackBackend
	}
	ttsDesc := cfg.TTSBackend
	if cfg.TTSFallbackBackend != "" {
		ttsDesc += "→" + cfg.TTSFallbackBackend
	}
	log.Printf("v2v-demo: listening — dialog=%s tts=%s stt=%s sessions=%s tz=%s; %d topic(s)",
		dialogDesc, ttsDesc, sttDesc, cfg.SessionStore, cfg.Timezone, len(topics))

	for u := range updates {
		a.dispatch(ctx, u)
	}
	// every chatWorker exits once ctx is cancelled (the same ctx that closed
	// the updates channel above), but one may still be mid-handleUpdate —
	// wait for it before the deferred sessions.Close() runs, or a save can
	// race a session-store shutdown.
	a.wg.Wait()
	log.Print("v2v-demo: shut down")
}

const (
	chatQueueSize  = 64               // pending updates per chat before overflow is dropped
	slowDownWindow = 30 * time.Second // at most one "slow down" notice per chat per window
	slowDownLine   = "Забагато повідомлень одразу — зачекайте, будь ласка, поки я відповім на попередні. / Too many messages at once — please wait until I answer the earlier ones."
)

// pruneLogsDaily re-applies LOG_RETENTION_DAYS once a day until ctx ends.
func pruneLogsDaily(ctx context.Context, cfg Config) {
	if cfg.LogRetentionDays <= 0 {
		return
	}
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := store.PruneLogs(cfg.DataDir, cfg.LogRetentionDays, time.Now()); err != nil {
				log.Printf("prune logs: %v", err)
			}
		}
	}
}

// dispatch routes an update to its chat's serial worker, spawning the worker
// on first contact. This keeps one chat's turns in strict arrival order (a
// per-chat lock would serialise them but not order them) while different
// chats run concurrently (B5).
func (a *app) dispatch(ctx context.Context, u telegram.Update) {
	// The non-blocking send happens under a.mu so an idle worker cannot be
	// evicted between "found its queue" and "queued the update".
	a.mu.Lock()
	ch := a.inbox[u.ChatID]
	if ch == nil {
		ch = make(chan telegram.Update, chatQueueSize)
		a.inbox[u.ChatID] = ch
		a.wg.Add(1)
		go a.chatWorker(ctx, u.ChatID, ch)
	}
	queued := true
	select {
	case ch <- u:
	default:
		queued = false
	}
	notify := false
	if !queued {
		// Queue full: never block the single update loop (that would stall
		// every other chat). Drop the update; tell the sender once per window.
		notify = time.Since(a.slow[u.ChatID]) >= slowDownWindow
		if notify {
			if a.slow == nil {
				a.slow = make(map[int64]time.Time)
			}
			a.slow[u.ChatID] = time.Now()
		}
	}
	a.mu.Unlock()

	if queued {
		return
	}
	log.Printf("dispatch (chat %d): queue full, dropped an update", u.ChatID)
	if notify {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.send(ctx, u.ChatID, slowDownLine)
		}()
	}
}

// defaultIdleEvict is how long a chat's worker may sit idle before it is
// released; app.idleEvict overrides it (tests).
const defaultIdleEvict = 15 * time.Minute

// chatWorker serves one chat's queue until ctx ends or the chat has been idle
// for idleEvict, at which point it deregisters itself (a later message starts
// a fresh worker, so a long-running bot does not hold a goroutine and a
// 64-slot channel per chat ever seen).
func (a *app) chatWorker(ctx context.Context, chatID int64, ch chan telegram.Update) {
	defer a.wg.Done()
	idle := a.idleEvict
	if idle <= 0 {
		idle = defaultIdleEvict
	}
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-ch:
			a.handleUpdate(ctx, u)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idle)
		case <-timer.C:
			a.mu.Lock()
			if len(ch) == 0 { // dispatch queues under a.mu, so this is exact
				delete(a.inbox, chatID)
				a.mu.Unlock()
				return
			}
			a.mu.Unlock()
			timer.Reset(idle)
		}
	}
}

// buildGenerator resolves one dialogue backend by name — the step
// newGenerator needs twice when a fallback is configured (once for
// DIALOG_BACKEND, once for DIALOG_FALLBACK_BACKEND). An empty
// DIALOG_MODEL lets each impl pick its own default.
func buildGenerator(name string, cfg Config) (dialog.Generator, error) {
	switch name {
	case "ollama":
		return dialog.NewOllama(cfg.OllamaBaseURL, cfg.DialogModel), nil
	case "openai":
		return dialog.NewOpenAI(cfg.OpenAIKey, cfg.DialogModel), nil
	case "gemini":
		return dialog.NewGemini(cfg.GeminiKey, cfg.DialogModel), nil
	default:
		return nil, fmt.Errorf("unknown DIALOG_BACKEND %q", name)
	}
}

// newGenerator selects the dialogue backend (DIALOG_BACKEND), wrapping it
// in a dialog.FailoverGenerator when DIALOG_FALLBACK_BACKEND is set.
func newGenerator(cfg Config) (dialog.Generator, error) {
	primary, err := buildGenerator(cfg.DialogBackend, cfg)
	if err != nil || cfg.DialogFallbackBackend == "" {
		return primary, err
	}
	fallback, err := buildGenerator(cfg.DialogFallbackBackend, cfg)
	if err != nil {
		return nil, fmt.Errorf("dialog fallback: %w", err)
	}
	return &dialog.FailoverGenerator{Primary: primary, Fallback: fallback}, nil
}

// buildTranscriber resolves one STT backend by name — the step
// newTranscriber needs twice when a fallback is configured. "none" is only
// valid as the primary (resolveText then declines voice messages outright);
// it makes no sense as a fallback target, so callers building a fallback
// reject it themselves (see newTranscriber).
func buildTranscriber(name string, cfg Config) (stt.Transcriber, error) {
	switch name {
	case "none":
		return nil, nil
	case "local":
		return stt.NewLocal(cfg.WhisperBin, cfg.WhisperModel, cfg.WhisperLang), nil
	case "openai":
		return stt.NewOpenAI(cfg.OpenAIKey, ""), nil
	case "whispercpp":
		return stt.NewWhisperCPP(cfg.WhisperCPPBin, cfg.FfmpegBin, cfg.WhisperCPPModelPath,
			cfg.WhisperCPPThreads, cfg.WhisperCPPLang), nil
	default:
		return nil, fmt.Errorf("unknown STT_BACKEND %q", name)
	}
}

// newTranscriber selects the STT backend (STT_BACKEND), wrapping it in a
// stt.FailoverTranscriber when STT_FALLBACK_BACKEND is set. "none" returns
// nil — resolveText then declines any voice message with a fixed line
// instead of attempting STT.
func newTranscriber(cfg Config) (stt.Transcriber, error) {
	primary, err := buildTranscriber(cfg.STTBackend, cfg)
	if err != nil || cfg.STTFallbackBackend == "" {
		return primary, err
	}
	if cfg.STTFallbackBackend == "none" {
		return nil, fmt.Errorf("STT_FALLBACK_BACKEND cannot be %q", "none")
	}
	fallback, err := buildTranscriber(cfg.STTFallbackBackend, cfg)
	if err != nil {
		return nil, fmt.Errorf("stt fallback: %w", err)
	}
	return &stt.FailoverTranscriber{Primary: primary, Fallback: fallback}, nil
}

// buildSynthesizer resolves one TTS backend by name — the step
// newSynthesizer needs twice when a fallback is configured. "none" is only
// valid as the primary (the update loop then skips SendVoice and replies
// with text only); it makes no sense as a fallback target, so callers
// building a fallback reject it themselves (see newSynthesizer).
func buildSynthesizer(name string, cfg Config) (tts.Synthesizer, error) {
	var (
		s  tts.Synthesizer
		id string
	)
	switch name {
	case "none":
		return nil, nil
	case "elevenlabs":
		s, id = tts.NewElevenLabs(cfg.ElevenKey), tts.ElevenLabsID
	case "azure":
		s, id = tts.NewAzure(cfg.AzureKey, cfg.AzureRegion), tts.AzureID
	case "espeak":
		s, id = tts.NewEspeak(cfg.EspeakBin, cfg.FfmpegBin), tts.EspeakID
	default:
		return nil, fmt.Errorf("unknown TTS_BACKEND %q", name)
	}
	if cfg.TTSCacheMB > 0 {
		// per backend (below any failover): a clip is only replayed by the
		// engine/model that made it
		s = tts.NewCached(s, id, filepath.Join(cfg.DataDir, "tts-cache"), int64(cfg.TTSCacheMB)<<20)
	}
	// each backend maps the abstract voice ("a"/"b") to its own voice id, so a
	// failover between paid backends never sends one backend's id to the other
	// (outside the cache: the cache key then carries the real voice id)
	switch name {
	case "elevenlabs":
		s = tts.WithVoices(s, cfg.ElevenVoiceA, cfg.ElevenVoiceB)
	case "azure":
		s = tts.WithVoices(s, cfg.AzureVoiceA, cfg.AzureVoiceB)
	}
	return s, nil
}

// newSynthesizer selects the TTS backend (TTS_BACKEND), wrapping it in a
// tts.FailoverSynthesizer when TTS_FALLBACK_BACKEND is set. "none" returns
// nil — the update loop then skips SendVoice and replies with text only.
func newSynthesizer(cfg Config) (tts.Synthesizer, error) {
	primary, err := buildSynthesizer(cfg.TTSBackend, cfg)
	if err != nil || cfg.TTSFallbackBackend == "" {
		return primary, err
	}
	if cfg.TTSFallbackBackend == "none" {
		return nil, fmt.Errorf("TTS_FALLBACK_BACKEND cannot be %q", "none")
	}
	fallback, err := buildSynthesizer(cfg.TTSFallbackBackend, cfg)
	if err != nil {
		return nil, fmt.Errorf("tts fallback: %w", err)
	}
	return &tts.FailoverSynthesizer{Primary: primary, Fallback: fallback}, nil
}
