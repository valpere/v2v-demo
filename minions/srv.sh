#!/usr/bin/env bash
# srv.sh — operate the live bot on its server (see docs/deploy.md §8).
#
# The host is an ssh alias (default `v2vdemo`, from ~/.ssh/config); override
# with V2V_HOST. The remote layout is ~/v2v-demo/{bot,.env,topics/,data/} run by
# the systemd unit `v2v-demo`.
#
# Usage:
#   ./minions/srv.sh status                 # active?, memory, RSS, errors, topics, disk
#   ./minions/srv.sh logs [N | -f]          # last N journal lines (default 50) / follow
#   ./minions/srv.sh restart
#   ./minions/srv.sh build                  # cross-compile for the server's arch
#   ./minions/srv.sh push-bin               # build + atomic replace + restart
#   ./minions/srv.sh push-topics [--without id,id]
#                                           # upload topics/, optionally dropping topics
#                                           # from the server's manifest, then restart
#   ./minions/srv.sh harden                 # one-time: systemd sandbox + MemoryMax drop-in, journald cap
#   ./minions/srv.sh pull-data              # archive the server's data/ to tmp/backup/ (0600)
#   ./minions/srv.sh push-env [file]        # upload a config as ~/v2v-demo/.env
#                                           # (default .env.server) + restart
#
# Secrets are never printed. push-env refuses a token that equals the local
# .env's token (two pollers on one token = 409 Conflict) unless FORCE=1.
set -euo pipefail
cd "$(dirname "$0")/.."

HOST=${V2V_HOST:-v2vdemo}
UNIT=v2v-demo
R() { ssh -o BatchMode=yes -o ConnectTimeout=15 "$HOST" "$@"; }

status() {
  # shellcheck disable=SC2016  # the script is meant to expand on the server
  R 'echo "service : $(systemctl is-active v2v-demo)"
     echo "host    : $(uptime -p)"
     ps -o rss=,etime= -C bot | awk "{printf \"bot     : RSS %d MB, running %s\n\", \$1/1024, \$2}"
     awk "/MemAvailable/{a=\$2} /MemTotal/{t=\$2} /SwapFree/{sf=\$2} /SwapTotal/{st=\$2} END{printf \"memory  : %d / %d MB available, swap used %d MB\n\", a/1024, t/1024, (st-sf)/1024}" /proc/meminfo
     echo "started : $(journalctl -u v2v-demo --no-pager -o cat | grep listening | tail -1 | cut -c1-170)"
     echo "errors  : $(journalctl -u v2v-demo --since "-1 hour" --no-pager -o cat | grep -ciE "error|panic|fail" || true) in the last hour"
     echo "disk    : $(df -h ~/v2v-demo | tail -1 | awk "{print \$3 \" used of \" \$2}"), data/ $(du -sh ~/v2v-demo/data 2>/dev/null | cut -f1)"'
}

arch() {
  case "$(R uname -m)" in
    x86_64) echo amd64 ;;
    aarch64) echo arm64 ;;
    *) echo "unknown server architecture" >&2; return 1 ;;
  esac
}

build() {
  local a out
  a=$(arch)
  out=tmp/deploy/bot-$a
  mkdir -p tmp/deploy
  GOOS=linux GOARCH=$a CGO_ENABLED=0 go build -o "$out" ./cmd/bot
  echo "built $out ($(du -h "$out" | cut -f1), $a)" >&2
  echo "$out"
}

restart() {
  R "sudo systemctl restart $UNIT; sleep 3; systemctl is-active $UNIT"
  R "journalctl -u $UNIT -n 1 --no-pager -o cat | cut -c1-170"
}

cmd=${1:-status}
shift || true

