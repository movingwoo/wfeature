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

The 270-test Node suite, focused Go shell handlers in both profiles and both
embedded server builds pass. Acceptance still requires browser checks for full
width, bottom alignment, safe areas, rotation, short windows and changed viewport
height. Browser launch and server binding are currently blocked by the sandbox;
a formula check alone must not be called rendered-browser evidence. The retained
proof and exact limit are in [the follow-up record](history/testing.md#horizontal-refinement-and-available-space).

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
- Repeat the embedded browser routes after final formatting and the grid runner's
  save-directory isolation correction. The final debug and release binaries build,
  but the current sandbox refuses server port binding.
- Recheck the repaired PWA update in both browsers. A late version-34 navigation
  can delete other legacy shell caches. Shells from version 35 use a protected prefix,
  and its cleanup only removes predecessors. Both overlapping-worker regression
  tests pass; the repaired two-version browser route remains blocked by port
  binding. See the validation history.

Layout sharing, server-side settings, additional services and external runtime
dependencies remain outside this request.

## Execution and acceptance follow-up

PWA acceptance still needs an explicit scope for installation, service-worker
updates, audio activation/recovery, touch, and save restoration. Existing local
Chromium/WebKit session checks cover only part of that path; see [testing](testing.md).
Real-phone suspension, long pauses, active soundtrack recovery, and operating-system
IME behavior require human/device observations.

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
  Local keypad storage and a server access key already exist.

Detailed historical counts, benchmark names, and rejected approaches remain in
[the maintenance record](history/maintenance.md). They are not current measurements.

## Paused work and provenance

KTF quick save/load resumed at the user's request and now runs through the shared
session, CLI and browser hosts. [Architecture](architecture.md) defines its state
and ownership contracts; [testing](testing.md) records supported variants,
restart/gameplay evidence and remaining device limitations. Unknown runtime
states are refused, and real native package validation still needs a matching
`.mif`/`.mod` archive. Other platforms remain outside this implementation's scope.
Earlier snapshot attempts remain in the history. SGS development remains paused;
existing SGS specifications and acceptance records are preserved.

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
