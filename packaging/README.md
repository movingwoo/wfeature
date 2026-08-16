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
They work from the **port**: the archive has no place to keep run state that
survives being moved, and the port is what the user is actually asking about.

**Each of them is one line, because the work is in the binary.** `wfeature-server
status` and `wfeature-server stop` ask `/api/status` what is there — the only way
to tell this server from a stranger holding the port, since a released binary's
path says nothing about what it is — and stop it through `/api/shutdown`, which
drains the way closing the window does. Nothing is stopped without that answer.
See `internal/launcher` and `docs/running.md`.

That used to be three implementations of the same procedure, one per operating
system, each finding the process behind the port with whichever tool that system
had: `lsof`, `ss` or `fuser` on Linux, `lsof` on macOS, `netstat` and `tasklist`
on Windows. They are gone. What is left in each script is what only a script can
do — set the working directory, clear macOS quarantine, hold a double-clicked
window open — and the one line that says what is starting and how to stop it.
That line is English, like the server's own output underneath it: the Korean in
a release is in the `README.txt` beside these, not in the window they open.

The repository's own `build.sh` and `start.sh` are a different set for a
different job: they take a profile and build from source, neither of which
exists in an archive. Its `stop.sh` and `status.sh` are the same one line these
are, pointed at whichever binary the checkout has built.

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
