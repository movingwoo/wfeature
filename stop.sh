#!/bin/sh
# Stops a server this checkout left running.
#
#   ./stop.sh             # the release server, and whatever holds the usual port
#   ./stop.sh debug       # the debug server
#   ./stop.sh 11599       # a server on some other port, started some other way
#
# Two things can be holding a port. One is a server ./start.sh started, which
# wrote down its pid. The other is a server started by hand — `make serve`
# closed with the terminal, or a `go run` whose parent was killed while the
# child kept the socket. The second kind leaves nothing behind to read, so it
# is found by asking who is listening.
#
# Nothing is killed on the strength of a port alone: the process has to look
# like this project's server, and what is about to be signalled is printed
# first. A stranger on the port is reported and left alone.
set -e
cd "$(dirname "$0")"

run_dir="var/run"
default_port="${WFEATURE_PORT:-11541}"
stopped=0

# A drain can take a moment — the server finishes the save it is writing — so
# TERM is given time before anything harder is considered.
stop_pid() {
	pid="$1"
	what="$2"
	echo "stopping $what (pid $pid)"
	kill "$pid" 2>/dev/null || true
	waited=0
	while [ "$waited" -lt 100 ]; do
		kill -0 "$pid" 2>/dev/null || return 0
		waited=$((waited + 1))
		sleep 0.1
	done
	echo "  it did not stop on its own; forcing it"
	kill -9 "$pid" 2>/dev/null || true
	return 0
}

stop_profile() {
	profile="$1"
	pid_file="$run_dir/server-$profile.pid"
	[ -f "$pid_file" ] || return 0
	pid="$(cat "$pid_file" 2>/dev/null || true)"
	if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
		stop_pid "$pid" "the $profile server"
		stopped=$((stopped + 1))
	else
		echo "the $profile server is not running; clearing its stale pid file"
	fi
	rm -f "$pid_file"
}

# owning_profile answers which profile's pid file claims a process, so that a
# bare stop can tell a leftover nobody owns from the other profile's server —
# which is somebody's running game, and named rather than killed.
owning_profile() {
	pid="$1"
	for profile in debug release; do
		file="$run_dir/server-$profile.pid"
		[ -f "$file" ] || continue
		if [ "$(cat "$file" 2>/dev/null || true)" = "$pid" ]; then
			echo "$profile"
			return 0
		fi
	done
	return 0
}

# clear_pid_file forgets a process stopped through its port rather than through
# its profile, so nothing is left claiming a pid that has gone.
clear_pid_file() {
	pid="$1"
	owner="$(owning_profile "$pid")"
	[ -n "$owner" ] && rm -f "$run_dir/server-$owner.pid"
	return 0
}

# listener_pid answers who holds a TCP port, or nothing at all. lsof is the
# portable answer between macOS and Linux; ss covers a Linux box without it.
listener_pid() {
	port="$1"
	if command -v lsof >/dev/null 2>&1; then
		lsof -ti "tcp:$port" -sTCP:LISTEN 2>/dev/null | head -1
		return 0
	fi
	if command -v ss >/dev/null 2>&1; then
		ss -lptnH "sport = :$port" 2>/dev/null |
			sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1
		return 0
	fi
	return 0
}

# A port is not proof of identity, and neither is a path: `go run` names its
# executable after the package, so the server can be anything from
# build/debug/wfeature-server to a `server` deep inside the Go build cache.
# Asking the port for the page settles it — the client the server carries is
# what nothing else on the machine will answer with.
is_our_server() {
	port="$1"
	command_line="$2"
	if command -v curl >/dev/null 2>&1; then
		if curl -sf --max-time 2 "http://127.0.0.1:$port/" 2>/dev/null | grep -q "wfeature"; then
			return 0
		fi
	elif command -v wget >/dev/null 2>&1; then
		if wget -qO- --timeout=2 "http://127.0.0.1:$port/" 2>/dev/null | grep -q "wfeature"; then
			return 0
		fi
	fi
	# Without an answer from the port — no HTTP client here, or a server too
	# busy to reply — the executable's own name is the fallback.
	case "$command_line" in
		*wfeature-server*) return 0 ;;
		*go-build*/*/server*) return 0 ;;
		*go-build*/server*) return 0 ;;
	esac
	return 1
}

# stop_port stops whoever is listening on a port, whichever profile it is. The
# port is the thing that was in the way, and a debug server holding it is as
# much in the way as a release one.
stop_port() {
	port="$1"
	pid="$(listener_pid "$port")"
	if [ -z "$pid" ]; then
		return 0
	fi
	command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
	if ! is_our_server "$port" "$command_line"; then
		echo "port $port is held by something that is not a wfeature server; leaving it alone:"
		echo "  pid $pid  $command_line"
		return 0
	fi
	owner="$(owning_profile "$pid")"
	echo "  $command_line"
	stop_pid "$pid" "the ${owner:+$owner }server on port $port"
	clear_pid_file "$pid"
	stopped=$((stopped + 1))
}

case "${1:-}" in
	-h | --help | help)
		echo "usage: ./stop.sh [debug|release|<port>]    (release by default)"
		exit 0
		;;
	debug | release)
		stop_profile "$1"
		;;
	"")
		stop_profile release
		# Then the usual port, whoever is on it: the debug server, or one
		# started by hand that never wrote a pid file. A bare command is the
		# one reached for when a port will not free up, so it clears the port
		# rather than asking which profile put something there.
		stop_port "$default_port"
		;;
	*[!0-9]* | "")
		echo "stop.sh: expected debug, release or a port number, got '$1'" >&2
		exit 2
		;;
	*)
		stop_port "$1"
		;;
esac

if [ "$stopped" -eq 0 ]; then
	echo "nothing to stop."
	if ! command -v lsof >/dev/null 2>&1 && ! command -v ss >/dev/null 2>&1; then
		echo "  (neither lsof nor ss is installed, so a server started by hand cannot be found)"
	fi
fi
