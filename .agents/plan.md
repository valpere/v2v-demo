# v2v-demo — implementation plan

A throwaway demo for a prospective client (a translation bureau). Goal: let
them talk to a voice assistant on Telegram and judge conversation quality +
voice naturalness. **Neutral scenario, fabricated data.** Not production, not a
framework — the simplest thing that shows the loop.

Requirements (the *what* / *why*, with source traces) live in
`docs/requirements.md`; the component / data-flow / grounding design is in
`docs/architecture.md` and is authoritative for *shape*. This file is the
file-by-file *how*. FR-/NFR-/D- IDs refer to the requirements file.

## What it does (one turn)

1. User sends a **voice** (or text) message to the Telegram bot.
2. Voice → download OGG to a validated temp path → `whisper-1` → transcript.
   Text → used directly.
3. `dialog.Handle(chatID, text)`:
   a. **Retrieve** — score the ~12 KB sections against the message
      (BM25-lite, in memory); keep those above a floor.
   b. **Grounding gate** — if the turn is a content question and nothing
      cleared the floor → escalate *without calling the LLM* (FR-12, NFR-9).
      If the turn is answering a pending slot question, skip the gate.
   c. **LLM call** (`gpt-4o-mini`, temp 0.2–0.3) — system prompt = persona +
      the hard grounding rule + retrieved sections verbatim + current slot
      state; messages = last ~10 turns + this one.
   d. **Parse** the response: spoken reply text + a fenced JSON trailer
      `{ "slots": {…}, "signal": "continue" | "lead_ready" | "escalate" }`.
      Strip the trailer before speaking.
   e. **Merge** `slots` into the session — validate; never clear a filled
      slot unless the user explicitly corrected it.
   f. `escalate` → replace reply with the handoff line, mark session.
      `lead_ready` → append a lead record (Zoho-field shape).
4. Reply text → ElevenLabs multilingual v2 (session's current voice) → OGG.
5. Telegram: `sendVoice` + `sendMessage` with the same text (so it's readable).
6. Append a turn record to the JSONL log: transcript, reply, signal, matched
   section titles, latency (FR-13).

## Quote parameters (FR-7)

`language pair · document type · volume · deadline · certification/notarization
· delivery`. Session holds them as six optional fields; "lead ready" = all six
set. No FSM — the state *is* which fields are still nil, and the LLM is
prompted to ask about exactly those.

## Package layout

```
cmd/bot/main.go     wiring: config, clients, per-chat session map, the update loop
internal/telegram/  long-poll getUpdates, file download, sendVoice/sendMessage,
                    "recording voice" chat action, /voice a|b
internal/stt/       OpenAI whisper-1: (audio bytes, mime) -> transcript
internal/tts/       ElevenLabs multilingual v2: (text, voiceID) -> ogg bytes
internal/kb/        load KB_PATH, split on "##" into titled sections
internal/dialog/    retrieve.go (BM25-lite + grounding gate) · dialog.go
                    (prompt build, LLM call, trailer parse, slot merge)
internal/store/     append-only JSONL: turn records + lead records (DATA_DIR)
```

## Details / decisions

- **Telegram:** long-polling (`getUpdates`), no webhook / public URL. Use
  `github.com/go-telegram/bot` (maintained, std-context API) — the one
  dependency. Everything else is stdlib `net/http`.
- **Retrieval:** in memory, no DB. Lowercase + tokenize; score each section by
  term frequency / overlap (BM25-lite is fine — ~12 short sections). Keep top
  ≤3 above a tuned floor. This exists mainly to *drive the escalate decision*
  and to keep the LLM's context tight (NFR-9); it is deliberately minimal.
  Design reference: `ragline`'s `internal/answer/decision.go` (not a
  dependency — see `docs/architecture.md` §6).
- **Grounding rule (in the system prompt):** "Answer only from the KNOWLEDGE
  BASE below. If the user asks something it does not cover, do not improvise —
  return `signal: escalate`. Never state a final price; say a manager will
  confirm." (NFR-7, NFR-9.)
- **LLM output format:** single chat call, no function-calling. Reply text,
  then a fenced ```json trailer with `slots` + `signal`. If the trailer is
  missing or unparseable → treat as `signal: escalate`, log it.
- **Slots:** `*string` (or small enums where the KB constrains values —
  e.g. certification ∈ {none, certified, notarized}). Merge only keys the LLM
  sent; log every slot change in the turn record.
- **History:** in-memory per chat ID, last 20 turns, dropped on restart.
- **Languages:** system prompt tells the model to detect uk / ru / en from the
  first message and stay in it, switching if the user does. One ElevenLabs
  multilingual voice ID covers all three. (Scope of RU — see requirements Q5.)
- **Voices:** `ELEVENLABS_VOICE_A` default; `/voice b` swaps to
  `ELEVENLABS_VOICE_B` for that chat only.
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
across restart · auth · the ragivka framework. The MVP substrate (SQLite etc.)
is in `docs/architecture.md` §7 — **not** this demo.

## Build order

1. config + `kb` (load & split) + `store` + `go build` — no external calls
2. `telegram` long-poll loop, echo text back
3. `dialog`: `retrieve` + grounding gate + LLM call + trailer parse + slot
   merge + turn log; wire onto text messages
4. `stt` — voice in
5. `tts` — voice out; `/voice` command
6. README refresh + `.env.example` check + a short smoke-test doc

After each step: `go build ./...`, `go vet ./...`, `go test ./... -race`.
