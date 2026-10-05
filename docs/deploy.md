# Deploy — Oracle Cloud Free Tier

Cheapest viable host for the demo: Oracle's **Always Free** Ampere A1 (ARM)
shape — free after the trial too, up to **2 OCPU / 12 GB RAM** across your
A1 instances ([official limits](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm):
1,500 OCPU-hours + 9,000 GB-hours per month, 200 GB boot/block storage in
total). **Read "Idle reclamation" under Cost & limits before relying on it.** The bot is a single Go binary, long-polls Telegram (no inbound
port to open), and reads `.env` itself (`cmd/bot/config.go`) — no secrets
manager needed for a demo.

This assumes the **client demo config** (`STT_BACKEND=openai`,
`DIALOG_BACKEND=openai`, ElevenLabs/Azure TTS — see `docs/smoke-test.md`
"Client demo config"), not local whisper.cpp/Ollama — those need real CPU
and are not worth running on a free instance for a demo.

## 0. Before you start

- **One token, one poller.** Telegram allows exactly one `getUpdates`
  consumer per token — if the server and a local `make run` share a token,
  both fail with `409 Conflict`. The server runs the public bot
  (`@v2v_demo_bot`, the address people already have); local development uses
  a separate dev bot created in @BotFather. The public token is in
  @BotFather → `/mybots` → the bot → **API Token** (don't Revoke it).
- **Server config = `.env.client`**, not the dev `.env`: `STT_BACKEND=openai`,
  `DIALOG_BACKEND=openai` (`gpt-4.1-mini`), `TTS_BACKEND=azure` (the
  ElevenLabs subscription is ending — don't depend on it). Needs
  `OPENAI_API_KEY` (positive balance), `AZURE_SPEECH_KEY` +
  `AZURE_SPEECH_REGION`, and the new bot's `TELEGRAM_BOT_TOKEN`.
- **The demo is public:** `data/turns.jsonl` stores users' messages and
  replies (the greeting says the chat is logged). Keep `data/` on the server
  only, never commit it, and delete old logs when the pitch is over.
  `SESSION_STORE=sqlite` keeps conversations across a restart.

## 1. Create the instance

1. cloud.oracle.com → sign up for **Always Free** (card verification, not
   charged for Always Free resources).
2. Compute → Instances → **Create instance**.
   - Image: **Ubuntu 24.04** (aarch64/ARM).
   - Shape: **VM.Standard.A1.Flex** — 1 OCPU / 6 GB is plenty for this bot
     alone; the Always Free ceiling is 2 OCPU / 12 GB in total (anything
     above it is disabled and deleted 30 days after the trial ends). If
     you're also co-hosting `shopogoda` (§7), use up to 2 / 12. A smaller
     memory size also helps with idle reclamation (see Cost & limits).
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
ssh ubuntu@<public-ip> 'mkdir -p ~/v2v-demo'   # quoted: an unquoted ~ expands on YOUR machine
scp bot ubuntu@<public-ip>:v2v-demo/bot          # ~140 MB: lingua-go embeds all language models (first upload only — see §6 for updates)
scp -r topics ubuntu@<public-ip>:~/v2v-demo/   # KB, prompts, greetings, topics.json
```

Either way, the instance needs: the `bot` binary, `topics/`, and a `.env`
(next step) all under one directory — `~/v2v-demo/` below.

## 4. `.env`

Build the server `.env` from `.env.client` (see §0): the three flips it
documents plus `TELEGRAM_BOT_TOKEN` (the **public** bot's), `OPENAI_API_KEY`,
`AZURE_SPEECH_KEY` / `AZURE_SPEECH_REGION`, `SESSION_STORE=sqlite`, and
`BOT_TIMEZONE=Europe/Kyiv` set explicitly (the instance clock is UTC, and
the "within 15 minutes / next business morning" line uses this zone).
Optional safety net: `TTS_FALLBACK_BACKEND=espeak` (+ `apt install
espeak-ng`) so a TTS outage degrades to a robotic voice instead of text.

```bash
scp .env.server ubuntu@<public-ip>:~/v2v-demo/.env   # never commit this file
ssh ubuntu@<public-ip> chmod 600 ~/v2v-demo/.env
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

# option B (cross-compiled), after a local rebuild. Never scp straight over the
# running binary ("Text file busy" — and the restart would run the old one):
# upload under another name, then rename, then restart.
scp bot ubuntu@<public-ip>:v2v-demo/bot.new
ssh ubuntu@<public-ip> 'mv -f ~/v2v-demo/bot.new ~/v2v-demo/bot && sudo systemctl restart v2v-demo'

# or simply: ./minions/srv.sh push-bin   (builds for the server's arch, does the above)
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

## 8. Runbook as actually deployed (2026-10-03)

What the first real deployment looked like — Stockholm home region, where
the A1 shape was **"Out of capacity" on five tries in a row**, so the bot runs
on the other Always Free shape, **VM.Standard.E2.1.Micro** (AMD x86, 1 GB).

- **Console order matters.** The wizard filters shapes by the selected image's
  architecture. Pick the *image first* (Ubuntu **without** `aarch64` in the
  name for x86), then Change shape → **"Specialty and previous generation"**
  (Micro is not under the "AMD" tile). With an aarch64 image Micro is hidden.
- **Public IP:** with "Create new VCN + public subnet" the *Automatically
  assign public IPv4* switch stays disabled. Create the instance without it,
  then Instances → the instance → Networking → Attached VNICs → the VNIC →
  IP administration → primary private IP → Actions → Edit → *Ephemeral public
  IP*. SSH (22) is open by default; the bot needs no inbound port.
- **Build for the host's architecture:** Micro is `GOOS=linux GOARCH=amd64`
  (A1 is `arm64`). The static binary is ~140 MB (lingua-go embeds its models).
- **1 GB needs swap:** `fallocate -l 2G /swapfile`, `chmod 600`, `mkswap`,
  `swapon`, an `/etc/fstab` line. Measured: the bot idles at ~140 MB RSS.
- **SSH alias** in `~/.ssh/config` (`Host v2vdemo`, `User ubuntu`,
  `IdentityFile`, `IdentitiesOnly yes`) → `ssh v2vdemo`, `scp … v2vdemo:~/v2v-demo/`.
- **Config:** `.env.server` in the repo root (gitignored by `.env.*`) is the
  server's `.env`; `scp` it to `~/v2v-demo/.env`, `chmod 600`.
- **Disable a topic on the public bot without touching the repo:** upload a
  filtered `topics/topics.json` (lyapko is off on the server) and
  `sudo systemctl restart v2v-demo`.
- **Update the binary:** `./minions/srv.sh push-bin` (builds for the server's arch,
  uploads as `bot.new`, renames over `bot` — never `scp` over the running binary —
  and restarts); logs: `./minions/srv.sh logs -f`.
- **TTS bridge:** the Azure key returned `401` on the token endpoint from
  every region (checked locally and on the server), so the server runs
  `TTS_BACKEND=elevenlabs` until that subscription period ends. Fix the Azure
  key/region *before* then (portal → Speech resource → Keys and Endpoint).
- **Idle-reclamation watch:** Micro has no memory criterion, so the instance
  can be reclaimed after 7 quiet days. Check `systemctl is-active v2v-demo`
  now and then, and retry creating an A1 later (it counts memory).

## 9. Status and open items (2026-10-03)

**Verified end to end on the live bot** (Telegram, `@v2v_demo_bot`): `/start` →
picker (5 topics on the server) → text turn → voice-in (OpenAI STT) → voice-out
(ElevenLabs) + text, slots collected correctly. 139 MB RSS, no errors in the
journal.

Open items, most urgent first:

1. **Azure TTS key returns 401** (every region, locally and on the server).
   Get a fresh KEY 1 + Location from the portal, put them into `.env.server`,
   check `issueToken` returns 200, switch the server back to `TTS_BACKEND=azure`.
   The ElevenLabs bridge ends with that subscription period — **record the end
   date here once known: ____**. Same commit: flip the code default in
   `cmd/bot/config.go` to `azure` (see the `.env.example` note).
2. **Rotate the OpenAI and Azure keys** — their first ~60 characters were
   printed into the 2026-10-03 session transcript. Update `.env.server`, `scp`
   it to `~/v2v-demo/.env`, restart.
3. **Idle reclamation (Micro).** Check `systemctl is-active v2v-demo` every few
   days; retry creating an A1 (memory criterion works in our favour) or move to
   Hetzner CX22 if the instance is reclaimed.
4. **Billing sanity check** after the trial ends (Console → Billing → Cost
   Analysis); consider a $1 budget alert. Do not click Upgrade unless needed.
5. **lyapko decision:** disabled on the server, shipped in the repo. Before
   enabling it publicly: re-check KB prices against the shop's listing, and
   raise the shop's own pregnancy/labour treatment claims with them (the bot
   deliberately does not repeat them).
6. **Live smoke sweeps** (`docs/smoke/<id>.md` §1–7 per topic) on the server
   config; the voice step (6e) by ear.
7. **Data hygiene:** `~/v2v-demo/data/` holds users' messages (turns, leads,
   sessions). Public demo → delete it when the pitch is over.
