# LGT platform

`internal/platform/lgt` loads ELF-based WIPI packages and provides their C and
AOT Java interfaces. It shares the ARM core and backend services with KTF, but
uses import-table binding and its own Java object model rather than the JVM.
Detailed slot layouts, traces, rendering decisions, and dated corpus results
are in [LGT history](history/lgt.md).

## Packages and loading

An archive contains `app_info`, a JAR, and `binary.mod`. The descriptor supplies
AID, PID, and the main class. Descriptor-named JARs take precedence. If that
match is absent, a single fallback is accepted; multiple candidates require
exactly one valid module. Candidate inspection shares an expanded-byte budget
and rejects malformed auxiliary JARs.

Descriptor and entry names accept UTF-8 and MS949. Resource lookup recognizes
plain, `P/`, and `P/<AID>/` names, with JAR resources ahead of outer-archive
resources. Case-insensitive collisions are resolved deterministically; canonical
duplicates, traversal, and invalid paths are rejected.

The module parser accepts ELF32 little-endian ARM executables. Addressed
sections share a bounded mapping, and `SHT_NOBITS` reserves zero-filled bytes.
Import dispatch validates the supported tables and reports unsupported calls.
Save ownership uses PID with AID as a fallback. Colliding identifiers are not
automatically rewritten without evidence for a different identity rule.

## Execution and Java

The platform executes native Clets and its supported AOT Java surface. Java
metadata, objects, arrays, exceptions, monitors, workers, resource streams,
and supported library methods belong to the LGT implementation. Collection
traces guest references and platform roots, including shown/focused widgets,
and releases unreachable resources.

String indexing, lengths, substrings, char arrays, and StringBuffer mutations
use UTF-16 units. Isolated surrogates survive through an internal WTF-8
representation. Guest stream mark/reset operations reach supported overrides.

The API scanner uses actual dispatch resolution, including inherited baked
slots. A method name alone does not count as an implemented body. Startup
imports are a lower bound: later lazy imports and virtual calls can reach
additional unsupported methods. The Java library remains partial.

## Display and input

Graphics contexts belong to guest memory. Supported drawing honors compact
context origins, clips, overlapping self-blits, and nested pixel callbacks.
Callback errors restore the client lock. The alternate direct 52-byte context
has no established field layout or discriminator; mixed compact/direct context
support is not claimed.

The panel image follows the supported flush path. A revision-specific
initialization correction removes a verified 24-row origin addition for one
exact native-module fingerprint. It is not a platform-wide coordinate shift.
[Compatibility rules and evidence](platform-compatibility.md) describe selection,
mutation checks, and validation.

Keys and repeats use the platform's lifecycle and clock boundaries. Native
Clets do not expose a general pointer-event contract. Host text entry supports
focused LWC fields and the bounded WIPI-C input-method append path; it does not
infer arbitrary game-owned fields. See [native text input](native-text-input.md).

## Storage, audio, and offline services

File/record services use the common save boundary. Storage read errors remain
visible at native boundaries, and a retained read failure prevents subsequent
writes. Existing save formats and canonical paths remain unchanged.

Java Player distinguishes pause from stop, preserves repeat across pause/resume,
and rejects duplicate play/resume. Resume restarts the score because the mixer
does not retain a playback cursor. [Audio](audio.md) and
[shared service contracts](architecture.md#shared-runtime-services) describe
sound formats, timeline ownership, and callbacks.

Network access is refused except for narrowly recognized local compatibility
services. Both observed socket-creation slots (`0x7d0` and `0x25a`) preserve that
policy. [Authentication](authentication.md) and [network](network.md) describe
the selected adapters and unsupported service-dependent paths.

## Validation and unresolved evidence

[Testing](testing.md) covers authored fixtures and local probes. [CLI commands](cli.md)
describe `runlgt`, import traces, routes, profiles, and memory watches. Historical
archive counts apply only to the runs recorded in the history.

Remaining limits include unsupported Java methods, incomplete widget painting
and WIPI-C database operations, service-dependent guest branches, the alternate
graphics context ABI, and unverified real-phone lifecycle recovery. A sampled
`resumeClet` read at `0x1` allowed later ticks but did not establish correct
in-game resume. Forced landscape rendering does not establish an automatic
screen-selection rule. Full gameplay, audible recovery, and long-term save
restoration require their own acceptance routes.
