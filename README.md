# W-Feature

옛날 피처폰 게임을 구동해볼 수 있는 에뮬레이터입니다.  
KTF·LGT·SKT 게임을 지원합니다. SKT의 GNEX/GVM `.SGS` 실행 경로도 포함하며, 일부 서비스는 아직 미지원입니다.

> [!IMPORTANT]
> ⚠️ **개발 중인 베타버전입니다.**  
> 아직 모든 게임을 지원하지 않습니다.  
> 오류 발생시 디버그 로그와 함께 제보 부탁드립니다.

서버를 기동 후 웹 브라우저로 접근하는 방식이며 PWA 설치를 지원합니다.  
기본 접속 정보는 `http://127.0.0.1:11541`입니다.  
파일을 폴더에 넣고 에뮬레이터를 실행합니다.  
  
모바일에서는 웹으로 서버에 접근하거나 apk, ipa를 설치하여 이용합니다.  

---

## 0. 아카이브 다운로드 시  

[**Releases**](https://github.com/movingwoo/wfeature/releases)에서 운영체제에 맞는 파일을 받습니다.  
압축을 풀고, 안에 있는 `games/` 폴더에 게임을 넣고 실행합니다.  
폰에서는 폴더에 넣을 수 없으니 화면의 **＋ 게임 추가** 버튼으로 넣습니다(지울 때도 같은 화면의 **게임 삭제**를 씁니다).  

| | 실행 | 처음 한 번 |
|---|---|---|
| Windows | `start.bat` 더블클릭 | SmartScreen → 추가 정보 → 실행, 방화벽 허용 |
| macOS | `start.command` 더블클릭 | Control-클릭 → 열기 (서명된 앱이 아님) |
| Linux | `./start.sh` | 권한 문제 시 `chmod +x start.sh wfeature-server` |
| Android *(실험)* | `.apk` 설치 | 출처를 알 수 없는 앱 허용 |
| iOS *(실험)* | `.ipa` 사이드로드 | AltStore·Sideloadly로 본인 Apple ID 서명 |

아카이브에도 `stop`/`status`가 같이 들어 있습니다(`stop.sh`·`stop.command`·`stop.bat`).  
서버 창을 닫아버렸거나 예전에 띄운 게 남아 있을 때 씁니다.  

압축 안의 `README.txt`를 읽어주세요.  
아카이브를 직접 만들려면 `make dist` 명령어를 사용합니다.  
폰 빌드는 `make mobile`이며 안드로이드 SDK와 Xcode가 필요합니다(러너마다 툴체인이 하나씩이라 `make mobile-android`·`make mobile-ios`로 나눠 부를 수도 있습니다).  
릴리스 CI가 데스크톱 다섯 개와 함께 만들며, **일곱 개가 전부 만들어져야 발행됩니다.**  
자세한 내용은 [`docs/running.md`](docs/running.md) 문서를 참조해주세요.  
버전별 변경 내역은 [`CHANGELOG.md`](CHANGELOG.md) 문서에 있습니다.

## 1. 직접 빌드 시 준비물

저장소에서 직접 빌드해 쓰는 경우엔 아래 순서대로 진행합니다.

- **Go 1.25 이상**
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

게임 선택 화면의 **＋ 게임 추가** 버튼으로 넣을 수도 있습니다.  
이렇게 넣은 파일은 `var/games/`가 아니라 `var/ext/`에 복사되며, 고른 원본 파일은 그대로 있습니다.  
**여기 들어간 게임만 화면의 게임 삭제 버튼으로 지울 수 있습니다** — `var/games/`에 직접 넣은 파일은 페이지에서 지워지지 않습니다.  
이전 버전에서 ＋ 게임 추가로 넣어 둔 게임은 `var/games/` 바로 아래에 있는데, 업데이트 후 처음 켤 때 `var/ext/`로 한 번 옮깁니다(폴더 안에 넣어 둔 것은 그대로 둡니다).  

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
백그라운드에 띄워 둔 채로 쓰려면 스크립트를 이용합니다.

```sh
./build.sh        # 빌드
./start.sh        # 백그라운드로 기동
./status.sh       # 지금 뭐가 몇 번 포트에 떠 있는지
./stop.sh         # 종료 (`./stop.sh 11599` 처럼 포트 지정 가능)
```

인자 없이 부르면 **릴리즈 프로필**입니다.  
디버그로 쓰려면 `./build.sh debug`, `./start.sh debug`처럼 붙입니다.  
`./start.sh` 뒤에 붙인 나머지 인자는 서버로 그대로 넘어갑니다(`./start.sh debug -addr 127.0.0.1:11599`).

`./status.sh`와 `./stop.sh`는 포트를 받습니다(`./stop.sh 11599`).  
스크립트 밖에서 띄운 서버도 정리합니다.  
포트만 보고 죽이지는 않으며 **그 포트가 wfeature인지 서버에게 물어본 뒤에만** 종료합니다.

Windows에서 `make` 없이 실행하는 방법은 [`docs/running.md`](docs/running.md) 문서를 참조해주세요.

주소나 폴더는 플래그로 바꿉니다.  
`-addr`, `-port`, `-games`, `-saves`, `-logs`, `-web`, `-number`, `-open`, `-version`  
환경 변수(`WFEATURE_ADDR`, `WFEATURE_PORT`, `WFEATURE_HOST`, `WFEATURE_GAME_ROOT`, `WFEATURE_SAVE_ROOT`, `WFEATURE_LOG_ROOT`, `WFEATURE_WEB_ROOT`, `WFEATURE_PHONE_NUMBER`, `WFEATURE_OPEN`)  

`-number`는 게임에게 알려줄 가입자 번호입니다. 요금제를 확인하는 게임이 있어 짧게 주면 그 화면을 넘어갑니다.

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

다른 앱을 보거나 연결이 끊겨도 게임 상태를 서버에 보관하며, **5분 시간 제한은 없습니다.**

같은 브라우저로 돌아오면 자동으로 이어갑니다. 다른 탭에서 진행 중이면 **이 화면에서 이어하기**로 조작권을 옮깁니다.

**🔄 재시작**은 현재 게임을 종료하고 새로고침 없이 게임 선택 화면으로 돌아갑니다.

게임은 서버 전체에서 실행·대기 합계 최대 4개를 유지합니다. 새 게임을 위해 오래된 대기 게임을 종료하거나 다른 기기의 같은 게임을 새로 시작할 때는 먼저 확인합니다. 4개 모두 실행 중이면 하나를 종료해야 시작할 수 있습니다. 서버 종료 시 보관된 진행도 끝납니다.

브라우저 데이터 삭제·다른 브라우저나 기기에서의 자동 이어하기는 지원하지 않습니다. 세이브 형식과 저장 위치는 그대로입니다.

게임을 고른 다음 **세이브 내보내기 / 세이브 가져오기**로 그 게임의 세이브를 파일 하나로 주고받을 수 있습니다.  
게임이 돌기 전 화면에만 있습니다 — 가져오기는 세이브를 덮어쓰기 때문입니다.

**＋ 게임 추가**로 넣은 게임은 같은 화면의 **게임 삭제**로 지웁니다.  
지워지는 것은 그 게임 파일뿐이고 **세이브는 남습니다** — 같은 파일을 다시 넣으면 이어서 할 수 있습니다.  
직접 폴더에 넣은 게임을 골랐을 때는 삭제 버튼이 눌리지 않습니다. 그 파일은 폴더에서 지워주세요.

**인증 호환** — 지원하는 인증서·번호·라이선스 검사는 게임 실행 시 자동으로 적용됩니다.
별도 설정 없이 동작합니다. 이전에 저장한 체크 상태는 사용하지 않습니다.
원본 아카이브·저장된 인증서는 보존하고 일반 진행 저장은 평소처럼 동작합니다.
일부 LGT의 동의 선택 통보·원격 세이브 조회도 로컬 처리하며 외부 전송은 하지 않습니다.
다른 인증 방식이나 누락된 게임 데이터는 해결하지 못합니다.

## 조작

키보드와 화면 키패드를 같이 쓸 수 있습니다.

| 키보드 | 전화기 키 |
|---|---|
| 방향키 | 위 / 아래 / 왼쪽 / 오른쪽 |
| Space | 가운데(확인) |
| Backspace | CLR |
| `\` | 통화 |
| M | 메뉴 (소프트키) |
| 1 2 3 | 1 2 3 |
| Q W E | 4 5 6 |
| A S D | 7 8 9 |
| Z X C | \* 0 # |

위 표는 키보드 기본값입니다.  
설정의 **⌨️ 키보드 매핑 변경**에서 임의의 키와 직접 매핑할 수 있습니다.
키패드 배치는 설정의 **🎮 키패드 타입**에서 고릅니다. `Type1`~`Type3`은 기본 배치이고, `Type4`는 빈 배치입니다. **🎮 키패드 세부 조정**에서 크기와 각 칸의 키를 바꿀 수 있습니다.
PC 웹 브라우저와 같은 넓은 화면 + 디버그 빌드에서는 화면 왼쪽에 실행 로그가 나옵니다.

## 설정

키패드 왼쪽 위 `Opts` 버튼을 누릅니다.

- **🎵 배경음 / 🔊 효과음** — 배경음과 효과음을 조절합니다.
- **🔍 화질** — 원본 / hq2x / hq3x / hq4x. 업스케일링 시 속도가 느려질 수 있습니다.
- **📱 화면** — 게임에게 알려줄 단말 화면입니다. 기본은 240×320이고 게임마다
  따로 기억하며, 바꾸면 다음 실행부터 적용됩니다. 확대되어 보이면 화면을 키우고
  축소되어 보이면 화면을 줄이면 됩니다.
- **🎮 키패드 타입** — `Type1` / `Type2` / `Type3` / `Type4` 중에서 고릅니다.
  `Type4`는 원하는 키만 배치할 수 있는 빈 타입입니다. 편집 내용은 타입마다 기억합니다.
- **🎮 키패드 세부 조정** — 크기와 폭 배분, 줄 높이를 조절하고 키패드의 칸을 눌러
  원하는 키를 지정하거나 비울 수 있습니다. **완료**를 누르면 게임 조작으로 돌아갑니다.
  **기본값으로 되돌리기**는 현재 타입의 배치와 크기를 초기화합니다.
- **📳 진동** — 게임이 요청한 진동을 실제로 울릴지 정합니다. 진동을 지원하지
  않는 브라우저에서는 나오지 않습니다.
- **⏩ 속도** — 0.25배에서 4배까지 속도를 조절합니다. **게임마다 따로
  기억하며** 돌아가는 중에도 바로 적용됩니다. 게임이 자기 속도를 하드웨어에
  맡겨 두어 원래 단말보다 빠르게 도는 경우 0.25~0.75배로 늦출 수 있습니다 —
  얼마나 빨라지는지는 게임마다 다르므로, 게임을 옮길 때마다 되돌릴 필요는
  없습니다.
- **⌨️ 키보드 매핑 변경** — 전화기 키마다 쓸 키보드 키를 정합니다. 키 이름을 누른 다음
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

게임 선택 화면의 **세이브 내보내기**는 이 폴더를 `.wfs` 파일 하나로 묶어 내려받고 **세이브 가져오기**는 그 파일을 되돌립니다.  

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

배포본은 `make dist VERSION=0.4.2`처럼 버전을 주면 다섯 OS용 아카이브로 묶습니다.
`v`로 시작하는 태그를 밀면 GitHub Actions가 같은 아카이브를 만들어 릴리스로 올리고 푸시마다 우분투·윈도우·macOS에서 서버를 실제로 띄워 봅니다.  
자세한 내용은 [`docs/running.md`](docs/running.md)에 있습니다.

## CLI

브라우저 없이 게임을 돌려보는 도구입니다.  
서버가 쓰는 것과 같은 에뮬레이터이며 tick 수·재현 스크립트·프로파일러·GDB를 붙일 수 있습니다.

```sh
make run ARGS="runktf var/games/ktf/game.zip -play"
```

`runktf`, `runskt`, `runlgt`도 브라우저와 동일하게 인증 호환을 자동 적용합니다. 비교 진단할 때만 `-no-auth`로 끌 수 있습니다. 결과는 `authentication: ktf-certificate-52`, `authentication: skt-license`처럼 표시됩니다. `unsupported`는 지원하는 방식이 발견되지 않았다는 뜻입니다. LGT의 지원 방식은 `lgt-cached-authentication`, `lgt-certificate-58`, `lgt-offline-notification`으로 표시됩니다. 마지막 방식은 게임에서 직접 고른 동의 여부를 로컬 처리하고 원격 세이브가 없다고 답합니다. 외부 전송은 하지 않으며 게임이 안내하는 재시작은 필요합니다.

`-framedir`로 남긴 프레임은 `contactsheet`로 한 장에 모아 보고 `framediff`로 두 빌드를 비교하며 `zoom`으로 한 프레임의 일부를 확대해 봅니다.  
`framestats`는 프레임에 실제로 무엇이 그려졌는지를 색 수와 켜진 픽셀 수로 알려주고 전부 단색이면 0이 아닌 값으로 끝납니다.  
명령과 플래그 전체, 재현 스크립트와 `ktfdump`는 [`docs/cli.md`](docs/cli.md)에 있습니다.

## 테스트

```sh
make test          # go test + Node 테스트
make acceptance    # 손에 있는 게임을 전부 돌려 보고서를 남김
```

`make acceptance`는 `var/games/` 아래 아카이브를 플랫폼별로 끝까지 몰아 보고
`var/acceptance/<날짜>.md`에 무엇이 어디서 멈췄는지 씁니다. 저장소에 게임이 없으므로
손에 코퍼스가 있는 사람만 돌릴 수 있고, 그래서 `make test`에는 들어 있지 않습니다.

무엇을 어디까지 검증하는지는 [`docs/testing.md`](docs/testing.md)에 있습니다.

## 문서

| 문서 | 내용 |
|---|---|
| [`docs/architecture.md`](docs/architecture.md) | Host / Runtime / Execution 계층 |
| [`docs/session.md`](docs/session.md) | 서버 세션 — 프로토콜, 페이싱, 프레임 스킵 |
| [`docs/running.md`](docs/running.md) | OS별 실행, 데이터 위치 |
| [`docs/mobile.md`](docs/mobile.md) | 안드로이드·iOS 앱 — 구조, 빌드, 한계 |
| [`docs/cli.md`](docs/cli.md) | CLI 명령과 플래그, 재현 스크립트, ktfdump |
| [`docs/armcore.md`](docs/armcore.md) | ARM 코어와 성능 |
| [`docs/jvm.md`](docs/jvm.md) | 바이트코드 인터프리터 |
| [`docs/ktf.md`](docs/ktf.md) | KTF 플랫폼 |
| [`docs/lgt.md`](docs/lgt.md) | LGT 플랫폼 |
| [`docs/skvm.md`](docs/skvm.md) | SKT / SKVM |
| [`docs/sgs.md`](docs/sgs.md) | SKT / GNEX·GVM SGS |
| [`docs/lcdui.md`](docs/lcdui.md) | LCDUI |
| [`docs/rms.md`](docs/rms.md) | RMS 저장소 |
| [`docs/network.md`](docs/network.md) | 네트워크 — 전 플랫폼 거부 정책과 그 표면 |
| [`docs/audio.md`](docs/audio.md) | 소리 |
| [`docs/hqx.md`](docs/hqx.md) | hqx 화면 확대 |
| [`docs/testing.md`](docs/testing.md) | 테스트 전략과 로컬 검증 |

작업 규칙은 [`AGENTS.md`](AGENTS.md)에 있습니다.

## 라이선스

MIT — [`LICENSE`](LICENSE)
다만 이 프로젝트 내 번들된 구성 요소는 각자의 라이선스를 유지합니다.

| 구성 요소 | 쓰임 | 라이선스 |
|---|---|---|
| `golang.org/x/text`, `golang.org/x/image` | Go 모듈, 정적 링크 | BSD-3-Clause |
| `golang.org/x/sys` | Go 모듈, amd64 대상에 정적 링크 | BSD-3-Clause |
| NeoDGM (Neo둥근모) | 임베드 폰트 | SIL OFL 1.1 (Reserved Font Name) |
| Galmuri9 | 임베드 폰트 | SIL OFL 1.1 (Reserved Font Name) |
| hqx | `internal/filter/hqx`의 결정 테이블 번역본 | MIT OR Apache-2.0 |

전문은 [`internal/licenses/THIRD-PARTY-NOTICES.md`](internal/licenses/THIRD-PARTY-NOTICES.md)에 있고 **모든 릴리스 바이너리가 이 파일을 안고 나갑니다.**  
`wfeature licenses`로 출력하거나 서버의 `/licenses`로 받을 수 있습니다.  

## 후원

이 프로젝트가 마음에 드셨다면 [GitHub Sponsors](https://github.com/sponsors/movingwoo)로 후원할 수 있습니다.  
후원은 전적으로 선택 사항이며, 후원 여부와 관계없이 모든 기능은 동일하게 제공됩니다.  
