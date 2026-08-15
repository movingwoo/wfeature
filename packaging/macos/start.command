#!/bin/sh
# Double-clicking this file in Finder runs it in Terminal, which is the only
# way a user who never opens a shell can start the server. Everything it does
# is what `wfeature-server` would do on its own, plus the two things a
# double-click cannot: pick the right working directory, and open the page.
set -e
cd "$(dirname "$0")"

# macOS quarantines every file that arrived in a downloaded archive, and an
# unsigned binary that carries the flag is refused with "cannot be opened".
# Opening this script is what clears the flag on this file; clearing it on the
# rest of the folder is therefore something only this script can do, and it is
# why the launcher exists rather than the bare binary.
xattr -dr com.apple.quarantine . 2>/dev/null || true

# The server binds before it logs, but not instantly, so the page is opened
# from a background shell a moment later instead of before the listener exists.
( sleep 1; open "http://127.0.0.1:11541" ) &

echo "wfeature 서버를 시작합니다. 이 창을 닫거나 Control-C를 누르면 멈춥니다."
echo
exec ./wfeature-server "$@"
