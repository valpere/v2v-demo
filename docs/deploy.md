# Deploy — Oracle Cloud Free Tier

Cheapest viable host for the demo: Oracle's **Always Free** Ampere A1 (ARM)
shape — free forever, not a trial, up to 4 OCPU / 24 GB RAM across your A1
instances. The bot is a single Go binary, long-polls Telegram (no inbound
port to open), and reads `.env` itself (`cmd/bot/config.go`) — no secrets
manager needed for a demo.

This assumes the **client demo config** (`STT_BACKEND=openai`,
`DIALOG_BACKEND=openai`, ElevenLabs/Azure TTS — see `docs/smoke-test.md`
"Client demo config"), not local whisper.cpp/Ollama — those need real CPU
and are not worth running on a free instance for a demo.

## 1. Create the instance

1. cloud.oracle.com → sign up for **Always Free** (card verification, not
   charged for Always Free resources).
2. Compute → Instances → **Create instance**.
   - Image: **Ubuntu 24.04** (aarch64/ARM).
   - Shape: **VM.Standard.A1.Flex** — 1 OCPU / 6 GB is plenty for this bot
     alone; you can go up to 4/24 at no cost. If you're also co-hosting
     `shopogoda` (§7), take the full 4 OCPU / 24 GB up front — still $0,
     and its Postgres + Redis want the headroom.
   - Keep the default VCN/subnet; add your SSH public key.
3. Wait for it to go **Running**, note the public IP.

No ingress rules needed beyond the default SSH (22) — the bot only makes
outbound calls (Telegram long-poll, OpenAI, ElevenLabs/Azure).

## 2. Install runtime deps

```bash
ssh ubuntu@<public-ip>
sudo apt update && sudo apt install -y ffmpeg git
```

`ffmpeg` is required (`FFMPEG_BIN` — used by the espeak TTS fallback and by
whispercpp, if either is ever turned on later). The client-config stack
above doesn't call it, but installing it now costs nothing and avoids a
surprise later if a fallback backend is added.

## 3. Get the binary onto the box

Two options — pick one:

**A. Build on the instance** (simplest, no cross-compile step):

```bash
# on the instance
curl -fsSL https://go.dev/dl/go1.26.6.linux-arm64.tar.gz | sudo tar -C /usr/local -xz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc
git clone https://github.com/valpere/v2v-demo.git && cd v2v-demo
make build
```

**B. Cross-compile locally and `scp`** (no Go toolchain on the instance):

```bash
# on your machine
GOOS=linux GOARCH=arm64 go build -o bot ./cmd/bot
scp bot ubuntu@<public-ip>:~/v2v-demo/bot
scp -r topics ubuntu@<public-ip>:~/v2v-demo/   # KB, prompts, greetings, topics.json
```

Either way, the instance needs: the `bot` binary, `topics/`, and a `.env`
(next step) all under one directory — `~/v2v-demo/` below.

## 4. `.env`

Copy `.env.example` → `.env` on the instance and fill in the **client demo
config**: `TELEGRAM_BOT_TOKEN`, `OPENAI_API_KEY` (STT + dialog),
`ELEVENLABS_API_KEY` + voice ids (or `AZURE_SPEECH_KEY`/`AZURE_SPEECH_REGION`
if using the Azure rollback). Leave `SESSION_STORE=memory` unless you want a
restart to resume mid-conversation, in which case set
`SESSION_STORE=sqlite`.

```bash
scp .env ubuntu@<public-ip>:~/v2v-demo/.env   # never commit this file
```

## 5. Run it as a systemd service

`/etc/systemd/system/v2v-demo.service`:

```ini
[Unit]
Description=v2v-demo Telegram bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/v2v-demo
ExecStart=/home/ubuntu/v2v-demo/bot
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

No `Environment=`/`EnvironmentFile=` line needed — the bot reads `.env`
from its working directory on its own (`readDotEnv`, `cmd/bot/config.go`).

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now v2v-demo
sudo systemctl status v2v-demo
journalctl -u v2v-demo -f      # tail logs
```

