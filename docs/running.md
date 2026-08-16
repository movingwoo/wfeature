# Running wfeature

Two things run: the **native CLI**, for driving a game from a terminal, and
the **local server**, which serves the browser client to a PWA. Both are the
same on Ubuntu, macOS and Windows, because the only tool involved is the Go
toolchain. Node is needed only to run the page-side tests.

## What is actually portable

**Supporting an operating system here means running the server on it**, which
makes portability the unit of distribution rather than a nicety: a release is
the server binary for an OS, a folder to drop games into, and a browser. The
client is a PWA and does not care which OS serves it.

The Go code cross-compiles and vets clean for `linux/amd64`, `linux/arm64`
and `windows/amd64`. It has no `exec.Command`, no `syscall` use, no hardcoded
`/tmp` and no hand-built path separators — every path goes through
`filepath.Join`, and the save keys that cross the Host boundary are
slash-separated names normalized on both sides.

The server is a Go program (`cmd/server`) that carries the client files inside
the binary, so nothing about it is shell- or runtime-specific. Every setting is
a flag, which behaves the same in PowerShell, where a `VAR=value command` prefix
does not exist.

**The `Makefile` is the one part that is not portable** — it is a POSIX shell
script in disguise. Windows does not need it: every target is one or two
commands, listed below.

## The local server

```sh
# Ubuntu and macOS
make serve                       # debug profile, in the foreground
make serve-release               # release profile, in the foreground

./build.sh                       # build both binaries for a profile
./start.sh                       # start the server in the background
./status.sh                      # what is running, on which port
./stop.sh                        # stop it again
```

```powershell
# Windows PowerShell — what `make serve` does, spelled out
go run -tags debug .\cmd\server
```

Then open `http://127.0.0.1:11541`, or the machine's address from a phone on
the same network — the server binds every interface, because being reachable
from a phone is the point of running it. **It has no authentication: anything
that can reach the port can read and write the save trees and list the games.**
Give `-addr 127.0.0.1:11541` to keep it to the machine it runs on.

The game runs **on the server**, and the page draws the frames it sends; see
[`session.md`](session.md). There is nothing to build for the browser: the
server carries the page and runs the emulator itself.

| Flag | Default | What it names |
|---|---|---|
| `-addr` | `:11541` | the listening address; `unix:/path/to.sock` listens on a socket file instead, for a reverse proxy (below) |
| `-web` | `web` | the client directory; when it is missing the binary serves its embedded copy |
| `-games` | `var/games` | the archives, grouped by platform directory |
| `-saves` | `var/savedata/<profile>/ktf` | this profile's KTF save tree |
| `-logs` | `var/logs` | where debug reports are written |
| `-number` | `01000000000` | the handset's subscriber number |

Each flag also reads an environment variable when it is not given —
`WFEATURE_ADDR`, `WFEATURE_WEB_ROOT`, `WFEATURE_GAME_ROOT`,
`WFEATURE_SAVE_ROOT`, `WFEATURE_LOG_ROOT`, `WFEATURE_PHONE_NUMBER` — and
`WFEATURE_HOST` with `WFEATURE_PORT` still compose an address.

`-number` is the odd one, because it is the only setting that changes what a
game sees rather than where it lives. One title treats any number of five
digits or more as a subscriber it can bill and then asks to download data its
own archive already carries; giving it a shorter number — `-number 9999` — is
what lets it start. Another title needs the full-length default for the
certificate it checks, so this is a choice rather than a better answer, and
[`network.md`](network.md) has both sides of it. The CLI reads the same
`WFEATURE_PHONE_NUMBER`.

Which build profile is served is **the binary that is running**, not a flag: a
server built with `-tags debug` collects the full diagnostics and writes to the
debug save tree. One process cannot serve the other profile, which is what keeps
a debug session from moving a release session's progress.

### Starting and stopping it in the background

`make serve` holds the terminal and Ctrl-C ends it, which is the right shape for
watching a log. Leaving one running is a different shape, and it has one trap
worth knowing: `make serve` and `go run` compile the server into a temporary
executable and run it as a **child**, so killing the `go run` process leaves the
child holding the port. That is the "the port is busy and I cannot find what has
it" case.

