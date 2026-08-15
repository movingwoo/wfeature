#!/bin/sh
# `./stop.sh` stops the server this folder started.
#
#     ./stop.sh              # the usual port
#     ./stop.sh 11542        # another one, if the server was moved
#
# Control-C in the window it was started from does the same thing. This exists
# for the window that was closed, or the server left running from a login that
# has since ended — the cases where there is nothing left to press Control-C in.
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
	if command -v lsof >/dev/null 2>&1; then
		lsof -ti "tcp:$port" -sTCP:LISTEN 2>/dev/null | head -1
	elif command -v ss >/dev/null 2>&1; then
		ss -lptnH "sport = :$port" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1
	elif command -v fuser >/dev/null 2>&1; then
		fuser "$port/tcp" 2>/dev/null | tr -d ' ' | head -1
	fi
}

is_wfeature() {
	if command -v curl >/dev/null 2>&1; then
		curl -s --max-time 2 "http://127.0.0.1:$port/api/status" 2>/dev/null |
			grep -q '"server":"wfeature"'
		return $?
	fi
	if command -v wget >/dev/null 2>&1; then
		wget -qO- --timeout=2 "http://127.0.0.1:$port/api/status" 2>/dev/null |
			grep -q '"server":"wfeature"'
		return $?
	fi
	# 물어볼 도구가 없으면 실행 파일 이름으로 판단한다.
	ps -p "$1" -o command= 2>/dev/null | grep -q "wfeature-server"
}

pid="$(listener)"
if [ -z "$pid" ]; then
	if command -v lsof >/dev/null 2>&1 || command -v ss >/dev/null 2>&1 ||
		command -v fuser >/dev/null 2>&1; then
		echo "포트 $port 에는 아무것도 없다. 서버는 이미 멈춰 있다."
		exit 0
	fi
	echo "포트를 쓰는 프로세스를 찾을 도구가 없다 (lsof, ss, fuser 중 하나가 필요하다)."
	echo "서버를 띄운 창에서 Control-C 를 누른다."
	exit 1
fi

if ! is_wfeature "$pid"; then
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
