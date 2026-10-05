# minions/ — dev helpers

Small tools used while building and testing the demo. Not part of the bot;
not run in CI. Each is standalone.

## `tts-audition.sh`

Synthesise a Ukrainian test phrase with an ElevenLabs voice and play it, to
judge voice naturalness before wiring a voice into the demo. The phrase
mixes a Latin surname and a EUR amount — where `eleven_multilingual_v2` is
most likely to slip on Ukrainian.

```
./minions/tts-audition.sh                 # voice A from .env, default phrase
./minions/tts-audition.sh b                # voice B
./minions/tts-audition.sh <voice_id>       # an explicit ElevenLabs voice id
./minions/tts-audition.sh a "Свій текст"   # custom phrase
MODEL=eleven_turbo_v2_5 ./minions/tts-audition.sh a
```

Reads `ELEVENLABS_API_KEY` / `ELEVENLABS_VOICE_A` / `ELEVENLABS_VOICE_B` from
`.env`. Output mp3 → `tmp/`. Needs `curl` + `mpv`/`ffplay`/`mplayer`.

## `dialog-probe/` — run smoke-test rows through the real dialog pipeline

Runs `docs/smoke-test.md` **text** rows (§2, §6, §7, §8) through the actual
`internal/dialog` pipeline — grounding gate + `hardEscalate` + the LLM — with
no Telegram and no bot restart. Use it to vet a dialogue model before wiring
it in, or to diff two models. Voice (§13) still needs the live bot.

Unlike `tgdrive/`, this is **part of the main module** (it imports
`internal/dialog` + `internal/kb`), so `go build ./...` compiles it; it adds
no dependency.

```
go run ./minions/dialog-probe  scenarios.txt        # or < scenarios.txt
echo 'скільки коштує сторінка' | go run ./minions/dialog-probe
go run ./minions/dialog-probe -model gpt-4o-mini -v  scenarios.txt
go run ./minions/dialog-probe -backend ollama        scenarios.txt
```

Scenario file, one turn per line: `# text` = header (no reset), `---` or a
blank line = fresh `Session`, anything else = one user turn in the current
session. Reads `.env` for the backend/model/keys/paths; flags override.
Per turn it prints the resolved `signal`, latency, whether the gate or the
LLM answered (`pre-LLM` / `N KB`), the slot delta, and a reply preview
(`-v` for the full text, `-slots` for the slot JSON).

## `i10-sample/` — build the I-10 demo audio

Runs the three `.engage/client-demo.md` §3 dialogues through the real
`dialog.Handle` pipeline (`DIALOG_BACKEND`/`DIALOG_MODEL` from `.env`), voices
every line with ElevenLabs — **Vira on voice A, the client on voice B** — and
stitches them into one mp3 (`tmp/i10-sample.mp3`). A *synthesised* sample,
not a live recording; the client makes their own in the bot.

```
go run ./minions/i10-sample                 # -> tmp/i10-sample.mp3
go run ./minions/i10-sample -out x.mp3 -pause 0.6 -gap 2.0
```

Needs `OPENAI_API_KEY` + `ELEVENLABS_API_KEY` + `ELEVENLABS_VOICE_A/B` in
`.env`, and `ffmpeg`. Part of the main module (imports `internal/`).

## `tgdrive/` — drive the live bot as a real user (CDP)

A tiny Chrome DevTools Protocol client that drives an already-open,
logged-in `web.telegram.org/a/` tab. Used to run `docs/smoke-test.md`
against the running bot as a real Telegram user — the Bot API can't send
*to* the bot, and a bot never receives another bot's messages, so this is
the only no-account-server way to script end-to-end turns. (Voice-*in* still
can't be tested this way — the web client uploads `.ogg` as a file, not a
voice message.)

Setup: `chromium --remote-debugging-port=9222`, log into web.telegram.org
(a burner account is cleaner — the session is full-account access), open the
`@v2v_demo_bot` chat.

```
cd minions/tgdrive
go run . eval  '<js>'      # run JS in the page, print its value
go run . type  '<text>'    # Input.insertText into the focused element
go run . key   '<Key>'     # press Enter / Escape
go run . click 'x,y'       # left click at page coords
go run . send  '<text>'    # focus the composer, type, press Enter
go run . read  <N>         # print the last N messages as "IN|OUT<tab>text"

./turn.sh "<message>" [wait_seconds]   # send, wait, print the bot's reply
```

Its own Go module (`minions/tgdrive/go.mod`) — `go build ./...` from the
repo root ignores it. One dependency: `github.com/gorilla/websocket`.

## `council/` — fan a code review out to several third-party agent CLIs

