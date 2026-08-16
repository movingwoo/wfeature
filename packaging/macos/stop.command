#!/bin/sh
# Double-clicking this file in Finder runs it in Terminal and stops the server.
#
#     ./stop.command              # the usual port
#     ./stop.command 11542        # another one, if the server was moved
#
# Closing the window the server runs in does the same thing. This exists for the
# window that was closed without stopping it, or a server left running from a
# session that has since ended.
#
# Nothing is killed on the strength of a port alone: the server is asked what it
# is first, and a stranger holding the port is reported and left alone. That
# rule lives in the binary now — see internal/launcher — so all three operating
# systems follow it the same way.
set -e
cd "$(dirname "$0")"
exec ./wfeature-server stop "$@"
