#!/usr/bin/env bash
# probe-all.sh — run every topic's dialog-probe scenario file and print a
# compact transcript: one line per user turn plus the resolved signal
# ([continue] / [lead_ready] / [escalate]), latency, gate-or-LLM and the slot
# delta. The sweep we repeat after any change to a KB, a system.md, the gate
# or the model.
#
# Scenarios live in tmp/probe-<topic-id>.txt (gitignored, see dialog-probe's
# format in TOOLS.md). Topics without a file are skipped.
#
# Usage:
#   ./minions/probe-all.sh                     # every topic in topics/topics.json
#   ./minions/probe-all.sh auto dental       # selected topics
#   ./minions/probe-all.sh -m gpt-4.1-mini     # override the model
#   ./minions/probe-all.sh -b ollama           # override the backend
set -euo pipefail
cd "$(dirname "$0")/.."

extra=()
ids=()
while [ $# -gt 0 ]; do
  case "$1" in
    -m) extra+=(-model "$2"); shift 2 ;;
    -b) extra+=(-backend "$2"); shift 2 ;;
    *)  ids+=("$1"); shift ;;
  esac
done
[ ${#ids[@]} -eq 0 ] && mapfile -t ids < <(jq -r '.[].id' topics/topics.json)

mkdir -p tmp
go build -o tmp/dialog-probe ./minions/dialog-probe

rc=0
for id in "${ids[@]}"; do
  f=tmp/probe-$id.txt
  if [ ! -f "$f" ]; then
    echo "== $id: no $f, skipped"
    continue
  fi
  echo "== $id"
  # keep the probe's own exit status: a crashed/failed probe must fail the sweep
  out=$(tmp/dialog-probe -topic "$id" "${extra[@]}" "$f" 2>&1) || { rc=1; echo "!! dialog-probe failed for $id"; }
  printf '%s\n' "$out" | grep -E '^#|^  (U:|\[)|rror|panic' | cut -c1-120 || true
done
exit "$rc"
