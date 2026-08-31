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
