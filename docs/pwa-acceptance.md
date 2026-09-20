# PWA release acceptance

This is the bounded acceptance path for the 0.5.0 prerelease. The browser PWA
remains the primary distribution target. A release candidate must distinguish
browser automation, physical-device observations, and untested routes. An
installed shell does not run the emulator offline: the server owns execution
and saves.

## Automated route

`web/acceptance/pwa.mjs` uses real Chromium or WebKit and two release-profile
server executables, first the previous release and then the candidate. It keeps
the same origin, persistent browser profile, and isolated data directories.
The runner explicitly disables the external web directory so each binary serves
its own embedded client, including the old service worker.
It uses repository-authored archives only; it never reads or changes a person's
library or saves. Files live under a unique hidden directory in `var/games`,
`var/ext`, and `var/savedata`. Logs, screenshots, browser storage and JSON results
remain under ignored `var/acceptance/pwa-<engine>-<timestamp>/`.

The route checks:

1. Load the previous release, activate its service worker, change keypad type
   and size through the page, and upload an archive through the file picker.
2. Run the persistence fixture, advance its counter with a touch on Confirm,
   stop it, and export a `.wfs` through the page. The counter writes an RMS
   record through the guest runtime; it is not a file seeded by the runner.
3. Stop the browser and server. Start the candidate on the same address and
   data paths, reopen the browser profile, update the worker, and reload.
   Compare archive and save hashes and retained browser settings. Check the old
   shell cache is retired and the manifest declares standalone mode and icons.
4. Start a fresh guest and read the saved counter through the actual canvas.
   Advance again, import the previous `.wfs`, and start another fresh guest.
   Require the old counter and identical exported backup bytes.
5. Check touch release reaches the guest. Observe the actual AudioContext,
   suspend it, and require a game-key gesture to resume it; this does not prove
   audible output. Open text input over an unsupported
   canvas and over a supported editor. Verify status, disabled submission,
   Korean composition isolation, guest readback, and constraint failure/retry.
6. Reload the page and confirm the retained guest text survives. Stop the
   server, reload the cached shell, restart the server, and load saved progress
   in a fresh guest. A cached page is not evidence of offline gameplay.

The fixture paints red, green, or blue for progress 0, 1, or 2; magenta reports
an RMS failure. A corner marker is white while a key is held and black after
release. This tests executable save restoration, rather than only copying bytes.
It establishes the shared upgrade/backup path and the authored SKT RMS route;
it does not establish progression recovery for every KTF/LGT/SKT archive.

## Running locally

Build the old server from an isolated source snapshot; keep the working tree
and its ignored data out of that snapshot. For example, from the repository root:

```sh
mkdir -p build/pwa-acceptance/baseline-source
git archive v0.4.2 | tar -x -C build/pwa-acceptance/baseline-source
(cd build/pwa-acceptance/baseline-source && \
  CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=0.4.2' \
  -o ../baseline-server ./cmd/server)
CGO_ENABLED=0 go build -trimpath \
  -ldflags='-s -w -X main.version=0.5.0-pre-acceptance' \
  -o build/pwa-acceptance/candidate-server ./cmd/server
```

Use an external Playwright installation with its matching Chromium and WebKit
browsers. It is an optional acceptance tool, not a bundled runtime dependency.
Set `PLAYWRIGHT_MODULE` to that installation's absolute `index.mjs` path. A
non-default browser binary can be selected with `PLAYWRIGHT_EXECUTABLE`.

```sh
node web/acceptance/pwa.mjs build/pwa-acceptance/baseline-server \
  build/pwa-acceptance/candidate-server chromium
node web/acceptance/pwa.mjs build/pwa-acceptance/baseline-server \
  build/pwa-acceptance/candidate-server webkit
```

Run against the final candidate after web changes. A result contains binary
hashes, fixture hash, browser version, duration, archive/save hashes, cache
names, passed assertions, and page errors. Preserve the commit and diff alongside
it when validating an uncommitted candidate. Failed attempts remain failed
records; do not relabel them when a later attempt succeeds.

