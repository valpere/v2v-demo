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
   `dialog.Handle(ctx, sess, kb, gen, systemPrompt, transcript)`.
3. `dialog.Handle` runs the sequence in **§"Behavioural spec (pseudocode)"**
   below — that section is authoritative; every step, constant, and edge case
   is spelled out there. LLM default `ollama` (`gemma4:cloud`);
   `DIALOG_BACKEND` switches to `openai` / `gemini` (D-13).
4. `Reply.Text` → `tts.Speak` (session voice) → OGG/Opus (default ElevenLabs
   `eleven_multilingual_v2`; `TTS_BACKEND=azure` — D-15).
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
                    prompt, parses the trailer, merges slots) · generator.go
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
}

type Client interface {
	Updates(ctx context.Context) (<-chan Update, error)      // long-poll loop
	DownloadVoice(ctx context.Context, fileID string) (oggPath string, err error)
	SendVoice(ctx context.Context, chatID int64, ogg []byte) error  // NO caption
	SendText(ctx context.Context, chatID int64, text string) error
	SendRecordingAction(ctx context.Context, chatID int64) error    // lasts ~5s server-side
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

// the fenced JSON block the model appends after the spoken reply
type trailer struct {
	Slots  QuoteSlots `json:"slots"`
	Signal Signal     `json:"signal"`
}

type Session struct {
	Slots     QuoteSlots
	History   []Msg  // trimmed to the last HistoryLimit (20) Msg entries ≈ 10 turns
	Voice     string // "a" | "b"; default "a"
	Lang      string // "uk" | "en"; last turn detectLang was confident (mid-switch propagates); "" until then
	Escalated bool
	LeadDone  bool   // a lead_ready already fired — later ones downgrade to continue (test-5_1)
}

type Generator interface {
	Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error)
}
// NewOllama(baseURL, model string) Generator   // default
// NewOpenAI(apiKey, model string) Generator
// NewGemini(apiKey, model string) Generator    // last resort — $25 prepay

type Reply struct {
	Text    string   // spoken text, trailer stripped (or the fixed handoff/apology line)
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

	STTBackend   string // "local" (default) | "openai" (I-10 client recording)
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
}
func LoadConfig() (Config, error)
// env + optional .env (tiny hand parser: KEY=VALUE lines, "#" comments, no
// quotes, no multiline, no "export "). Env→field map (the .env.example keys):
//   TELEGRAM_BOT_TOKEN TTS_BACKEND ELEVENLABS_API_KEY ELEVENLABS_VOICE_A/_B
//   AZURE_SPEECH_KEY AZURE_SPEECH_REGION AZURE_VOICE_A/_B
//   STT_BACKEND WHISPER_BIN WHISPER_MODEL WHISPER_LANG
//   DIALOG_BACKEND DIALOG_MODEL GEMINI_API_KEY OLLAMA_BASE_URL OPENAI_API_KEY
//   KB_PATH SYSTEM_PROMPT_PATH GREETING_PATH DATA_DIR
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
(gemma4:cloud / Gemini Flash / gpt-4o-mini all have the headroom).
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
return len(tokenize(userText)) <= SlotAnswerMaxTok
       && strings.HasSuffix(trimspace(lastAsst), "?")   // the bot just asked something
       && session.Slots has at least one nil field
```

So a short message only bypasses the gate when it is plausibly an answer to
the bot's own question — "5 pages" after "how many pages?" bypasses; a bare
"apostille?" out of nowhere does not.

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
if overlap < GateFloor   { return true }    // content question the KB barely covers -> escalate pre-LLM
return false
```

### parseTrailer(raw string) (spoken string, tr *trailer)

```
find the LAST fenced block whose opening fence is ```json / ```JSON / bare ```
    immediately followed by a line starting with "{"
if none:
    return trimspace(raw), nil
parse the block body as JSON into a trailer value
if json error OR trailer.Signal not in {continue, lead_ready, escalate}:
    return trimspace(raw with that fenced block removed), nil
spoken := trimspace(raw with that fenced block + trailing whitespace removed)
return spoken, &trailer
```

Lenient on the fence spelling, strict on the JSON: a malformed trailer is
`nil`, and Handle step 8 turns that into a fixed handoff line — the raw model
output is never spoken.

### Handle(ctx, sess, kb, gen, systemPrompt, userText) (Reply, error)

`esc(sess)` = shorthand for `{ sess.Escalated = true; return Reply{Text:
handoffLine(sessLang(sess)), Signal: SignalEscalate}, nil }`.

```
0. if l := detectLang(userText); l != "" { sess.Lang = l }   // updates every confident turn (mid-switch)
1. if hardEscalate(userText):  esc(sess)              // B3: unambiguous handoff trigger
2. slotAnswer := isSlotAnswer(sess, userText)
3. overlap    := kbOverlap(userText, kb)
4. if !isSmallTalk(userText) && groundingGate(overlap, slotAnswer):  esc(sess)  // content question the KB barely covers
5. sysPrompt := systemPrompt
              + "\n\n--- KNOWLEDGE BASE ---\n"  + join("\n\n", "## "+s.Title+"\n"+s.Body for s in kb)   // WHOLE KB
              + "\n\n--- COLLECTED SO FAR ---\n" + jsonCompact(sess.Slots)
              + (sess.Lang != "" ?  "\n\n--- CONVERSATION LANGUAGE ---\n<Ukrainian|English> so far; "
                                    + "stay in it unless the client clearly switches. Never reply in Russian."  : "")