```sh
./build.sh [debug|release]                 # the Makefile's targets, one command
./start.sh [debug|release] [server args…]  # background; prints where it is
./status.sh [port]                         # what is running, on which port
./stop.sh [port]                           # stops it again
```

**A bare command means the release profile** in the two that take one, since
that is the build a machine is left running. `make serve` still means debug,
because watching a log is what the debug build is for. Both profiles can run at
once, as long as they are not asked for the same port.

`start.sh` does one thing: it builds the profile if it has to, starts the
server in the background with its log under `var/run/`, and prints what the
server then says about itself. There is no pid file — nothing needs one any
more.

`stop.sh` and `status.sh` are one line each, because the work is
`wfeature-server stop` and `wfeature-server status`. Both take a port and
nothing else: **the server is asked what it is** rather than looked up in a
process list, so a stranger holding the port is reported and left alone, and
`go run`'s executable-in-the-build-cache is recognised like any other.

A stop asks the server to stop itself, which drains the same way Ctrl-C does
and finishes a save in flight. Only a server that will not answer is signalled,
and only one that ignores the signal is forced.

### /api/status

The server answers `GET /api/status` with what it is:

```json
{"server":"wfeature","profile":"debug","version":"dev","pid":4242}
```

The profile is the binary that is running, and from outside the process there is
nothing else to read it from: two servers on two ports look alike, and the
executable's path says nothing when it was started with `go run` or renamed into
a release archive. `status` and `stop` use it for three things — naming the
profile, telling this server from a stranger holding the port before anything is
stopped, and knowing which process to signal if the polite stop below does not
work. The version is the one stamped by `make dist`, and `dev` in every other
build.

### /api/shutdown

`POST /api/shutdown` stops the server the way closing its window does: it stops
accepting, finishes what is in flight, and exits. **Only a caller on this
machine may use it** — the server binds every interface so a phone can play, and
a stop anyone on that network could send is a way to end somebody's game from
the next room. A request from anywhere else is refused with 403.

It exists because the alternative is finding the process behind a port from
outside, which took a different tool on every operating system (`lsof`, `ss`,
`fuser`, `netstat`) and still could not tell this server from a stranger. With
the route, one implementation in `internal/launcher` behaves the same
everywhere, and Windows gets the same graceful stop as the others rather than a
`taskkill`.

Windows has no equivalent of these scripts in a checkout; the PowerShell block
above and closing the window are the procedure there. A release archive is
different — it carries `start`, `stop` and `status` for all three systems,
Windows included (see `packaging/README.md`).

### Behind a reverse proxy, on a Unix socket

`-addr unix:/run/wfeature/wfeature.sock` listens on a socket file instead of a
port. It exists for one arrangement: a reverse proxy in front, terminating TLS
and forwarding to a path, so the server itself is never on the network. The
same string works through `WFEATURE_ADDR`.

Nothing above the listener knows which it got — the page, the save API and the
session WebSocket are the same either way.

**The proxy has to pass the browser's `Host` through.** The WebSocket handshake
refuses an `Origin` whose host is not the host the request arrived on, which is
what stops any page on the internet from opening a session on someone's
machine. A proxy that rewrites `Host` to `localhost` while the browser sends
`Origin: https://games.example.com` fails that check, and the page then loads
and never starts a game. nginx needs it spelled out; Caddy does it by default.

```nginx
location / {
    proxy_pass http://unix:/run/wfeature/wfeature.sock;
    proxy_set_header Host $host;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;      # the session is a WebSocket
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 1h;                       # a parked game outlives an idle socket
}
```

```caddy
games.example.com {
    reverse_proxy unix//run/wfeature/wfeature.sock
}
```

**The socket is created 0660**, so the proxy reaches it through the group rather
than by being world-writable. Run the server under a group the proxy is in —
which is what `Group=` is for:

```ini
[Service]
ExecStart=/opt/wfeature/wfeature-server -addr unix:/run/wfeature/wfeature.sock
User=wfeature
Group=www-data
RuntimeDirectory=wfeature
Restart=on-failure
```

`RuntimeDirectory=` is worth the line: systemd makes and removes
`/run/wfeature` for the service, so the socket lives somewhere the service owns
rather than in `/tmp`.