## Physical-device route

Use the same server over an HTTPS origin trusted by the phone. Localhost on a
developer computer is suitable for automated worker tests; plain HTTP to that
computer's LAN address does not establish phone PWA installation. Record device,
OS/browser versions, origin, candidate revision, build profile, archive hashes,
and observed results without naming individual games in tracked documents.

| Route | Actions | Pass condition |
| --- | --- | --- |
| Installation | Install through Chrome on Android and Add to Home Screen on iOS; close and open the installed icon. | Standalone view, usable safe areas, library available. |
| Update | Install/open the previous release first; preserve origin and data, replace server binary, reopen the icon and reload. | New UI appears, settings/library/saves remain, no stale module errors. |
| Touch | Hold, slide, release outside the pad, open settings while holding, and return from another app. | No stuck movement, duplicate press or accidental game input from settings. |
| Audio | Start a known audible route through a user gesture; change both volumes, lock the phone, return, and press a game key. | Music/effects heard as expected and recover without duplicate playback. |
| Text | Select a supported guest editor, open 🔤 text input, compose Korean, cancel once and submit once; try a screen without an editor. | Correct availability guidance, no leaked keypad input, complete text delivered once. |
| Retention | Leave the app for more than five minutes, lock/unlock, interrupt the network, and return; test takeover in another tab separately. | Same retained session resumes while the server remains running; no stuck keys. |
| Progress | On each available platform, reach a reproducible scene, save in-game, stop the server, update it, restart and load; export, change progress, import and load again. | The recorded scene/progress returns from a fresh runtime. |

A missing original archive, inaudible output, or absent device produces a recorded
skip with its reason, never a pass. The previously requested Jlet original is
still needed. Native APK/IPA upgrade signing, storage and document pickers require
separate app installation checks; desktop WebKit does not prove WKWebView behavior.
Do not clear site data, change origin, uninstall the app, or delete saves as part
of the preservation route. Those are different operations with different data
lifetimes.

## CI boundary and cost

Keep Go, Node, race, vet and existing OS smoke checks as ordinary CI gates.
Run the two browser routes as opt-in prerelease acceptance for this slice.
No browser downloads, npm runtime dependencies, or new per-push CI jobs are added;
additional recurring CI minutes are therefore zero. Record local browser-route
seconds separately from building both servers and downloading browsers.
Promote this route to CI only after selecting a runner and measuring that full
cold-start cost. Physical installation, audible playback and long phone
suspension remain manual release observations regardless of automation.

## Planning reconciliation

