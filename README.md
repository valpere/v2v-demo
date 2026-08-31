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

UK + EN only (the KB carries both languages under each heading). Text
messages work too (STT skipped). `/voice b` swaps to the second voice for
comparison.

## Run

```bash
make env                  # cp .env.example .env — then fill TELEGRAM_BOT_TOKEN and ELEVENLABS_API_KEY
# dev path needs: the `openai-whisper` CLI + ffmpeg on PATH (local STT — first
# run downloads the model to ~/.cache/whisper), and `ollama` logged in to a
# Pro/Max account (gemma4:cloud). The default voice IDs (premade Sarah/George)
# work on the ElevenLabs Free plan.
go run ./cmd/bot
```

For the client-facing sample recording: flip `STT_BACKEND=openai`
(+ `OPENAI_API_KEY`) — local CPU transcription is too slow for a live demo —
and upgrade ElevenLabs to Starter to swap in the Ukrainian library voices
(Free rejects library voices via the API).

Other alternates (config only, no code change):
`DIALOG_BACKEND=openai|gemini` (+ `OPENAI_API_KEY` / `GEMINI_API_KEY`),
`TTS_BACKEND=azure` (+ `AZURE_SPEECH_KEY` / `AZURE_SPEECH_REGION`).

Then message the bot on Telegram.

## Status

In progress — build-order steps 1–4 done (config + KB + store; Telegram
long-poll; dialogue core on text messages — grounding gate, Ollama
`gemma4:cloud`, JSONL turn/lead log; local `openai-whisper` STT for voice
in). Voice out (ElevenLabs TTS + `/voice`) and the config-flag alternates
are steps 5–7. Tracked in `.agents/plan.md`; `make help` for dev tasks. Not
connected to Zoho, telephony, or any real RAG store; see the plan's "Out of
scope".
