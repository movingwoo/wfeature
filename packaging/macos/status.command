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
printf "이 창은 닫아도 됩니다. Enter 를 누르면 닫힙니다: "
read -r _
