# Maintenance and unresolved decisions

The ignored root `TODO.md` is the local execution queue. This page keeps durable
constraints, evidence triggers and unresolved decisions; it does not authorize
implementing every possible compatibility gap. Dated reviews and retired plans
are in [maintenance history](history/maintenance.md).

<a id="keypad-density-and-available-space"></a>

## Keypad grid acceptance

The fourteen-by-nine grid keypad, its version-2 storage, migration from the
seven-column layout and the available-space sizing are implemented. The
contract is in [the web host reference](../web/README.md#grid-keypad),
repeatable checks are in [testing](testing.md#grid-keypad), the measured layout
and browser routes are in
[the validation history](history/testing.md#keypad-grid-validation-2026-10-08),
and the rendered Chromium/WebKit runs, including the version-34 and version-35
upgrades to shell version 41 on 2026-10-08, are in
[the final browser record](history/testing.md#final-fourteen-column-browser-validation).

Acceptance is still open:

- Run a local real game through gameplay with merged keypad buttons. The recorded
  KTF run confirms menu and character-selection input only; its screenshot name
  does not establish gameplay.
- Observe a physical phone using L-shaped buttons, two fingers, rotation and
  reload. Automated touch contacts do not replace this observation.

Layout sharing, server-side settings, additional services and external runtime
dependencies are outside this request.

## Execution and acceptance follow-up

[PWA acceptance](pwa-acceptance.md) defines the release route across a service
worker upgrade — saves, settings, audio activation, touch and text input — in
Chromium and WebKit, and the physical-device table it cannot fill itself.
Real-phone suspension, long pauses, active soundtrack recovery and
operating-system IME behavior need human or device observation.

Three questions are open only in dated records and have not been rechecked:

- An LGT title's page report at medium speed showed transient drops (0.67x
  guest speed at 4.4 fps and 57.1 ms per tick) and a later severe window down to
  0.06x (635.6 ms per tick) before recovering; a CLI replay and a WebSocket
  replay did not reproduce it
  ([gameplay QA](gameplay-qa-2026-09-20.md#outstanding-investigation)).
- A KTF C title advances gameplay faster than expected; extra card paints were
  excluded as the cause, and its timer and update loop were not investigated
  ([KTF QA](ktf-qa-2026-09-21.md#speed-and-text-evidence-still-open)).
- A guest callback that blocks has no out-of-band way to export a report; the
  one evidenced key-callback wait was fixed separately
  ([LGT key callback hang](lgt-key-callback-hang-2026-09-23.md)).

Questions that wait for evidence rather than work:

- One KTF native (BREW) archive stops on its loading screen: it blits from a
  surface nothing writes after start-up
  ([history](history/ktf.md#implementation-what-writes-this-for-a-title-nothing-is-unanswered-for)).
- Automatic landscape selection needs a package or handset contract; a
  successful forced-size run is not that evidence.
- The available dated SKT corpus supplies MIDlet progression evidence but no
  real Jlet save route.
- The LGT `resumeClet` read at `0x1` and the intermittent WebKit Blob failure
  remain reproduction questions, not defects inferred fixed from later ticks.
- The alternative LGT 52-byte graphics context needs field offsets and a
  discriminator. The KTF native environment return is established for tag-2
  word results only
  ([KTF QA](ktf-qa-2026-09-21.md#native-environment-return-and-the-slow-case));
  other tags and wide returns need address, width, initialization and validity
  rules. Neither is a reproduced defect with a known repair. See [KTF](ktf.md),
  [LGT](lgt.md) and [runtime validation](testing.md#runtime-boundary-regressions).

## Sound: what needs a handset or an ear

The playback contracts in [audio](audio.md) and
[audio ownership](audio-ownership.md) are implemented and tested in Go, in Node
and in rendered Chromium/WebKit output. The remainder cannot be settled there:

- **The original APK report.** A KTF title on the Android app (probably 0.5.0;
  phone, WebView version and output route unknown) sounded different. The
  coarse-clock scheduling defect found for it is repaired in the shared page
  ([APK audio timing](history/audio.md#apk-audio-timing)); the reporter's scene
  has not been compared on a physical phone, and LGT/SKT have not been listened
  to on the app. Compare one playback at a time with the same archive, scene,
  version, speed, BGM/effects settings and physical output:

  | Route | Server | Client | What the pair separates |
  | --- | --- | --- | --- |
  | A | PC | Chrome on the phone | the browser baseline |
  | B | the phone (the app's own server) | Chrome on the phone, `http://127.0.0.1:11541` | A/B: local emulation and delivery load |
  | C | the phone | the app's WebView | B/C: WebView, settings, lifecycle and output |

  For B, keep the app's server alive and check that switching apps does not end
  or pause the session; the app and the browser keep separate settings. Record
  the mixed output with event arrivals, clock steps, sample rate, latency, tick
  cost and drops, and distinguish rhythm, pitch/timbre, clipping and silence
  after returning to the foreground.
- **Handset decisions on PCM.** Whether a stream wave stops at its gate or plays
  out, same-channel retrigger, and a short fade for waves that end loud (25 KTF
  and 62 LGT decoded waves end above a quarter of full scale) are listening
  decisions; the census is in
  [PCM contract evidence](history/audio.md#pcm-contract-evidence).
- **Lateness tolerance.** A late batch re-anchors when it holds only the clock
  frontier or is at most 200 ms late (`MAX_REANCHOR_LATE` in `web/audio.js`);
  a later one is refused and current output is reconstructed. The 200 ms bound
  comes from a slowed desktop browser, not a phone; tuning it needs lateness
  distributions from devices.
  [The live sessions](testing.md#live-audio-session-acceptance) show the two
  cases seen so far.
- **Silent files.** 23 files whose stream notes select waves they do not carry
  and twelve HPS files that hold only device commands need original runtime
  evidence, not a guessed mapping
  ([sounds without audible events](history/audio.md#sounds-without-audible-events)).

Measured-work candidates for timbre, none started: the page maps 128 programs
onto 16 oscillator families and plays drums as a short filtered-noise burst, so
distinct attack/decay/sustain/release shapes per family and a small procedural
drum model could separate instruments without importing a sample bank. Measure
clipping of the actual mixed MIDI/PCM output at the supported slider settings
before adding headroom or a limiter, and measure native CPU and memory of the
retained sources before tuning the source budgets. No vendor sound bank is
guessed from SysEx, and the synthesis engine is not to be replaced wholesale.

## Performance and optional features

Remeasure before changing ownership, scheduling or interpreter strategy:

- KTF session/SVC work needs paired fixed-work measurements on an idle machine;
  the old overloaded-machine timing is not a usable speed comparison.
- Frame copies, debug stack profiling and boundary string reads need a current
  allocation profile before optimization. Borrowed/owned buffer contracts are
  in [architecture](architecture.md#shared-runtime-services).
- A native-code backend needs conformance, interpreter fallback, pure-Go core
  compatibility and a demonstrated improvement; see [ARM execution](armcore.md).
- Keypad JSON preset sharing and external-access add-ons need a product scope.
  Local keypad storage exists. The server has no access key or other
  authentication, so external access needs one before it is offered.

Historical counts, benchmark names and rejected approaches are in
[the maintenance record](history/maintenance.md); they are not current
measurements.

## Paused work and provenance

Quick save/load is implemented for KTF (Java/AOT, older modules and native
packages), LGT (Clets and AOT Java) and SKT (Java and SGS) through the shared
session, CLI and browser hosts.
[Architecture](architecture.md#quick-load-and-ordinary-saves) defines the state
and save-ownership contracts and [testing](testing.md) records the supported
variants and evidence. Real native package validation still needs a matching
`.mif`/`.mod` archive. Earlier snapshot attempts and component costs are in
[the history](history/maintenance.md#quick-save-implementation-measurements).

SGS feature development is paused; existing SGS specifications and acceptance
records are preserved.

Do not import assets, fixtures or notices with unclear provenance. The SMAF test
provenance question (`internal/audio/smaf/smaf_test.go` describes carried-over
cases; the third-party notices describe the SMAF/MIDI code as project-authored)
needs evidence before attribution changes. Existing bundled licenses and
copyright notices stay intact. Release-facing Korean changelog proofreading
belongs to the release review.

## Documentation ownership

[The documentation index](README.md) separates maintained references from
history. Update the owning reference when behavior changes. Add dated evidence to
the existing subject history when a trace, measurement or rejected design must be
retained; avoid a new file per task, PR, audit date or work assignment.

Keep source revision, test conditions, outcome and limitations with
measurements. Mark superseded claims historical. Remove completed checklist prose
once the behavior and evidence have an owner. Maintain relative links and
registry evidence anchors when consolidating files. Local reports and
game-specific artifacts stay outside tracked documentation.
