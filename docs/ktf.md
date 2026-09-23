# KTF platform

`internal/platform/ktf` loads KTF packages, executes their ARM code, and composes
WIPI services with the shared backend and JVM. The CLI and server use the same
session path. Detailed layouts, call traces, earlier package investigations,
and dated measurements are in [KTF history](history/ktf.md).

## Packages and execution

The ordinary package contains `__adf__`, an AID-selected JAR, and a
`client.bin<decimal BSS size>` image. The loader validates descriptor fields,
ZIP paths, duplicate names, entry counts, expansion bounds, and image/BSS size.
It maps the image at `0x00100000`, enters Thumb code at `0x00100001`, and
validates the returned initialization and class metadata before using it.
The combined image and BSS are limited to 256 MiB.

The JVM context sits beside an ordinary image so representable object-header
displacements retain canonical image class addresses during compiled access
checks. Runtime aliases and inherited field payloads are described in the
[startup compatibility investigation](startup-compatibility-2026-09-19.md#ktf-startup-exception-1379b56de66e).

The earlier native package path accepts the supported `.mif`/`.mod` shape.
It has its own loader and platform interfaces inside the same platform package.
A folder name containing BREW does not identify that format; detection uses
archive contents. The recorded native loading stall still needs the matching
archive before it can be reproduced.

Guest workers can park with nested Go/ARM calls intact. Host callbacks and
worker execution use bounded instruction windows. AOT nesting is bounded per
logical worker, including derived ARM calls. The JVM/ARM bridge carries the
current `jvm.Invocation`, preserving monitors, class initialization, and the
instruction budget across Java → ARM → Java reentry.
A client-thread serial callback does not consume a guest worker grant, so a
self-requeueing UI cannot starve a receiver. Each round retains the configured
worker limit and the existing serial callback pacing.

Normal native returns use the established register convention. No environment
return-slot precedence is implemented: its address, width, initialization, and
validity rules lack concrete ABI evidence. Existing helper stacks and argument
containers retain their arena lifetime; regression coverage proves no premature
reuse, not a new reclamation policy.

## Lifecycle and input

Sessions drive startup, pause, resume, and unconditional `destroyApp(true)`
before unwinding workers. A failing teardown callback is diagnosed and cannot
prevent termination; repeated close does not call it twice.

Keys reach cards or the guest event queue according to the active path. User
events at or above `0x5000` retain their type and two arguments. Unknown reserved
event kinds below that boundary remain errors. WIPI Java supports Host pointer
events; earlier native packages do not gain pointer support from that fact.

Host keyboard/IME entry supports active LWC fields, the sole direct text child
of a shown Shell when explicit focus is absent, and the observed non-modal
KFC form containing one listened text field. The guest owns acceptance and
persistence. Synchronous `doModal` remains unsupported. See
[native text input](native-text-input.md) for target ownership and validation.

## Graphics and sound

The runtime provides guest framebuffers, clipping, pixel operations, images,
text, and the implemented WIPI drawing surface. KTF Java printable ASCII uses
five-pixel advances; Korean syllables retain ten-pixel advances. Measurement,
anchors, and rendering agree. NUL padding draws nothing and advances zero.
Native WIPI-C text retains its own metrics.

Partial LCD flushes copy the requested region into a retained Host image without
changing their source. Pixels outside the region survive. An unchanged Card
paint preserves that image; changed screen bytes present normally. An explicit
flush during paint is not overwritten by another implicit full flush. Forced
worker slices do not request unsolicited Host paints; repaint, yield, and idle
paths still paint.

Optional intermediate frames reach the Host while guest callbacks are running.
They are owned copies, sampled at most once per 1/60 wall-clock second and
sent without waiting for a consumer. The browser uses its existing PNG queue.
See [session presentation](session.md#presentation-and-audio).

Mutable Image surfaces and Clip state use weak object ownership. Graphics keeps
its Image reachable. Collection releases orphan surfaces and stopped audio;
playing orphans remain until playback finishes, including repeating playback.
Shared audio timing and borrowed-buffer rules are in
[architecture.md](architecture.md#shared-runtime-services).

## Files and records

File input/output streams and direct File operations share one cursor. Stream
close does not close the underlying File. Reads report storage I/O failures.
Java and WIPI-C file/record mutations stage bytes, cursors, handles, and deletion
ledgers before publishing changes. Multi-key changes use `SaveBatchStore`.
The earlier native package retains its existing buffered write/flush policy.
See [save integrity](session.md#save-integrity) for failure recovery and limits.

A saved file can share its name with a packaged resource directory. A lookup
below that saved file falls through to the archive without recording a storage
failure. The bounded local slot service also accepts the first, zero-based
slot. See [slot creation and save overlay evidence](ktf-save-slots-2026-09-23.md).

## Diagnostics and limits

Use [CLI commands](cli.md) for `runktf`, `ktfdump`, routes, imports, profiles,
and memory watches. [Testing](testing.md) describes opt-in local probes and
what each acceptance level establishes.

The checked inventories in `internal/platform/ktf/testdata/` are authoritative
for registered Java surface gaps and fixed-value stubs:
`wipi_java_surface.txt`, `wipi_java_gaps.txt`, and `wipi_java_stubs.txt`.
A registration or static reference is not proof that a real caller executes it.

Known limits include animated image callbacks, unapplied graphics alpha,
partial widget rendering, synchronous modal input, unconfirmed native return
slots, and missing archive evidence for automatic landscape selection and the
native loading stall. Published instance-field divergence is diagnosed rather
than repaired using an invented precedence rule. Corpus startup counts do not
establish complete gameplay or save restoration.
