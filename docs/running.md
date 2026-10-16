# Running wfeature

Two things run: the **native CLI**, for driving a game from a terminal, and the
**local server**, which serves the browser client to a PWA. Both behave the
same on Ubuntu, macOS and Windows, because the only build tool involved is the
Go toolchain. Node is needed only to run the page-side tests.

## What is actually portable

**Supporting an operating system means running the server on it.** A release
is the server binary for an OS, a folder to drop games into, and a browser; the
client is a PWA and does not depend on which OS serves it.

The Go code cross-compiles and vets clean for the five release targets in the
`Makefile`'s `DIST_PLATFORMS` (`darwin/arm64`, `darwin/amd64`, `linux/amd64`,
`linux/arm64`, `windows/amd64`) and for the phone builds, `android/arm64` and
`ios/arm64`. Its `syscall` use is confined to file locks behind per-OS build
files (`internal/backend/save_lock_unix.go`, `save_lock_windows.go`, and
`save_lock_other.go` for a system with no known lock), error classification and
`SIGTERM`. It has no hardcoded `/tmp` and no hand-built path separators: every
path goes through `filepath.Join`, and save keys that cross the Host boundary
are slash-separated names normalized on both sides. The one `exec.Command` in a
shipped binary is `internal/launcher`'s, which opens a browser; the developer
tools under `internal/tools` also run subprocesses.

A phone runs the same server rather than a port of it; what each app does with
it, and what iOS forbids that Android allows, is in [`mobile.md`](mobile.md).

The server (`cmd/server`) carries the client files inside the binary, so
nothing about it is shell- or runtime-specific. Every setting is a flag, which
behaves the same in PowerShell, where a `VAR=value command` prefix does not
exist.

**The `Makefile` and the `*.sh` helpers are not portable** — the `Makefile` is
POSIX shell. Windows does not need them: every target is one or two commands,
listed below.

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
the same network. The server binds every interface, because being reachable
from a phone is the point of running it. **It has no authentication: anything
that can reach the port can read and write the save trees, list the games, and
add or remove a game added from the page.** Give `-addr 127.0.0.1:11541` to keep
it to the machine it runs on.

