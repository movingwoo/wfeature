# Maintenance and unresolved decisions

The ignored root `TODO.md` is the local execution queue. This page keeps durable
constraints, evidence triggers, and unresolved decisions. It does not authorize
implementation of every possible compatibility gap. Dated reviews and retired
plans are preserved in [maintenance history](history/maintenance.md).

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

Quick save/load remains paused after the experimental implementation was rolled
back. In-memory retention and `.wfs` save exports are not emulator snapshots.
The continuation and restoration research is historical; resuming it requires
an explicit user request. SGS development also remains paused; existing SGS
specifications and acceptance records are preserved.

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