A socket file left behind by a kill or a power cut does not block the next
start. Binding onto one fails with "address already in use" — which reads
exactly like a port conflict and is not one — so the server connects to it
first and removes it only when nothing answers. **A socket a live server is
serving is never removed**: starting a second server on it fails with `a server
is already serving this socket`, and the first one keeps running. A clean stop
removes the file itself.

**`start.sh`, `stop.sh` and `status.sh` are written for a port.** They ask a
port what is behind it, and neither that nor the stop route reaches a path. That is deliberate rather than missing:
a deployment that wants a reverse proxy has a service manager already, and the
unit above is the whole of what those scripts would have done. To ask a socket
server what it is, the same endpoint the scripts use answers over the socket:

```sh
curl --unix-socket /run/wfeature/wfeature.sock http://localhost/api/status
```

## A release build

`make server` and `make server-release` write `build/<profile>/wfeature-server`.
That binary is the whole deliverable: it carries the page, the scripts and the
styles, so it only needs a `games/` directory beside it.

```text
wfeature-server            the binary, ~11 MB
games/ktf/<game>.zip       what to play
savedata/<profile>/...     written on the first save
logs/                      written when a report is taken
```

When the working directory has no `var/`, the server reads and writes the
directory the executable sits in, so a copy dropped anywhere with a `games/`
folder beside it works without flags. A checkout keeps using `var/`.

```sh
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o wfeature-server.exe ./cmd/server
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o wfeature-server     ./cmd/server
```

## The release archives

`make dist` writes what a user downloads: one archive per platform under
`build/dist`, each holding the release server, its three launchers (`start`,
`stop`, `status`), the notices and an empty `games/` tree. Building them by
hand takes the version as an argument rather than deriving one, because a
release number is a decision; in CI it comes from the tag, which is the same
decision written down somewhere durable:

```sh
make dist VERSION=0.2.0
```

```text
wfeature-<version>-darwin-arm64.tar.gz     wfeature-<version>-windows-amd64.zip
wfeature-<version>-darwin-amd64.tar.gz     wfeature-<version>-linux-amd64.tar.gz
                                           wfeature-<version>-linux-arm64.tar.gz
SHA256SUMS                                 checksums for all five
```

```text
wfeature-server[.exe]      the release binary, ~12 MB, stamped with the version
start.command / .sh / .bat the launcher
README.txt                 Korean, for the person who downloaded it
LICENSE.txt, THIRD-PARTY-NOTICES.md
games/{ktf,lgt,skt}/       empty, plus a README naming what goes where
```

The empty `games/` tree is what makes `dataRoot` choose the extracted folder,
so an archive without it would write saves into whatever directory the launcher
happened to start from. The sources for everything but the binary are in
[`packaging/`](../packaging/README.md), which also explains why a launcher
exists at all rather than a bare binary.

`-version` prints what a user is running (`wfeature-server 0.1.0 (release)`),
and the startup log carries it too. A checkout reports `dev`. The version goes
to stdout and the log to stderr, so `wfeature-server -version | …` reads the
answer and nothing else — which is what the smoke job and anyone pasting a
version into a report depend on.

**A pushed tag publishes them.** `.github/workflows/release.yml` runs on a tag
matching `v*`. It waits on `checks.yml` — the same gate every push runs, which
is `gofmt`, `vet`, both test profiles, the race detector, and a real run of the
server on Ubuntu, Windows and macOS — and only then builds the five archives
with `make dist VERSION=<tag without its v>`, verifies the checksums it just
wrote, and creates the GitHub release with the archives and `SHA256SUMS`
attached. The per-OS run is the reason for the dependency rather than a copy of
the test steps: four of the five archives are cross-compiled, and a build that
succeeds is not a binary that starts.

The release notes are that version's section of [`CHANGELOG.md`](../CHANGELOG.md),
read out of the file by the tag's number. A tag whose version has no section
fails the release instead of publishing an empty one, which is what keeps the
file current. The workflow adds nothing to an archive — it runs the same
`make dist` — so a release can still be built and published by hand if it is
ever unavailable.

Running it from the Actions tab instead of pushing a tag builds and checks
everything and publishes nothing: the archives are left as a run artifact,
which is how the packaging is tried out before a version number is spent.

The tag is the version. `v0.2.0` builds `0.2.0`, stamps it into the binary's
`-version`, and names the archives after it.

