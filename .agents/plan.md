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
   `stt.Transcribe` → transcript (default local `whisper-cli`, `STT_BACKEND`
   switches to `whisper-1` — D-13).
2. `cmd/bot` starts the recording-action ticker, then calls
   `dialog.Handle(ctx, sess, kb, gen, systemPrompt, transcript)`.
3. `dialog.Handle` runs the sequence in **§"Behavioural spec (pseudocode)"**
   below — that section is authoritative; every step, constant, and edge case
   is spelled out there. LLM default `gemini` (`gemini-flash-latest`);
   `DIALOG_BACKEND` switches to `ollama` / `openai` (D-13).
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
internal/stt/       Transcriber interface; local.go (ffmpeg ogg->wav + shell
                    whisper-cli) · openai.go (whisper-1 API). STT_BACKEND picks.
internal/tts/       Synthesizer interface; elevenlabs.go (eleven_multilingual_v2,
                    output_format opus) · azure.go (SSML + ogg-opus header).
                    TTS_BACKEND picks. Google = documented, not built.
internal/kb/        load KB_PATH, split on "##" into titled sections
internal/dialog/    retrieve.go (BM25-lite + grounding gate) · dialog.go
                    (loads prompt/system.md, builds the prompt, parses the
                    trailer, merges slots) · generator.go (Generator interface)
                    · ollama.go + openai.go + gemini.go (DIALOG_BACKEND)
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
// NewLocal(bin, ggmlModel, lang string) Transcriber   // ffmpeg + whisper-cli
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
	Lang      string // "uk" | "en"; set from the first non-empty user turn, langOf fallback
	Escalated bool
}

type Generator interface {
	Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error)
}
// NewGemini(apiKey, model string) Generator
// NewOllama(baseURL, model string) Generator
// NewOpenAI(apiKey, model string) Generator

type Reply struct {
	Text    string   // spoken text, trailer stripped
	Signal  Signal   // continue | lead_ready | escalate
	Matched []string // titles of the KB sections used this turn
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

// retrieve.go
func retrieve(query string, kb []Section) (hits []Section, topScore float64) // BM25-lite
func groundingGate(topScore float64, isSlotAnswer bool) (forceEscalate bool)

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

	STTBackend   string // "local" | "openai"
	WhisperBin   string
	WhisperModel string
	WhisperLang  string // "auto" | "uk" | "en"

	DialogBackend string // "gemini" | "ollama" | "openai"
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
    ScoreFloor       = 0.15   // retrieve: min score to count as "relevant"
    TopK             = 3      // retrieve: max sections passed to the LLM
    TitleWeight      = 3.0    // retrieve: a query term in the title counts 3x
    HistoryLimit     = 20     // Session.History cap, in Msg entries ≈ 10 turns (NOT 20 turns)
    SlotAnswerMaxTok = 6      // isSlotAnswer: an answer to a slot question is short
)
```

### retrieve(query string, kb []Section) (hits []Section, topScore float64)

```
qterms  := tokenize(lower(strip_punct(query)))            // whitespace split
qterms  = drop(qterms, stopwords)                          // uk + en, see below
if len(qterms) == 0 { return nil, 0 }

for each sec in kb:
    body  := tokenize(lower(strip_punct(sec.Body)))
    title := tokenize(lower(strip_punct(sec.Title)))
    raw   := 0.0
    for each distinct t in qterms:
        tf := count(t, body) + TitleWeight*count(t, title)
        if tf > 0 { raw += 1 + ln(tf) }
    sec.score := raw / ln(2 + len(body))                   // length normalisation
sort kb by score desc
topScore := kb[0].score (or 0 if empty)
hits     := first min(TopK, n) sections with score >= ScoreFloor
return hits, topScore
```

- **stopwords** — a fixed ~40-word list of uk + en function words
  (`і а але або в на з до що як це так ні the a an of to in on for and or is
  are …`), a package-level `var stopwords = map[string]bool{…}`. Rationale:
  ragline found unfiltered stopwords cause any two docs to match on "the"/"of"
  (see `docs/architecture.md` §5). Keep the list in one place, no config.
- `count(t, tokens)` is exact-match token count, no stemming.
- Deterministic: same query + same KB always yields the same ordering (stable
  sort, ties broken by section index).

### isSlotAnswer(session *Session, userText string) bool

```
return len(tokenize(userText)) <= SlotAnswerMaxTok
       && session.Slots has at least one nil field
