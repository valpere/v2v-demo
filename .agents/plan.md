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
2. Voice → download OGG to a validated temp path → `stt.Transcribe` → transcript.
   Text → used directly. STT default = **local `whisper-cli`** (ogg→wav via
   `ffmpeg`); `STT_BACKEND=openai` switches to the `whisper-1` API (D-13).
3. `dialog.Handle(chatID, text)`:
   a. **Retrieve** — score the ~12 KB sections against the message
      (BM25-lite, in memory); keep those above a floor.
   b. **Grounding gate** — if the turn is a content question and nothing
      cleared the floor → escalate *without calling the LLM* (FR-12, NFR-9).
      If the turn is answering a pending slot question, skip the gate.
   c. **LLM call** via `dialog.Generator` (temp 0.2–0.3) — default =
      Ollama **`gemma4:cloud`** at `localhost:11434`; `DIALOG_BACKEND=openai`
      + `DIALOG_MODEL=gpt-4o-mini` is the rollback (D-13). System prompt =
      **`prompt/system.md`** (persona + conversation playbook + hard rules +
      output format) + `--- KNOWLEDGE BASE ---` + retrieved sections verbatim
      + `--- COLLECTED SO FAR ---` + slot-state JSON; messages = last ~10
      turns + this one.
   d. **Parse** the response: spoken reply text + a fenced JSON trailer
      `{ "slots": {…}, "signal": "continue" | "lead_ready" | "escalate" }`.
      Strip the trailer before speaking.
   e. **Merge** `slots` into the session — validate; never clear a filled
      slot unless the user explicitly corrected it.
   f. `escalate` → replace reply with the handoff line, mark session.
      `lead_ready` → append a lead record (Zoho-field shape).
4. Reply text → `tts.Speak` (session's current voice) → OGG/Opus. Default =
   **ElevenLabs `eleven_multilingual_v2`**; `TTS_BACKEND=azure` switches to
   Azure Neural (`uk-UA-*Neural`) (D-15).
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
examples/dialogues.md  worked example conversations — test material and the
                   recorded-sample script; optionally one short example as
                   few-shot if output consistency is poor
kb/translation-bureau.md  the fictional FromToBridge knowledge base
```

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
  - `ollama` (default) → `POST $OLLAMA_BASE_URL/v1/chat/completions` (OpenAI-
    compatible), `DIALOG_MODEL` default `gemma4:cloud`. Request `"think": false`
    if supported — keep latency down; the grounding rule carries the discipline.
  - `openai` → `api.openai.com`, `DIALOG_MODEL` default `gpt-4o-mini`.
  - `gemini` → native `POST generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`,
    header `x-goog-api-key: $GEMINI_API_KEY`, body `{systemInstruction, contents,
    generationConfig:{temperature}}`, `DIALOG_MODEL` default `gemini-flash-latest`.
    Covered by Val's Google AI Pro credits; candidate for default pending a UA test.
  Build `ollama` first (step 3); `openai` + `gemini` in the rollback batch (step 6).
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
- **History:** in-memory per chat ID, last 20 turns, dropped on restart.
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
3. `dialog`: `retrieve` + grounding gate + `Generator` (ollama impl first) +
   trailer parse + slot merge + turn log; wire onto text messages
4. `stt`: `local` impl (ffmpeg + whisper-cli) — voice in
5. `tts`: `elevenlabs` impl — voice out; `/voice` command
6. rollback batch: `openai` + `gemini` impls for `dialog.Generator`, `openai`
   for `stt`, `azure` for `tts`; verify `STT_BACKEND` / `DIALOG_BACKEND` /
   `TTS_BACKEND` switch cleanly
7. README refresh + `.env.example` check + a short smoke-test doc

After each step: `go build ./...`, `go vet ./...`, `go test ./... -race`.
