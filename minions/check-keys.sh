#!/usr/bin/env bash
# check-keys.sh — are the credentials in an env file actually accepted?
#
# Prints one OK/FAIL line per service and NEVER the secret itself: keys are
# passed to curl on stdin (not argv, so they don't show up in `ps`), and only
# HTTP codes / public identifiers (the bot's @username) are echoed. Exit
# status 1 if any check failed.
#
#   telegram    getMe  -> @username of the bot the token belongs to
#   openai      a 1-token chat call to <DIALOG_MODEL>, plus a whisper-1 permission probe
#               (when the config selects openai for anything)
#   azure       Speech issueToken for AZURE_SPEECH_REGION (HTTP 200 = valid)
#   elevenlabs  only with --tts (it synthesises 1 word, ~4 credits)
#
# Usage:
#   ./minions/check-keys.sh                    # .env.server
#   ./minions/check-keys.sh .env               # another file
#   ./minions/check-keys.sh .env.server --tts  # + ElevenLabs
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

f=.env.server
tts=0
for a in "$@"; do
  case "$a" in
    --tts) tts=1 ;;
    *) f=$a ;;
  esac
done
[ -f "$f" ] || { echo "no such file: $f" >&2; exit 2; }

val() { # val KEY -> value, surrounding quotes stripped
  local v
  v=$(awk -F= -v k="$1" '$1 == k { print substr($0, index($0, "=") + 1); exit }' "$f")
  v=${v%\"}; v=${v#\"}; v=${v%\'}; v=${v#\'}
  printf '%s' "$v"
}

body=$(mktemp -p "$PWD/tmp" 2>/dev/null || mktemp)
trap 'rm -f "$body"' EXIT
fail=0
ok()   { printf 'OK   %-10s %s\n' "$1" "$2"; }
bad()  { printf 'FAIL %-10s %s\n' "$1" "$2"; fail=1; }
skip() { printf 'skip %-10s %s\n' "$1" "$2"; }

# which backends this config actually selects — a missing key for a selected
# one is a FAIL, not a skip
sel=" $(val DIALOG_BACKEND) $(val DIALOG_FALLBACK_BACKEND) $(val STT_BACKEND) $(val STT_FALLBACK_BACKEND) $(val TTS_BACKEND) $(val TTS_FALLBACK_BACKEND) "
selected() { case "$sel" in *" $1 "*) return 0 ;; esac; return 1; }
# defaults the bot applies when a *_BACKEND is unset
[ -z "$(val DIALOG_BACKEND)" ] && sel="$sel ollama "
[ -z "$(val STT_BACKEND)" ] && sel="$sel local "
[ -z "$(val TTS_BACKEND)" ] && sel="$sel elevenlabs "
missing() { # missing SERVICE REASON
  if selected "$1"; then bad "$1" "$2 — and this config selects the $1 backend"; else skip "$1" "$2"; fi
}

echo "== $f"

# --- telegram
tok=$(val TELEGRAM_BOT_TOKEN)
if [ -z "$tok" ]; then
  bad telegram "TELEGRAM_BOT_TOKEN empty"
else
  code=$(printf 'url = "https://api.telegram.org/bot%s/getMe"\n' "$tok" |
    curl -s -K - -o "$body" -w '%{http_code}' --max-time 20)
  if [ "$code" = 200 ]; then
    ok telegram "@$(jq -r .result.username "$body") (id $(jq -r .result.id "$body"))"
  else
    bad telegram "HTTP $code"
  fi
fi

# --- openai (stt + dialog)
oak=$(val OPENAI_API_KEY)
model=$(val DIALOG_MODEL); model=${model:-gpt-4.1-mini}
if [ -z "$oak" ]; then
  missing openai "OPENAI_API_KEY empty"
else
  # A real 1-token chat call (a fraction of a cent), not GET /v1/models: a
  # project-restricted key is often forbidden to *list* models yet allowed to
  # chat, which made the model-list probe a false FAIL on the working key.
  code=$(printf 'Authorization: Bearer %s\nContent-Type: application/json\n' "$oak" |
    curl -s -H @- -o /dev/null -w '%{http_code}' --max-time 30 \
      -d "{\"model\":\"$model\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"max_tokens\":1}" \
      https://api.openai.com/v1/chat/completions)
  if [ "$code" = 200 ]; then
    ok openai "chat with $model works"
  else
    bad openai "HTTP $code for $model (401 bad key, 403/404 key not allowed that model, 429 no balance)"
  fi

  # A restricted key can chat yet lack the Audio permission — voice would then
  # break silently. Probe whisper-1 with a deliberately invalid file: the
  # permission check runs before the file is parsed, so 400 = allowed (bad
  # file, as intended), 401/403/404 = not allowed. Costs nothing.
  if selected openai; then
    printf 'not audio' >"$body.bin"
    code=$(printf 'Authorization: Bearer %s\n' "$oak" |
      curl -s -H @- -o /dev/null -w '%{http_code}' --max-time 30 \
        -F model=whisper-1 -F "file=@$body.bin;filename=probe.ogg" \
        https://api.openai.com/v1/audio/transcriptions)
    rm -f "$body.bin"
    case "$code" in
      400) ok openai "whisper-1 reachable (probe file rejected, as expected)" ;;
      *)   bad openai "whisper-1: HTTP $code (403 = the key lacks the Audio permission)" ;;
    esac
  fi
fi

# --- azure speech
azk=$(val AZURE_SPEECH_KEY); azr=$(val AZURE_SPEECH_REGION)
if [ -z "$azk" ] || [ -z "$azr" ]; then
  missing azure "AZURE_SPEECH_KEY / AZURE_SPEECH_REGION not set"
else
  code=$(printf 'Ocp-Apim-Subscription-Key: %s\n' "$azk" |
    curl -s -H @- -X POST -H 'Content-Length: 0' -o /dev/null -w '%{http_code}' --max-time 20 \
      "https://$azr.api.cognitive.microsoft.com/sts/v1.0/issueToken")
  if [ "$code" = 200 ]; then
    ok azure "region $azr"
  else
    bad azure "HTTP $code in region $azr (401 = key invalid or from another region)"
  fi
fi

# --- elevenlabs (costs a few credits, so opt-in)
elk=$(val ELEVENLABS_API_KEY); elv=$(val ELEVENLABS_VOICE_A)
if [ "$tts" = 0 ]; then
  skip elevenlabs "pass --tts to synthesise one word"
elif [ -z "$elk" ] || [ -z "$elv" ]; then
  missing elevenlabs "ELEVENLABS_API_KEY / ELEVENLABS_VOICE_A not set"
else
  code=$(printf 'xi-api-key: %s\n' "$elk" |
    curl -s -H @- -H 'Content-Type: application/json' -o "$body" -w '%{http_code}' --max-time 30 \
      -X POST "https://api.elevenlabs.io/v1/text-to-speech/$elv?output_format=mp3_22050_32" \
      -d '{"text":"Тест","model_id":"eleven_multilingual_v2"}')
  if [ "$code" = 200 ]; then
    ok elevenlabs "synthesised $(stat -c %s "$body") bytes"
  else
    bad elevenlabs "HTTP $code"
  fi
fi

exit "$fail"
