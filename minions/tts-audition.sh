#!/usr/bin/env bash
# tts-audition.sh — synthesise a Ukrainian test phrase with an ElevenLabs
# voice and play it, to judge voice naturalness before wiring it into the demo.
#
# The phrase deliberately mixes a Latin surname and a EUR amount — the spots
# where eleven_multilingual_v2 is most likely to slip on Ukrainian.
#
# Usage:
#   sbin/tts-audition.sh                  # voice A from .env, default phrase
#   sbin/tts-audition.sh b                # voice B from .env
#   sbin/tts-audition.sh <voice_id>       # an explicit ElevenLabs voice ID
#   sbin/tts-audition.sh a "Свій текст"   # custom phrase
#   MODEL=eleven_turbo_v2_5 sbin/tts-audition.sh a
#
# Reads ELEVENLABS_API_KEY / ELEVENLABS_VOICE_A / ELEVENLABS_VOICE_B from .env.
# Output mp3 lands in tmp/ (gitignored).
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
env_file="$repo_root/.env"
out_dir="$repo_root/tmp"

[[ -f "$env_file" ]] || { echo "no .env at $env_file" >&2; exit 1; }

get() { grep -E "^$1=" "$env_file" | head -1 | cut -d= -f2- | tr -d '[:space:]'; }

key=$(get ELEVENLABS_API_KEY)
[[ -n "$key" ]] || { echo "ELEVENLABS_API_KEY not set in .env" >&2; exit 1; }

sel=${1:-a}
case "$sel" in
	a|A) voice=$(get ELEVENLABS_VOICE_A); label=A ;;
	b|B) voice=$(get ELEVENLABS_VOICE_B); label=B ;;
	*)   voice=$sel; label=$sel ;;
esac
[[ -n "$voice" ]] || { echo "no voice id for '$sel'" >&2; exit 1; }

text=${2:-"Доброго дня! Пані Kovalenko, засвідчений переклад диплома коштуватиме приблизно 45 євро за сторінку. Готова прийняти замовлення."}
model=${MODEL:-eleven_multilingual_v2}

mkdir -p "$out_dir"
out="$out_dir/audition-${label}-$(date +%Y%m%d-%H%M%S).mp3"

body=$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1], "model_id": sys.argv[2], "output_format": "mp3_44100_128"}))' "$text" "$model")

code=$(curl -sS -o "$out" -w '%{http_code}' -X POST \
	-H "xi-api-key: $key" -H 'Content-Type: application/json' \
	-d "$body" \
	"https://api.elevenlabs.io/v1/text-to-speech/${voice}")

if [[ "$code" != "200" ]]; then
	echo "HTTP $code:" >&2
	cat "$out" >&2 && echo >&2
	rm -f "$out"
	exit 1
fi

echo "voice $label ($voice) · $model · $(stat -c%s "$out") bytes"
echo "→ $out"

for p in mpv ffplay mplayer; do
	command -v "$p" >/dev/null || continue
	case "$p" in
		mpv)     exec mpv --really-quiet "$out" ;;
		ffplay)  exec ffplay -autoexit -nodisp -loglevel quiet "$out" ;;
		mplayer) exec mplayer -really-quiet "$out" ;;
	esac
done
echo "(no audio player found — open the file manually)"