Runs the same review prompt (a git range's log + diff, embedded directly —
not left for each agent to fetch, since some run with no shell trusted) in
parallel through several independent coding-agent CLIs installed on this
machine (`opencode`, `kilo`, `cursor-agent`, `kiro-cli`; `codex`/`omp` exist
but are off by default — see the script header for why), each in the
safest read-only/plan mode that CLI offers. Use it to get a second (third,
fourth…) opinion on a change before or after committing it — a poor man's
multi-model code review when `/code-review` or `/fix-review` isn't set up
for this repo, or when you specifically want *non-Claude* eyes on it.

```
./minions/council/run.sh                              # reviews HEAD~3..HEAD, all default agents
./minions/council/run.sh -r 31f3e75..HEAD              # a specific range
./minions/council/run.sh -a opencode,kiro-cli -t 1200   # pick agents, longer per-agent timeout
./minions/council/run.sh -A -p 'internal/dialog internal/kb' -b brief.md   # FULL audit of the current tree
```

`-A` audits the tree instead of a commit range: no diff is embedded, the prompt
lists the files in scope (narrowed with `-p`, line counts included) and the
agents read them; `-b` adds the focus for that area. **Agents always run in
`OUTDIR/src`, a clean `git archive HEAD` snapshot** — never in the working tree —
so they cannot read `.env*`, `tmp/`, `data/` or uncommitted edits (live keys and
user messages would otherwise go to third-party model providers).

Reports land in `tmp/council/<UTC timestamp>/<agent>.md` (+ `.stderr.log` +
`.exit`); the script prints a summary table and a `git status --short` at
the end — every agent runs read-only/plan-mode plus an explicit
"don't edit anything" instruction in the prompt, but that's best-effort,
not a sandbox guarantee, so the status check is the actual tripwire. Read
the reports yourself (or hand them to Claude) — treat every finding as a
claim to verify against the real code, not a verdict.

## `srv.sh` — operate the live bot on its server

One command per thing we kept typing by hand over `ssh`/`scp` (layout and
rationale: `docs/deploy.md` §8). The host is an ssh alias — `v2vdemo` by
default, `V2V_HOST=…` to override.

```
./minions/srv.sh status                      # active?, host uptime, bot RSS, free memory/swap, startup line, errors/hour, disk
./minions/srv.sh logs [N | -f]               # last N journal lines (50) / follow
./minions/srv.sh restart
./minions/srv.sh build                       # cross-compile for the server's arch (amd64 Micro / arm64 A1) -> tmp/deploy/bot-<arch>
./minions/srv.sh push-bin                    # build + atomic replace (scp to bot.new, mv) + restart
./minions/srv.sh push-topics [--without id,id]   # upload topics/, optionally filter the server's manifest (lyapko is off live), restart, list live topics
./minions/srv.sh push-env [file]             # upload a config (default .env.server) as ~/v2v-demo/.env, chmod 600, restart
```

`push-env` refuses a Telegram token equal to the local `.env`'s (two pollers on
one token = 409 Conflict) unless `FORCE=1`. Nothing secret is printed.

## `env-show.sh` — print `.env*` files with secrets masked

Use this instead of `cat`/`grep` on env files — those print the keys into the
terminal and into an agent's transcript (it happened once, 2026-10-03). Keys
containing `KEY`/`TOKEN`/`SECRET`/`PASSWORD` show only `<set, len N>`.

```
./minions/env-show.sh                        # .env and .env.server
./minions/env-show.sh .env.client .env       # explicit files
```

## `check-keys.sh` — are the credentials in an env file actually accepted?

One OK/FAIL line per service, never the secret (keys go to curl on stdin).
Exit 1 if anything failed.

```
./minions/check-keys.sh                      # .env.server: Telegram getMe (@username), OpenAI 1-token chat, Azure Speech issueToken
./minions/check-keys.sh .env --tts           # another file, + ElevenLabs (synthesises one word, ~4 credits)
```

The OpenAI check is a real 1-token chat call on purpose: a project-restricted
key is often forbidden to *list* models (403) yet allowed to chat.

## `probe-all.sh` — the dialog-probe sweep over every topic

Runs `dialog-probe` for each topic that has a `tmp/probe-<id>.txt` scenario
file and prints a compact transcript (one line per user turn: signal, latency,
gate-or-LLM, slot delta). The sweep to repeat after touching a KB, a
`system.md`, the gate or the model.

```
./minions/probe-all.sh                       # every topic in topics/topics.json
./minions/probe-all.sh lyapko dental         # selected topics
./minions/probe-all.sh -m gpt-4.1-mini -b openai
```
