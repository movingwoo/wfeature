#!/bin/sh
# Says which servers are up, on which port, from which profile.
#
#   ./status.sh              # every wfeature server this machine is running
#   ./status.sh 11599        # just that port
#
# Two questions get answered together, because either one alone misleads. A pid
# file says a profile was started but not whether it is still up or what port it
# ended on; a listening port says something is there but not which build it is.
# So the pid files are read first, and then every port they claim — plus the
# usual one — is asked who it belongs to.
set -e
cd "$(dirname "$0")"

run_dir="var/run"
default_port="${WFEATURE_PORT:-11541}"
found=0

port_of_pid() {
	pid="$1"
	if command -v lsof >/dev/null 2>&1; then
		lsof -Pan -p "$pid" -iTCP -sTCP:LISTEN 2>/dev/null |
			sed -n 's/.*:\([0-9][0-9]*\) (LISTEN).*/\1/p' | head -1
		return 0
	fi
	if command -v ss >/dev/null 2>&1; then
		ss -lptnH 2>/dev/null | sed -n "s/.*:\([0-9][0-9]*\) .*pid=$pid,.*/\1/p" | head -1
		return 0
	fi
	return 0
}

listener_pid() {
	port="$1"
	if command -v lsof >/dev/null 2>&1; then
		lsof -ti "tcp:$port" -sTCP:LISTEN 2>/dev/null | head -1
		return 0
	fi
	if command -v ss >/dev/null 2>&1; then
		ss -lptnH "sport = :$port" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1
		return 0
	fi
	return 0
}

# The profile is the binary that is running, so the binary is what gets asked:
# /api/status answers with its own build. A path cannot answer it — `go run`
# compiles into the build cache under a name of its own, and a released binary
# is called wfeature-server whichever profile built it — so the path is only the
# fallback for a server too busy to reply.
profile_of() {
	port="$1"
	command_line="$2"
	status=""
	if command -v curl >/dev/null 2>&1; then
		status="$(curl -sf --max-time 2 "http://127.0.0.1:$port/api/status" 2>/dev/null || true)"
	elif command -v wget >/dev/null 2>&1; then
		status="$(wget -qO- --timeout=2 "http://127.0.0.1:$port/api/status" 2>/dev/null || true)"
	fi
	profile="$(printf '%s' "$status" | sed -n 's/.*"profile":"\([^"]*\)".*/\1/p')"
	if [ -n "$profile" ]; then
		version="$(printf '%s' "$status" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
		echo "$profile${version:+ ($version)}"
		return 0
	fi
	case "$command_line" in
		*build/debug/*) echo "debug (from its path; the server did not answer)" ;;
		*build/release/*) echo "release (from its path; the server did not answer)" ;;
		*) echo "unknown (the server did not answer)" ;;
	esac
}

report_port() {
	port="$1"
	pid="$(listener_pid "$port")"
	[ -n "$pid" ] || return 0
	command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
	case "$command_line" in
		*wfeature-server* | *go-build*/*/server* | *go-build*/server*) ;;
		*)
			echo "port $port  not wfeature: $command_line"
			return 0
			;;
	esac
	echo "port $port  pid $pid  profile: $(profile_of "$port" "$command_line")"
	echo "           http://127.0.0.1:$port"
	echo "           $command_line"
	found=$((found + 1))
}

if [ -n "${1:-}" ]; then
	case "$1" in
		-h | --help | help)
			echo "usage: ./status.sh [port]"
			exit 0
			;;
		*[!0-9]*)
			echo "status.sh: expected a port number, got '$1'" >&2
			exit 2
			;;
	esac
	report_port "$1"
	[ "$found" -eq 0 ] && echo "nothing is listening on port $1."
	exit 0
fi

# A pid file whose process is gone is worth saying out loud: it is the trace a
# crash leaves, and the reason a port can be free while a file claims a server.
claimed=""
for profile in debug release; do
	pid_file="$run_dir/server-$profile.pid"
	[ -f "$pid_file" ] || continue
	pid="$(cat "$pid_file" 2>/dev/null || true)"
	if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
		echo "the $profile server is not running, but $pid_file still claims pid ${pid:-?}"
		echo "           ./stop.sh $profile clears it"
		continue
	fi
	port="$(port_of_pid "$pid")"
	[ -n "$port" ] || port="unknown"
	echo "$profile  pid $pid  port $port  (started by ./start.sh)"
	[ "$port" = "unknown" ] || echo "           http://127.0.0.1:$port"
	echo "           log $run_dir/server-$profile.log"
	# The pid file names the profile it was started as; the server names the
	# profile it was built as. They disagree when the binary was rebuilt as the
	# other profile under a running server, which is worth seeing rather than
	# trusting the file.
	if [ "$port" != "unknown" ]; then
		serving="$(profile_of "$port" "$(ps -p "$pid" -o command= 2>/dev/null || true)")"
		case "$serving" in
			"$profile" | "$profile "*) ;;
			*) echo "           the server says it is $serving" ;;
		esac
	fi
	found=$((found + 1))
	claimed="$claimed $port"
done

# Whatever else is on the usual port: a server started by hand, or one whose
# pid file was lost. A profile above has already accounted for it otherwise.
case " $claimed " in
	*" $default_port "*) ;;
	*) report_port "$default_port" ;;
esac

if [ "$found" -eq 0 ]; then
	echo "no wfeature server is running."
	if ! command -v lsof >/dev/null 2>&1 && ! command -v ss >/dev/null 2>&1; then
		echo "  (neither lsof nor ss is installed, so only ./start.sh's own servers can be seen)"
	fi
fi
