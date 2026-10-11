# Maintenance and unresolved decisions

The ignored root `TODO.md` is the local execution queue. This page keeps durable
constraints, evidence triggers, and unresolved decisions. It does not authorize
implementation of every possible compatibility gap. Dated reviews and retired
plans are preserved in [maintenance history](history/maintenance.md).

## Keypad density and available space

The follow-up halves each former cell horizontally, producing a fixed 14 by 9
board. Existing assigned buttons and custom merged shapes retain their footprint
through paired columns. Former single empty cells become two independent empty
cells. Version 2 is stored separately as `wfeature:keypadGridV2`; the former
`wfeature:keypadGrid` record stays intact. Already-open version-1 tabs cannot
overwrite refined arrangements, and failed writes leave the source available.

Remove the keypad's decorative side and bottom padding. The game column shares
its available width with the board, up to the existing 480 px cap. Fill the
height below the game through the bottom safe-area boundary. Dynamic viewport
height and all four safe-area insets determine the available space; rows and
columns never change with device, OS, orientation or browser chrome. Smaller
windows scale the same coordinates instead of dropping cells or adding rows.

The 270-test Node suite, both Go test profiles, race checks, vet and both embedded
server builds pass. Chromium and WebKit release runs and a Chromium debug run
verify full width, bottom alignment, safe areas, rotation, short windows, label
fitting and input. At 240 by 320 px the start menu scrolls inside the game area
without covering the keypad. The retained proof is in
[the final browser record](history/testing.md#final-fourteen-column-browser-validation).

## Keypad grid acceptance

The initial 7 by 9 grid, legacy migration, connected button shapes and editor
were implemented before the refinement above. The former size, left/right share
and top-row controls are removed. The maintained contract is in [the web host reference](../web/README.md#grid-keypad),
and repeatable checks are in [testing](testing.md#grid-keypad). The measured
layout and completed browser routes are recorded in
[the validation history](history/testing.md#keypad-grid-validation-2026-10-08).

Acceptance is still open:

- Run a local real game through gameplay with merged keypad buttons. The recorded
  KTF run confirms menu and character-selection input only; its screenshot name
  does not establish gameplay.
- Observe a physical phone using L-shaped buttons, two fingers, rotation and
  reload. Automated touch contacts do not replace this observation.

Both browsers passed the version-34 and version-35 updates to the then-current
shell version 41 on 2026-10-08,
including retained assignments, save bytes and offline shell loading. The new
prefix protects the current cache from late legacy-worker cleanup, and cleanup
only removes predecessors. The runner waits for asynchronous cache retirement
after navigation; response completion alone does not establish that it is done.

Layout sharing, server-side settings, additional services and external runtime
dependencies remain outside this request.

## Execution and acceptance follow-up

[PWA acceptance](pwa-acceptance.md) defines the release route across a service
worker upgrade — saves, settings, audio activation, touch and text input — in
Chromium and WebKit, and the physical-device table it cannot fill itself.
Real-phone suspension, long pauses, active soundtrack recovery, and operating-system
IME behavior require human/device observations.

Three questions were left open only in dated records and have not been
rechecked since:

- An LGT title's page report at medium speed showed temporary drops to 0.67x
  guest speed (4.4 fps, 57.1 ms per tick) before recovering; two attempts did
  not reproduce it ([gameplay QA](gameplay-qa-2026-09-20.md#outstanding-investigation)).
- A KTF C title advances gameplay faster than expected; extra card paints were
  excluded as the cause and its timer and update loop were not investigated
  ([KTF QA](ktf-qa-2026-09-21.md)).
- A guest callback that blocks has no out-of-band way to export a report; the
  one evidenced key-callback wait was fixed on its own
  ([LGT key callback hang](lgt-key-callback-hang-2026-09-23.md)).

The KTF native loading stall needs a matching `.mif`/`.mod` archive. A folder
label is not sufficient. Automatic landscape selection needs a package or handset
contract; a successful forced-size run is not that evidence. The available dated
SKT corpus supplies MIDlet progression evidence but no real Jlet save route.
The LGT resume callback read at `0x1` and intermittent WebKit Blob failure remain
reproduction questions, not fixed defects inferred from successful later ticks.

The alternative LGT 52-byte context needs field offsets and a discriminator.
The KTF environment return slot needs address, width, initialization, and validity
rules. Neither is a reproduced defect with a known repair. See [KTF](ktf.md),
[LGT](lgt.md), and [runtime validation](testing.md#runtime-boundary-regressions).

## Sound: what needs a handset or an ear

The playback contracts in [audio](audio.md) and
[audio ownership](audio-ownership.md) are implemented and tested in Go, in Node
and in rendered Chromium/WebKit output. What is left cannot be settled there:

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
  cost and drops, and tell rhythm, pitch/timbre, clipping and silence after
  returning to the foreground apart.
- **Handset decisions on PCM.** Whether a stream wave stops at its gate or plays
  out, same-channel retrigger, and a short fade for the waves that end loud
  (25 KTF and 62 LGT decoded waves end above a quarter of full scale) are
  listening decisions; the census is in
  [PCM contract evidence](history/audio.md#pcm-contract-evidence).
- **Lateness tolerance.** A late batch re-anchors when it holds only the clock
  frontier or is at most 200 ms late; a later one is refused and current
  output is reconstructed. The 200 ms bound comes from a slowed desktop
  browser, not a phone; tuning it needs lateness distributions from devices;
  [the live sessions](testing.md#live-audio-session-acceptance) show the two
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

Remeasure before changing ownership, scheduling, or interpreter strategy:

- KTF session/SVC work needs paired fixed-work measurements on an idle machine;
  the old overloaded-machine timing is not a usable speed comparison.
- Frame copies, debug stack profiling, and boundary string reads need a current
  allocation profile before optimization. Borrowed/owned buffer contracts remain
  explicit in [architecture](architecture.md#shared-runtime-services).
- A native-code backend needs conformance, interpreter fallback, pure-Go core
  compatibility, and a demonstrated improvement; see [ARM execution](armcore.md).
- Keypad JSON preset sharing and external-access add-ons need a product scope.
  Local keypad storage exists. The server has no access key or other
  authentication, so external access needs one before it is offered.

Detailed historical counts, benchmark names, and rejected approaches remain in
[the maintenance record](history/maintenance.md). They are not current measurements.

## Paused work and provenance

Quick save/load runs on KTF (Java/AOT, older modules and native packages), LGT
(Clets and AOT Java) and SKT (Java and SGS) through the shared session, CLI and
browser hosts. [Architecture](architecture.md#quick-load-and-ordinary-saves)
defines the state and save-ownership contracts; [testing](testing.md) records
supported variants, restart/gameplay evidence and remaining device limitations.
Unknown runtime states are refused, and real native package validation still
needs a matching `.mif`/`.mod` archive. Earlier snapshot attempts and the
implementation's component costs remain in
[the history](history/maintenance.md#quick-save-implementation-measurements).
SGS feature development remains paused; existing SGS specifications and
acceptance records are preserved.

Do not import assets, fixtures, or notices with unclear provenance. The recorded
SMAF test provenance question needs evidence before changing attribution.
Existing bundled licenses and copyright notices remain intact. Release-facing
Korean changelog proofreading belongs to the release review.

## Documentation ownership

[The documentation index](README.md) separates maintained references from history.
Update the owning reference when behavior changes. Add dated evidence to the
existing subject history when a trace, measurement, or rejected design must be
retained. Avoid a new file per task, PR, audit date, or work assignment.

Keep source revision, test conditions, outcome, and limitations with measurements.
Mark superseded claims historical. Remove completed checklist prose once the
behavior and evidence have an owner. Maintain relative links and registry evidence
anchors when consolidating files. Local reports and game-specific artifacts stay
outside tracked documentation.
