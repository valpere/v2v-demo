# v2v-demo — implementation plan

A throwaway demo for a prospective client (a translation bureau). Goal: let
them talk to a voice assistant on Telegram and judge conversation quality +
voice naturalness. **Neutral scenario, fabricated data.** Not production, not a
framework — the simplest thing that shows the loop.

Requirements (the *what* / *why*, with source traces) live in
`docs/requirements.md`; the component / data-flow / grounding design is in
`docs/architecture.md` and is authoritative for *shape*. This file is the
file-by-file *how*. FR-/NFR-/D- IDs refer to the requirements file.

## What it does (one turn) — overview

1. Telegram voice (or text) arrives. Voice → OGG to a validated temp path →
   `stt.Transcribe` → transcript (dev default `STT_BACKEND=local` — the
   `openai-whisper` CLI; `STT_BACKEND=openai` `whisper-1` — mandatory for the
   I-10 client recording, B2).
2. `cmd/bot` starts the recording-action ticker, then calls
   `dialog.Handle(ctx, sess, kb, gen, systemPrompt, transcript, time.Now().In(a.loc))`.
3. `dialog.Handle` runs the sequence in **§"Behavioural spec (pseudocode)"**
   below — that section is authoritative; every step, constant, and edge case
   is spelled out there. LLM dev default `ollama` (`gemma4:cloud`);
   `DIALOG_BACKEND` switches to `openai` (the client artefact — D-20) /
   `gemini` (D-13).
4. `Reply.Text` → `tts.Spoken` (strips markdown / arrows / currency codes the
   voice would spell out — the text message keeps the original) → `tts.Speak`
   (session voice) → OGG/Opus (default ElevenLabs `eleven_multilingual_v2`;
   `TTS_BACKEND=azure` — D-15).
5. Telegram: `SendVoice` (no caption) then one `SendText(Reply.Text)`.
6. `store.AppendTurn` always; `store.AppendLead` iff `Reply.Signal ==
   lead_ready`.

## Quote parameters (FR-7)

`language pair · document type · volume · deadline · certification/notarization
· delivery`. Session holds them as six optional fields; "lead ready" = all six
set. No FSM — the state *is* which fields are still nil, and the LLM is
prompted to ask about exactly those.

## Package layout

```
cmd/bot/main.go     wiring: config, clients, per-chat session map (mutex-guarded),
                    the update loop — owns /voice a|b parsing, the greeting
                    (greeting.md body once per chat), the recording ticker,
                    getUpdates offset, per-chat serialization, store calls
internal/telegram/  long-poll getUpdates, file download, SendVoice (no caption)
                    / SendText / SendRecordingAction — transport only, no
                    command or session logic
internal/stt/       Transcriber interface; local.go (shell openai-whisper CLI
                    on the ogg) · openai.go (whisper-1 API). STT_BACKEND picks.
internal/tts/       Synthesizer interface; elevenlabs.go (eleven_multilingual_v2,
                    output_format opus) · azure.go (SSML + ogg-opus header).
                    TTS_BACKEND picks. Google = documented, not built.
internal/kb/        load KB_PATH, split on "##" into titled sections
internal/dialog/    gate.go (kbOverlap + hardEscalate + isSlotAnswer +
                    isSmallTalk + groundingGate) · lang.go (detectLang —
                    lingua-go) · dialog.go (loads prompt/system.md, builds the
                    prompt, parses the JSON reply, merges slots) · generator.go
                    (Generator interface) · openai_compat.go (shared
                    OpenAI-compat client) + ollama.go / openai.go /
                    gemini.go (DIALOG_BACKEND)
internal/store/     append-only JSONL: turn records + lead records (DATA_DIR)

prompt/system.md   the assistant persona + conversation playbook + hard rules
                   + output format (authored, not generated — do not rewrite)
prompt/greeting.md the fixed bilingual opening message (I-7 / FR-14)
examples/dialogues.md  worked example conversations — test material and the
                   recorded-sample script; optionally one short example as
                   few-shot if output consistency is poor
kb/translation-bureau.md  the fictional FromToBridge knowledge base
```

## Type surface

The exact Go shape. Implement these signatures verbatim; the `@schema` blocks
in `docs/requirements.md` §1 mirror the data structs.

