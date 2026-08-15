#!/bin/sh
# Starts the server in the background and leaves the terminal free.
#
#   ./start.sh                        # release profile on the usual port
#   ./start.sh debug                  # debug profile, for the diagnostics
#   ./start.sh debug -addr :11599     # anything after the profile goes to the server
#
# This is the development launcher, not the one a release archive carries —
# that one is packaging/<os>/start.* and runs in the foreground, where closing
# the window is the whole of stopping it. Here the server outlives the shell,
# so what it writes down is what ./stop.sh reads: the process it started, and
# nothing else on the machine.
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
run_dir="var/run"
pid_file="$run_dir/server-$profile.pid"
log_file="$run_dir/server-$profile.log"

# A pid file outlives a crash, so what decides whether a server is up is the
# process, not the file.
if [ -f "$pid_file" ]; then
	running_pid="$(cat "$pid_file" 2>/dev/null || true)"
	if [ -n "$running_pid" ] && kill -0 "$running_pid" 2>/dev/null; then
		echo "start.sh: the $profile server is already running (pid $running_pid)."
		echo "           ./stop.sh $profile stops it; its log is $log_file"
		exit 1
	fi
	rm -f "$pid_file"
fi

if [ ! -x "$binary" ]; then
	echo "$binary is missing; building it first."
	./build.sh "$profile" >/dev/null
fi

mkdir -p "$run_dir"
# The log is truncated per run: it is this run's story, and a log that grows
# across weeks of restarts is one nobody reads.
: > "$log_file"
"./$binary" "$@" >>"$log_file" 2>&1 &
pid=$!
echo "$pid" > "$pid_file"

# The server logs the address it actually bound, which is the only place the
# answer is certain: it can come from an argument, from WFEATURE_ADDR, or from
# the default, and a port already taken means it never bound at all.
url=""
attempt=0
while [ "$attempt" -lt 50 ]; do
	if ! kill -0 "$pid" 2>/dev/null; then
		rm -f "$pid_file"
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

if [ -z "$url" ]; then
	echo "start.sh: the server is up (pid $pid) but has not reported an address yet."
	echo "           its log is $log_file"
	exit 0
fi

# Every interface prints as [::], which is not something to paste into a
# browser: the port is the part that matters, and whether a phone can reach it
# is decided by whether the bind was loopback or not.
port="${url##*:}"
echo "wfeature $profile server is running (pid $pid)."
echo "  http://127.0.0.1:$port"
case "$url" in
	*"[::]"* | *"0.0.0.0"*)
		echo "  bound on every interface, so a phone on the same network can reach it too"
		;;
	*)
		echo "  bound at $url only"
		;;
esac
echo "  log:  $log_file"
echo "  stop: ./stop.sh $profile"
