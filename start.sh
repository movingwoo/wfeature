#!/bin/sh
# Starts the server in the background and leaves the terminal free.
#
#   ./start.sh                        # release profile on the usual port
#   ./start.sh debug                  # debug profile, for the diagnostics
#   ./start.sh debug -addr :11599     # anything after the profile goes to the server
#
# This is the development launcher, not the one a release archive carries —
# that one is packaging/<os>/start.* and runs in the foreground, where closing
# the window is the whole of stopping it.
#
# Backgrounding is all this does. Whether a server is already up, which one it
# is and how to stop it are `./status.sh` and `./stop.sh`, which ask the server
# rather than keep run state here: a pid file outlives the process it names,
# and this checkout kept two of them.
#
# `make serve` is still the way to watch a server in the foreground.
set -e
cd "$(dirname "$0")"

profile="${1:-release}"
case "$profile" in
	debug | release) shift 2>/dev/null || true ;;
	-h | --help | help)
		echo "usage: ./start.sh [debug|release] [server arguments...]    (release by default)"
		exit 0
		;;
	# A first argument for the server rather than a profile, so the profile is
	# the default one and every argument still belongs to the server.
	-*) profile="release" ;;
	*)
		echo "start.sh: unknown profile '$profile'; expected debug or release" >&2
		exit 2
		;;
esac

binary="build/$profile/wfeature-server"
log_file="var/run/server-$profile.log"

if [ ! -x "$binary" ]; then
	echo "$binary is missing; building it first."
	./build.sh "$profile" >/dev/null
fi

mkdir -p "$(dirname "$log_file")"
# The log is truncated per run: it is this run's story, and a log that grows
# across weeks of restarts is one nobody reads.
: > "$log_file"
"./$binary" "$@" >>"$log_file" 2>&1 &
pid=$!

# The server logs the address it actually bound, which is the only place the
# answer is certain: it can come from an argument, from WFEATURE_ADDR, or from
# the default, and a port already taken means it never bound at all.
attempt=0
while [ "$attempt" -lt 50 ]; do
	if ! kill -0 "$pid" 2>/dev/null; then
		echo "start.sh: the server exited immediately:" >&2
		echo >&2
		sed 's/^/  /' "$log_file" >&2
		exit 1
	fi
	url="$(sed -n 's/.*url=\([^ ]*\).*/\1/p' "$log_file" | head -1)"
	[ -n "$url" ] && break
	attempt=$((attempt + 1))
	sleep 0.2
done

if [ -z "${url:-}" ]; then
	echo "start.sh: the server is up (pid $pid) but has not reported an address yet."
	echo "           its log is $log_file"
	exit 0
fi

echo "wfeature $profile server is running (pid $pid)."
echo "  log:  $log_file"
echo
# What it is and where to reach it comes from the server itself, so this says
# the same thing ./status.sh does rather than a second version of it.
"./$binary" status "${url##*:}" || true
