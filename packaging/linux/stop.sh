#!/bin/sh
# `./stop.sh` stops the server this folder started.
#
#     ./stop.sh              # the usual port
#     ./stop.sh 11542        # another one, if the server was moved
#
# Control-C in the window it was started from does the same thing. This exists
# for the window that was closed, or the server left running from a login that
# has since ended.
#
# Nothing is killed on the strength of a port alone: the server is asked what it
# is first, and a stranger holding the port is reported and left alone. That
# rule lives in the binary now — see internal/launcher — so it no longer needs
# whichever of lsof, ss or fuser this machine happens to have.
set -e
cd "$(dirname "$0")"
exec ./wfeature-server stop "$@"
