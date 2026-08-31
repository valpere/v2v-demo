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

| Concern | Env | Default | Alternate |
|--|--|--|--|
| Dialogue LLM | `DIALOG_BACKEND` | `ollama` (`gemma4:cloud`) | `openai` (`gpt-4o-mini`), `gemini` (`gemini-flash-latest`) |
| STT | `STT_BACKEND` | `local` (whisper CLI) | `openai` (`whisper-1` API) |
| TTS | `TTS_BACKEND` | `elevenlabs` | `azure` (`uk-UA-*Neural`) |

Each alternate needs its credential (`OPENAI_API_KEY`, `GEMINI_API_KEY`,
`AZURE_SPEECH_KEY` + `AZURE_SPEECH_REGION`); the bot refuses to start
otherwise. `gemini` is built but not fundable yet (429), `openai` is $0
balance — both are for the client-recording milestone.

**Client sample recording** (`docs/` I-10): `STT_BACKEND=openai` (+ prepay
OpenAI), ElevenLabs → Starter + UA library voice IDs, then record 2–3 min.

## Testing

- `make check` — gofmt + `go vet` + `go test ./... -race` (run after each change).
- `make audition ARGS="a"` — synthesise a UA test phrase, play it.
- **Manual smoke test:** `docs/smoke-test.md` — a scripted run through the
  quote flow, voice, `/voice`, escalation, and language switching. Turn and
  lead logs land in `data/*.jsonl`.

## Status

Build-order steps 1–7 complete (`.agents/plan.md`). Full text+voice loop,
all backends wired, `make check` green. Pending: the manual smoke round and
the I-10 client recording. Not connected to Zoho, telephony, or any real RAG
store — see the plan's "Out of scope".
