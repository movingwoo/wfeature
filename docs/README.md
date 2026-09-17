# Documentation

Start with the maintained references below. `history/` preserves call traces,
measurements, rejected approaches, and older implementation descriptions.
Historical claims apply to their recorded revision and may be superseded.
The ignored root `TODO.md` is the local work queue.

## Running and developing

| Document | Use it for |
| --- | --- |
| [Running](running.md) | Builds, launchers, deployment, access control, data, and releases. |
| [Mobile](mobile.md) | Android/iOS packaging and device limitations. |
| [CLI](cli.md) | Commands, flags, routes, scans, and diagnostic tools. |
| [Testing](testing.md) | Required gates, fixtures, acceptance, and evidence limits. |
| [Maintenance](maintenance.md) | Unresolved decisions, paused work, and documentation ownership. |

## Runtime references

| Document | Use it for |
| --- | --- |
| [Architecture](architecture.md) | Host / Runtime / Execution, archive bounds, profiles, and shared service contracts. |
| [Sessions](session.md) | Browser protocol, retention, input, frames, audio, and save integrity. |
| [JVM](jvm.md) | Java execution, class library, and semantic limits. |
| [ARM](armcore.md) | Instruction boundary, threads, memory, profiling, and backend conformance. |
| [KTF](ktf.md), [LGT](lgt.md), [SKT Java](skvm.md) | Platform loading, behavior, and remaining limitations. |
| [LCDUI](lcdui.md), [native text input](native-text-input.md) | Screens, widgets, Host keyboard/IME, and input ownership. |
| [RMS](rms.md) | MIDP record storage and layout. |
| [Audio](audio.md), [hqx](hqx.md) | Sound formats/timelines and image scaling. |
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
