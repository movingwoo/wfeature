#!/bin/sh
# Says whether a server is up, on which port, from which build.
#
#   ./status.sh            # the usual port
#   ./status.sh 11599      # another one
#
# The server is asked what it is rather than guessed at from a process list:
# `go run` names its executable after the package and a released binary is
# called the same thing whichever profile built it, so the path answers
# nothing. See internal/launcher.
set -e
cd "$(dirname "$0")"

for candidate in build/release/wfeature-server build/debug/wfeature-server; do
	[ -x "$candidate" ] && exec "$candidate" status "$@"
done
exec go run ./cmd/server status "$@"
