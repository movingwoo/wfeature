#!/bin/sh
# Double-clicking this file in Finder runs it in Terminal and says whether the
# server is up and on which port.
#
#     ./status.command            # the usual port
#     ./status.command 11542      # another one, if the server was moved
#
# The server is asked what it is rather than guessed at from a process list: a
# port can be held by anything, and only this server answers /api/status. A
# port that answers nothing is checked for a listener before it is called free,
# because a stranger holding it answers nothing either.
set -e
cd "$(dirname "$0")"

port="${1:-${WFEATURE_PORT:-11541}}"

fetch() {
	curl -s --max-time 2 "http://127.0.0.1:$port/api/status" 2>/dev/null || true
}

listener() {
	lsof -ti "tcp:$port" -sTCP:LISTEN 2>/dev/null | head -1
}

answer="$(fetch)"
case "$answer" in
	*'"server":"wfeature"'*)
		version="$(printf '%s' "$answer" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
		profile="$(printf '%s' "$answer" | sed -n 's/.*"profile":"\([^"]*\)".*/\1/p')"
		echo "wfeature 서버가 돌고 있다."
		echo "  주소     http://127.0.0.1:$port"
		echo "  버전     ${version:-알 수 없음} ($profile)"
		address="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)"
		[ -n "$address" ] && echo "  다른 기기 http://$address:$port"
		echo "  멈추기   stop.command 를 더블클릭"
		exit 0
		;;
esac

pid="$(listener)"
if [ -n "$pid" ]; then
	echo "포트 $port 를 쓰는 것이 있지만 wfeature 서버가 아니다."
	echo "  pid $pid  $(ps -p "$pid" -o command= 2>/dev/null || true)"
	echo "다른 포트로 띄우려면 터미널에서: ./wfeature-server -addr :11542"
	exit 0
fi

echo "포트 $port 에는 wfeature 서버가 없다. start.command 를 더블클릭해서 띄운다."