8. Optional: `TTS_FALLBACK_BACKEND=espeak` on the server (`apt install
   espeak-ng ffmpeg`) as a spare tire; `apt upgrade` now and then.
9. Cosmetic: the translation greeting is hard-wrapped, so Telegram shows
   breaks mid-sentence (the lyapko greeting is not).

## 10. Rotating credentials

Order that avoids downtime: **create the new key → test it → push it → verify the bot →
revoke the old key.** (Portal labels below are from memory and may have moved.)

```bash
# 1. put the new values into .env.server (never commit it), then:
./minions/check-keys.sh            # every line must say OK (add --tts to test ElevenLabs)
./minions/srv.sh push-env          # uploads as ~/v2v-demo/.env (chmod 600) and restarts
./minions/srv.sh status            # active, 0 errors; send the bot a text + a voice message
# 2. only now revoke the old key in the provider's console
```

- **OpenAI** — platform.openai.com → the **same project that has `gpt-4.1-mini` under
  *Allowed models*** (a "protected" model: needs org verification and a positive prepaid
  balance) → API keys → *Create new secret key* → `OPENAI_API_KEY`. A key from another
  project returns 403/404 for the model; `check-keys.sh` shows it as `FAIL openai`.
  The same key serves STT (`whisper-1`) and the dialogue.
  Links (log in): <https://platform.openai.com/api-keys> (create / revoke keys),
  <https://platform.openai.com/settings/organization/billing/overview> (balance;
  *Allowed models* are under Settings → your project → Limits).
  Billing: the **prepaid balance belongs to the organization, not the key** — a new key in
  the same org needs no new top-up; at zero balance calls return `429 insufficient_quota`
  (`check-keys.sh`: `FAIL openai`). If you enable auto-recharge, also set a monthly budget.
