#!/usr/bin/env bash
# env-show.sh — print .env-style files with every secret masked.
#
# Use this instead of cat / grep on .env*: a plain `grep` over a file with
# keys prints them into the terminal (and into an agent's transcript). Values
# whose key contains KEY / TOKEN / SECRET / PASSWORD show only `set|EMPTY` and
# their length; everything else is printed as is.
#
# Usage:
#   ./minions/env-show.sh                       # .env and .env.server
#   ./minions/env-show.sh .env.client .env      # explicit files
set -euo pipefail
cd "$(dirname "$0")/.."

files=("$@")
[ ${#files[@]} -eq 0 ] && files=(.env .env.server)

for f in "${files[@]}"; do
  if [ ! -f "$f" ]; then
    echo "== $f: missing"
    continue
  fi
  echo "== $f"
  awk -F= '
    /^[[:space:]]*#/ || NF < 2 { next }
    { k = $1; v = substr($0, index($0, "=") + 1) }
    k ~ /(KEY|TOKEN|SECRET|PASSWORD)/ {
      printf "%-26s <%s, len %d>\n", k, (length(v) ? "set" : "EMPTY"), length(v); next
    }
    { printf "%-26s %s\n", k, v }' "$f"
done