case "$cmd" in
  status) status ;;

  logs)
    case "${1:-}" in
      -f) R "journalctl -u $UNIT -f -o cat" ;;
      *)  R "journalctl -u $UNIT -n ${1:-50} --no-pager -o cat" ;;
    esac ;;

  restart) restart ;;

  build) build >/dev/null ;;

  push-bin)
    bin=$(build)
    # scp to a side name then mv: overwriting a running binary fails with
    # "Text file busy", a rename does not.
    scp -q "$bin" "$HOST:v2v-demo/bot.new"
    R 'chmod +x ~/v2v-demo/bot.new && mv -f ~/v2v-demo/bot.new ~/v2v-demo/bot'
    restart ;;

  push-topics)
    without=""
    [ "${1:-}" = "--without" ] && { without=${2:?--without needs a comma-separated id list}; shift 2; }
    # Build the manifest that will go live FIRST: if jq fails nothing has been
    # touched. Everything except topics.json is streamed over, then the
    # manifest is swapped in with a rename — the server never holds a mix of
    # the full manifest and a half-filtered one.
    mkdir -p tmp
    jq --arg w "$without" '($w | split(",")) as $x | map(select(.id as $i | $x | index($i) | not))' \
      topics/topics.json >tmp/topics.server.json
    jq -e 'length > 0' tmp/topics.server.json >/dev/null || { echo "no topics left to push" >&2; exit 1; }
    tar --exclude=topics/topics.json -cf - topics | R 'tar -xf - -C ~/v2v-demo'
    scp -q tmp/topics.server.json "$HOST:v2v-demo/topics/topics.json.new"
    R 'mv -f ~/v2v-demo/topics/topics.json.new ~/v2v-demo/topics/topics.json'
    restart
    echo "live topics: $(R "jq -r '[.[].id] | join(\", \")' ~/v2v-demo/topics/topics.json 2>/dev/null || grep -o '\"id\": *\"[a-z]*\"' ~/v2v-demo/topics/topics.json | tr '\n' ' '")" ;;

  push-env)
    f=${1:-.env.server}
    [ -f "$f" ] || { echo "no such file: $f" >&2; exit 2; }
    # same parsing as the bot's readDotEnv: optional `export `, optional quotes
    tok() {
      awk '{ sub(/^[ \t]*export[ \t]+/, "") }
           index($0, "TELEGRAM_BOT_TOKEN=") == 1 { v = substr($0, index($0, "=") + 1); gsub(/^["'"'"']|["'"'"']$/, "", v); print v; exit }' "$1"
    }
    if [ -f .env ] && [ "${FORCE:-0}" != 1 ] &&
       [ "$(tok "$f" | sha256sum)" = "$(tok .env | sha256sum)" ]; then
      echo "REFUSING: $f and the local .env use the SAME Telegram token (409 Conflict)." >&2
      echo "Use a separate dev bot locally, or FORCE=1 if you stopped the local bot." >&2
      exit 1
    fi
    # staged: a failed upload never leaves a truncated .env behind
    scp -q "$f" "$HOST:v2v-demo/.env.new"
    R 'chmod 600 ~/v2v-demo/.env.new && mv -f ~/v2v-demo/.env.new ~/v2v-demo/.env'
    restart ;;

  harden)
    # One-time (idempotent): systemd sandboxing + limits as a drop-in, and a
    # journald size cap. Source of truth for docs/deploy.md §5.
    R 'sudo mkdir -p /etc/systemd/system/v2v-demo.service.d /etc/systemd/journald.conf.d'
    R 'sudo tee /etc/systemd/system/v2v-demo.service.d/hardening.conf >/dev/null' <<'UNIT'
[Unit]
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/home/ubuntu/v2v-demo/data
ProtectKernelTunables=yes
ProtectControlGroups=yes
MemoryMax=600M
UNIT
    R 'sudo tee /etc/systemd/journald.conf.d/size.conf >/dev/null' <<'JOURNAL'
[Journal]
SystemMaxUse=200M
JOURNAL
    R 'sudo systemctl daemon-reload && sudo systemctl restart systemd-journald'
    restart ;;

  pull-data)
    # Copy the server's data/ (turns, leads, sessions.db) to a private local
    # archive — the Oracle reclamation risk makes this the only backup.
    mkdir -p tmp/backup
    out=tmp/backup/data-$(date -u +%Y%m%dT%H%M%SZ).tgz
    ( umask 077; R 'tar -czf - -C ~/v2v-demo data' >"$out" )
    echo "saved $out ($(du -h "$out" | cut -f1))" ;;

  *) sed -n '2,27p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
