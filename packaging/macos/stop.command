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
# Nothing is killed on the strength of a port alone. Who holds the port is
# asked first and what it is second, in that order: an HTTP answer alone cannot
# tell an empty port from a stranger's, because a stranger answers "not found"
# to a request meant for this server and a missing answer reads the same either
# way.
set -e
cd "$(dirname "$0")"

port="${1:-${WFEATURE_PORT:-11541}}"

listener() {
	lsof -ti "tcp:$port" -sTCP:LISTEN 2>/dev/null | head -1
}

is_wfeature() {
	curl -s --max-time 2 "http://127.0.0.1:$port/api/status" 2>/dev/null |
		grep -q '"server":"wfeature"'
}

pid="$(listener)"
if [ -z "$pid" ]; then
	echo "포트 $port 에는 아무것도 없다. 서버는 이미 멈춰 있다."
	exit 0
fi

if ! is_wfeature; then
	echo "포트 $port 를 쓰는 것은 wfeature 서버가 아니다. 건드리지 않는다."
	echo "  pid $pid  $(ps -p "$pid" -o command= 2>/dev/null || true)"
	exit 1
fi

echo "wfeature 서버를 멈춘다 (pid $pid)."
# 저장 중이면 마무리하고 끝나도록 먼저 정상 종료를 요청한다.
kill "$pid" 2>/dev/null || true
waited=0
while [ "$waited" -lt 100 ]; do
	kill -0 "$pid" 2>/dev/null || {
		echo "멈췄다."
		exit 0
	}
	waited=$((waited + 1))
	sleep 0.1
done

echo "정상 종료에 응답하지 않아 강제로 멈춘다."
kill -9 "$pid" 2>/dev/null || true
echo "멈췄다."
