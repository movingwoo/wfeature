#!/bin/sh
# Stops a server this checkout left running.
#
#   ./stop.sh             # whatever holds the usual port
#   ./stop.sh 11599       # a server on some other port
#
# The work is `wfeature-server stop`: the server is asked what it is, asked to
# stop itself, and only signalled if it will not. A stranger holding the port
# is reported and left alone. See internal/launcher — that used to be this
# script, three times over, once per operating system.
set -e
cd "$(dirname "$0")"

# Any build can answer for any other, because the question goes over HTTP to
# the port. Prefer one that is already built and fall back to source, so a
# fresh checkout can still stop a server it started with `make serve`.
for candidate in build/release/wfeature-server build/debug/wfeature-server; do
	[ -x "$candidate" ] && exec "$candidate" stop "$@"
done
exec go run ./cmd/server stop "$@"
