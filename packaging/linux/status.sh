#!/bin/sh
# `./status.sh` says whether the server is up and on which port.
#
#     ./status.sh            # the usual port
#     ./status.sh 11542      # another one, if the server was moved
#
# The server is asked what it is rather than guessed at from a process list: a
# port can be held by anything, and only this server answers /api/status.
set -e
cd "$(dirname "$0")"
exec ./wfeature-server status "$@"