The archives are unsigned on both macOS and Windows, so a first run is refused
until the user says otherwise — Gatekeeper's "unidentified developer" and
SmartScreen's blue banner. Each `README.txt` says how to get past its own, and
the macOS launcher clears the quarantine flag from the rest of the folder,
which is the one thing the binary cannot do for itself.

Verified by extracting the archive and running it: the macOS launcher clears a
quarantine flag it was given, serves `/` from the embedded client, lists a game
dropped in `games/ktf/`, and names the extracted folder as its data root. The
other four are cross-compiled only — the `smoke` job below runs the *binary* on
Ubuntu and Windows, which is a different thing from extracting the archive a
user downloads, and the launchers and the quarantine step are what it does not
cover.

## The native CLI

```sh
go run ./cmd/cli runktf var/games/ktf/<game>.zip -play
go run ./cmd/cli runlgt var/games/lgt/<game>.zip -cheat
go run ./cmd/cli runskt var/games/skt/<game>.jar
```

The same commands work in PowerShell with `\` separators. Every command and
flag is in [`cli.md`](cli.md).

## Where the data lives

```
var/games/<platform>/          game archives
var/savedata/<profile>/<platform>/<owner>/   saves
var/logs/                      debug run reports
```

The picker lists the `.zip` and `.jar` files one level under `var/games/` and
the ones sitting in it directly, which the page groups as `기타`. The directory
name is a label and nothing else — the platform comes from the archive's bytes
— and nothing deeper than one level is scanned.

`<owner>` is the game's PID for KTF and LGT and its `MIDlet-Name` for
SKT. The browser reaches the same tree through the save API:
`/api/saves/<owner>` is KTF (the form the page has always used) and
`/api/saves/<platform>/<owner>` is any other platform.

Nothing under `var/` is ever touched by a rebuild — that is why it is outside
the source and build trees.

## What was verified, and where

| | Ubuntu | macOS | Windows |
|---|---|---|---|
| `go build ./...` | cross-compiled ✔ | **run ✔** | cross-compiled ✔ |
| `go vet ./...` | cross-compiled ✔ | **run ✔** | cross-compiled ✔ |
| `go test ./...` | — | **run ✔** | — |
| `node --test web/*.test.mjs` | — | **run ✔** | — |
| Go server serves index, `games.json`, save API | — | **run ✔** | — |
| a real game over a session at ~19fps, 13KB frames | — | **run ✔** | — |
| single release binary serving its embedded client | — | **run ✔** | — |
| a `make dist` archive extracted and launched | cross-compiled ✔ | **run ✔** | cross-compiled ✔ |

Only macOS was executed *here*: this repository is developed on one machine, and
cross-compiling proves a build, not a run. What the table does show is that
nothing in the Go tree is platform-specific enough to fail to compile or to trip
`vet`, and that the paths and process handling the server depends on were
written for all three.

The gap that leaves is what `.github/workflows/checks.yml` is for. Its `smoke`
job runs on Ubuntu, Windows and macOS runners and, on each, builds the release
server, stages it the way an archive does, and then checks the seven things a
download has to do: report its version, bind its port, answer `/api/status`,
serve the page it carries, name the folder beside it as its data root, say what
it is when asked with `status`, and stop when asked with `stop` — draining
rather than being killed. The last two are why the job matters most on Windows:
that is the one system where a stop has no signal to fall back on, so the route
the server answers is the whole of it there. That
is a real run on each OS rather than a compile. `verify.yml` calls it on every
push and pull request, and `release.yml` calls it before it publishes, so no
tag can ship a binary this never started.

**Until that workflow has run on a push, Ubuntu and Windows remain unverified by
execution** — the job was written and its assertions were checked against a
local macOS run of the same steps, but no CI run has happened yet. The first
push is what turns the Ubuntu and Windows columns into "run".

To close the gap by hand instead, run this on each OS and compare:

```sh
go vet ./... && go test ./... && node --test web/*.test.mjs
make serve   # or the PowerShell block above
curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:11541/
curl -sS http://127.0.0.1:11541/games.json | head -c 120
```

On Windows the first run also raises a firewall prompt, because the server
binds every interface. Allowing it on the private network is what lets a phone
reach the page.
