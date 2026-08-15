# packaging

The files a release archive carries beside the server binary. `make dist`
stages one directory per platform out of this tree and archives it; nothing
here is compiled or read at runtime.

```
packaging/macos/    start.command  stop.command  status.command  README.txt
packaging/linux/    start.sh       stop.sh       status.sh       README.txt
packaging/windows/  start.bat      stop.bat      status.bat      README.txt
packaging/games/    README.txt     shared by every platform
```

`stop` and `status` are the other half of leaving a server running. Closing the
window stops it and Ctrl-C stops it, but neither is available from the next
login, and a server nobody can find is what a held port looks like from outside.
They work from the **port** rather than from a pid file: the archive has no
place to keep run state that survives being moved, and the port is what the user
is actually asking about. Which is why `/api/status` exists — it is the only way
to tell this server from a stranger holding the port, since a released binary's
path says nothing about what it is. Nothing is killed without that answer.

The repository's own `build.sh`, `start.sh`, `stop.sh` and `status.sh` are a
different set for a different job: they take a profile, build from source and
keep pid files under `var/run/`. None of that exists in an archive, so the
scripts here are written separately rather than shared.

The launcher exists because of two things a bare binary cannot do when it is
double-clicked: set the working directory to the extracted folder, so the
`games/` tree beside the binary is the one that gets read, and open the page.
On macOS it does a third — clearing the quarantine flag the rest of the folder
inherited from the download, which is the failure a first-time user hits.

The archive ships an **empty `games/` tree**, and that is load-bearing rather
than a courtesy: `dataRoot` in `cmd/server/main.go` treats the executable's own
directory as the data root only when a `games/` directory sits beside it, and
otherwise falls back to `var/` under whatever directory the process was
launched from. A release without that folder would write saves somewhere the
user did not choose.

The `README.txt` files are end-user text for the same audience as the root
`README.md`, so they follow it in being Korean; the rest of the documentation
stays English.

Text destined for Windows is converted to CRLF while it is staged — `.bat`
files are parsed line by line by `cmd.exe`, and the READMEs open in whatever
editor a first-time user has. The sources here stay LF like the rest of the
repository.
