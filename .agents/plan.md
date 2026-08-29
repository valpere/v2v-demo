# ftb-demo — implementation plan

A throwaway demo for a prospective client (a translation bureau). Goal: let
them talk to a voice assistant on Telegram and judge conversation quality +
voice naturalness. **Neutral scenario, fabricated data.** Not production, not a
framework — the simplest thing that shows the loop.

Requirements (the *what* / *why*, with source traces) live in
`docs/requirements.md`. FR-/NFR-/D- IDs below refer to that file.

## What it does

1. User sends a **voice message** to the Telegram bot.
2. Bot downloads the OGG/Opus file, transcribes it (OpenAI `whisper-1`).
3. Transcript + rolling conversation history + a static knowledge base go to
   the dialogue LLM (`gpt-4o-mini`).
4. LLM replies naturally, asks the next clarifying question, and collects the
   quote parameters (see below).
5. Reply text → ElevenLabs TTS (multilingual v2) → bot sends a **voice
   message** back.
6. When the LLM decides the request is out of scope or the user asks for a
   person, it emits an escalation signal; the bot posts a fixed handoff line
   and logs the session.
7. Every turn (transcript, reply, matched-or-escalated, latency) is appended
   to a JSONL log — this is the "conversation history / quality monitoring"
   the client asked about, shown in miniature.

Text messages are also accepted (transcription step skipped) so the demo is
testable without voice.

## Quote parameters the assistant must collect

language pair · document type · volume (pages/words) · deadline ·
certification / notarization needed · delivery method

When all six are known, the assistant summarizes them back and says a manager
will send a quote — then emits the "lead ready" signal; the bot logs a
structured lead record (the shape that would become a Zoho lead).

## Package layout

```
cmd/bot/main.go        wiring: load config, construct clients, run the update loop
internal/telegram/     long-polling client (getUpdates), file download, sendVoice/sendMessage
internal/stt/          OpenAI Whisper client: (audio bytes, mime) -> transcript string
internal/tts/          ElevenLabs client: (text, voiceID) -> mp3/ogg bytes
internal/dialog/       system prompt build, history buffer, LLM call, signal parsing
internal/kb/           load KB_PATH into a string for the system prompt
internal/store/        append-only JSONL: turn log + lead records (DATA_DIR)
```

## Details / decisions

- **Telegram:** long-polling (`getUpdates`), no webhook / public URL. Use
  `github.com/go-telegram/bot` (maintained, std-context API) — or raw HTTP if
  the dep is a problem. One dependency max here.
- **Escalation / lead signals:** the dialogue LLM is instructed to end its
  reply with a control line on its own line: `((ESCALATE: reason))` or
  `((LEAD_READY))`. `internal/dialog` strips this line from the spoken text
  and returns it as a typed enum. Do NOT use OpenAI function-calling for the
  demo — keep it a single chat call.
- **History:** in-memory per chat ID, last N=20 turns, dropped on restart.
  Fine for a demo.
- **Languages:** the system prompt tells the model to detect the user's
  language (uk / ru / en) from the first message and stay in it, switching if
  the user switches. ElevenLabs multilingual v2 handles all three from one
  voice ID.
- **Voices:** `ELEVENLABS_VOICE_A` default; `/voice b` command in chat swaps
  to `ELEVENLABS_VOICE_B` for that chat. Lets the client compare two voices.
- **Latency:** send a Telegram "recording voice" chat action while the
  STT→LLM→TTS chain runs. No streaming needed for the demo.
- **Config:** all via env (see `.env.example`); load `.env` if present
  (`github.com/joho/godotenv` or a 20-line parser — prefer the parser).
- **Errors:** on any upstream failure, reply with a plain text apology and log
  the error; never crash the loop.
- **Security:** validate the Telegram file path before writing; write
  downloaded audio to a temp file with `os.CreateTemp`, delete after use.
  Never log full API keys.

## Out of scope (say so if asked, don't build)

Real Zoho connection · real telephony / phone numbers · vector DB / real RAG
(KB is just stuffed in the prompt) · multi-tenant · persistence across restart
· auth · the ragivka framework.

## Build order

1. config + kb + store (no external calls) + `go build`
2. telegram long-poll loop echoing text
3. dialog (LLM) on text messages, signal parsing, turn log
4. stt — voice in
5. tts — voice out, `/voice` command
6. README + `.env.example` check + a smoke test doc
