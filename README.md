# v2v-demo

A throwaway demo: a **Telegram voice assistant for a translation bureau**.
Neutral scenario, fabricated data (`kb/translation-bureau.md`). Built to let a
prospective client hear how the assistant sounds and holds a conversation —
not production code, not a framework.

## Loop

```
Telegram voice msg ─► local Whisper STT ─► retrieve KB + grounding gate ─► Gemini Flash ─► ElevenLabs TTS ─► Telegram voice reply
                      alt: whisper-1 API                                 alt: gemma4:cloud / gpt-4o-mini   alt: Azure Neural
                                             │
                                             ├─ collects: language pair, doc type, volume,
                                             │            deadline, certification, delivery
                                             └─ escalates to a human on out-of-scope / on request
```

UK + EN only. Text messages work too (STT skipped). `/voice b` swaps to the
second voice for comparison.

## Run

```bash
cp .env.example .env      # TELEGRAM_BOT_TOKEN, GEMINI_API_KEY, ELEVENLABS_API_KEY + two voice IDs, WHISPER_MODEL
# default path needs: ffmpeg, a whisper.cpp `whisper-cli` binary + a ggml model
go run ./cmd/bot
```

Alternates (config only, no code change): `STT_BACKEND=openai`,
`DIALOG_BACKEND=ollama|openai` (+ `OLLAMA_BASE_URL` / `OPENAI_API_KEY`),
`TTS_BACKEND=azure` (+ `AZURE_SPEECH_KEY` / `AZURE_SPEECH_REGION`).

Then message the bot on Telegram.

## Status

Scaffold only — implementation tracked in `.agents/plan.md`. Not connected to
Zoho, telephony, or any real RAG store; see the plan's "Out of scope".
