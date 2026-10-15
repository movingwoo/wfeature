# Documentation

Start with the maintained references below. `history/` preserves call traces,
measurements, rejected approaches, and older implementation descriptions;
its claims apply to their recorded revision and may be superseded. The ignored
root `TODO.md` is the local work queue. The root `README.md` and `CHANGELOG.md`
are written in Korean for players; everything here is English.

## Running and developing

| Document | Use it for |
| --- | --- |
| [Running](running.md) | Builds, launchers, deployment, access control, data, and releases. |
| [Mobile](mobile.md) | Android/iOS packaging and device limitations. |
| [CLI](cli.md) | Commands, flags, routes, scans, and diagnostic tools. |
| [Testing](testing.md) | Required gates, fixtures, acceptance, and evidence limits. |
| [PWA acceptance](pwa-acceptance.md) | The release-time browser route across a PWA upgrade, the device table, and what CI does not cover. |
| [Maintenance](maintenance.md) | Unresolved decisions, paused work, and documentation ownership. |

## Outside `docs/`

| Document | Use it for |
| --- | --- |
| [Web host](../web/README.md) | The page: keypad, settings, game controllers, fullscreen, text input, PWA. |
| [mobile](../mobile/README.md) | The Android and iOS shells and their build scripts. |
| [packaging](../packaging/README.md) | What a release archive carries beside the server binary. |
| [Runtime data](../var/README.md) | The ignored `var/` layout: games, added games, saves, logs, reports. |
| [MIDP](../internal/api/midp/README.md), [WIPI Java](../internal/api/wipi/README.md) | How the Java class libraries are declared in Go. |
| [JVM](../internal/jvm/testdata/README.md), [LGT](../internal/platform/lgt/testdata/README.md), [SKT](../internal/platform/skt/testdata/README.md) fixtures | What each authored test fixture exercises. |
| [Third-party notices](../internal/licenses/THIRD-PARTY-NOTICES.md) | Every bundled component's licence, as shipped in each release. |

## Runtime references

