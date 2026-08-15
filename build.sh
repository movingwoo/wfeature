#!/bin/sh
# Builds the CLI and the server for one profile.
#
#   ./build.sh            # release, which is what a bare command means here
#   ./build.sh debug      # debug, for the diagnostics
#
# The build flags live in the Makefile and are not repeated here: this script
# picks the targets and reports where the binaries landed, so that there is one
# place a flag can be wrong. `make debug` and `make server` do the same thing
# for anyone who prefers typing them.
set -e
cd "$(dirname "$0")"

profile="${1:-release}"
case "$profile" in
	debug) targets="debug server" ;;
	release) targets="release server-release" ;;
	-h | --help | help)
		echo "usage: ./build.sh [debug|release]    (release by default)"
		exit 0
		;;
	*)
		echo "build.sh: unknown profile '$profile'; expected debug or release" >&2
		exit 2
		;;
esac

if ! command -v make >/dev/null 2>&1; then
	echo "build.sh: make is not installed" >&2
	exit 1
fi

# shellcheck disable=SC2086
make $targets

echo
echo "built the $profile profile:"
ls -lh "build/$profile" | tail -n +2 | awk '{ printf "  %-24s %s\n", $9, $5 }'
echo
echo "start the server with ./start.sh $profile"
