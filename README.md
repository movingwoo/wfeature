# W-Feature

옛날 피처폰 게임을 구동해볼 수 있는 에뮬레이터입니다.  
KTF·LGT·SKT 게임을 지원합니다.  

> [!IMPORTANT]
> ⚠️ **개발 중인 베타버전입니다.**  
> 모든 게임을 지원하지 않습니다.  
> 지원 게임 목록 : [`docs/support.md`](docs/support.md)

서버를 기동 후 웹 브라우저로 접근하는 방식이며 PWA 설치를 지원합니다.  
기본 접속 정보는 `http://127.0.0.1:11541`입니다.  
파일을 폴더에 넣고 에뮬레이터를 실행합니다.  
운영체제 별 설치 프로그램이나 별도 앱은 없습니다.

**게임은 서버에서 돌고 브라우저는 화면만 받습니다.**  
자세한 내용은 [`docs/session.md`](docs/session.md) 문서를 참조해주세요.

---

## 0. 아카이브 다운로드 시  

[**Releases**](https://github.com/movingwoo/wfeature/releases)에서 운영체제에 맞는 파일을 받습니다.  
압축을 풀고, 안에 있는 `games/` 폴더에 게임을 넣고 실행합니다.  

| | 실행 | 처음 한 번 |
|---|---|---|
| Windows | `start.bat` 더블클릭 | SmartScreen → 추가 정보 → 실행, 방화벽 허용 |
| macOS | `start.command` 더블클릭 | Control-클릭 → 열기 (서명된 앱이 아님) |
| Linux | `./start.sh` | 권한 문제 시 `chmod +x start.sh wfeature-server` |

아카이브에도 `stop`/`status`가 같이 들어 있습니다(`stop.sh`·`stop.command`·`stop.bat`).  
서버 창을 닫아버렸거나 예전에 띄운 게 남아 있을 때 씁니다.  

압축 안의 `README.txt`를 읽어주세요.  
아카이브를 직접 만들려면 `make dist` 명령어를 사용합니다.  
자세한 내용은 [`docs/running.md`](docs/running.md) 문서를 참조해주세요.  
버전별 변경 내역은 [`CHANGELOG.md`](CHANGELOG.md) 문서에 있습니다.

## 1. 직접 빌드 시 준비물

저장소에서 직접 빌드해 쓰는 경우엔 아래 순서대로 진행합니다.

- **Go 1.24 이상**
- (선택) **Node.js** — 페이지 쪽 테스트를 돌릴 때만 사용

설치된 Golang 버전은 이렇게 확인합니다.

```sh
go version
```

## 2. 게임 파일 넣기

저장소 안 `var/games/` 아래에 넣습니다.  

어느 플랫폼으로 읽을지는 폴더 이름이 아니라 파일 내용을 보고 정합니다.  
폴더는 카테고리 명으로 사용되며 아무 이름이나 지어도 됩니다.  
2 Depth 폴더부터는 인식하지 않습니다.

## 3. 실행

```sh
make serve
```

빌드가 끝나면 브라우저에서 `http://127.0.0.1:11541`로 접속합니다.  
이미 빌드해 뒀다면 서버만 띄우면 됩니다.

```sh
./build/debug/wfeature-server
```

`make serve`는 터미널을 붙잡고 있고 Ctrl-C로 멈춥니다.  
띄워 둔 채로 쓰려면 스크립트 네 개를 씁니다 (macOS·리눅스).

```sh
./build.sh        # 빌드
./start.sh        # 백그라운드로 기동 — 주소와 pid를 알려줍니다
./status.sh       # 지금 뭐가 몇 번 포트에 떠 있는지
./stop.sh         # 종료 (`./stop.sh 11599` 처럼 포트를 줄 수 있습니다)
```

인자 없이 부르면 **릴리즈 프로필**입니다.  
디버그로 쓰려면 `./build.sh debug`, `./start.sh debug`처럼 붙입니다.  
`./start.sh` 뒤에 붙인 나머지 인자는 서버로 그대로 넘어갑니다(`./start.sh debug -addr 127.0.0.1:11599`).

`./status.sh`와 `./stop.sh`는 포트를 받습니다(`./stop.sh 11599`).  
스크립트 밖에서 띄운 서버도 정리하고, 포트만 보고 죽이지는 않습니다 —
**그 포트가 wfeature인지 서버에게 물어본 뒤에만** 종료합니다.

Windows에서 `make` 없이 실행하는 방법은 [`docs/running.md`](docs/running.md) 문서를 참조해주세요.

주소나 폴더는 플래그로 바꿉니다.  
`-addr`, `-games`, `-saves`, `-logs`, `-web`, `-version`
환경 변수(`WFEATURE_ADDR`, `WFEATURE_PORT`, `WFEATURE_HOST`, `WFEATURE_GAME_ROOT`, `WFEATURE_SAVE_ROOT`, `WFEATURE_LOG_ROOT`, `WFEATURE_WEB_ROOT`)

### 소켓 방식

`-addr`에 `unix:` 를 붙이면 포트 대신 소켓 파일로 띄웁니다.  

```sh
./build/release/wfeature-server -addr unix:/run/wfeature/wfeature.sock
```

소켓 파일은 `0660`으로 만들어지므로 프록시와 **그룹을 맞춰야** 합니다(systemd `Group=`).  
프록시 설정에서 **브라우저의 `Host` 헤더를 그대로 넘겨야 합니다.**  

`./start.sh`·`./stop.sh`·`./status.sh` 는 포트를 기준으로 동작하므로 이 방식에는 쓰지 않습니다.  
리버스 프록시 설정 예시와 systemd 유닛은 [`docs/running.md`](docs/running.md) 문서에 있습니다.

## 4. 게임 시작하기

폴더에 넣은 게임 목록 중 하나를 선택하고 **실행**합니다.

처음 시작은 시간이 걸릴 수 있습니다.  

## 조작

키보드와 화면 키패드를 같이 쓸 수 있습니다.

| 키보드 | 전화기 키 |
|---|---|
| 방향키 | 위 / 아래 / 왼쪽 / 오른쪽 |
| Space | 가운데(확인) |
| Backspace | CLR |
| `1` `2` `3` | 1 2 3 |
| Q W E | 4 5 6 |
| A S D | 7 8 9 |
| Z X C | \* 0 # |

위 표는 키보드 기본값입니다.  
설정의 **⌨️ 키 설정**에서 임의의 키와 직접 매핑할 수 있습니다.

키패드는 배치 버튼을 눌러 `Type1` → `Type2` → `Type3` 순으로 전환합니다.

PC 웹 브라우저와 같은 넓은 화면 + 디버그 빌드에서는 화면 왼쪽에 실행 로그가 나옵니다.

## 설정

키패드 왼쪽 위 `Opts` 버튼을 누릅니다.

- **🎵 배경음 / 🔊 효과음** — 배경음과 효과음을 조절합니다.
- **🔍 화질** — 원본 / hq2x / hq3x / hq4x. 업스케일링 시 속도가 느려질 수 있습니다.
- **📱 화면** — 게임에게 알려줄 단말 화면입니다. 기본은 240×320이고 게임마다
  따로 기억하며, 바꾸면 다음 실행부터 적용됩니다. 확대되어 보이면 화면을 키우고
  축소되어 보이면 화면을 줄이면 됩니다.
- **⏩ 속도** — 0.25배에서 4배까지 속도를 조절합니다. 게임이 자기 속도를
  하드웨어에 맡겨 두어 원래 단말보다 빠르게 도는 경우 0.25~0.75배로 늦출 수
  있습니다.
- **⌨️ 키 설정** — 전화기 키마다 쓸 키보드 키를 정합니다. 키 이름을 누른 다음
  새 키를 누르면 바뀌고, `Esc` 는 취소, `✕` 는 그 자리를 비웁니다. 이미 쓰고
  있는 키를 고르면 그 키를 쓰던 자리가 대신 빕니다. **기본값으로** 를 누르면
  위 표로 돌아갑니다. 키보드가 있는 환경에서만 나옵니다 — 키를 한 번이라도
  누른 브라우저에 나타나며, 폰에서는 나오지 않습니다.
- **🎯 치트** — 별도 설명을 참조해주세요.
- **🐞 디버그 로그 저장** — 디버그 빌드에서 로그를 서버에 저장합니다.
- **🔄 재시작** — 게임 선택 화면으로 돌아갑니다.

## 세이브

게임 내 저장 시 아래 경로에 저장됩니다.

```
var/savedata/<빌드 프로필>/<플랫폼>/<게임PID>/
```

debug 빌드와 release 빌드는 세이브를 따로 씁니다.  

## 치트

메모리 변조 치트 도구입니다.  
범위를 좁혀가며 원하는 주소를 찾을 수 있습니다.  

1. 게임에서 바꾸고 싶은 값(예: 소지금)을 확인한다
2. 치트 패널에 그 숫자를 넣고 **이 값으로 찾기**를 누른다
3. 게임으로 돌아가 값이 달라지게 만든다
4. 새 값을 넣고 다시 **이 값으로 찾기**. 후보가 몇 개로 줄 때까지 반복한다

찾은 주소는 **고정**해두면 그 값이 계속 유지됩니다.  
**쓰기 감시**에 주소를 넣으면 어떤 코드가 그 값을 건드리는지 보입니다.  
찾아놓은 값 테이블은 **저장** 및 **불러오기** 할 수 있습니다.

---

# 개발자용

여기서부터는 저장소를 직접 만지는 사람을 위한 내용입니다.  
기술 문서는 모두 `docs/` 아래에 있습니다.

## 빌드 프로필

개발용·운영용 서버를 따로 두지 않고 같은 코드를 두 프로필로 빌드합니다.  
debug는 로그와 진단을 모두 포함하고 release는 진단을 최대한 줄입니다.  
프로필마다 출력 폴더가 달라서 서로 덮어쓰지 않습니다.

| 명령 | 결과물 |
|---|---|
| `make debug` | `build/debug/wfeature` |
| `make release` | `build/release/wfeature` |
| `make server` | `build/debug/wfeature-server` — 웹 자산이 들어 있는 단일 실행 파일 |
| `make server-release` | `build/release/wfeature-server` |

서버도 프로필별로 빌드됩니다.  
프로필을 고르는 플래그는 없으며 돌고 있는 바이너리가 곧 프로필입니다.

```sh
make serve              # 디버그
make serve-release      # 릴리즈
```

배포본은 `make dist VERSION=0.2.0`이 다섯 OS용 아카이브로 묶습니다.  
`v`로 시작하는 태그를 밀면 GitHub Actions가 같은 아카이브를 만들어 릴리스로 올리고 푸시마다 우분투·윈도우·macOS에서 서버를 실제로 띄워 봅니다.  
자세한 내용은 [`docs/running.md`](docs/running.md)에 있습니다.

## CLI

브라우저 없이 게임을 돌려보는 도구입니다.  
서버가 쓰는 것과 같은 에뮬레이터이며 tick 수·재현 스크립트·프로파일러·GDB를 붙일 수 있습니다.

```sh
make run ARGS="runktf var/games/ktf/game.zip -play"
```

`-framedir`로 남긴 프레임은 `contactsheet`로 한 장에 모아 보고 `framediff`로 두 빌드를 비교하며, `zoom`으로 한 프레임의 일부를 확대해 봅니다.  
명령과 플래그 전체, 재현 스크립트와 `ktfdump`는 [`docs/cli.md`](docs/cli.md)에 있습니다.

## 테스트

```sh
make test          # go test + Node 테스트
```

무엇을 어디까지 검증하는지는 [`docs/testing.md`](docs/testing.md)에 있습니다.

## 문서

| 문서 | 내용 |
|---|---|
| [`docs/architecture.md`](docs/architecture.md) | Host / Runtime / Execution 계층 |
| [`docs/session.md`](docs/session.md) | 서버 세션 — 프로토콜, 페이싱, 프레임 스킵 |
| [`docs/running.md`](docs/running.md) | OS별 실행, 데이터 위치 |
| [`docs/cli.md`](docs/cli.md) | CLI 명령과 플래그, 재현 스크립트, ktfdump |
| [`docs/armcore.md`](docs/armcore.md) | ARM 코어와 성능 |
| [`docs/jvm.md`](docs/jvm.md) | 바이트코드 인터프리터 |
| [`docs/ktf.md`](docs/ktf.md) | KTF 플랫폼 |
| [`docs/lgt.md`](docs/lgt.md) | LGT 플랫폼 |
| [`docs/skvm.md`](docs/skvm.md) | SKT / SKVM |
| [`docs/lcdui.md`](docs/lcdui.md) | LCDUI |
| [`docs/rms.md`](docs/rms.md) | RMS 저장소 |
| [`docs/network.md`](docs/network.md) | 네트워크 — 전 플랫폼 거부 정책과 그 표면 |
| [`docs/audio.md`](docs/audio.md) | 소리 |
| [`docs/hqx.md`](docs/hqx.md) | hqx 화면 확대 |
| [`docs/testing.md`](docs/testing.md) | 테스트 전략과 로컬 검증 |
| [`docs/support.md`](docs/support.md) | 지원 게임 목록 — 타이틀별 구동·테스트 상태 |

작업 규칙은 [`AGENTS.md`](AGENTS.md)에 있습니다.

## 라이선스

MIT — [`LICENSE`](LICENSE)
다만 이 프로젝트 내 번들된 구성 요소는 각자의 라이선스를 유지합니다.

| 구성 요소 | 쓰임 | 라이선스 |
|---|---|---|
| `golang.org/x/text`, `golang.org/x/image` | Go 모듈, 정적 링크 | BSD-3-Clause |
| NeoDGM (Neo둥근모) | 임베드 폰트 | SIL OFL 1.1 (Reserved Font Name) |
| Galmuri9 | 임베드 폰트 | SIL OFL 1.1 (Reserved Font Name) |
| hqx | `internal/filter/hqx`의 결정 테이블 번역본 | MIT OR Apache-2.0 |

전문은 [`internal/licenses/THIRD-PARTY-NOTICES.md`](internal/licenses/THIRD-PARTY-NOTICES.md)에 있고 **모든 릴리스 바이너리가 이 파일을 안고 나갑니다.**  
`wfeature licenses`로 출력하거나 서버의 `/licenses`로 받을 수 있습니다.  

## 후원

이 프로젝트가 마음에 드셨다면 [GitHub Sponsors](https://github.com/sponsors/movingwoo)로 후원할 수 있습니다.  
후원은 전적으로 선택 사항이며, 후원 여부와 관계없이 모든 기능은 동일하게 제공됩니다.  
