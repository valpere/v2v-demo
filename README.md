# v2v-demo

A throwaway demo: a **Telegram voice assistant for a translation bureau**.
Neutral scenario, fabricated data (`kb/translation-bureau.md`, bilingual
UK/EN). Built to let a prospective client hear how the assistant sounds and
holds a conversation — not production code, not a framework. Design notes:
`docs/`, `.agents/plan.md`.

## Loop

```
Telegram voice / text ─► STT ─► whole KB + grounding gate ─► LLM ─► TTS ─► voice + text reply
                         │      │                            │        │
                    whisper CLI │  hardEscalate / kbOverlap   gemma4   ElevenLabs
                    (whisper-1) │  / small-talk / slot bypass  :cloud   (Azure)
                                │
                                ├─ collects 6 quote params: language pair, doc type,
                                │  volume, deadline, certification, delivery
                                ├─ lead_ready → one JSONL LeadRecord (Zoho-shaped, sent nowhere)
                                └─ hands off to a human on out-of-scope / liability / on request
```

UK + EN only. `detectLang` (lingua-go) tracks the conversation language and
feeds it to STT, the prompt, and the fixed lines; a mid-dialogue switch is
followed. Text messages skip STT. `/voice a|b` swaps the TTS voice.

## Run

```bash
make env      # cp .env.example .env
# then set TELEGRAM_BOT_TOKEN and ELEVENLABS_API_KEY (Text-to-Speech permission)
make run      # go run ./cmd/bot — long-poll, no webhook; then DM the bot
```

Dev path needs:
- `whisper` (openai-whisper CLI) + `ffmpeg` on PATH — first voice message
  downloads `~/.cache/whisper/large-v3-turbo.pt` (~1.5 GB), so the first turn
  is slow;
- `ollama` logged in to a Pro/Max account (`gemma4:cloud`);
- ElevenLabs **Free** is enough — the default voice IDs are premade Sarah /
  George (Free rejects library voices, incl. the Ukrainian ones, with 402).

## Backends (config only, no code change)

| Concern | Env | Dev default | Client artefact / alternate |
|--|--|--|--|
| Dialogue LLM | `DIALOG_BACKEND` | `ollama` (`gemma4:cloud`) | `openai` (`gpt-4.1-mini`) — D-20, for latency + rule-following; `gemini` (`gemini-flash-latest`, last resort) |
| STT | `STT_BACKEND` | `local` (whisper CLI) | `openai` (`whisper-1` API) |
| TTS | `TTS_BACKEND` | `elevenlabs` + premade voices | `elevenlabs` + Starter UA voices; `azure` (`uk-UA-*Neural`); `none` (text-only, for bulk smoke-testing) |

Each alternate needs its credential (`OPENAI_API_KEY`, `GEMINI_API_KEY`,
`AZURE_SPEECH_KEY` + `AZURE_SPEECH_REGION`); the bot refuses to start
otherwise.

**Client sample recording** (I-10): the dev defaults are free but slow
(`gemma4:cloud` on the Ollama free tier: 13–86 s/turn). Copy **`.env.client`**
in — it flips STT + dialogue + the UA library voices together — after a
prepaid OpenAI key ($5) and ElevenLabs Starter ($6), then record 2–3 min.

## Testing

- `make check` — gofmt + `go vet` + `go test ./... -race` (run after each change).
- `make audition ARGS="a"` — synthesise a UA test phrase, play it.
- **Manual smoke test:** `docs/smoke-test.md` — a scripted run through the
  quote flow, voice, `/voice`, escalation, and language switching. Turn and
  lead logs land in `data/*.jsonl`.

## Status

Build-order steps 1–7 complete (`.agents/plan.md`). Full text+voice loop,
all backends wired, `make check` green. The manual smoke test (`docs/smoke-test.md`
§1–18) is done — on the dev stack and re-verified on the **client stack**
(`whisper-1` + `gpt-4.1-mini` + ElevenLabs Starter UA voices; `.env.client`).
The I-10 sample is built (`minions/i10-sample` → `tmp/i10-sample.mp3`).
Left: hand it to the client; an always-on host is deferred (D-08). Not
connected to Zoho, telephony, or any real RAG store — see the plan's
"Out of scope".