```

Cheap heuristic, deliberately loose: a short message while slots are still
being collected is treated as an answer, not a new content question, so the
grounding gate does not escalate on "5 pages" or "next week".

### groundingGate(topScore float64, slotAnswer bool) (forceEscalate bool)

```
if slotAnswer          { return false }   // let the LLM handle it with slot context
if topScore < ScoreFloor { return true }  // content question, nothing relevant -> escalate pre-LLM
return false
```

This is the whole anti-waffle mechanism: a content question the KB cannot
answer never reaches the Generator.

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

```
0. lang := langOf(userText); if lang != "" && sess.Lang == "" { sess.Lang = lang }
1. slotAnswer     := isSlotAnswer(sess, userText)
2. hits, topScore := retrieve(userText, kb)
3. if groundingGate(topScore, slotAnswer):
       sess.Escalated = true
       return Reply{Text: handoffLine(sessLang(sess)), Signal: SignalEscalate,
                    Matched: nil}, nil                // pre-LLM escalate: no sections were "used"
4. sysPrompt := systemPrompt
              + "\n\n--- KNOWLEDGE BASE ---\n"  + join("\n\n", "## "+hit.Title+"\n"+hit.Body for hit in hits)
              + "\n\n--- COLLECTED SO FAR ---\n" + jsonCompact(sess.Slots)
5. hist := trimTail(append(sess.History, Msg{"user", userText}), HistoryLimit)
6. raw, err := gen.Generate(ctx, sysPrompt, hist)
   if err != nil:
       sess.Escalated = true
       return Reply{Text: apologyLine(sessLang(sess)), Signal: SignalEscalate,
                    Matched: titles(hits)}, nil       // degrade, never bubble the error
7. spoken, tr := parseTrailer(raw)
8. if tr == nil:                                      // missing / unparseable trailer
       sess.Escalated = true
       return Reply{Text: handoffLine(sessLang(sess)), Signal: SignalEscalate,
                    Matched: titles(hits)}, nil       // NEVER speak untrailered raw output
9. merge (6 explicit field assignments, not a loop — QuoteSlots is a struct):
       if tr.Slots.LanguagePair  != nil { sess.Slots.LanguagePair  = tr.Slots.LanguagePair }
       if tr.Slots.DocType       != nil { sess.Slots.DocType       = tr.Slots.DocType }
       … and the other four. Never assign nil (that would clear a filled slot).
10. signal := tr.Signal                               // trust the model
    if signal == SignalEscalate:
        sess.Escalated = true
        spoken = handoffLine(sessLang(sess))          // A3: replace the spoken text
    // NOTE: no continue->lead_ready upgrade. The model owns lead_ready and the
    //       system prompt ties it to the read-back summary (REQ-DLG-04). See B4.
