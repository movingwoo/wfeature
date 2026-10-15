# Runtime data

This directory is the root for local runtime data that is neither source nor a
build artifact. A released server uses the same layout in the directory beside
its executable; see `docs/running.md`, "A release build".

- `games/`: user-provided game archives, put here by hand. The picker reads
  this directory and one level of group directories below it.
- `ext/`: game archives that arrived through the page's ＋ 게임 추가 button. They
  are a root of their own because what the page added is what the page may
  delete: an archive written here and one dropped into `games/` are otherwise
  the same bytes, and the picker is a directory read rather than a registry, so
  the file's location is the record. Removing one leaves its saves alone — a
  save is keyed by what is inside the archive, so adding the same file again
  lands on the same progress. The empty `.adopted` file records that the
  one-time move of archives an older build left loose in `games/` has run;
  deleting it makes that move run again on the next start.
- `routes/`: scripted ways back to a scene, for `runktf`, `runlgt` and
  `runskt -route`. A route is written against one archive, usually named after
  it, and reaching a late scene can take thousands of ticks to script, so the
  ones worth keeping live here beside the archives they drive. `docs/cli.md`,
  "Routes", has the syntax.
- `run/server-<profile>.log`: the log of the server `./start.sh` started for
  that profile, truncated on each start. No pid file is kept: `./status.sh` and
  `./stop.sh` ask the server on its port instead.
- `savedata/<profile>/<platform>/<owner>/`: saves, one directory per game, where
  `<platform>` is `ktf`, `lgt` or `skt` and `<owner>` is the PID (KTF, LGT) or
  program number (SKT). A KTF owner splits into `db/`, `jdb/` and `fs/`. This
  tree is the source of truth for saves — the CLI and the browser both read and
  write it in place. Each build profile owns its own tree, so playing a debug
  build never moves a release build's progress. Both binaries pick the profile
  from their own build tag; the server's `-saves` flag or `WFEATURE_SAVE_ROOT`
  moves its KTF tree (the other platforms are its siblings), and the CLI's
  `-save` flag moves its tree. Quick saves live in the reserved sibling
  `.wfeature-quicksave/owners/<owner>/`.
- `savedata/pending-import/`: save data brought over from another emulator whose
  game this build cannot identify yet. It is kept in the layout it arrived in
  rather than converted, so `wfeature importsaves var/savedata/pending-import`
  can pick it up once the platform that claims it lands.
- `logs/`: the reports a debug server writes, and the page logs posted beside
  them, named `<UTC timestamp>[-<label>].log`. A report is at most 2 MiB, at
  most ten are accepted a minute, and reports older than 14 days or beyond
  64 MiB in total are dropped oldest first. A release server writes none.
- `acceptance/<date>.md` and `<date>.ndjson`: what `make acceptance` found when
  it ran every local archive probe, as a report and one record per archive.
  One pair per day it is run, because the answer is only true of a date and of
  the corpus in `games/` at the time. The rows are archive file names, which is
  why they live here rather than in `docs/`.
- `custom/`: a local working area for modified copies of archives. Nothing in
  the build reads it.

Everything under `var/` except this file is excluded from Git (`/var/*` in
`.gitignore`): the archives are not this project's to publish, saves belong to
whoever played, and a run's reports are that run's alone. The trees are shared
by the debug and release builds except where a path names the profile. Nothing
here is touched by a rebuild.