- **Azure Speech** — portal.azure.com → search *Speech* → your Speech resource (free tier
  F0 if none: one per region per subscription) → *Keys and Endpoint* → **KEY 1** →
  `AZURE_SPEECH_KEY`, and **Location/Region** as the short name (`swedencentral`, not the
  endpoint URL) → `AZURE_SPEECH_REGION`. To rotate without downtime put KEY 2 in the bot,
  then *Regenerate* KEY 1. `401` from `check-keys.sh` = wrong/regenerated key or a key from
  a resource in another region.
  Links: <https://portal.azure.com/> (log in) · official rotation guide
  <https://learn.microsoft.com/azure/ai-services/rotate-keys> · region short names
  <https://learn.microsoft.com/azure/ai-services/speech-service/regions> · F0 free-tier limits
  <https://azure.microsoft.com/pricing/details/cognitive-services/speech-services/>.
  Billing: **no prepaid balance** — Azure bills in arrears to the subscription's payment
  method (a card is required even for F0). F0 is hard-capped (≈500k TTS chars / 5 STT
  hours per month): past the quota calls fail with 429, they are not billed. A billing
  problem looks like 429/403, **not 401** — 401 is a key that is not accepted; also check
  Portal → Subscriptions → status *Active*.
- **Telegram** — @BotFather → `/mybots` → the bot → *API Token* → *Revoke current token*.
  This invalidates the old token **immediately**, so run `push-env` right after; the local
  dev bot has its own token (never share one token between two pollers: 409 Conflict).
  Link: <https://t.me/BotFather>.
- **ElevenLabs** — only while its subscription lasts (the bridge); `--tts` tests it.
  Link (log in): <https://elevenlabs.io/app/settings/api-keys>.

## Cost & limits

- **$0** as long as the A1 instances stay within 2 OCPU / 12 GB in total.
- **Idle reclamation — a real risk for this bot.** Oracle's docs: "Idle
  Always Free compute instances may be reclaimed by Oracle. … idle if,
  during a 7-day period: CPU utilization for the 95th percentile is less
  than 20%, network utilization is less than 20%, memory utilization is
  less than 20% (applies to A1 shapes only)." A long-polling Telegram bot
  uses almost no CPU or network, so the first two are met whatever the
  traffic; only **memory** can keep it above the line. The docs say nothing
  about Pay As You Go accounts being exempt, and I did not verify that.
  Levers: (1) size memory so the bot's resident memory is ≥ ~20% of it
  (measure with `ps -o rss -C bot` / `free -m` after a day, then resize —
  1 OCPU with a small amount of RAM instead of 6 GB); (2) co-hosting
  shopogoda adds Postgres/Redis memory, which helps; (3) if it is reclaimed
  anyway, the move is Hetzner CX22 (no reclamation policy). Don't count on
  "real traffic" — it does not move CPU or network above 20%.
- Co-hosting shopogoda (§7) counts against the same 2 OCPU/12 GB ceiling —
  bot + Postgres + Redis is modest, but skipping its observability stack
  (Prometheus/Grafana/Jaeger) keeps real headroom for v2v-demo alongside it.
- If this ever needs to grow past the free tier (local whisper.cpp/Ollama,
  higher traffic), the cheapest paid step up is Hetzner CX22
  (~€4.2/month) — same systemd setup, no bot-side changes.

---

поки перетвори частовикористовувані однотипні запити та виклики у посіпак
