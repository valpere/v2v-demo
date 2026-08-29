# ftb-demo

A throwaway demo: a **Telegram voice assistant for a translation bureau**.
Neutral scenario, fabricated data (`kb/translation-bureau.md`). Built to let a
prospective client hear how the assistant sounds and holds a conversation —
not production code, not a framework.

## Loop

```
Telegram voice msg → Whisper STT → gpt-4o-mini (+ KB + history) → ElevenLabs TTS → Telegram voice reply
                                          │
                                          ├─ collects: language pair, doc type, volume,
                                          │            deadline, certification, delivery
                                          └─ escalates to a human on out-of-scope / on request
```

Text messages work too (STT skipped). `/voice b` swaps to the second voice for
comparison.

## Run

```bash
cp .env.example .env      # fill TELEGRAM_BOT_TOKEN, OPENAI_API_KEY, ELEVENLABS_API_KEY, two voice IDs
go run ./cmd/bot
```

Then message the bot on Telegram.

## Status

Scaffold only — implementation tracked in `.agents/plan.md`. Not connected to
Zoho, telephony, or any real RAG store; see the plan's "Out of scope".