```go
// ── internal/telegram ────────────────────────────────────────────
type Update struct {
	ChatID      int64
	Text        string // empty for a voice-only message
	VoiceFileID string // empty for a text message
	IsStart     bool   // /start, or the first message in this chat

	CallbackData string // an inline-keyboard tap's payload; empty otherwise
	CallbackID   string // that tap's callback query id — AnswerCallback needs it
}

// Button: one inline-keyboard button, one per row (a handful of topics
// never needs a denser layout).
type Button struct {
	Label string
	Data  string // echoed back as Update.CallbackData when tapped
}

type Client interface {
	Updates(ctx context.Context) (<-chan Update, error)      // long-poll loop
	DownloadVoice(ctx context.Context, fileID string) (oggPath string, err error)
	SendVoice(ctx context.Context, chatID int64, ogg []byte) error  // NO caption
	SendText(ctx context.Context, chatID int64, text string) error
	SendRecordingAction(ctx context.Context, chatID int64) error    // lasts ~5s server-side
	SendButtons(ctx context.Context, chatID int64, text string, buttons []Button) error
	AnswerCallback(ctx context.Context, callbackID string) error    // required, or the tap spinner never clears
}
// The reply text goes out once, via SendText — never also as a SendVoice
// caption. The update loop keeps "recording voice" visible for the whole
// turn by re-calling SendRecordingAction on a ~4s ticker (see cmd/bot loop).

// ── internal/stt ─────────────────────────────────────────────────
type Transcriber interface {
	// langHint is "" | "uk" | "en"; oggPath is a local file.
	Transcribe(ctx context.Context, oggPath, langHint string) (string, error)
}
// NewLocal(bin, model, lang string) Transcriber        // openai-whisper CLI; bin default "whisper", model default "turbo"
// NewOpenAI(apiKey, model string) Transcriber          // whisper-1

// ── internal/tts ─────────────────────────────────────────────────
type Synthesizer interface {
	// returns OGG/Opus mono, ready for Telegram sendVoice.
	Speak(ctx context.Context, text, voiceID, lang string) ([]byte, error)
}
// NewElevenLabs(apiKey string) Synthesizer
// NewAzure(key, region string) Synthesizer

// ── internal/kb ──────────────────────────────────────────────────
type Section struct {
	Title string // the "## " heading text
	Body  string // everything until the next "## "
}
func Load(path string) ([]Section, error) // split KB_PATH on "## "

// ── internal/dialog ──────────────────────────────────────────────
type Msg struct {
	Role string // "user" | "assistant"
	Text string
}

type QuoteSlots struct {
	LanguagePair  *string `json:"language_pair"`
	DocType       *string `json:"doc_type"`
	Volume        *string `json:"volume"`
	Deadline      *string `json:"deadline"`
	Certification *string `json:"certification"`
	Delivery      *string `json:"delivery"`
}
func (s QuoteSlots) Complete() bool // all six non-nil

type Signal string

const (
	SignalContinue  Signal = "continue"
	SignalLeadReady Signal = "lead_ready"
	SignalEscalate  Signal = "escalate"
)

// the whole model response — one JSON object; Reply is the only text spoken
type modelReply struct {
	Reply  string     `json:"reply"`
	Slots  QuoteSlots `json:"slots"`
	Signal Signal     `json:"signal"`
}

// All fields exported with explicit json tags — a SESSION_STORE=sqlite row
// is encoding/json, and an unexported field silently vanishes across a
// restart (that's why leadSlots/gateStrike were renamed from their original
// unexported form).
type Session struct {
	Slots      QuoteSlots `json:"slots"`
	History    []Msg      `json:"history"`     // trimmed to the last HistoryLimit (20) Msg entries ≈ 10 turns
	Voice      string     `json:"voice"`       // "a" | "b"; default "a"
	Lang       string     `json:"lang"`        // "uk" | "en"; last turn detectLang was confident (mid-switch propagates); "" until then
	Escalated  bool       `json:"escalated"`
	LeadDone   bool       `json:"lead_done"`   // a lead_ready already fired this session
	LeadSlots  string     `json:"lead_slots"`  // slot JSON at the last recorded lead; repeat lead_ready — same slots -> continue (test-5_1), changed -> a corrected LeadRecord (11d)
	GateStrike bool       `json:"gate_strike"` // gate fired last turn (-> clarifyLine); a 2nd hit in a row escalates
	Topic      string     `json:"topic"`       // which topics.json bundle this session belongs to (multi-topic feature; empty in single-topic mode)
}

type Generator interface {
	Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error)
}
// NewOllama(baseURL, model string) Generator   // default
// NewOpenAI(apiKey, model string) Generator
// NewGemini(apiKey, model string) Generator    // last resort — $25 prepay

type Reply struct {
	Text    string   // spoken text (mr.Reply) or the fixed handoff/apology line)
	Signal  Signal   // continue | lead_ready | escalate
	Matched []string // log-only: KB section titles that had a query-term hit; nil on an early escalate
}

// the core: one user turn -> one reply, mutating sess.
func Handle(
	ctx context.Context,
	sess *Session,
	kb []Section,
	gen Generator,
	systemPrompt string,
	userText string,
	now time.Time,   // current time in BOT_TIMEZONE; zero -> omit the CURRENT TIME block
) (Reply, error)

// gate.go — the whole KB always goes in the prompt; these only feed the gate
func kbOverlap(query string, kb []Section) float64      // fraction of meaningful query terms found in the whole KB
func hardEscalate(query string) bool                    // unambiguous handoff triggers (B3)
func isSlotAnswer(sess *Session, userText string) bool  // short + bot just asked + a slot is nil
func isSmallTalk(userText string) bool                  // greeting / thanks / farewell — bypasses the gate (test-5_1)
func groundingGate(overlap float64, slotAnswer bool) (forceEscalate bool)

// lang.go — language detection (lingua-go; D-19)
func detectLang(text string) string    // "uk" | "en" | "" — lingua over {uk,en,ru}; ru maps to "uk"
func sessLang(sess *Session) string    // sess.Lang or "en"; only feeds the fixed lines

// ── internal/store ──────────────────────────────────────────────
type TurnRecord struct {
	Time      time.Time  `json:"time"`
	ChatID    int64      `json:"chat_id"`
	UserText  string     `json:"user_text"`
	ReplyText string     `json:"reply_text"`
	Signal    string     `json:"signal"`
	Matched   []string   `json:"matched"`      // empty on a pre-LLM escalate
	Slots     QuoteSlots `json:"slots"`        // snapshot after this turn's merge
	LatencyMS int64      `json:"latency_ms"`
}

// the shape a Zoho lead would take — written to the log, not sent anywhere
type LeadRecord struct {
	Time          time.Time `json:"time"`
	ChatID        int64     `json:"chat_id"`
	LanguagePair  string    `json:"language_pair"`
	DocType       string    `json:"doc_type"`
	Volume        string    `json:"volume"`
	Deadline      string    `json:"deadline"`
	Certification string    `json:"certification"`
	Delivery      string    `json:"delivery"`
}

func AppendTurn(dir string, r TurnRecord) error // dir/turns.jsonl
func AppendLead(dir string, r LeadRecord) error // dir/leads.jsonl

// ── cmd/bot ─────────────────────────────────────────────────────
type Config struct {
	TelegramToken string

	TTSBackend   string // "elevenlabs" | "azure"
	ElevenKey    string
	ElevenVoiceA string
	ElevenVoiceB string
	AzureKey     string
	AzureRegion  string
	AzureVoiceA  string
	AzureVoiceB  string

	STTBackend   string // "none" | "local" (default) | "openai" (I-10 client recording)
	WhisperBin   string // openai-whisper CLI; default "whisper"
	WhisperModel string // openai-whisper model name; default "turbo"
	WhisperLang  string // "auto" | "uk" | "en"

	DialogBackend string // "ollama" (default) | "openai" | "gemini"
	DialogModel   string
	GeminiKey     string
	OllamaBaseURL string
	OpenAIKey     string

	KBPath           string
	SystemPromptPath string
	GreetingPath     string
	DataDir          string

	SessionStore  string // "memory" (default) | "sqlite" (SESSION_STORE)
	SessionDBPath string // SQLite file; only used when SessionStore=="sqlite"; default "./data/sessions.db"

	TopicsPath string // topics.json manifest (TOPICS_PATH); missing/single-entry -> one synthetic topic, no picker
}

// topics/topics.json: a JSON array of these.
type topicManifestEntry struct {
	ID, Title, KB, SystemPrompt, Greeting string
}

// topicBundle is a fully loaded topic — its own KB, persona and greeting.
// app.topics map[string]topicBundle + app.topicIDs []string (display
// order; map iteration isn't stable) replace the old singular
// kb/sys/greeting fields on app.
type topicBundle struct {
	ID, Title string
	KB        []Section
	Sys       string
	Greeting  string
}
func loadTopics(cfg Config) (topics map[string]topicBundle, ids []string, err error)
func LoadConfig() (Config, error)
// env + optional .env (tiny hand parser: KEY=VALUE lines, "#" comments, no
// quotes, no multiline, no "export "). Env→field map (the .env.example keys):
//   TELEGRAM_BOT_TOKEN TTS_BACKEND ELEVENLABS_API_KEY ELEVENLABS_VOICE_A/_B
//   AZURE_SPEECH_KEY AZURE_SPEECH_REGION AZURE_VOICE_A/_B
//   STT_BACKEND WHISPER_BIN WHISPER_MODEL WHISPER_LANG
//   DIALOG_BACKEND DIALOG_MODEL GEMINI_API_KEY OLLAMA_BASE_URL OPENAI_API_KEY
//   KB_PATH SYSTEM_PROMPT_PATH GREETING_PATH DATA_DIR
//   SESSION_STORE SESSION_DB_PATH TOPICS_PATH
```

