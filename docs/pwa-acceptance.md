# PWA release acceptance

This is the bounded acceptance path for a release of the browser PWA, the
primary distribution target. A release candidate's record must keep browser
automation, physical-device observations and untested routes apart. An
installed shell does not run the emulator offline: the server owns execution
and saves.

## Automated route

`web/acceptance/pwa.mjs` drives real Chromium or WebKit against two
release-profile server executables, first the previous release and then the
candidate, on the same origin with one persistent browser profile (390×844,
touch enabled). It passes `-web` a directory that does not exist, so each binary
serves its own embedded client, including the old service worker. It uses only
the authored `persistence-skt.zip` and `text-input-skt.zip` fixtures and never
reads or changes a person's library or saves: the run's games are under
`var/games/.<run>`, its uploads under `var/ext/<run>` (removed at the end) and
its saves under `var/savedata/<run>`. Logs, screenshots, browser storage and
`result.json` stay under the ignored `var/acceptance/pwa-<engine>-<timestamp>/`.

The candidate's cache is named `wfeature-pwa-v<N>` in `web/service-worker.js`
(`v57` on 2026-10-14), outside the cleanup prefix of already-installed legacy
workers. It retires legacy caches and strictly older versions in the new
prefix. The runner reads the candidate's cache name from its embedded worker and
waits for cache retirement after navigation, including the worker's
asynchronous work after the response completes. The
[final grid validation record](history/testing.md#final-fourteen-column-browser-validation)
records successful version-34 and version-35 upgrades in both engines; earlier
records keep the overlapping-worker failure and its regression proof. The
baseline may also be the seven-column grid build: its version-1 layout must
migrate to paired columns in the candidate's fourteen-column grid while the old
record stays byte-for-byte intact in its separate storage slot.

The route's ten checks are made over six steps:

1. Load the previous release, activate its service worker, change keypad type
   and a key assignment through the page, adjust size on a baseline that still
   has sliders, and upload an archive through the file picker.
2. Run the persistence fixture, advance its counter with a touch on Confirm,
   stop it, and export a `.wfs` through the page. The counter writes an RMS
   record through the guest runtime; it is not a file seeded by the runner.
3. Stop the browser and server. Start the candidate on the same address and
   data paths, reopen the browser profile, update the worker, and reload.
   Compare archive and save hashes and retained browser settings. Check that
   the old shell cache is retired and that the manifest declares standalone
   mode and icons.
4. Start a fresh guest and read the saved counter through the canvas. Advance
   again, import the previous `.wfs` and accept the page's own **가져오기**
   question, and start another fresh guest. Require the old counter and
   identical exported backup bytes.
5. Check that touch release reaches the guest. Observe the AudioContext, suspend
   it, and require a game-key gesture to resume it; this does not prove audible
   output. Open text input over an unsupported canvas and over a supported
   editor. Verify status, disabled submission, Korean composition isolation,
   guest readback, and constraint failure/retry.
6. Reload the page and confirm the retained guest text survives. Stop the
   server, reload the cached shell, restart the server, and load saved progress
   in a fresh guest. A cached page is not evidence of offline gameplay.

The fixture paints red, green or blue for progress 0, 1 or 2; magenta reports
an RMS failure. A corner marker is white while a key is held and black after
release. This tests executable save restoration, not only copied bytes. It
establishes the shared upgrade/backup path and the authored SKT RMS route, not
progression recovery for every KTF/LGT/SKT archive.

## Running locally

Build the old server from an isolated source snapshot that excludes the working
tree and its ignored data. For example, from the repository root:

```sh
mkdir -p build/pwa-acceptance/baseline-source
git archive v0.5.2 | tar -x -C build/pwa-acceptance/baseline-source
(cd build/pwa-acceptance/baseline-source && \
  CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=0.5.2' \
  -o ../baseline-server ./cmd/server)
CGO_ENABLED=0 go build -trimpath \
  -ldflags='-s -w -X main.version=0.6.0' \
  -o build/pwa-acceptance/candidate-server ./cmd/server
```

Use an external Playwright installation with its matching Chromium and WebKit
browsers; it is an optional acceptance tool, not a bundled runtime dependency.
Set `PLAYWRIGHT_MODULE` to that installation's absolute `index.mjs` path, and
`PLAYWRIGHT_EXECUTABLE` to select a non-default browser binary.

```sh
node web/acceptance/pwa.mjs build/pwa-acceptance/baseline-server \
  build/pwa-acceptance/candidate-server chromium
node web/acceptance/pwa.mjs build/pwa-acceptance/baseline-server \
  build/pwa-acceptance/candidate-server webkit
```

Run it against the final candidate after web changes. A result records binary
hashes, the fixture hash, browser version, duration, archive/save hashes, cache
names, passed assertions and page errors. When validating an uncommitted
candidate, keep the commit and diff beside it. A failed attempt stays a failed
record; do not relabel it when a later attempt succeeds.

## Physical-device route

Use the same server over an HTTPS origin the phone trusts. Localhost on a
developer computer suits automated worker tests; plain HTTP to that computer's
LAN address does not establish phone PWA installation. Record device, OS and
browser versions, origin, candidate revision, build profile, archive hashes and
observed results, without naming individual games in tracked documents.

| Route | Actions | Pass condition |
| --- | --- | --- |
| Installation | Install through Chrome on Android and Add to Home Screen on iOS; close and open the installed icon. | Standalone view, usable safe areas, library available. |
| Update | Install/open the previous release first; preserve origin and data, replace server binary, reopen the icon and reload. | New UI appears, settings/library/saves remain, no stale module errors. |
| Touch | Hold, slide, release outside the pad, open settings while holding, and return from another app. | No stuck movement, duplicate press or accidental game input from settings. |
| Audio | Start a known audible route through a user gesture; change both volumes, lock the phone, return, and press a game key. | Music/effects heard as expected and recover without duplicate playback. |
| Text | Select a supported guest editor, open 🔤 text input, compose Korean, cancel once and submit once; try a screen without an editor. | Correct availability guidance, no leaked keypad input, complete text delivered once. |
| Retention | Leave the app for more than five minutes, lock/unlock, interrupt the network, and return; test takeover in another tab separately. | Same retained session resumes while the server remains running; no stuck keys. |
| Progress | On each available platform, reach a reproducible scene, save in-game, stop the server, update it, restart and load; export, change progress, import (cancel the page's question once, then accept it) and load again. | Cancel sends nothing; the recorded scene/progress returns from a fresh runtime. |

A missing original archive, inaudible output or absent device is a recorded skip
with its reason, never a pass. The previously requested Jlet original is still
needed. Native APK/IPA upgrade signing, storage and document pickers need
separate app installation checks; desktop WebKit does not prove WKWebView
behavior. The iOS app's web view answers a JavaScript `confirm` "no" without
showing anything, so 세이브 가져오기 there needs its own check that the page's
question appears and a yes restores. Do not clear site data, change origin,
uninstall the app or delete saves as part of the preservation route; those
operations have different data lifetimes.

## CI boundary and cost

Go, Node, race, vet, `gofmt`, `govulncheck` and the per-OS server smoke checks
are the ordinary CI gates ([testing](testing.md#continuous-integration)). The
two browser routes stay opt-in prerelease acceptance: no browser download, npm
runtime dependency or per-push CI job is added, so they cost no recurring CI
minutes. Record local browser-route seconds separately from building both
servers and downloading browsers. Move this route into CI only after selecting a
runner and measuring its full cold-start cost. Physical installation, audible
playback and long phone suspension remain manual release observations
regardless of automation.

## Planning reconciliation

On 2026-09-18 the GitHub PR API reported #144–151 and #153 merged, and #152 and
#170 closed without merge; the former local review queue was obsolete. Those
states did not by themselves authorize restarting SGS or quick-save work. The
source at that date included the later merged text-input, mobile preference,
rendering, audio and runtime-boundary changes (#171–178). This reconciliation
was not a real-game acceptance run.

## Recorded result: 2026-09-18

The final runs used the embedded clients from both release binaries: `v0.4.2`
(`6d62c35797081ef0e235daa47fef2194d5aacb51`) and the working candidate based on
`b1dd1cff97654109ba9c954f4ad24507e3015341` with this change. Both ran at a 390
by 844 touch-enabled viewport with an isolated persistent browser profile, and
both reported ten passed route checks and no page errors.

| Engine | Version | Route time | Evidence directory under `var/acceptance/` |
| --- | --- | --- | --- |
| Chromium | 151.0.7922.34 | 6.493 s | `pwa-chromium-1789692119165` |
| WebKit | 26.6 | 6.076 s | `pwa-webkit-1789692119166` |

These times include starting and replacing the servers and opening the browsers,
but exclude compilation and browser installation; they are local measurements,
not projected CI timings. Each directory contains `result.json`, server logs,
the old and restored `.wfs`, and screenshots of both text-input states and the
restored guest counter. Binary SHA-256 values:

- Baseline: `0af7b99276533ee4abbc5a9ea536d1d45df918f45457219246b17182f1983054`.
- Candidate: `066806026d5912e6d5fd80f13559176a0293099357361886722da516b26de30e`.

Both replaced `wfeature-shell-v15` with `wfeature-shell-v28`, kept library and
uploaded archive bytes and all saved files across server replacement, restored
the chosen keypad type and size, and read the old guest counter in a new
runtime. After that counter changed, importing the old backup restored the old
value and exported identical backup bytes. The screenshots were also inspected
for the available and unavailable text-input states.

The route found two Host gaps. Game input did not call the audio resume boundary
after a context was suspended; game-key presses and supported canvas touches
now do. A replaced worker could recreate its old cache through a late fetch
response; navigation now repeats retirement after its cache write, cache writes
extend the fetch lifetime, and offline reads use only the current shell. Cache
retirement preserves caches outside `wfeature-shell-*`. The runner waits for
activation and settled resource requests before checking retirement, because
opening a new cache does not prove that a new worker controls the page.

Only the two final directories above support this upgrade claim. Earlier
exploratory runs that served the working tree's external `web/` for both
binaries are not embedded-client upgrade evidence. A WebKit attempt using
Playwright's forced offline mode failed navigation with an internal browser
error; the final unavailable-server step instead stops the real server and
loads the cached shell in both engines. It does not claim airplane-mode or
physical suspension coverage.

`make test` passed with 208 Node cases, as did `make test-debug`, the internal
race suite and `go vet ./...`. The authored fixture packages contain only the
declared fixture classes and matching JAR/descriptor pairs. Physical
installation, real sound, phone suspension, APK/IPA upgrades and per-platform
real-game progression remain the manual routes above. The user's report that
text input already worked is kept as user feedback; this UI change did not
expand platform editor support.

## iPhone initial audio recovery (2026-09-20)

A physical iPhone PWA report described silence on first launch, with audio
starting after a trip to the Home Screen and back. The reported OS version was
iOS 27, and no matching physical device was available. The earlier desktop
browser audio checks exercised playback after additional game input and did not
cover this first-launch route.

The page resumes both `suspended` and `interrupted` audio contexts. A game start,
game input or foreground return also checks whether the audio clock advances
over 500 ms; if a visible page still reports `running` with an unchanged clock,
the page attempts one suspend/resume cycle, preserving the graph and volumes.
The check does not use signal amplitude, because a musical rest or a muted
slider is not a device failure. Audio events do not schedule recovery checks,
and returning to the library does not create an audio context. State
transitions and recovery errors go through the browser debug report boundary.

This addresses the missing interruption state and the stalled-clock mechanism
reported in [WebKit issue 263627](https://bugs.webkit.org/show_bug.cgi?id=263627).
It does not establish that either mechanism caused the reported iOS 27 silence.
A device that advances its audio clock but produces no physical output cannot be
detected by this check, and a resume promise the OS never settles is outside the
verified recovery path.

Regression tests cover initial interruption, a stalled running clock, healthy
muted playback, foreground return, background/closed contexts, and recovery
failure without an automatic retry loop; the interruption regression fails
against the previous `web/audio.js`, which never called `resume()` for
`interrupted`. Physical acceptance remains: launch the installed PWA fresh,
enter a known audible route without leaving the app, then visit the Home Screen
and return. Confirm both music and effects before and after the switch, and
save the debug report if silence persists.

Validation: all 226 page tests and `go test ./internal/webhost ./web` passed,
and `make server` built the embedded debug client. Fresh Chromium and desktop
WebKit contexts produced a nonzero signal on the first click (peak about
0.0266). With a `currentTime` getter frozen until `suspend()`, each engine
performed exactly one recovery and produced the same signal; normal playback
performed none. This is fault injection, not reproduction of an iPhone OS bug.
The real-archive route also produced nonzero output in both engines. Local
evidence is in `build/qa-ios-audio-first.log`,
`build/qa-ios-audio-{chromium,webkit}.log` and `build/qa-ios-audio-node.log`.

## 0.5.0 final candidate (2026-09-23)

The local release-profile candidate used runtime revision `47b07bb`, stamped
`0.5.0`, built with Go 1.27.1 on darwin/arm64; its SHA-256 is
`03d7da36a44daa57ca5d53cc63b92d5c4427196ad3c3ca133624c68bcb9f5f36`. The
baseline was the embedded client from `v0.4.2`.

Both complete upgrade routes passed all ten assertions with no page errors:

| Engine | Version | Seconds | Local evidence under `var/acceptance/` |
| --- | --- | --- | --- |
| Chromium | 153.0.8010.12 | 13.098 | `pwa-chromium-1790160630932/result.json` |
| WebKit | 26.6 | 5.344 | `pwa-webkit-1790160630932/result.json` |

These runs retired `wfeature-shell-v15` for `wfeature-shell-v30`, preserved
settings and archive/save bytes, restored guest progress and backups, and
checked touch, text input, audio-context resumption, reconnection and
cached-shell load. The first attempts stopped because the runner selected an
`OK` key absent from the new Type2 layout; the runner now taps `5`, which both
Type2 layouts contain and the authored fixture maps to FIRE. The failed attempts
remain recorded as `pwa-chromium-1790160524887` and
`pwa-webkit-1790160524919`; no runtime fix was needed.

The ordinary suite (227 Node tests), debug suite, internal race suite, vet and
both server-profile builds passed, and the keypad checks passed after the runner
correction. This is browser automation; physical-device, audible-output,
native-app upgrade and per-platform real-game coverage remain as described
above.

## 0.5.1 release candidate (2026-10-01)

The candidate used runtime revision `deaba53a28c699436859d730de20cc8cc0c56330`,
with release metadata and acceptance-runner changes in the working tree. The
darwin/arm64 server came from the local `make dist VERSION=0.5.1` archive, built
with Go 1.27.1 and `CGO_ENABLED=0`; its SHA-256 is
`8b9cce01097707555244cfc19668199e5ac5d2549470313f5d0886ed24018359`. The
baseline was the published `v0.5.0` darwin/arm64 server, SHA-256
`6489cf0759cc1a15477a86cb3ec0207871992b8af32fcb05665dadb057f956c7`.

Both upgrade routes passed all ten assertions with no page errors:

| Engine | Version | Seconds | Local evidence under `var/acceptance/` |
| --- | --- | --- | --- |
| Chromium | 153.0.8010.12 | 12.649 | `pwa-chromium-1790820803031/result.json` |
| WebKit | 26.6 | 5.341 | `pwa-webkit-1790820804190/result.json` |

The routes replaced `wfeature-shell-v30` with `wfeature-shell-v32`, preserved
library, upload and save bytes and keypad settings, restored guest progress
through a fresh runtime and a `.wfs` backup, and covered text, touch,
audio-context resumption, retained-session reload and cached-shell loading.

The first PWA and frame-delivery attempts timed out on `#settings-toggle`,
because settings had moved into a keypad cell. The PWA runner now accepts both
versions' controls, and the frame-delivery and rapid-fire runners use the
current control; only acceptance scripts changed. Failed records remain under
`pwa-{chromium,webkit}-1790820687390` and
`frame-delivery-{chromium,webkit}-1790820740262`.

After that correction both frame-delivery routes passed all eleven assertions,
including lossless WebP decoding, pixel-exact update composition, explicit
redraws, reconnection and partial hq2x updates (evidence in
`frame-delivery-chromium-1790820815928/result.json` and
`frame-delivery-webkit-1790820809777/result.json`), and both rapid-fire routes
passed all nine checks. None of the six successful browser runs reported a page
error.

`make test` (271 Node tests), `make test-debug`, `go test -race ./internal/...`
and `go vet ./...` passed. The debug server built and reported its profile. All
five desktop archives, the APK and the IPA built locally with version `0.5.1`,
and the combined set passed `make dist-check` with `DISTCHECK_FLAGS=-phones`.
Local phone build numbers were `488`; CI derives the final build numbers from
the tagged commit. Artifacts, checksums, command logs and the source diff are
under ignored `build/release-0.5.1/`. The
[CI run for the runtime revision](https://github.com/movingwoo/wfeature/actions/runs/36804040435)
also passed its Ubuntu, Windows and macOS smoke checks.

These runs use authored fixtures. They do not close the
[WebKit Blob error observed during a real SKT reload](skt-resume-2026-09-30.md#validation).
Physical installation, audible output, native-app upgrades, real-game saved
progression, phone heat/data use and WebP encoding cost on the intended free
server remain outside this release check.

## 0.6.0 pre-release check (2026-10-11)

The route from a 0.5.2 server to the 0.6.0 candidate passed all ten checks in
Chromium and WebKit. The rest of that release check (corpus comparison, reviews,
upgrade rehearsal, `make dist-check`) is recorded in
[testing](testing.md#pre-release-check-for-060).
