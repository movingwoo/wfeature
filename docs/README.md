# Documentation

Start with the maintained references below. `history/` preserves call traces,
measurements, rejected approaches, and older implementation descriptions;
its claims apply to their recorded revision and may be superseded. The ignored
root `TODO.md` is the local work queue. The root `README.md` and `CHANGELOG.md`
are written in Korean for players; everything here is English. `images/` holds
the page screenshots the root `README.md` shows.

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
| [Web host](../web/README.md) | The page: keypad, settings, game controllers, fullscreen, text input, PWA, and `text.properties`, where all of its words are. |
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
| [KTF](ktf.md), [LGT](lgt.md), [SKT Java](skvm.md), [SKT native](skt-native.md) | Platform loading, behavior, and remaining limitations. |
| [SKT checkpoints](skt-checkpoints.md) | The SKT Java and SGS quick save/load contract; KTF and LGT are in [architecture](architecture.md#quick-load-and-ordinary-saves) and their platform pages. |
| [LCDUI](lcdui.md), [native text input](native-text-input.md) | Screens, widgets, Host keyboard/IME, and input ownership. |
| [Rapid fire](rapid-fire.md) | The keypad's rapid-fire switch, its timing and measured cost. |
| [WIPI storage](wipi-storage.md) | The one contract KTF, LGT and SKT keep for WIPI files, directories, space and databases. |
| [RMS](rms.md) | MIDP record storage and layout. |
| [Audio](audio.md), [audio ownership](audio-ownership.md), [hqx](hqx.md) | Sound formats/timelines, clip ownership and output recovery, and image scaling. |
| [Network](network.md), [authentication](authentication.md) | Offline policy and recognized compatibility services. |
| [Code-revision compatibility](platform-compatibility.md) | Embedded exception registry and rendering evidence. |

## Subject histories

| Record | Contents |
| --- | --- |
| [KTF](history/ktf.md), [LGT](history/lgt.md), [SKT](history/skt.md) | Detailed layouts, original investigations, routes, compatibility follow-up, and the dated QA records of 2026-09-19 to 2026-09-30. |
| [ARM](history/armcore.md) | Interpreter optimizations, benchmarks, and profile evidence. |
| [Sessions](history/session.md) | Protocol and retention decisions, previous measurements, and the 2026-09-23 CPU saturation investigation. |
| [Testing](history/testing.md) | Detailed probe instructions, dated acceptance, runtime regressions, settling, WebKit observations, and the 2026-09-23 text input audit. |
| [Coverage](history/coverage.md) | The 2026-09-12 API audits; not a current missing-feature queue. |
| [Authentication](history/authentication.md) | Scheme recognition, controlled routes, and acceptance evidence. |
| [Maintenance](history/maintenance.md) | Superseded plans, performance watches, paused snapshot research, and the 2026-10-07 quick-save review. |
| [Audio](history/audio.md) | The cross-platform playback audit, corpus evidence, unverified vendor mappings, and the app timing investigation. |

## Source review

[Source review, 2026-10-16](source-review-2026-10-16.md) is a full-source
review: 153 code and 9 documentation findings, each with its resolution, and the
limits the fixes left. It stays here while its follow-up items are open, then
moves to `history/`.

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