## 6. Updating

```bash
# option A instance, after a git pull:
cd ~/v2v-demo && git pull && make build && sudo systemctl restart v2v-demo

# option B (cross-compiled), after a local rebuild:
scp bot ubuntu@<public-ip>:~/v2v-demo/bot
ssh ubuntu@<public-ip> sudo systemctl restart v2v-demo
```

## 7. Co-hosting a second bot (shopogoda)

Optional: the same Always Free instance has room for `shopogoda`
(`~/wrk/projects/telegram_bot/shopogoda/`) alongside `v2v-demo` — a
completely separate project/repo, own Telegram bot token, own directory.
Nothing below touches that repo's own files; it only says how to *run* its
existing `docker/docker-compose.prod.yml` on this box.

**Needs Docker** (v2v-demo doesn't; shopogoda's Postgres + Redis do):

```bash
sudo apt install -y docker.io docker-compose-v2
sudo usermod -aG docker $USER   # log out/in once for this to take effect
```

**Get its own copy onto the box**, in its own directory (don't nest it
under `~/v2v-demo/`):

```bash
git clone https://github.com/valpere/shopogoda.git ~/shopogoda
```

**Config.** `docker/docker-compose.prod.yml` reads `../.env.production` for
the bot container, and interpolates a few `${VAR}` host vars for
Postgres/Grafana straight from a `.env` file **next to the compose file**
(`~/shopogoda/docker/.env`) — compose parses the whole file up front, so
`DB_PASSWORD` and `GRAFANA_ADMIN_PASSWORD` must be set there even for
services you don't start (their `:?required` guards fail at parse time
otherwise):

```bash
scp .env.production ubuntu@<public-ip>:~/shopogoda/.env.production   # its own bot token, DB/Redis creds
# ~/shopogoda/docker/.env — DB_PASSWORD=... and GRAFANA_ADMIN_PASSWORD=... at minimum
```

**Start only what a demo needs** — skip Prometheus/Grafana/Jaeger (extra
RAM, extra ports to reason about, no value for a demo). `docker compose up`
lets you name a subset of services and it starts only those, leaving the
rest defined but not run:

```bash
cd ~/shopogoda/docker
docker compose -f docker-compose.prod.yml up -d bot postgres redis
```

**Ports.** If `BOT_WEBHOOK_URL` is unset, shopogoda long-polls like
v2v-demo (checked in `internal/config/config.go`) and port 8080 is only its
internal `/health` endpoint — leave it unpublished / firewalled, same "no
inbound port" posture as §1. Postgres (5432) and Redis (6379) only need to
be reachable from the bot container on the compose network — don't publish
them to the host's public interface. Switching shopogoda to real webhook
mode (TLS reverse proxy, opening 8080) is a bigger step, out of scope here.

**Updating:**

```bash
ssh ubuntu@<public-ip>
cd ~/shopogoda && git pull
cd docker && docker compose -f docker-compose.prod.yml build bot \
  && docker compose -f docker-compose.prod.yml up -d bot postgres redis
```

## Cost & limits

- **$0** as long as the instance stays within Always Free limits (1 A1
  instance up to 4 OCPU/24 GB, or several smaller ones summing to that).
  Oracle does reclaim idle Always Free instances after long periods of
  zero activity — a long-polling bot with real traffic avoids that, but if
  the demo goes quiet for weeks, log in and touch it.
- Co-hosting shopogoda (§7) counts against the same 4 OCPU/24 GB ceiling —
  bot + Postgres + Redis is modest, but skipping its observability stack
  (Prometheus/Grafana/Jaeger) keeps real headroom for v2v-demo alongside it.
- If this ever needs to grow past the free tier (local whisper.cpp/Ollama,
  higher traffic), the cheapest paid step up is Hetzner CX22
  (~€4.2/month) — same systemd setup, no bot-side changes.
