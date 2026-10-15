# packaging

The files a release archive carries beside the server binary. `make dist`
stages one directory per platform out of this tree and archives it; nothing
here is compiled or read at runtime.

```
packaging/macos/    start.command  stop.command  status.command  README.txt
packaging/linux/    start.sh       stop.sh       status.sh       README.txt
packaging/windows/  start.bat      stop.bat      status.bat      README.txt
packaging/games/    README.txt     shared by every platform, staged as games/README.txt
```

**`start` exists because a double-clicked binary cannot do two things**: set
the working directory to the extracted folder, so the `games/` tree beside the
binary is the one that is read, and open the page, which `start` asks of the
server with `-open` once its port answers (on a Linux machine with no
`DISPLAY` or `WAYLAND_DISPLAY` the server skips the browser). On macOS it does
a third: clearing the quarantine flag the rest of the folder inherited from the
download (`xattr -dr com.apple.quarantine .`), which is the failure a
first-time user hits with an unsigned binary. Each `start` passes its own
arguments on to the server. The Windows one runs the server in its own window
with `cmd /k`, so a startup error stays readable and closing the window stops
the server.

**`stop` and `status` are one line each, because the work is in the binary.**
`wfeature-server status` and `wfeature-server stop` ask `/api/status` what is on
the port — the only way to tell this server from a stranger holding it, since a
released binary's path says nothing about what it is — and stop it through
`/api/shutdown`, which drains as closing the window does. Nothing is stopped
without that answer. Both take an optional port (`stop 11542`), because the
archive has no place to keep run state that survives being moved, and the port
is what the user is asking about. See `internal/launcher` and
`docs/running.md`. They exist for the server whose window was closed without
stopping it, or that was left running from an earlier login.

These scripts replace three per-OS implementations that found the process
behind a port with whatever tool the system had: `lsof`, `ss` or `fuser` on
Linux, `lsof` on macOS, `netstat` and `tasklist` on Windows. What remains in
each script is what only a script can do — set the working directory, clear
macOS quarantine, hold a double-clicked window open (`pause` on Windows, a
prompt in `status.command`) — and one line saying what is starting and how to
stop it. That line is English, like the server's own output beneath it; the
Korean in a release is in the `README.txt` beside these, not in the window they
open.

The repository's own `build.sh` and `start.sh` are a different set for a
different job: they take a profile and build from source, neither of which
exists in an archive. Its `stop.sh` and `status.sh` are the same one line,
pointed at whichever binary the checkout has built, with `go run ./cmd/server`
as the fallback.

**The archive ships an empty `games/` tree, and that is load-bearing.**
`dataRoot` in `cmd/server/main.go` treats the executable's own directory as the
data root only when a `games/` directory sits beside it, and otherwise falls
back to `var/` under the directory the process was launched from. A release
without that folder would write saves somewhere the user did not choose.

The `README.txt` files are end-user text for the same audience as the root
`README.md`, so they are Korean; the rest of the documentation stays English.

Text destined for Windows is converted while it is staged: CRLF, because
`cmd.exe` parses `.bat` files line by line and the READMEs open in whatever
editor a first-time user has, and a UTF-8 BOM on the two READMEs so an editor
does not guess CP949. The `.bat` files get no BOM, because `cmd.exe` would echo
it. The sources here stay LF like the rest of the repository.
`make dist-check` verifies all of this in the built archives
(`docs/running.md`, "What was verified, and where").
