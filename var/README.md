# Runtime data

This directory is the root for local runtime data that is neither source nor a
build artifact.

- `games/`: user-provided game archives, put here by hand
- `ext/`: game archives that arrived through the page's ＋ 게임 추가 button.
  They are a root of their own because what the page added is what the page may
  delete: an archive written here and one dropped into `games/` are otherwise
  the same bytes in the same shape, and the picker is a directory read rather
  than a registry, so where the file is has to be what answers. Removing one
  leaves its saves alone — a save is keyed by what is inside the archive, so
  adding the same file again lands on the same progress. The empty `.adopted`
  file in here records that the one-time move of what an older build left loose
  in `games/` has run; deleting it only makes that move happen again.
- `routes/`: scripted ways back to a scene, for `runktf`/`runlgt -route`. A
  route is written against one archive — usually named after it — and reaching
  a late scene can take thousands of ticks to script, so the ones worth keeping
  live here beside the archives they drive. `docs/cli.md` has the syntax.
- `run/`: what `./start.sh` wrote down about the server it started — one pid
  file and one log per profile. `./stop.sh` reads them; a pid file left behind
  by a crash is cleared rather than believed, since what decides whether a
  server is up is the process rather than the file.
- `savedata/<profile>/ktf/<PID>/<db|jdb|fs>/`: file and record-database saves,
  one directory per game. This tree is the source of truth for saves — the CLI
  and the browser both read and write it in place. Each build profile owns its
  own tree, so playing a debug build never moves a release build's progress.
  Both binaries pick the profile from their own build tag, and
  `WFEATURE_SAVE_ROOT` overrides it.
- `savedata/pending-import/`: save data brought over from another emulator whose
  game this build cannot identify yet. It is kept in the layout it arrived in
  rather than converted, so `wfeature importsaves var/savedata/pending-import`
  can pick it up once the platform that claims it lands.
- `logs/`: the reports a debug session writes, and the page logs posted beside
  them. A release server writes none.
- `acceptance/<date>.md`: what `make acceptance` found when it ran every local
  archive probe. One file per day it is run, because the answer is only ever
  true of a date and of the corpus that was in `games/` at the time. Its rows
  are archive file names, which is why it lives here rather than in `docs/`.

Everything above except this file is excluded from Git — the archives are not
ours to publish, saves belong to whoever played, and a run's reports are that
run's alone — and the trees are shared by the debug and release builds except
where a path names the profile.
