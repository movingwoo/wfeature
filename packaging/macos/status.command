#!/bin/sh
# Double-clicking this file says whether the server is up and on which port.
#
#     ./status.command            # the usual port
#     ./status.command 11542      # another one, if the server was moved
set -e
cd "$(dirname "$0")"
./wfeature-server status "$@"

# A double-clicked window closes the moment the script ends, and the answer
# would go with it.
echo
printf "You can close this window. Press Enter to close it: "
read -r _
