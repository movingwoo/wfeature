#!/bin/sh
# `./start.sh` from the extracted directory. It is the same as running
# ./wfeature-server, with the working directory fixed to this folder so the
# games/ tree beside the binary is the one that gets read.
#
# `-open` shows the page once the port is answering. On a headless box — this
# is as likely to be a server reached over SSH as a desktop — there is no
# session to open it in and the server says so instead of trying.
set -e
cd "$(dirname "$0")"

echo "wfeature 서버를 시작합니다. Control-C를 누르면 멈춥니다."
echo
exec ./wfeature-server -open "$@"
