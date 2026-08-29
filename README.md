# v2v-demo

A throwaway demo: a **Telegram voice assistant for a translation bureau**.
Neutral scenario, fabricated data (`kb/translation-bureau.md`). Built to let a
prospective client hear how the assistant sounds and holds a conversation —
not production code, not a framework.

## Loop

```
Telegram voice msg ─► whisper-1 STT ─► whole KB + grounding gate ─► gemma4:cloud ─► ElevenLabs TTS ─► Telegram voice reply
                      alt: local whisper.cpp                        alt: gpt-4o-mini / Gemini Flash   alt: Azure Neural
                                          │
                                          ├─ collects: language pair, doc type, volume,
                                          │            deadline, certification, delivery
                                          └─ escalates to a human on out-of-scope / on request
```

UK + EN only. Text messages work too (STT skipped). `/voice b` swaps to the
second voice for comparison.

## Run

```bash
cp .env.example .env      # TELEGRAM_BOT_TOKEN, OPENAI_API_KEY, ELEVENLABS_API_KEY + two voice IDs
# default path needs: an OpenAI key (whisper-1 STT) and `ollama` logged in to a
# Pro/Max account (gemma4:cloud dialogue)
go run ./cmd/bot
```

Alternates (config only, no code change): `STT_BACKEND=local`
(+ ffmpeg, a `whisper-cli` binary, a ggml model),
`DIALOG_BACKEND=openai|gemini` (+ `OPENAI_API_KEY` / `GEMINI_API_KEY`),
`TTS_BACKEND=azure` (+ `AZURE_SPEECH_KEY` / `AZURE_SPEECH_REGION`).

Then message the bot on Telegram.

## Status

Scaffold only — implementation tracked in `.agents/plan.md`. Not connected to
Zoho, telephony, or any real RAG store; see the plan's "Out of scope".