## Behavioural spec (pseudocode)

The type surface pins the *shape*; this pins the *behaviour*. Implement these
step-for-step. Constants are named here and must be package-level `const`s in
`internal/dialog`.

```
const (
    GateFloor        = 0.25   // kbOverlap below this on a content question -> escalate pre-LLM
    HistoryLimit     = 20     // Session.History cap, in Msg entries ≈ 10 turns (NOT 20 turns)
    SlotAnswerMaxTok = 6      // isSlotAnswer: max tokens (also requires: bot just asked, a slot is nil)
    RecordingTick    = 4 * time.Second  // cmd/bot re-sends the recording action at this interval
)
```

**B1 decision (2026-08-29):** the *whole* KB goes into every system prompt
(gemma4:cloud / Gemini Flash / gpt-4.1-mini all have the headroom).
There is **no retrieval-for-context**. A keyword-overlap score is computed
*only* to feed the grounding gate. If exact-match overlap proves too weak for
inflected Ukrainian in testing, add a stemmer (B1 fallback — candidates:
`github.com/amakukha/stemmers_ukrainian`, `github.com/dbklim/Uk_Stemmer`,
k-centre.uacorpus.org tools) and re-tune `GateFloor`; do not reintroduce
per-section retrieval.

**Bilingual KB (D-18, 2026-08-31, step 3):** `kb/translation-bureau.md` was
English-only, so `kbOverlap` scored every Ukrainian content question 0 and
the gate false-escalated it pre-LLM. Fixed by making the KB bilingual — an
English block then a Ukrainian block under each `## ` heading (headings
`English / Українською`), ~19 KB, 13 sections. The B1 stemmer note now
applies only to within-Ukrainian inflection, not the cross-language wall.

### kbOverlap(query string, kb []Section) float64

```
qterms := drop(tokenize(lower(strip_punct(query))), stopwords)   // uk+en stoplist
if len(qterms) == 0 { return 0 }
haystack := lower(strip_punct(join(" ", sec.Title + " " + sec.Body for sec in kb)))
hit := count of DISTINCT qterms that appear as a substring token in haystack
return float64(hit) / float64(len(qterms))     // fraction of the meaningful query the KB covers
```

