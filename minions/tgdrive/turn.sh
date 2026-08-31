#!/bin/bash
# turn.sh "<message>" [wait_seconds] — send a message to the open web.telegram
# chat, wait, print the bot's reply. Needs cdp.go's chat already opened.
cd "$(dirname "$0")" || exit 1
msg="$1"; w="${2:-60}"
go run . send "$msg" >/dev/null
sleep "$w"
echo "── YOU: $msg"
go run . read 3 | grep '^IN' | tail -2 | sed 's/^IN\t/── BOT: /'
echo
