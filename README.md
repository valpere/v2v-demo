# v2v-demo

A throwaway demo: a **Telegram voice assistant for a translation bureau**.
Neutral scenario, fabricated data (`kb/translation-bureau.md`). Built to let a
prospective client hear how the assistant sounds and holds a conversation —
not production code, not a framework.

## Loop

```
Telegram voice msg ─► local openai-whisper STT ─► whole KB + grounding gate ─► gemma4:cloud ─► ElevenLabs TTS ─► Telegram voice reply
                      alt: whisper-1 API (for the client recording)               alt: gpt-4o-mini / Gemini Flash   alt: Azure Neural
                                          │
                                          ├─ collects: language pair, doc type, volume,
                                          │            deadline, certification, delivery
                                          └─ escalates to a human on out-of-scope / on request
```

UK + EN only. Text messages work too (STT skipped). `/voice b` swaps to the
second voice for comparison.

## Run

```bash
cp .env.example .env      # TELEGRAM_BOT_TOKEN, ELEVENLABS_API_KEY + two voice IDs
# dev path needs: the `openai-whisper` CLI + ffmpeg on PATH (local STT — first
# run downloads the model to ~/.cache/whisper), and `ollama` logged in to a
# Pro/Max account (gemma4:cloud)
go run ./cmd/bot
```

For the client-facing sample recording, flip `STT_BACKEND=openai` (+ an
`OPENAI_API_KEY`) — local CPU transcription is too slow for a live demo.

Other alternates (config only, no code change):
`DIALOG_BACKEND=openai|gemini` (+ `OPENAI_API_KEY` / `GEMINI_API_KEY`),
`TTS_BACKEND=azure` (+ `AZURE_SPEECH_KEY` / `AZURE_SPEECH_REGION`).

Then message the bot on Telegram.

## Status

Scaffold only — implementation tracked in `.agents/plan.md`. Not connected to
Zoho, telephony, or any real RAG store; see the plan's "Out of scope".
