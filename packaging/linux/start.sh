#!/bin/sh
# `./start.sh` from the extracted directory. It is the same as running
# ./wfeature-server, with the working directory fixed to this folder so the
# games/ tree beside the binary is the one that gets read.
set -e
cd "$(dirname "$0")"

# A Linux box is as likely to be a headless server reached over SSH as it is a
# desktop, so the page is opened only when there is a session to open it in.
if [ -n "$DISPLAY" ] || [ -n "$WAYLAND_DISPLAY" ]; then
	if command -v xdg-open >/dev/null 2>&1; then
		( sleep 1; xdg-open "http://127.0.0.1:11541" >/dev/null 2>&1 ) &
	fi
fi

echo "wfeature 서버를 시작합니다. Control-C를 누르면 멈춥니다."
echo
exec ./wfeature-server "$@"