| Document | Use it for |
| --- | --- |
| [Architecture](architecture.md) | Layers and packages, archive detection and bounds, profiles, checkpoints and save locks, and shared service contracts. |
| [Sessions](session.md) | Browser protocol, retention, input, frames, audio, and save integrity. |
| [JVM](jvm.md) | Java execution, class library, and semantic limits. |
| [ARM](armcore.md) | Instruction boundary, threads, memory, profiling, and backend conformance. |
| [KTF](ktf.md), [LGT](lgt.md), [SKT Java](skvm.md) | Platform loading, behavior, and remaining limitations. |
| [SKT checkpoints](skt-checkpoints.md) | The SKT Java and SGS quick save/load contract; KTF and LGT are in [architecture](architecture.md#quick-load-and-ordinary-saves) and their platform pages. |
| [LCDUI](lcdui.md), [native text input](native-text-input.md) | Screens, widgets, Host keyboard/IME, and input ownership. |
| [Rapid fire](rapid-fire.md) | The keypad's rapid-fire switch, its timing and measured cost. |
| [RMS](rms.md) | MIDP record storage and layout. |
| [Audio](audio.md), [audio ownership](audio-ownership.md), [hqx](hqx.md) | Sound formats/timelines, clip ownership and output recovery, and image scaling. |
| [Network](network.md), [authentication](authentication.md) | Offline policy and recognized compatibility services. |
| [Code-revision compatibility](platform-compatibility.md) | Embedded exception registry and rendering evidence. |

## Subject histories

| Record | Contents |
| --- | --- |
| [KTF](history/ktf.md), [LGT](history/lgt.md), [SKT](history/skt.md) | Detailed layouts, original investigations, routes, and compatibility follow-up. |
| [ARM](history/armcore.md) | Interpreter optimizations, benchmarks, and profile evidence. |
| [Sessions](history/session.md) | Protocol and retention decisions, previous measurements. |
| [Testing](history/testing.md) | Detailed probe instructions, dated acceptance, runtime regressions, settling, and WebKit observations. |
| [Coverage](history/coverage.md) | The 2026-09-12 API audits; not a current missing-feature queue. |
| [Authentication](history/authentication.md) | Scheme recognition, controlled routes, and acceptance evidence. |
| [Maintenance](history/maintenance.md) | Superseded plans, performance watches, and paused snapshot research. |
| [Audio](history/audio.md) | The cross-platform playback audit, corpus evidence, unverified vendor mappings, and the app timing investigation. |

## Dated investigation records

These are dated QA and investigation reports written before the rule below
was applied to them. Each records the reproduction, the repair and what stayed
open on its date; the maintained behavior is in the references above, and an
open item that is still open is carried in [maintenance](maintenance.md).

| Record | Contents |
| --- | --- |
| [Startup compatibility, 2026-09-19](startup-compatibility-2026-09-19.md) | Three startup investigations and the KTF new-character relay and slot service. |
| [Gameplay QA, 2026-09-20](gameplay-qa-2026-09-20.md) | SKT resource archives, refresh pacing and a tutorial, and an LGT agreement screen. |
| [KTF gameplay QA, 2026-09-20](ktf-gameplay-qa-2026-09-20.md) | KTF additional-content receipts, a scene camera and repaint cadence. |
| [KTF QA, 2026-09-21](ktf-qa-2026-09-21.md) | KTF 1.2 startup regressions, a cadence survey, image lifetime and menu text. |
| [KTF C input and bitmaps](ktf-c-input-bitmap-investigation.md) | The KTF C input method and BMP transparency with a high palette index. |
| [KTF graphics and input QA, 2026-09-23](ktf-graphics-input-qa-2026-09-23.md) | Dialogue clipping and timer-deferred C text input. |
| [KTF input lifetime, 2026-09-23](ktf-input-lifetime-2026-09-23.md) | Text input kept available after a title rejects an empty name. |
| [KTF save slots, 2026-09-23](ktf-save-slots-2026-09-23.md) | Save names that are also resource directories, the first slot and collector roots. |
| [LGT QA, 2026-09-20](lgt-qa-2026-09-20.md) | Five LGT cases: rendering, the name editor, audio and pacing. |
| [LGT QA, 2026-09-21](lgt-qa-2026-09-21.md) | Ten LGT cases: startup, certificates, rendering and media. |
| [LGT key callback hang, 2026-09-23](lgt-key-callback-hang-2026-09-23.md) | A key callback that waited on a worker. |
| [SKT name input, 2026-09-23](skt-name-input-2026-09-23.md) | A name field drawn over a placeholder canvas. |
| [SKT QA, 2026-09-29](skt-qa-2026-09-29.md) | Serial repaint and input windows, the Call key and tile clipping. |
| [SKT resume, 2026-09-30](skt-resume-2026-09-30.md) | Resume without a prior start for two class sets. |
| [Text input audit, 2026-09-23](text-input-audit-2026-09-23.md) | Five text-input defect classes, the route matrix and its outcome taxonomy. |
| [CPU saturation, 2026-09-23](cpu-saturation-investigation-2026-09-23.md) | Skipping unchanged frames, deferring scaling, and frame-copy measurements. |
| [Quick-save review, 2026-10-07](quicksave-review-2026-10-07.md) | Eight quick save/load findings across KTF, LGT and SKT and their repairs. |

## Paused SGS work

[SGS overview](sgs.md) and [acceptance](sgs-completion.md) describe implemented
scope and the pause. Existing detailed contracts remain available:
[opcodes](sgs-opcodes.md), [host state](sgs-host-state.md),
[images](sgs-images.md), [SIS](sgs-sis.md),
[resource formatting](sgs-resource-format.md), [scroll](sgs-scroll.md),
[text effects](sgs-text-effects.md), and [vectors](sgs-vectors.md).
Their preservation does not resume implementation or acceptance work.

## Keeping this structure small

Update an existing reference for changed behavior. Keep dated evidence in its
subject history; do not create another report per task or PR. A new standalone
document needs a distinct audience or reusable technical contract. Preserve
reproduction conditions and limitations when merging records, update links,
and keep local game-specific reports outside Git.
