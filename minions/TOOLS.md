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