On 2026-09-18 the GitHub PR API reported #144–151 and #153 merged, #152 closed
without merge, and #170 closed without merge. The former local review queue is
obsolete. These states do not authorize restarting SGS or quick-save work.
The current source includes the later merged text-input, mobile preference,
rendering, audio and runtime-boundary changes (#171–178). This reconciliation
is not a new real-game acceptance run.


## Recorded result: 2026-09-18

The final runs below use the embedded clients from both release binaries:
`v0.4.2` (`6d62c35797081ef0e235daa47fef2194d5aacb51`) and the working candidate
based on `b1dd1cff97654109ba9c954f4ad24507e3015341` with this change.
Both run at a 390 by 844 touch-enabled viewport with an isolated persistent
browser profile. Both report ten passed route checks and no page errors.

| Engine | Version | Route time | Evidence directory under `var/acceptance/` |
| --- | --- | --- | --- |
| Chromium | 151.0.7922.34 | 6.493 s | `pwa-chromium-1789692119165` |
| WebKit | 26.6 | 6.076 s | `pwa-webkit-1789692119166` |

These times include starting and replacing the servers and opening the browsers,
but exclude compilation and browser installation. They are local measurements,
not projected CI timings. Each directory contains `result.json`, server logs,
the old and restored `.wfs`, and screenshots of both text-input states and the
restored guest counter. Binary SHA-256 values:

- Baseline: `0af7b99276533ee4abbc5a9ea536d1d45df918f45457219246b17182f1983054`.
- Candidate: `066806026d5912e6d5fd80f13559176a0293099357361886722da516b26de30e`.

Both replace `wfeature-shell-v15` with `wfeature-shell-v28`, retain library and
uploaded archive bytes, retain all saved files at server replacement, restore
the chosen keypad type and size, and read the old guest counter in a new runtime.
After changing that counter, importing the old backup restores the old value and
exports identical backup bytes. The screenshots were also inspected for the
available and unavailable text-input states.

The route found two Host gaps. Game input did not call the existing audio
resume boundary after a context was suspended; game-key presses and supported
canvas touches now do. A replaced worker could recreate its old cache through
a late fetch response. Navigation now repeats retirement after its cache write,
cache writes extend the fetch lifetime, and offline reads use only the current
shell. Cache retirement preserves caches outside `wfeature-shell-*`. The runner
waits for activation and settled resource requests before checking retirement;
opening a new cache alone does not prove a new worker controls the page.

Only the two final directories above support this upgrade claim. Earlier
exploratory runs that served the working tree's external `web/` for both binaries
are not embedded-client upgrade evidence. A WebKit attempt using Playwright's
forced offline mode failed navigation with an internal browser error; the final
unavailable-server route instead stops the actual server and loads the cached
shell in both engines. It does not claim airplane-mode or physical suspension
coverage.

`make test` passes, including 208 Node cases; `make test-debug`, the internal
race suite, and `go vet ./...` also pass. The authored fixture packages contain
only the declared fixture classes and matching JAR/descriptor pairs.
Physical installation, actual sound, phone suspension, APK/IPA upgrades and
per-platform real-game progression remain the explicit manual routes above.
The user's report that text input already works is retained as user feedback;
this UI change does not expand platform editor support.


## iPhone initial audio recovery (2026-09-20)

A physical iPhone PWA report identifies silence on first launch, with audio
starting after a trip to the Home Screen and back. The reported OS version is
iOS 27; this environment has no matching physical device. The earlier desktop
browser audio checks exercised playback after additional game input and did not
prove this first-launch route.

The page now resumes both `suspended` and `interrupted` audio contexts. A game
start, game input, or foreground return also checks whether the audio clock
advances over 500 ms. If a visible page still reports `running` with an unchanged
clock, it attempts one suspend/resume cycle, preserving the graph and volumes.
The check does not use signal amplitude: a musical rest or a muted slider is not
a device failure. Audio events themselves do not schedule recovery checks, and
returning to the library does not create an audio context. State transitions and
recovery errors use the existing browser debug report boundary.

This addresses the missing interruption state and the stalled-clock mechanism
reported in [WebKit issue 263627](https://bugs.webkit.org/show_bug.cgi?id=263627).
It does not establish that either mechanism caused the reported iOS 27 silence.
A device that advances its audio clock but produces no physical output cannot
be detected by this check. A resume promise that the OS never settles also
remains outside the verified recovery path.

Regression tests cover initial interruption, a stalled running clock, healthy
muted playback, foreground return, background/closed contexts, and recovery
failure without an automatic retry loop. Physical acceptance remains: launch
the installed PWA fresh, enter a known audible route without leaving the app,
then visit the Home Screen and return. Confirm both music and effects before
and after the switch; save the debug report if silence persists.

Validation: all 226 page tests and `go test ./internal/webhost ./web` pass;
`make server` builds the embedded debug client. Fresh Chromium and desktop
WebKit contexts produce a nonzero signal on the first click (peak about 0.0266).
With a deliberately frozen `currentTime` getter until `suspend()`, each engine
performs exactly one recovery and produces the same signal; normal playback
performs none. This is fault injection, not reproduction of an iPhone OS bug.
The existing real-archive route also produces nonzero output in both engines.
Local evidence is in `build/qa-ios-audio-first.log`,
`build/qa-ios-audio-{chromium,webkit}.log`, and
`build/qa-ios-audio-node.log`. The interruption regression fails against the
previous `web/audio.js` because it never calls `resume()` for `interrupted`.
