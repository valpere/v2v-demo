# HANDOFF — v2v-demo build session

Read this first, then `AGENTS.md`, then `.agents/plan.md`. Snapshot: **2026-08-31**.

---

## 1. What this is

A throwaway Telegram voice assistant for a fictional translation bureau
("FromToBridge"), built to let a prospective client judge conversation quality
and voice naturalness. Not production, not a framework. Commercial context
(fee, deadline, client) is in `.engage/` (gitignored) and the engagement repo
`~/wrk/freelance/freelancehunt/projects/1649832-розробник-ai-агента/` — you do
not need it to build.

**Authoritative docs, in order:**
- `.agents/plan.md` — file-by-file *how*, the Type surface, and the
  **Behavioural spec (pseudocode)** which is authoritative for every step,
  constant, and edge case. Build order is at the bottom.
- `docs/requirements.md` — the *what/why*, tumanomir notation, 42 traced
  `[REQ-*]` items, `@schema` config/data blocks.
- `docs/architecture.md` — components, turn flow, the 4-layer grounding
  mechanism, MVP evolution.
- `AGENTS.md` — coding conventions and non-goals.

---

## 2. Current status

Snapshot: **2026-08-31** (build-order steps 1–7 done — the manual smoke
round and the I-10 recording are what's left).

| | |
|--|--|
| Prereqs / accounts | **done** — see §4 |
| `.env` | **complete** for the dev path |
| Step 1 — config + `kb` + `store` | **done** (`f7f84f5`) |
| Step 2 — `telegram` long-poll + echo | **done** (`b6324d1`) — verified live |
| Step 3 — `dialog` core on text messages | **done** — gate + Ollama generator + trailer parse + slot merge + JSONL log; live gemma4:cloud smoke passed |
| KB | now **bilingual** (EN + UK block per `## ` section, ~19 KB, D-18) — fixed the cross-language grounding gate; see §3 |
| Step 4 — `stt` local (openai-whisper CLI) | **done** — voice → DownloadVoice → Transcribe → dialog.Handle; recording-action ticker; live `tiny` smoke on a real .ogg passed |
| Step 5 — `tts` ElevenLabs + greeting + `/voice` | **done** — `Speak` → Ogg/Opus mono (live smoke: HTTP 200 on Free, `opus_48000_128`); `SendVoice` + `SendText`; greeting once per chat; `/voice a\|b`. Full text+voice loop runs. |
| Step 5 fixes (Val's live test `tmp/test-5_1`) | **done** — per-chat FIFO worker (message-order bug); `isSmallTalk` gate bypass (greetings/thanks no longer escalate); bare/unknown `/…` handled in cmd/bot; `WHISPER_LANG` default → `uk` + system.md "RU → reply uk" (STT was drifting uk→ru); `Session.LeadDone` (no duplicate lead). See `.agents/changes.md`. |
| Step 6 — alternates | **done** — `dialog` openai + gemini generators (openai_compat.go shared), `stt` whisper-1, `tts` azure. Backend switch = `newGenerator/Transcriber/Synthesizer` + config `validate()`. Wire format live-verified via quota errors (gemini/whisper-1 real keys → 429, not 400); azure build+httptest only (no key). |
| Step 7 — README + smoke doc | **done** — `README.md` refreshed, `.env.example` checked against `config.go`, `docs/smoke-test.md` written (the scripted manual run). |
| Live smoke round | **done** (2026-08-31) — CDP-driven `web.telegram.org/a/` (`tmp/tgdrive/`, burner acct). Greeting, quote→lead_ready (clean LeadRecord), voice-out, gate/hardEscalate/small-talk, uk↔en switch, ru→uk, `/voice`. Fixed a real gate bug: normal Ukrainian was escalating (`isSlotAnswer` trailing-`?` too strict; `kbOverlap` missed inflection) → `contains "?"` + prefix/stem match (commit `055544f`). Voice-IN untested (web uploads `.ogg` as a file). |
| Left | alternate backends once balances are funded (§8) · the I-10 client recording (§8) |
| `make check` (gofmt + vet + `go test -race`) | clean |
| minions-curator | wired — `.claude/settings.local.json` hooks + `.claude/skills/curate-minions/` (commit `89fbf80`); active next session |
| Deps | `github.com/go-telegram/bot` v1.24.0; `github.com/pemistahl/lingua-go` v1.4.0 (language detection — D-19; +~126 MB binary, accepted) |

**Your task:** the build order and a live smoke round are done. What's left
is the I-10 client recording (§8) once OpenAI/ElevenLabs are funded, and a
re-check of the openai/gemini/azure backends against real balances.
After each step: `make check` (`make help` lists all targets). Build-time
notes / deviations live in `.agents/changes.md` (gitignored).

---

## 3. Decisions already settled — do NOT relitigate

These are baked into `.agents/plan.md` and `docs/`. Fuller rationale in the
engagement repo's `docs/10-decisions.md` (D-01…D-18) if you need the "why".
D-18 = the bilingual KB (added at build step 3 — see §3 Grounding).

- **Dialogue LLM:** default `ollama` `gemma4:cloud`. Backend order
  `ollama → openai (gpt-4o-mini) → gemini`. Gemini is **last resort** — its
  API needs a $25 prepay that is not paid (429 "prepayment credits depleted");
  build the `gemini` impl but do not expect to run it.
- **STT is dual-mode:**
  - Dev / default: `STT_BACKEND=local` → the **openai-whisper CLI** (`whisper`
    on PATH, installed via pipx). `WHISPER_MODEL=turbo` (= large-v3-turbo:
    faster than `medium` on CPU AND better Ukrainian — bench 2026-08-31,
    Ryzen 7700: turbo 12 s vs medium 15 s on a 4.6 s clip), auto-downloads to
    `~/.cache/whisper` (~1.5 GB) on the first voice message. Ingests `.ogg`
    directly — **no manual ffmpeg ogg→wav step** (ffmpeg is still a system
    dep, used internally by whisper). Command shape is in `.agents/plan.md`
    STT decisions bullet.
  - `STT_BACKEND=openai` (`whisper-1` API) is a **mandatory flip for the I-10
    client sample recording only** — local CPU latency fails the <10 s target
    live. It is a later milestone, not part of the build.
- **TTS:** ElevenLabs `eleven_multilingual_v2`, `TTS_BACKEND=elevenlabs`
  default, `azure` (uk-UA-*Neural) the rollback.
  - Dev voices are the premade **Sarah** (`EXAVITQu4vr4xnSDxMaL`, A) and
    **George** (`JBFqnCBsd6RMkjVDRZzb`, B) — the ElevenLabs **Free** API
    rejects library voices (incl. the Ukrainian ones) with HTTP 402.
  - The auditioned UA library voices (`2OXYbN1uGomXXJtv9Dq6` /
    `4nLP0u2B3yI0lyzATFnN`, kept as comments in `.env`) swap in **only** when
    Starter is activated for the client recording.
- **Grounding (B1):** the **whole KB goes into every system prompt** (~19 KB,
  **bilingual** — an EN block and a UK block under each `## ` heading; this is
  what lets `kbOverlap` score a Ukrainian query — D-18, resolved step 3, see
  `.agents/changes.md`). There is **no retrieval-for-context**. `kbOverlap` is
  a keyword score used *only* by the grounding gate (`< GateFloor` on a content
  question → escalate before the LLM). A Ukrainian stemmer is the documented
  fallback if within-Ukrainian inflection proves too weak — **do not
  reintroduce per-section retrieval.**
- **B3** `isSlotAnswer` requires "the bot just asked (with a ?)"; a
  `hardEscalate` keyword list runs before the slot-answer bypass; `isSmallTalk`
  (greeting/thanks/farewell) also bypasses the gate (test-5_1).
  **B4** `lead_ready` while `!Slots.Complete()` → `continue` + warn; a second
  `lead_ready` after one already fired (`Session.LeadDone`) → `continue`.
  **B5** `cmd/bot`: one **serial worker goroutine per chat** fed by a per-chat
  channel — strict arrival order (a per-chat mutex serialised but didn't order;
  fixed test-5_1). Any `/…` message is handled in `cmd/bot`, never `Handle`.
  All spelled out in `.agents/plan.md`.
- **Language (D-19):** `dialog.detectLang` (`lingua-go`, uk/en/ru → ru maps
  to uk) sets `sess.Lang` **every turn it's confident** (mid-dialogue switch
  works). `sess.Lang` feeds: the STT `langHint` (`cmd/bot`), a soft
  `--- CONVERSATION LANGUAGE ---` hint in the prompt, and the fixed
  handoff/apology lines. First voice turn (no `sess.Lang` yet) → `WHISPER_LANG`.
- **Package layout is fixed by `.agents/plan.md`.** Do not restructure. One
  dependency only (`go-telegram/bot`); everything else is stdlib `net/http`.

### Do NOT

- Prepay OpenAI, prepay Gemini, or upgrade ElevenLabs to Starter. All three
  are deferred to the client-recording milestone.
- Reintroduce KB retrieval (B1).
- Restructure packages. Add dependencies beyond `go-telegram/bot` and
  `lingua-go` (D-19) without asking.
- Commit `.env`. Add anything under `~/.claude/` or `<repo>/.claude/` without
  asking Val first (stated Причина/Ціль/Проблеми, then wait).
- Delete or weaken a test to make it pass.

---

## 4. Environment — verified 2026-08-31

| tool | state |
|--|--|
| `go` | 1.26.6 |
| `ollama` | 0.33.1, **logged in to Pro** — `gemma4:cloud` generates OK. Cloud models **cold-start slow (20–70 s on the first call)** — that is not a hang. |
| `whisper` | openai-whisper (pipx), `~/.local/bin/whisper` |
| `ffmpeg` | `/usr/bin/ffmpeg` |
| `opencode` | 1.18.20, `~/.opencode/bin/opencode`, ollama provider wired (smoke-tested) |
| `mpv` | for `minions/tts-audition.sh` playback |

**`.env` (complete for dev):**
- `TELEGRAM_BOT_TOKEN` — live, bot is **@v2v_demo_bot**
- `ELEVENLABS_API_KEY` — live, Free plan, "Text to Speech" permission only
- `ELEVENLABS_VOICE_A/B` — Sarah / George (see §3)
- `STT_BACKEND=local`, `WHISPER_BIN=whisper`, `WHISPER_MODEL=turbo`
- `DIALOG_BACKEND=ollama`, `DIALOG_MODEL=gemma4:cloud`, `OLLAMA_BASE_URL=http://localhost:11434`
- `OPENAI_API_KEY` — present but **$0 balance** (key restricted to whisper-1 +
  gpt-4o-mini; only used at the client-recording milestone)
- `GEMINI_API_KEY` — present but **no credits** (see §3)

Never `source .env` into your shell — it would leak secrets into child
processes and subagents. Read individual values with `grep` when a script
needs them (as `minions/tts-audition.sh` does).

---

## 5. Subagent workflow (opencode → Ollama Pro, ~free)

`opencode.json` in this repo defines four agents (models picked for cost from
`~/wrk/common/common/ollama-*.md`):

| agent | model | role |
|--|--|--|
| `coder` (subagent) | `minimax-m3:cloud` | implements a build-order step per `.agents/plan.md`; appends to `.agents/changes.md` |
| `reviewer` | `deepseek-v4-pro:cloud` | read-only: bugs, security, non-idiomatic Go |
| `tester` | `deepseek-v4-flash:cloud` | runs build/vet/test, minimal fixes, writes `.agents/test-report.md` |
| `documenter` | `glm-5.3-flash:cloud` | README + godoc to match code |

**Invoke:**
```
opencode run --agent coder --auto "Implement build-order step 1 (config + kb + store) exactly per .agents/plan.md. Do not go beyond that step."
opencode run --agent tester --auto "Run build/vet/test -race, fix minimal, report."
opencode run --agent reviewer "Review internal/config and internal/kb."
```
- `--auto` auto-approves permissions not explicitly denied. `opencode.json`
  still **denies `git push` and `rm -rf`**, and **asks on `git commit`**.
- `.agents/changes.md`, `.agents/test-report.md`, `.agents/summary.md` are
  gitignored scratch — the agents write there.
- **You (the orchestrating session) own review and commits.** Model-generated
  Go is not trusted blind — read the diff, run the full check locally, then
  commit. Squash-ish, one commit per build-order step is reasonable.

Alternative: implement directly yourself if opencode output quality is poor
for a given step. The plan is precise enough for either path.

---

## 6. Testing the running demo

- **Voice audition:** `./minions/tts-audition.sh a` | `b` | `<voiceID>` `["text"]`
  → synthesises a UA test phrase (Latin surname + € amount), mp3 to `tmp/`,
  plays via mpv.
- **Run the bot:** `go run ./cmd/bot` — long-polling, no webhook. Then DM
  **@v2v_demo_bot** on Telegram.
- **Text messages skip STT** — use them for fast dialogue-core iteration;
  send a voice message to exercise whisper (slow first time — model download).

---

## 7. Git

- Direct-to-main convention for this repo (it has a GitHub remote). Commit and
  push freely once a build-order step passes `build && vet && test -race`.
- Tracked: source, `Makefile`, `docs/`, `.agents/plan.md`, `minions/`,
  `AGENTS.md`, `.env.example`, `HANDOFF.md`, `.claude/settings.json` (bash
  allowlist for the build loop) + `.claude/hooks/` + `.claude/skills/`
  (session-end / session-recall). Gitignored: `.env`,
  `tmp/`, `.engage/`, `.agents/{changes,test-report,summary}.md`, `/bot`,
  `*.db`, `.claude/settings.local.json`.
- End commit messages with:
  `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`

---

## 8. Open items / later milestones

- `WHISPER_MODEL=turbo` downloads (~1.5 GB) on the first voice message —
  expect a long first turn. On this box it was already pulled (bench
  2026-08-31). Steady-state ~12 s/short turn — most of it is Python startup
  + model load, which the CLI shell-out repeats every turn; a warm STT
  server would kill that but is out of scope (plan shells the CLI).
- **I-10 client sample recording** (later): flip `STT_BACKEND=openai`
  (+ prepay OpenAI $5), upgrade ElevenLabs to Starter, swap in the UA voice
  IDs, record a 2–3 min conversation, attach to the client thread.
- **Before the client demo:** fund the AI Studio project and re-enable
  `GEMINI_API_KEY` (commented out in `.env` + `~/.bashrc:150` — the `.env`
  key 429s, no credits). Development runs on `ollama` `gemma4:cloud`; the
  `gemini` generator is built and wire-verified, just not fundable-yet.
- Host stays local (D-08) until the client gets an unattended link.