11. sess.History = trimTail(append(hist, Msg{"assistant", spoken}), HistoryLimit)
12. return Reply{Text: spoken, Signal: signal, Matched: titles(hits)}, nil
```

`sessLang(sess)` returns `sess.Lang` if set, else `"en"`.

The caller — the `cmd/bot` update loop — owns everything around `Handle`:
detecting `/start` / an unseen chat and sending `prompt/greeting.md` once;
the STT step and its error/empty-transcript handling; the recording-action
ticker; `tts.Speak` + `telegram.SendVoice` then `telegram.SendText` (one
text, no caption); `store.AppendTurn` **always**; `store.AppendLead` iff
`Reply.Signal == SignalLeadReady`; `getUpdates` offset management and
per-chat serialization. **That loop still needs its own pseudocode** —
flagged by the consilium (B5), pending Val's calls on B1–B5.

### langOf(text string) string

```
if text has any Cyrillic rune -> "uk"   (RU is out of scope; Cyrillic == uk here)
else if text has any Latin letter -> "en"
else -> ""                               (digits/punctuation only: undetermined)
```

Used only to pick the fixed handoff / apology line, via `sessLang(sess)`
which prefers `Session.Lang` (locked from the first non-empty turn, step 0)
and falls back to `"en"` only when nothing was ever detected. So an STT
failure on turn 3 of a Ukrainian chat still gets a Ukrainian handoff line.
The *conversation* language is the model's job (REQ-DLG-05), not this
function's.

### Fixed lines

`handoffLine(lang)` and `apologyLine(lang)` return a package-level constant
string per language. The handoff wording is the one in
`examples/dialogues.md` cases 3–5; the apology is a short "щось пішло не так,
з'єдную з менеджером" / "something went wrong, connecting you to a manager".

## Details / decisions

- **Telegram:** long-polling (`getUpdates`), no webhook / public URL. Use
  `github.com/go-telegram/bot` (maintained, std-context API) — the one Go
  dependency. Everything else is stdlib `net/http`.
- **STT (D-13):** default `local` — `ffmpeg -i in.ogg -ar 16000 -ac 1 out.wav`
  then `whisper-cli -m $WHISPER_MODEL -l $lang -otxt -nt -f out.wav`, read the
  `.txt`. `WHISPER_BIN`, `WHISPER_MODEL` (ggml path), `WHISPER_LANG` (auto|uk|en).
  `openai` impl posts the ogg to `whisper-1`. Both satisfy
  `Transcribe(ctx, oggPath, langHint) (string, error)`; a `local` failure does
  **not** auto-fallback to `openai` — the switch is the `STT_BACKEND` env only
  (keep it predictable for a demo).
- **Dialogue Generator (D-13):** `Generate(ctx, systemPrompt, msgs) (string, error)`,
  three impls, `DIALOG_BACKEND` picks:
  - `gemini` (default) → native `POST generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`,
    header `x-goog-api-key: $GEMINI_API_KEY`, body `{systemInstruction, contents,
    generationConfig:{temperature}}`, `DIALOG_MODEL` default `gemini-flash-latest`.
    Covered by a Google AI Pro subscription's API credits. Confirm on a UA
    dialogue test before it stays default; fall back to `ollama` if it regresses.
  - `ollama` → `POST $OLLAMA_BASE_URL/v1/chat/completions` (OpenAI-compatible),
    `DIALOG_MODEL` default `gemma4:cloud`. Request `"think": false` if supported.
  - `openai` → `api.openai.com`, `DIALOG_MODEL` default `gpt-4o-mini`.
  Build `gemini` first (step 3); `ollama` + `openai` in the alternates batch (step 6).
- **Retrieval:** in memory, no DB. Lowercase + tokenize; score each section by
  term frequency / overlap (BM25-lite is fine — ~12 short sections). Keep top
  ≤3 above a tuned floor. This exists mainly to *drive the escalate decision*
  and to keep the LLM's context tight (NFR-9); it is deliberately minimal.
  Design reference: `ragline`'s `internal/answer/decision.go` (not a
  dependency — see `docs/architecture.md` §6).
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
  chat only. ElevenLabs Free plan during build → Starter before the client
  link.
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

1. config + `kb` (load & split) + `store` + `go build` — no external calls
2. `telegram` long-poll loop, echo text back
3. `dialog`: `retrieve` + grounding gate + `Generator` (`gemini` impl first) +
   trailer parse + slot merge + turn log; wire onto text messages
4. `stt`: `local` impl (ffmpeg + whisper-cli) — voice in
5. `tts`: `elevenlabs` impl — voice out; `/voice` command
6. alternates batch: `ollama` + `openai` impls for `dialog.Generator`,
   `openai` for `stt`, `azure` for `tts`; verify `STT_BACKEND` /
   `DIALOG_BACKEND` / `TTS_BACKEND` switch cleanly
7. README refresh + `.env.example` check + a short smoke-test doc

After each step: `go build ./...`, `go vet ./...`, `go test ./... -race`.