- **stopwords** — a fixed ~40-word uk+en function-word list, package-level
  `var stopwords map[string]bool`. Rationale: without it "the"/"of"/"і"/"на"
  inflate the overlap and the gate never fires (ragline's tsquery lesson).
- exact-match, no stemming (B1 fallback if this misses inflected forms).
- deterministic.

### hardEscalate(query string) bool   (B3)

```
q := lower(query)
for kw in escalateKeywords { if q contains kw { return true } }
return false
```

- **escalateKeywords** — a short fixed list of unambiguous triggers, uk+en:
  `"повернути гро" "повернення кош" "поверніть гро" "refund" "скарг"
   "complaint" "суд" "позов" "court" "дайте людину" "з людиною"
   "справжн" "real person" "talk to a person" "менеджера напряму"`.
  These force a handoff regardless of message length or slot state. The
  2-term cases (sworn + non-listed language; interpreting + booking) stay with
  the model's own `escalate` and the `prompt/system.md` hard rules.

### isSlotAnswer(session *Session, userText string) bool   (B3)

```
lastAsst := the most recent Msg{Role:"assistant"} in session.History, "" if none
if !contains(lastAsst, "?")            { return false }   // the bot just asked something
if session.Slots.Complete()            { return false }
if filledSlots(session.Slots) >= 2     { return true }    // mid-quote: any reply counts, no length cap
return len(tokenize(userText)) <= SlotAnswerMaxTok
```

So a short message only bypasses the gate when it is plausibly an answer to
the bot's own question — "5 pages" after "how many pages?" bypasses; a bare
"apostille?" out of nowhere does not.

**No `GateStrike` special-case here (removed 2026-09-05).** `clarifyLine`'s
"ask" tail ends in "?" specifically so a reply to *it* also qualifies —
before this, `isSlotAnswer` returned `false` outright whenever `GateStrike`
was set, so *any* reply right after a first gate strike — including a
correct one like "5 сторінок" after "Скільки сторінок?" — failed the
check and, on low KB overlap (typical for a bare number), immediately
escalated to a human. Traded for: a second genuinely off-topic *short*
message now also reaches the model (as a presumed slot answer) instead of
pre-LLM escalating, since the heuristic (length + "?" + nil slot) has no
way to tell the two apart by content. See REQ-DLG-13.

### isSmallTalk(userText string) bool   (added 2026-08-31, test-5_1)

```
if len(tokenize(userText)) > 8 || userText contains "?"  { return false }
q := " " + lower(strip_punct(userText)) + " "
return any pleasantryMarker is a substring of q   // greeting / thanks / farewell
```

A greeting / thank-you / farewell is a conversation boundary, not a content
question — it must reach the assistant, never pre-escalate. `Handle` checks
`!isSmallTalk(userText)` before calling `groundingGate` (same role as the
slot-answer bypass). "привіт, а скільки коштує апостиль?" still gates (the
"?").

### groundingGate(overlap float64, slotAnswer bool) (forceEscalate bool)

```
if slotAnswer            { return false }   // answering the bot's question
if overlap < GateFloor   { return true }    // KB can't help with this message
return false
```

A gate hit does **not** escalate by itself (2026-09-04). In `Handle`:
```
if !isSmallTalk(text) && groundingGate(overlap, slotAnswer):
    if sess.gateStrike: return esc()                    // 2nd unmatched msg in a row
    sess.gateStrike = true
    append (user, clarifyLine(sess)) to history
    return {clarifyLine(sess), continue}                // off-topic / gibberish -> polite line, no LLM
sess.gateStrike = false                                 // this turn is real
```
`clarifyLine` states the bureau only translates documents + lists the still-nil
slots (or the "already complete" tail). Deterministic — no hallucination, no
statement on an off-topic subject possible. `hardEscalate` (incl. explicit
"хочу менеджера"), a model `signal: escalate`, and the 2nd strike still hand
off at once.

### parseResponse(raw string) *modelReply

```
s := trimspace(raw)
strip a leading ```json / ``` fence line and its closing ``` if present
start, end := first "{", last "}" in s
if not found OR end <= start:
    return nil
json.Unmarshal(s[start:end+1]) into a modelReply
if json error OR reply is blank OR signal not in {continue, lead_ready, escalate}:
    return nil
return &modelReply
```

The contract is one JSON object `{reply,slots,signal}`; the parser is lenient
about a fence or prose around it, strict on the result. A `nil` makes Handle
step 8 reply with the fixed handoff line — the raw model output is never
spoken. Each backend forces valid JSON its own way ("model connection
context"): `NewOpenAI` sets `response_format:{"type":"json_object"}`,
`NewOllama` relies on the prompt, a gemini path would use `responseMimeType`.

### Handle(ctx, sess, kb, gen, systemPrompt, userText, now) (Reply, error)

`esc(sess)` = shorthand for `{ sess.Escalated = true; return Reply{Text:
handoffLine(sessLang(sess)), Signal: SignalEscalate}, nil }`.

```
0. if l := detectLang(userText); l != "" { sess.Lang = l }   // updates every confident turn (mid-switch)
1. if hardEscalate(userText):  esc(sess)              // B3: unambiguous handoff trigger
1b. if looksLikeInjection(userText):  clarifyLine + gateStrike (a repeat -> esc)  // pasted JSON/fence -> never reaches the LLM
2. slotAnswer := isSlotAnswer(sess, userText)
3. overlap    := kbOverlap(userText, kb)
4. if !isSmallTalk(userText) && groundingGate(overlap, slotAnswer):  esc(sess)  // content question the KB barely covers
5. sysPrompt := systemPrompt
              + "\n\n--- KNOWLEDGE BASE ---\n"  + join("\n\n", "## "+s.Title+"\n"+s.Body for s in kb)   // WHOLE KB
              + "\n\n--- COLLECTED SO FAR ---\n" + jsonCompact(sess.Slots)
              + (sess.Lang != "" ?  "\n\n--- CONVERSATION LANGUAGE ---\n<Ukrainian|English> so far; "
                                    + "stay in it unless the client clearly switches. Never reply in Russian."  : "")
              + (!now.IsZero() ?  "\n\n--- CURRENT TIME ---\n" + officeStatus(now)  : "")   // no clock in the bot; injected per turn (BOT_TIMEZONE)
6. hist := trimTail(append(sess.History, Msg{"user", userText}), HistoryLimit)
7. raw, err := gen.Generate(ctx, sysPrompt, hist)
   if err != nil:
       sess.Escalated = true
       return Reply{Text: apologyLine(sessLang(sess)), Signal: SignalEscalate}, nil   // degrade, never bubble
8. mr := parseResponse(raw)
9. if mr == nil:  esc(sess)                           // NEVER speak un-parseable raw output
   spoken := trimspace(mr.Reply)
10. merge — 6 explicit field assignments (QuoteSlots is a struct, no reflection):
       if mr.Slots.LanguagePair  != nil { sess.Slots.LanguagePair  = mr.Slots.LanguagePair }
       if mr.Slots.DocType       != nil { sess.Slots.DocType       = mr.Slots.DocType }
       … the other four. Never assign nil (that would clear a filled slot).
11. signal := mr.Signal
    if signal == SignalLeadReady && !sess.Slots.Complete():   // B4 guard
        log.Warn("lead_ready with incomplete slots", slots)
        signal = SignalContinue
    else if signal == SignalLeadReady && sess.LeadDone:       // a lead already fired this session
        if compactSlots(sess.Slots) == sess.leadSlots:
            signal = SignalContinue                           // spurious re-trigger (test-5_1)
        else:
            sess.leadSlots = compactSlots(sess.Slots)         // a real correction -> a fresh LeadRecord (11d)
    else if signal == SignalLeadReady:
        sess.LeadDone = true; sess.leadSlots = compactSlots(sess.Slots)
    if signal == SignalEscalate:
        sess.Escalated = true
        spoken = handoffLine(sessLang(sess))          // A3: escalate always speaks the fixed line
    // No continue->lead_ready upgrade. The model owns lead_ready; system.md
    // ties it to the read-back summary (REQ-DLG-04).
12. matched := titles of kb sections whose Body contains a query term   // log-only, informational
13. sess.History = trimTail(append(hist, Msg{"assistant", spoken}), HistoryLimit)
14. return Reply{Text: spoken, Signal: signal, Matched: matched}, nil
```

`sessLang(sess)` returns `sess.Lang` if set, else `"en"`. An `esc(sess)` early
return has `Matched: nil` — nothing was consulted.

### cmd/bot update loop   (B5)

One **serial worker goroutine per chat**, fed by a per-chat buffered channel,
so a chat's turns run in strict **arrival order** while different chats run
concurrently. (A per-chat `sync.Mutex` — the original B5 — serialises but does
NOT order: two quick messages race for the lock and ~half the time invert.
Fixed 2026-08-31, test-5_1.)

```
// sessionStore replaces a bare map: Load/Save/Delete/Close, backed by
// memory (default) or SQLite (SESSION_STORE=sqlite). Both implementations
// are copy-semantics — Load never returns a pointer aliased to stored
// state — so handleUpdate's single load/save pair (below) is the only
// place a mutation becomes durable.
type sessionStore interface {
    Load(chatID int64) (sess *Session, found bool, err error)
    Save(chatID int64, sess *Session) error
    Delete(chatID int64) error
    Close() error
}

sessions : sessionStore                 // memory (map[int64][]byte) or SQLite
inbox    : map[int64]chan Update         // per-chat FIFO; guarded by mu

main:
    cfg := LoadConfig()
    stt := stt.New(cfg); gen := dialog.New(cfg); tts := tts.New(cfg); tg := telegram.New(cfg)
    topics, topicIDs := loadTopics(cfg)                // >1 entry -> a picker is shown after /start
    updates := tg.Updates(ctx)                        // long-poll; the client owns offset advance,
                                                      //   advancing only after an Update is taken from the channel
    for u := range updates:
        dispatch(u)                                   // -> inbox[u.ChatID]; spawns chatWorker on first contact

chatWorker(ch):  for u := range ch { handleUpdate(u) }   // sequential, in order

handleUpdate(u Update):
    defer recover-and-log                             // a panic in one turn never kills the worker

    // single load/save point: Load here, a deferred Save below (skipped
    // only when the turn deleted the session, e.g. /reset). No per-turn
    // lock needed — the worker is the serialization.
    sess, found, err := sessions.Load(u.ChatID)
    if sess == nil { sess = &Session{Voice: "a"} }
    deleted := false
    defer func() { if !deleted { sessions.Save(u.ChatID, sess) } }()

    // slash commands (after greeting, after transcript) are handled here,
    // never reach dialog.Handle: "/voice a|b" switches sess.Voice;
    // "/reset" | "/clean" calls sessions.Delete + sets deleted=true — since
    // first-contact is derived from Load's found flag (not a separate
    // "seen" set), this also makes the greeting replay on the next message.
    // Any other "/..." gets a one-line /voice usage hint.

    // an inline-keyboard tap resolves before anything else — may be the
    // very first message this chat ever sends.
    if u.CallbackID != "":
        id := trimPrefix(u.CallbackData, "topic:")
        topic, valid := topics[id]
        if valid { sess.Topic = id }
        else { log("unknown topic callback", u.CallbackData) }
        tg.AnswerCallback(ctx, u.CallbackID)           // required, or the tap spinner never clears
        if valid { tg.SendText(ctx, u.ChatID, topic.Greeting) }
        return

    if u.IsStart || !found:
        if len(topics) > 1:
            sendTopicPicker(ctx, tg, u.ChatID, topicIDs, topics)   // one button per topic, in topicIDs order
            return                                    // can't answer this message without a topic pick
        for id, t := range topics { sess.Topic = id; tg.SendText(ctx, u.ChatID, t.Greeting) }  // exactly one entry
        if u.IsStart { return }                       // /start carries no other content

    // 1. transcript
    var text string
    if u.VoiceFileID != "":
        if stt == nil:                                 // STT_BACKEND=none
            tg.SendText(ctx, u.ChatID, voiceUnavailableLine(sessLang(sess)))
            return                                    // no download attempted, no turn record
        stopTicker := startRecordingTicker(ctx, tg, u.ChatID)   // re-sends every RecordingTick
        ogg, err := tg.DownloadVoice(ctx, u.VoiceFileID)
        if err == nil { text, err = stt.Transcribe(ctx, ogg, cfg.WhisperLang); os.Remove(ogg) }
        stopTicker()
        if err != nil || stt.IsNonSpeech(text):        // empty, or a Whisper hallucination on silence/cough
            tg.SendText(ctx, u.ChatID, sttFailLine(sessLang(sess)))   // "не розчув, повторіть / didn't catch that"
            return                                    // no Handle, no turn record
    else:
        text = u.Text

    // 2. slash commands — handled here, never reach Handle
    if text starts with "/":
        if lower(trim(text)) in {"/voice a","/voice b"}:
            sess.Voice = last char; tg.SendText(ctx, u.ChatID, voiceSwitched(sessLang(sess)))
        else if lower(trim(text)) in {"/reset","/clean"}:
            sessions.Delete(u.ChatID); deleted = true
            tg.SendText(ctx, u.ChatID, "Сесію очищено. / Session cleared.")
        else:  // "/voice", "/help", any unknown "/..."
            tg.SendText(ctx, u.ChatID, voiceHelp(sess))
        return

    // 3. topic gate — only reachable in multi-topic mode (single-topic mode
    // auto-assigns sess.Topic at first contact above, so this never fires there)
    if sess.Topic == "":
        if len(topics) > 1:
            sendTopicPicker(ctx, tg, u.ChatID, topicIDs, topics)   // re-show, don't guess
            return
        for id := range topics { sess.Topic = id }                // single-topic mode fallback
    topic := topics[sess.Topic]

    // 4. dialogue turn
    start := now()
    stopTicker := startRecordingTicker(ctx, tg, u.ChatID)
    reply, _ := dialog.Handle(ctx, sess, topic.KB, gen, topic.Sys, text, time.Now().In(a.loc))  // Handle never returns a non-nil error
    ogg, terr := tts.Speak(ctx, reply.Text, voiceID(cfg, sess.Voice), sessLang(sess))
    stopTicker()

    // 5. deliver
    if terr == nil { tg.SendVoice(ctx, u.ChatID, ogg) }          // no caption
    tg.SendText(ctx, u.ChatID, reply.Text)                       // text always goes out once

    // 6. log — always
    store.AppendTurn(cfg.DataDir, TurnRecord{... reply ..., LatencyMS: since(start)})
    if reply.Signal == SignalLeadReady:
        store.AppendLead(cfg.DataDir, leadFrom(u.ChatID, sess.Slots))
```

- **offset / dedup:** `telegram.Client.Updates` advances the `getUpdates`
  offset only after an update is delivered on the channel and pulled by a
  goroutine. A crash before that re-delivers the update → the greeting or a
  turn may repeat. Acceptable for a demo; a duplicate lead in the log is the
  worst case. Do **not** add persistence (D-7).
- **`startRecordingTicker`** returns a `stop func()`; it calls
  `SendRecordingAction` immediately and every `RecordingTick` until `stop()`
  or `ctx` cancellation.
- everything the loop calls that talks to a network (`SendText`, `SendVoice`,
  `Speak`, `Transcribe`) logs its error and continues — never a `panic`, never
  a process exit (REQ-NFR-03).

### detectLang(text string) string   (lingua-go; D-19)

```
if trimspace(text) == "" -> ""
lang, ok := lingua{Ukrainian, English, Russian}.DetectLanguageOf(text)
if !ok                          -> ""            // not enough signal ("ok", "5")
if lang == English              -> "en"
if lang in {Ukrainian, Russian} -> "uk"          // RU is out of scope + Whisper mis-hears uk as ru
else                            -> ""
```

`Handle` step 0 sets `sess.Lang` to this **on every confident turn**, so a
mid-dialogue uk↔en switch propagates; an inconclusive turn leaves it. It
feeds three things: the STT `langHint` (`cmd/bot` — voice follows the
conversation language once known), a soft `--- CONVERSATION LANGUAGE ---`
line in the system prompt (step 5), and `sessLang(sess)` for the fixed
handoff / apology / stt-fail / voice-switch lines (`sess.Lang` or `"en"`).
So an STT failure on turn 3 of a Ukrainian chat still gets a Ukrainian
handoff line. The *conversation* language stays the model's job (REQ-DLG-05);
the prompt hint only nudges and explicitly allows a clear switch.

**Why lingua-go and not the old Cyrillic/Latin heuristic (D-19):** for the
uk↔en case the two agree on every realistic message (we fold RU→uk anyway),
but lingua is right on mixed-script and abstains on too-short input. Cost:
lingua-go embeds all 75 language models unconditionally → **binary 10 MB →
136 MB**. Accepted — local demo, disk is cheap.

### Fixed lines

Package-level constant strings, one per language (`"uk"` / `"en"`), no
formatting:

| helper | used by | uk / en gist |
|---|---|---|
| `handoffLine(lang)` | every escalate path | the handoff wording from `examples/dialogues.md` cases 3–5 |
| `apologyLine(lang)` | `dialog.Handle` Generator error | "щось пішло не так, з'єдную з менеджером" / "something went wrong, connecting you to a manager" |
| `sttFailLine(lang)` | cmd/bot, STT error / empty transcript | "не розчув(ла), повторіть, будь ласка" / "sorry, I didn't catch that — could you repeat?" |
| `voiceSwitched(lang)` | cmd/bot, `/voice` command | "гаразд, тепер інший голос" / "ok, switched voice" |

Helpers the update loop needs: `voiceID(cfg, "a"|"b")` picks the ElevenLabs or
Azure voice ID for the active `TTS_BACKEND`; `greetingBody(path)` applies the
`prompt/greeting.md` extraction rule (content after the first `---` line,
further `---` lines dropped, trimmed); `leadFrom(chatID, slots)` builds a
`LeadRecord` (nil slots become `""`).

## Details / decisions

- **Telegram:** long-polling (`getUpdates`), no webhook / public URL. Use
  `github.com/go-telegram/bot` (maintained, std-context API). Two
  dependencies total — this and `lingua-go` (D-19, language detection);
  every HTTP client (Ollama / OpenAI / Gemini / ElevenLabs / Azure / the
  Telegram file download) is stdlib `net/http`.
- **STT (D-13, dual-mode):** dev / code default is `local` — shell out to the
  `openai-whisper` CLI (Val installed it via pipx, `whisper` on `PATH`):
  `whisper <ogg> --model $WHISPER_MODEL --task transcribe --output_format txt
  --output_dir <tmpdir> --fp16 False --verbose False` (add `--language uk|en`
  only when `WHISPER_LANG != auto`), then read `<tmpdir>/<ogg-stem>.txt`. The
  CLI decodes the ogg with its own internal ffmpeg call — no manual
  conversion. `$WHISPER_MODEL` is a model NAME (`turbo` default = large-v3-turbo:
  faster than `medium` on CPU AND better Ukrainian — benchmarked 2026-08-31 on a
  Ryzen 7700, turbo 12 s vs medium 15 s on a 4.6 s clip; `small` ~5 s but drops
  UA punctuation; `large-v3` if turbo quality is short), auto-downloaded to
  `~/.cache/whisper` on first use (one-time blocking download ~1.5 GB). Free, no key.
  **For the I-10 client recording, flip `STT_BACKEND=openai`:** the `openai`
  impl posts the ogg to `whisper-1` (~2–4 s, ~$0.006/min, < $2 total) — even at
  `turbo`, local CPU transcription (~12 s/turn, mostly Python + model load which
  the shell-out repeats every turn) fails NFR-2 in a live setting (B2 substance;
  its default-flip reverted, option A 2026-08-29 — OpenAI needs a $5 prepay,
  deferred to that step). Both impls
  satisfy `Transcribe(ctx, oggPath, langHint) (string, error)`; a failure does
  **not** auto-switch backends — only the `STT_BACKEND` env does.
- **Dialogue Generator (D-13; dual-mode per D-20):** `Generate(ctx, systemPrompt, msgs) (string, error)`,
  three impls, `DIALOG_BACKEND` picks. `ollama` + `openai` share
  `openai_compat.go` — one retry on a transient (transport error / 5xx / 429).
  - `ollama` (**dev default**) → `POST $OLLAMA_BASE_URL/v1/chat/completions` (OpenAI-compatible),
    `DIALOG_MODEL` default `gemma4:cloud`. Needs `ollama` logged in to a
    Pro/Max account. Free, good Ukrainian — but the cloud free tier's shared
    queue runs **13–86 s/turn** (D-20), fine while building, too slow for a
    live client demo.
  - `openai` → `api.openai.com`, `DIALOG_MODEL` default `gpt-4.1-mini`. The
    **client-facing artefact** (I-10, D-20): dedicated infra, ~2–5 s/turn,
    ~$0.01 per 10-turn conversation, shares the OpenAI key with the
    `whisper-1` STT flip. See `.env.client`.
  - `gemini` (last resort) → native `POST generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`,
    header `x-goog-api-key: $GEMINI_API_KEY`, body `{systemInstruction, contents,
    generationConfig:{temperature}}`, `DIALOG_MODEL` default `gemini-flash-latest`.
    Was the intended default (assumed covered by Google AI Pro) but the AI
    Studio project returns 429 "prepayment credits are depleted" on every model
    (2026-08-29) and needs a **$25 prepay** to enable — demoted to last resort.
    Promote back only if that is paid and a UA test then passes. Current
    concrete Flash model is `gemini-3.6-flash`; `gemini-flash-latest` is a
    maintained alias.
  Build `ollama` first (step 3); `openai` + `gemini` in the alternates batch (step 6).
- **KB in the prompt (B1):** the whole KB (~19 KB, bilingual — EN + UK block
  per section, D-18) is in every system prompt. No retrieval-for-context.
  `kbOverlap` is a keyword-overlap score used **only** by the grounding gate. Design lineage: `ragline`'s `Decide` (retrieve →
  score → answer-or-escalate) collapsed to "score-for-gate only" because the
  KB is tiny (not a dependency — see `docs/architecture.md` §6). Fallback if
  exact-match overlap misses inflected Ukrainian: add a stemmer (see the B1
  note in the Behavioural spec), do not bring per-section retrieval back.
- **Persona + playbook + grounding rule:** all in `prompt/system.md` —
  authored, load it verbatim, do not paraphrase into code. The six quote
  slots, the intake order, "never a final price", the escalate list, and the
  output-format spec all live there (NFR-7, NFR-9).
- **LLM output format:** single chat call, no function-calling. One JSON
  object `{"reply","slots","signal"}` — `reply` is the spoken text, `slots`
  all six keys (`null` when unknown), `signal` (`continue` | `lead_ready` |
  `escalate`). Missing / unparseable object or a blank `reply` → treat as
  `signal: escalate`, log it. Each backend forces valid JSON its own way (see
  parseResponse).
- **Slots:** the six keys from `prompt/system.md`
  (`language_pair, doc_type, volume, deadline, certification, delivery`) as
  `*string`. Merge only keys the model filled with a non-null value this turn;
  never clear a filled slot unless the user corrected it; log every change in
  the turn record.
- **History:** in-memory per chat ID, last 20 Msg entries (≈10 turns), dropped on restart.
- **Languages:** Ukrainian + English only (Q5 — no RU). System prompt tells the
  model to detect uk / en from the first message and stay in it, switching if
  the user does. One multilingual voice per provider covers both. A RU message
  is not tested or prompted for (the model will likely still answer).
- **TTS (D-15):** `elevenlabs` impl → `POST /v1/text-to-speech/{voice_id}`,
  `model_id: eleven_multilingual_v2`, `output_format: opus_48000_128`, header
  `xi-api-key`. `azure` impl → `POST {region}.tts.speech.microsoft.com/...v1`,
  SSML body, `X-Microsoft-OutputFormat: ogg-48khz-16bit-mono-opus`, key or
  10-min token. Same `Speak(ctx, text, voiceID, lang) ([]byte, error)`.
  Build ElevenLabs first (that's the demo path); Azure with the openai
  rollback batch (step 6).
  `tts.Spoken(text, lang) string` runs on the reply before `Speak` (not
  before `SendText`): drops markdown emphasis / list & heading markers,
  rewrites arrow shorthand (`UA ⇄ EN`, `uk→en`) to a dash, turns currency
  codes/symbols (`EUR`, `€`, `USD`, `$`) into the spoken word in the reply
  language, and spells out abbreviations Azure uk-UA mangles — `ПДВ` →
  "пе де ве" (Azure read it as "Проблем Дальнього Востока"), `ЄДРПОУ`,
  `NDA`, `EET` → "за київським часом". Backstop for `prompt/system.md`'s
  "write for the ear" rule.
- **Voices:** `VOICE_A` / `VOICE_B` per backend
  (`ELEVENLABS_VOICE_A`/`_B`, `AZURE_VOICE_A`/`_B`); `/voice b` swaps for that
  chat only. ElevenLabs Free during build → Starter before the client link.
  **Free API caveat:** library voices (incl. the Ukrainian ones) 402 on Free —
  dev uses premade Sarah (F) `EXAVITQu4vr4xnSDxMaL` + George (M)
  `JBFqnCBsd6RMkjVDRZzb`; the UA library pair is swapped in with the Starter
  upgrade for the client recording (D-15, same pattern as the STT flip).
- **Latency:** "recording voice" chat action while the STT→LLM→TTS chain runs
  (NFR-2). No streaming.
- **Config:** env only (`.env.example`); load `.env` if present with a small
  hand-rolled parser, not a dependency.
- **Errors / degradation:** see `docs/architecture.md` §8. Never crash the
  loop; recover panics per handler; on LLM/TTS failure fall back to a text
  apology + handoff.
- **Security:** validate the Telegram `file_path` before use; `os.CreateTemp`
  for downloaded audio, delete after transcription; never log full keys.

## Out of scope (say so if asked, don't build)

Real Zoho connection · telephony / phone numbers · real-time full-duplex voice
· any database (Postgres, SQLite, pgvector, FTS) · multi-tenant · persistence
across restart · auth · the ragivka framework · **Google Cloud TTS** (the
`Synthesizer` interface should accept it, but don't build the impl). The MVP
substrate (SQLite etc.) is in `docs/architecture.md` §7 — **not** this demo.

## Build order

1. **[done]** config + `kb` (load & split) + `store` + `go build` — no external calls
2. **[done]** `telegram` long-poll loop, echo text back
3. **[done]** `dialog`: `gate.go` (kbOverlap + hardEscalate + isSlotAnswer + gate) +
   `Generator` (`ollama` first) + JSON-reply parse + slot merge; onto text messages.
   Also: KB made bilingual (see the Bilingual KB note above).
4. **[done]** `stt`: `local` impl (shell the openai-whisper CLI — the dev default) — voice in.
   `cmd/bot`: recording-action ticker around STT + the dialogue turn.
5. **[done]** `tts`: `elevenlabs` impl — voice out; `/voice` command.
   Also: the greeting (`greeting.md` body once per chat, REQ-UX-02).
6. **[done]** alternates batch: `openai` + `gemini` for `dialog.Generator`
   (ollama + openai share `openai_compat.go`), `openai` (`whisper-1`) for
   `stt`, `azure` for `tts`. `newGenerator`/`newTranscriber`/`newSynthesizer`
   + config `validate()` gate each backend. Wire format live-verified via
   the real keys' quota errors (gemini 429, whisper-1 429 — not 400);
   azure is build + httptest only (no key in `.env`).
7. **[done]** README refresh + `.env.example` check + a short smoke-test doc
   (`docs/smoke-test.md`).

After each step: `make check` (gofmt + `go vet ./...` + `go test ./... -race`).