6. hist := trimTail(append(sess.History, Msg{"user", userText}), HistoryLimit)
7. raw, err := gen.Generate(ctx, sysPrompt, hist)
   if err != nil:
       sess.Escalated = true
       return Reply{Text: apologyLine(sessLang(sess)), Signal: SignalEscalate}, nil   // degrade, never bubble
8. spoken, tr := parseTrailer(raw)
9. if tr == nil:  esc(sess)                           // NEVER speak untrailered raw output
10. merge — 6 explicit field assignments (QuoteSlots is a struct, no reflection):
       if tr.Slots.LanguagePair  != nil { sess.Slots.LanguagePair  = tr.Slots.LanguagePair }
       if tr.Slots.DocType       != nil { sess.Slots.DocType       = tr.Slots.DocType }
       … the other four. Never assign nil (that would clear a filled slot).
11. signal := tr.Signal
    if signal == SignalLeadReady && !sess.Slots.Complete():   // B4 guard
        log.Warn("lead_ready with incomplete slots", slots)
        signal = SignalContinue
    else if signal == SignalLeadReady && sess.LeadDone:       // a lead already fired this session
        signal = SignalContinue                               // (no duplicate LeadRecord; added test-5_1)
    else if signal == SignalLeadReady:
        sess.LeadDone = true
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
sessions : map[int64]*Session          // guarded by mu sync.Mutex (map access only)
seen     : map[int64]bool               // greeted this chat? guarded by mu
inbox    : map[int64]chan Update         // per-chat FIFO; guarded by mu

main:
    cfg := LoadConfig()
    stt := stt.New(cfg); gen := dialog.New(cfg); tts := tts.New(cfg); tg := telegram.New(cfg)
    kb  := kb.Load(cfg.KBPath); sys := readFile(cfg.SystemPromptPath); greet := greetingBody(cfg.GreetingPath)
    updates := tg.Updates(ctx)                        // long-poll; the client owns offset advance,
                                                      //   advancing only after an Update is taken from the channel
    for u := range updates:
        dispatch(u)                                   // -> inbox[u.ChatID]; spawns chatWorker on first contact

chatWorker(ch):  for u := range ch { handleUpdate(u) }   // sequential, in order

handleUpdate(u Update):
    defer recover-and-log                             // a panic in one turn never kills the worker
    sess := getOrCreateSession(u.ChatID)              // no per-turn lock — the worker is the serialization

    // slash commands (after greeting, after transcript) are handled here,
    // never reach dialog.Handle: "/voice a|b" switches sess.Voice; any other
    // "/..." gets a one-line /voice usage hint.

    if u.IsStart || firstMessage(u.ChatID):
        tg.SendText(ctx, u.ChatID, greet); markSeen(u.ChatID)
        if u.IsStart { return }                       // /start carries no other content

    // 1. transcript
    var text string
    if u.VoiceFileID != "":
        stopTicker := startRecordingTicker(ctx, tg, u.ChatID)   // re-sends every RecordingTick
        ogg, err := tg.DownloadVoice(ctx, u.VoiceFileID)
        if err == nil { text, err = stt.Transcribe(ctx, ogg, cfg.WhisperLang); os.Remove(ogg) }
        stopTicker()
        if err != nil || trimspace(text) == "":
            tg.SendText(ctx, u.ChatID, sttFailLine(sessLang(sess)))   // "не розчув, повторіть / didn't catch that"
            return                                    // no Handle, no turn record
    else:
        text = u.Text

    // 2. slash commands — handled here, never reach Handle
    if text starts with "/":
        if lower(trim(text)) in {"/voice a","/voice b"}:
            sess.Voice = last char; tg.SendText(ctx, u.ChatID, voiceSwitched(sessLang(sess)))
        else:  // "/voice", "/help", any unknown "/..."
            tg.SendText(ctx, u.ChatID, voiceHelp(sess))
        return

    // 3. dialogue turn
    start := now()
    stopTicker := startRecordingTicker(ctx, tg, u.ChatID)
    reply, _ := dialog.Handle(ctx, sess, kb, gen, sys, text)     // Handle never returns a non-nil error
    ogg, terr := tts.Speak(ctx, reply.Text, voiceID(cfg, sess.Voice), sessLang(sess))
    stopTicker()

    // 4. deliver
    if terr == nil { tg.SendVoice(ctx, u.ChatID, ogg) }          // no caption
    tg.SendText(ctx, u.ChatID, reply.Text)                       // text always goes out once

    // 5. log — always
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
- **Dialogue Generator (D-13):** `Generate(ctx, systemPrompt, msgs) (string, error)`,
  three impls, `DIALOG_BACKEND` picks:
  - `ollama` (default) → `POST $OLLAMA_BASE_URL/v1/chat/completions` (OpenAI-compatible),
    `DIALOG_MODEL` default `gemma4:cloud`. Request `"think": false` if supported.
    Needs `ollama` logged in to a Pro/Max account. Verified on a UA dialogue
    test (~5 s, fluent Ukrainian, correct grounding, valid JSON trailer).
  - `openai` → `api.openai.com`, `DIALOG_MODEL` default `gpt-4o-mini`. First
    alternate; shares the OpenAI key with the I-10 `whisper-1` STT flip.
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
  JSON-trailer spec all live there (NFR-7, NFR-9).
- **LLM output format:** single chat call, no function-calling. Reply text,
  then a fenced ```json trailer with `slots` (all six keys, `null` when
  unknown) + `signal` (`continue` | `lead_ready` | `escalate`). If the trailer
  is missing or unparseable → treat as `signal: escalate`, log it.
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
   `Generator` (`ollama` first) + trailer parse + slot merge; onto text messages.
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