A browser page on another site is a different caller from the network: it
can send a POST to `127.0.0.1` without asking anyone first. So every request
that changes something — any method but `GET` and `HEAD` — is refused with 403
when it carries an `Origin` whose host is not the `Host` it arrived on, the
rule the session WebSocket already keeps. A request with no `Origin` (`stop`,
curl, a script) is answered as before, and the page's own requests carry its
own origin, by address, by name, in the phone apps and behind a proxy that
passes `Host` through ([below](#behind-a-reverse-proxy-on-a-unix-socket)).
**DNS rebinding is not covered**: a page whose own name is made to resolve to
this machine sends an `Origin` and a `Host` that agree. A list of accepted
`Host` names would close that, and would also refuse every name a person
reaches the server by that the server cannot know — a `.local` name, a
router's name for the machine, a proxy's — so there is none; keep the server
off networks and browsers you do not trust, or bind it to loopback.

A page opened by the machine's address over plain
HTTP is not a secure context, so the browser gives it no game controller and
registers no service worker; `127.0.0.1`, the phone apps and an HTTPS proxy are
secure contexts.

The game runs **on the server**, and the page draws the frames it sends; see
[`session.md`](session.md). There is nothing to build for the browser.

| Flag | Default | What it names |
|---|---|---|
| `-addr` | `:11541` | the listening address; `unix:/path/to.sock` listens on a socket file instead, for a reverse proxy (below) |
| `-web` | `web` | the client directory; when it is missing the binary serves its embedded copy |
| `-games` | `<data>/games` | the archives, grouped by platform directory |
| `-ext` | `<data>/ext` | the archives added from the page, which are the ones it may remove |
| `-saves` | `<data>/savedata/<profile>/ktf` | this profile's KTF save tree; the other platforms' trees are its siblings |
| `-logs` | `<data>/logs` | where debug reports are written |
| `-number` | `01000000000` | the handset's subscriber number: 1 to 11 digits |
| `-open` | off | open the page in a browser once the server answers on its port |
| `-version` | — | print `wfeature-server <version> (<profile>)` to stdout, then exit |

`<data>` is `var` when the working directory has a `var/` directory, otherwise
the executable's own directory when a `games/` directory sits beside it, and
otherwise `var` ([A release build](#a-release-build)). The startup log names
the choice as `data=var/` or `data="beside the executable"`, together with the
client source (`client=web` or `client=embedded`) and each root.

Each flag reads an environment variable when it is not given — `WFEATURE_ADDR`,
`WFEATURE_WEB_ROOT`, `WFEATURE_GAME_ROOT`, `WFEATURE_ADDED_ROOT`,
`WFEATURE_SAVE_ROOT`, `WFEATURE_LOG_ROOT`, `WFEATURE_PHONE_NUMBER`,
`WFEATURE_OPEN` (any non-empty value) — and `WFEATURE_HOST` with
`WFEATURE_PORT` compose the default address. `WFEATURE_PORT` is also the
default port of `status` and `stop`.

`-number` is the only setting that changes what a game sees rather than where
it lives. One title treats any number of five digits or more as a subscriber it
can bill and then asks to download data its own archive already carries; a
shorter number such as `-number 9999` lets it start. Another title needs the
full-length default for the certificate it checks, so the value is a choice
rather than a better answer; [`network.md`](network.md) has both sides. The CLI
reads the same `WFEATURE_PHONE_NUMBER`.

The build profile served is **the binary that is running**, not a flag: a
server built with `-tags debug` collects the full diagnostics and writes to the
debug save tree. One process cannot serve the other profile, which keeps a
debug session from moving a release session's progress. Every HTTP route the
server answers is listed in [`web/README.md`](../web/README.md#server).

### Adding a game from the page, and taking it back off

`POST /api/games?name=<file name>` writes one archive into the added root, and
the picker's **＋ 게임 추가** button calls it. The name is percent-encoded in the
query rather than carried in a header, because these names are Korean and a
header value is Latin-1. The route exists for a device with no reachable
folder: on Android since 11 a file manager cannot open the directory an app
keeps its files in.

The name is **refused rather than repaired**: a separator, a climb, a leading
dot or an extension other than `.zip` or `.jar` is answered with a sentence the
page shows, because a name that needs repairing is a request to write somewhere
else. The bytes are not examined — the platform is read from the content when
the archive is loaded — so the archive lands in the added root with no
platform directory. The write is atomic (a temporary file beside the target,
then a rename), so re-uploading a corrected archive never leaves a truncated
file. Uploads are bounded at 32 MiB; the largest archives of the era are a few
megabytes.

**Which root a game is in decides whether the page may delete it.** The library
is `var/games`, grouped by whatever directories its owner made; page uploads
land in `var/ext`. `DELETE /api/games?game=<picker path>` removes one archive
from `var/ext` and refuses everything else with a sentence the page shows —
including what in `var/ext` is not an archive the upload could have written,
such as the `.adopted` marker or an upload still in progress.
`games.json` marks those entries `"added": true` with an `ext/` path, and the
picker lists them under 추가한 게임, the only entries on which **게임 삭제** is
enabled.

The location is the record by design. An archive the page wrote and one a
person dropped into the library are the same bytes, and `games.json` is a
directory read every time; a registry kept beside it could drift from the disk,
and the location of a file cannot. A reserved directory inside `var/games` would
take a name out of the owner's own group names.

- A game that is playing when its archive is deleted **keeps playing**: the
  archive is read once at the start, and a parked session holds the game
  itself.
- Removing a game **does not touch its saves.** A save is keyed by what is
  inside the archive, so adding the same file again lands on the same progress.
  The save routes work on `ext/` paths as they do on library ones.
- **The page asks its own question before deleting.** `window.confirm` in the
  Android app is decided by the app's `WebChromeClient` (which the file picker
  behind ＋ 게임 추가 requires), where it can answer "no" with nothing on screen.
  `web/confirm.js` puts the dialog in the page so the question and answer are
  the same everywhere; the restart button uses it too.

Deleting matters for the same reason adding does: on Android, without it the
only way to clear a game is to clear the app's data and lose every save.

**The one move an upgrade makes.** Games added before the added root existed
sit in the game root, where a removal cannot reach them. The first start after
the upgrade moves what is directly in the game root into the added root —
`AdoptLooseGames` in `internal/webhost/adopt.go`, called by the desktop server
and the in-process one (`internal/appserver`), so every host takes the same
step. It does not move group directories, which are a library someone filed,
and never overwrites a name the added root already holds. It runs **once**,
recorded by an empty `.adopted` file in the added root, because a person may
still drop an archive into the game root by hand and a sweep on every start
would refile it. An unfinished move leaves the marker unwritten so the next
start completes it.

### Starting and stopping it in the background

`make serve` holds the terminal and Ctrl-C ends it, which suits watching a log.
`make serve` and `go run` compile the server into a temporary executable and
run it as a **child**, so killing the `go run` process leaves the child holding
the port — the usual cause of a busy port with no visible owner. The scripts
below avoid that:

```sh
./build.sh [debug|release]                 # `make debug server` or `make release server-release`
./start.sh [debug|release] [server args…]  # background; prints where it is
./status.sh [port]                         # what is running, on which port
./stop.sh [port]                           # stops it again
```

**A bare `build.sh` or `start.sh` means the release profile**, the build a
machine is left running; `make serve` still means debug. Both profiles can run
at once on different ports.

- `build.sh` reports only the CLI and server binaries it builds; other
  executables in the output directory are not build profiles.
- `start.sh` builds the profile if `build/<profile>/wfeature-server` is
  missing, starts it in the background with its log in
  `var/run/server-<profile>.log` (truncated on each start), waits for the
  server to log its address, and prints what `status` reports. A server that
  exits at once has its log printed and `start.sh` exits 1. Arguments after
  the profile go to the server; a first argument starting with `-` keeps the
  release profile. There is no pid file.
- `status.sh` and `stop.sh` run `wfeature-server status` and
  `wfeature-server stop` from whichever of `build/release` and `build/debug` is
  built, falling back to `go run ./cmd/server`. Both take a port and nothing
  else (`-port N` or a bare number; default 11541 or `WFEATURE_PORT`): **the
  server is asked what it is** rather than looked up in a process list, so a
  stranger holding the port is reported and left alone, and `go run`'s
  executable in the build cache is recognised like any other.

A stop asks the server to stop itself, which drains as Ctrl-C does: it finishes
a save in flight and closes every game, including one whose page is still
connected, so what a title wrote reaches the save folder before the process
ends. `stop` reports the server stopped once the process it named has exited,
not when its port goes quiet: the drain closes the listener first and closes
the games after, so a `start` that followed a quiet port could run beside a
server still holding their save folders. It waits up to 15 seconds, past the
server's own 10-second drain. Only a server that does not answer is
signalled, and only one that ignores the signal is forced; a forced stop
closes nothing. All output of `status` and `stop` is English.

### /api/status

The server answers `GET /api/status` with what it is:

```json
{"server":"wfeature","profile":"debug","version":"dev","pid":4242}
```

The profile cannot be read from outside the process any other way: two servers
on two ports look alike, and the executable's path says nothing when it was
started with `go run` or renamed into a release archive. `status` and `stop`
use the answer to name the profile, to tell this server from a stranger before
anything is stopped, and to know which process to signal if the graceful stop
fails. The version is the one stamped by `make dist`, and `dev` in every other
build.

### /api/shutdown

`POST /api/shutdown` stops the server the way closing its window does: it stops
accepting, answers `{"stopping":true}` with 202, finishes what is in flight,
closes the games it holds, and exits. **Only a caller on this machine may use
it**, because the server binds every interface and a stop anyone on the network
could send would end someone's game from the next room. A request from
elsewhere is refused with 403 and logged.

A reverse proxy on the same machine forwards every visitor from here — over the
[Unix socket](#behind-a-reverse-proxy-on-a-unix-socket) or over loopback — so a
request carrying `Forwarded`, `X-Forwarded-For` or `X-Real-IP` is refused as
well, whatever peer handed it over. That holds only if the proxy sets one of
them: Caddy does by default, and the nginx block below sets it explicitly.

The route replaces finding the process behind a port from outside, which took a
different tool on every operating system (`lsof`, `ss`, `fuser`, `netstat`)
and could not tell this server from a stranger. One implementation in
`internal/launcher` now behaves the same everywhere, and Windows gets the same
graceful stop rather than a `taskkill`.

A checkout has no Windows equivalent of these scripts; the PowerShell block
above and closing the window are the procedure there. A release archive carries
`start`, `stop` and `status` for all three systems (see
[`packaging/README.md`](../packaging/README.md)).

### Behind a reverse proxy, on a Unix socket

`-addr unix:/run/wfeature/wfeature.sock` listens on a socket file instead of a
port, for one arrangement: a reverse proxy in front, terminating TLS and
forwarding to a path, so the server itself is never on the network. The same
string works through `WFEATURE_ADDR`. Nothing above the listener knows which it
got — the page, the save API and the session WebSocket are the same either way.

**The proxy must pass the browser's `Host` through.** The WebSocket handshake
and every route that changes something refuse an `Origin` whose host is not
the host the request arrived on, which stops any page on the internet from
opening a session or writing a save on someone's machine. A
proxy that rewrites `Host` to `localhost` while the browser sends
`Origin: https://games.example.com` fails that check, and the page loads but
never starts a game. nginx needs it spelled out; Caddy does it by default.

```nginx
location / {
    proxy_pass http://unix:/run/wfeature/wfeature.sock;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;  # keeps /api/shutdown local
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
than by being world-writable. Run the server under a group the proxy is in,
which is what `Group=` is for:

```ini
[Service]
ExecStart=/opt/wfeature/wfeature-server -addr unix:/run/wfeature/wfeature.sock
User=wfeature
Group=www-data
RuntimeDirectory=wfeature
Restart=on-failure
```

`RuntimeDirectory=` makes systemd create and remove `/run/wfeature` for the
service, so the socket lives somewhere the service owns rather than in `/tmp`.

A socket file left by a kill or a power cut does not block the next start.
Binding onto one fails with "address already in use", which looks like a port
conflict, so the server connects to it first and removes it only when nothing
answers. **A socket a live server is serving is never removed**: a second
server fails with `a server is already serving this socket`, and a path that
exists but is not a socket fails with `the path exists and is not a socket`. A
clean stop removes the file.

**`start.sh`, `stop.sh` and `status.sh` work on a port only**; neither they nor
the stop route reach a socket path. That is deliberate: a deployment behind a
reverse proxy has a service manager, and the unit above is everything those
scripts would do. To ask a socket server what it is:

```sh
curl --unix-socket /run/wfeature/wfeature.sock http://localhost/api/status
```

## A release build

`make server` and `make server-release` write `build/<profile>/wfeature-server`;
`make debug` and `make release` write the CLI as `build/<profile>/wfeature`.
The server binary is the whole deliverable: it carries the page, the scripts
and the styles, and needs only a `games/` directory beside it.

```text
wfeature-server            the binary, ~11 MB
games/ktf/<game>.zip       what to play
ext/<game>.zip             what the page added, and what it may delete
savedata/<profile>/...     written on the first save
logs/                      written when a report is taken
```

When the working directory has no `var/`, the server reads and writes the
directory the executable sits in — provided a `games/` directory is beside it —
so a copy dropped anywhere with a `games/` folder works without flags. A
checkout keeps using `var/`.

```sh
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o wfeature-server.exe ./cmd/server
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o wfeature-server     ./cmd/server
```

**Every build is profile-guided, including the two above and
`go run ./cmd/server`.** `cmd/server/default.pgo` and `cmd/cli/default.pgo` are
committed profiles of real game runs, and `go build` reads a `default.pgo`
beside a main package without a flag, so no build path can miss it. It was
worth about 12% of a session's host time when measured and changes nothing a
guest can see. `make pgo` regenerates it from local archives named by the
Makefile's `PGO_*` variables; [`armcore.md`](armcore.md#profile-guided-build)
describes the runs and
[`history/armcore.md`](history/armcore.md#implementation-the-host-profiles-next-answer-was-a-build-flag-and-it-was-12)
has the measurements.

## The release archives

`make dist` writes what a user downloads: one archive per entry of
`DIST_PLATFORMS` under `build/dist`, each holding the release server, its three
launchers (`start`, `stop`, `status`), the notices and an empty `games/` tree,
then writes `SHA256SUMS` (`make checksums`). The version is stamped into the
binary (`-X main.version`) and the archive names. It is set by hand because a
release number is a decision; the Makefile's `VERSION ?=` default is the last
release (currently `0.5.2`), so pass it explicitly. In CI it comes from the
tag.

```sh
make dist VERSION=0.5.2
```

```text
wfeature-<version>-darwin-arm64.tar.gz     wfeature-<version>-windows-amd64.zip
wfeature-<version>-darwin-amd64.tar.gz     wfeature-<version>-linux-amd64.tar.gz
                                           wfeature-<version>-linux-arm64.tar.gz
SHA256SUMS                                 checksums for everything beside it
```

```text
wfeature-server[.exe]      the release binary, ~12 MB, stamped with the version
start.command / .sh / .bat the launcher
README.txt                 Korean, for the person who downloaded it
LICENSE.txt, THIRD-PARTY-NOTICES.md
games/{ktf,lgt,skt}/       empty, plus a README naming what goes where
```

Windows gets a `.zip` because Explorer opens it without a tool, and its text is
converted while staged: CRLF throughout, plus a UTF-8 BOM on the two Korean
READMEs so an editor cannot guess CP949; the `.bat` files stay ASCII without a
BOM, because `cmd.exe` would echo one. The `.tar.gz` archives are written with
`COPYFILE_DISABLE=1` so macOS adds no `._` files.

The empty `games/` tree is what makes `dataRoot` choose the extracted folder;
without it the server would write saves into whatever directory the launcher
started from. The sources for everything but the binary are in
[`packaging/`](../packaging/README.md), which also explains why a launcher
exists rather than a bare binary.

The phone builds are the other two files. `make mobile` (`mobile-android` and
`mobile-ios`) writes `wfeature-<version>-android-arm64.apk` and
`wfeature-<version>-ios-arm64.ipa` into the same directory and rewrites
`SHA256SUMS` over everything there, so a release lists seven files. They are a
separate command because each needs a toolchain the desktop build does not — an
Android SDK, and Xcode — and a machine with neither must still be able to build
the desktop archives. See [`mobile.md`](mobile.md).

`-version` prints what a user is running (`wfeature-server 0.1.0 (release)`),
and the startup log carries it too; a checkout reports `dev`. The version goes
to stdout and the log to stderr, so `wfeature-server -version | …` reads only
the answer, which the smoke job and version reports depend on.

**A pushed tag publishes them.** `.github/workflows/release.yml` runs on a tag
matching `v*`. It first calls `checks.yml`, the gate every push runs (see
[What was verified, and where](#what-was-verified-and-where)), then builds on
three machines at once:

| job | machine | what it builds |
|---|---|---|
| `desktop` | Ubuntu | the five archives, `make dist` with `CGO_ENABLED=0` |
| `android` | Ubuntu | the APK, `make mobile-android` |
| `ios` | macOS | the IPA, `make mobile-ios` |

The split follows the runners: the Android SDK is on the Linux runner and Xcode
is on the macOS one, and no runner has both. A `publish` job gathers the seven
files, runs `make checksums` over all of them, runs
`make dist-check DISTCHECK_FLAGS=-phones`, and creates the GitHub release.
**A failure in any of the three build jobs publishes nothing**; a release
missing its APK is the failure this shape prevents. The first releases built
the five archives in CI and uploaded the phone builds by hand afterwards, a step
that was forgotten once; `-phones` is the check that now refuses a set without
them.

- **The version is the tag.** `v0.2.0` builds `0.2.0`, stamps it into
  `-version` and names the archives after it.
- **A tag with a suffix publishes as a pre-release** (`v0.4.0-pre`,
  `v0.5.0-rc1`; `v0.4.0` is not). Every release carries the same seven files.
- **The notes are that version's section of [`CHANGELOG.md`](../CHANGELOG.md)**,
  found by the tag's number with any suffix removed, so `v0.4.0-pre` and
  `v0.4.0` read the same section. A version with no section fails the release
  instead of publishing empty notes.
- **The title is `W-Feature <tag>`**, because a release page shows the title
  above the notes and the tag alone does not say what it is a version of.
- **The `android` job needs the signing key**, `ANDROID_KEYSTORE_BASE64`, a
  repository secret holding `mobile/android/debug.keystore`. Without it the job
  fails rather than letting `build.sh` make a fresh key, because a new key each
  release is an app no phone can update onto. The `android` and `ios` jobs check
  out the full history, because the APK's `versionCode` and the bundle build
  number are the commit count and a shallow checkout counts one.
- **The IPA is unsigned on purpose**: whoever installs it re-signs it with their
  own Apple ID.
- **Running the workflow from the Actions tab** (input `version`, default
  `0.0.0-dev`) builds and checks everything and publishes nothing; the files are
  kept as a run artifact named `wfeature-<version>`, which is how packaging is
  tried before a version number is spent.

The workflow adds nothing to an archive — it runs the same `make dist` and
`make mobile` — so a release can be built and published by hand if it is ever
unavailable.

The archives are unsigned on macOS and Windows, so a first run is refused until
the user overrides Gatekeeper's "unidentified developer" or SmartScreen's
banner. Each `README.txt` says how, and the macOS launcher clears the quarantine
flag from the rest of the folder, which the binary cannot do for itself.

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
var/ext/                       archives added from the page (the only ones it may remove)
var/savedata/<profile>/<platform>/<owner>/   saves
var/logs/                      debug run reports
var/run/                       start.sh's per-profile server log
```

[`var/README.md`](../var/README.md) lists every directory under `var/`.

The picker lists the `.zip` and `.jar` files one level under `var/games/`, and
those directly in it, which the page groups as `기타`. A directory name is a
label only — the platform comes from the archive's bytes — and nothing deeper
than one level is scanned. A group directory may be a symbolic link to one kept
elsewhere, and is listed like any other.

`<owner>` is the game's PID for KTF and LGT, and for SKT its program number
(`DD-ProgName`), or the MIDlet name for a bare JAR that has none. Inside it,
each save is a file named by its key with the case spelled out: an upper-case
letter is written `#` and the letter, and `#` is written `##` (`rms/#Option`,
`fs/save##1`), so names that differ only by case are separate files on every
system. A folder holding such names also holds `.wfeature-names#1`, which says
so and moves with it; a folder an earlier version wrote is converted the first
time it is used. See [save keys on disk](session.md#save-keys-on-disk). The
browser reaches the same tree through the save API: `/api/saves/<owner>` is KTF
(the form the page has always used) and `/api/saves/<platform>/<owner>` is any
other platform; it takes and lists keys, not file names. One game's saves leave
and return as a `.wfs` container through
`GET`/`POST /api/savepack?game=<picker path>`, which the page's save export and
import use; an import writes the saves it carries over the ones on disk. An
import is refused before it writes anything when the container holds no saves
(no export writes one), more than 65,536 entries (the quick save's bound on a
save tree), a name that is both a file and a folder, a name the folder on
disk has the other shape for, or, on macOS, a name that is not UTF-8. Other
write failures can still leave some entries restored and the rest as they
were, with nothing removed. On a filesystem that ignores Unicode
normalization, as macOS does by default, a restored save that lands on an
existing file under a differently normalized name keeps that file's spelling
and is not removed with the entries the backup left out. Case is not such a
spelling: a name that differs only by case is another save.

An owner the save tree cannot use as a folder name — longer than 64 bytes, or
holding `/ \ : * ? " < > |`, a control character or an invisible direction
mark — has no save folder. The server refuses to start such a game and says
why, rather than running it with saves that would vanish when it closes, and
logs the owner; the save export and import say the same.

A per-game save folder may be a symbolic link to a directory kept elsewhere.
Saves are read and written through it, and quick save and quick load work as
they do anywhere else. The quick save is kept beside the link, in
`.wfeature-quicksave/owners/<folder>/`, never in the directory the link leads
to. Lock files also live in a reserved sibling directory beside the target, so
opening the same saves through the link and the real path cannot start two
competing sessions.

On a save location that cannot be written, a game still starts and reads its
saves, and the game's own save fails as a write. On a filesystem without file
locks, games run normally. In both cases the log says once per folder that it
has no file lock, which means a second server or CLI process is not kept out
of it.

Nothing under `var/` is touched by a rebuild; that is why it is outside the
source and build trees.

## What was verified, and where

| | Ubuntu | macOS | Windows |
|---|---|---|---|
| `go build ./...` | CI ✔ | **run ✔** | cross-compiled ✔ |
| `go build ./cmd/server` | CI ✔ | CI ✔ | CI ✔ |
| `go vet ./...` | CI ✔ | **run ✔** | cross-compiled ✔ |
| `gofmt -l cmd internal mobile`, `govulncheck ./...` | CI ✔ | — | — |
| `make test`, `make test-debug`, `go test -race ./internal/...` | CI ✔ | **run ✔** | — |
| Android server cross-compiles (`GOOS=android GOARCH=arm64`) | CI ✔ | — | — |
| iOS library and Swift app compile | — | CI ✔ | — |
| Go server serves index, `games.json`, save API | — | **run ✔** | — |
| a real game over a session at ~19fps, 13KB frames | — | **run ✔** | — |
| single release binary serving its embedded client | CI ✔ (smoke) | **run ✔**, CI ✔ (smoke) | CI ✔ (smoke) |
| a `make dist` archive extracted and launched | cross-compiled ✔ | **run ✔** | cross-compiled ✔ |

Only macOS was executed by hand: the repository is developed on one machine,
and cross-compiling proves a build, not a run. The CI rows are
`.github/workflows/checks.yml`: its `test` job on Ubuntu, and its `smoke` job on
Ubuntu, Windows and macOS runners, which builds only `./cmd/server` there, also
cross-compiles the Android server on Linux and compiles the iOS library and app
on macOS. The rest of the tree is compiled for Windows only by cross-compiling. `verify.yml` calls
`checks.yml` on every push and pull request, and `release.yml` calls it before
it publishes, so no tag can ship a binary that was never started. What the table
shows is that nothing in the Go tree is platform-specific enough to fail to
compile or to trip `vet`, and that the paths and process handling the server
depends on were written for all three.

The `smoke` job builds the release server (`-X main.version=ci`), stages it
with an empty `games/{ktf,lgt,skt}` tree as an archive does, and checks that it
reports `wfeature-server ci (release)`, binds `127.0.0.1:11541`, answers
`/api/status`, serves the page it carries, names `beside the executable` as its
data root, answers `status`, and stops on `stop` by draining (`shutting down`
in its log) rather than being killed. The last two matter most on Windows, the
one system where a stop has no signal to fall back on. Ubuntu and Windows are
therefore verified by execution for starting, serving, reporting and stopping —
not for playing a game.

```sh
make dist VERSION=0.5.2
make dist-check
```

`make dist-check` (`internal/tools/distcheck`; flags in
[`cli.md`](cli.md#tools-under-internaltools)) reads the archives back, and the
release workflow runs it before publishing. It automates what the 0.3.0 and
0.3.1 sets were read by hand for:

- every entry extracts inside the archive's own folder, and none is a link;
- each archive holds the server, its three launchers, both READMEs, the licence
  and the notices, and the empty `games/{ktf,lgt,skt}` tree, and nothing else;
- the launchers and the binary in a Unix archive keep their executable bit and
  nothing else in it has one;
- Windows text is converted and Unix text is not: the two Korean READMEs open
  with a UTF-8 BOM and are CRLF throughout, the three `.bat` launchers are CRLF
  and ASCII without one, and no file in a `.tar.gz` carries a CR;
- the licence, the notices and both READMEs are byte-identical to this
  repository's files;
- `SHA256SUMS` covers every archive beside it and every hash matches;
- the archive built for the machine running the check unpacks a server that
  answers with the version stamped into it (`-run=false` skips this);
- with `-phones`, the APK and the IPA are present and named for this version.

What it does not answer is what that server then does, which is the smoke job
and, for a release, the same check by hand: the 0.3.1 native archive was run
from its extracted folder and served the page from its embedded client
(`client=embedded`, saves `beside the executable`), answered `/api/status`
with the stamped version, and drained on `/api/shutdown`. The macOS archive
was verified the same way, including its launcher clearing a quarantine flag
and listing a game dropped in `games/ktf/`; the launchers and the quarantine
step are what the smoke job does not cover.

**Talk to the server you started, and prove it before stopping it.** A
development machine often has servers left over from earlier sessions, each
answering `/api/status` and each willing to drain on `/api/shutdown` from any
local caller, because that route has no authentication and should not. A check that starts a server, waits for
`/api/status` and then posts a shutdown will do all three to another process if
its own failed to start. `/api/status` reports the PID for this reason: compare
it with the spawned process's before the request that stops it.

To repeat the checks by hand on another OS:

```sh
go vet ./... && go test ./... && node --test web/*.test.mjs
make serve   # or the PowerShell block above
curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:11541/
curl -sS http://127.0.0.1:11541/games.json | head -c 120
```

On Windows the first run raises a firewall prompt, because the server binds
every interface. Allowing it on the private network lets a phone reach the
page.

## A game asks to restart

If an LGT title asks to press its OZ key and restart, press the emulator's
OK/confirmation key and let the game exit before launching it again. The guest
may save its acknowledgment only through that action; restarting the emulator
immediately repeats the notice. Do not delete saves to dismiss it. See the
[recorded restart investigation](startup-compatibility-2026-09-19.md#lgt-restart-notice-e934256c238a).
