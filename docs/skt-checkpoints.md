# SKT checkpoints

SKT Java/MIDP/WIPI and GNEX/GVM SGS use the shared checkpoint envelope, one slot
per archive, CLI commands and browser keypad controls. Restoration works in a
fresh process and across debug/release profiles. A load does not replay guest
startup, constructors, initializers or termination callbacks.

Ordinary saves follow [the shared rule](architecture.md#quick-load-and-ordinary-saves):
flush the running game's already-issued writes before capture or replacement,
preserve later saves, and rebuild file/database handles from the current store.
Checkpoint records contain execution state and handle metadata, not host-owned
copies of ordinary save files. Guest arrays previously read from a file remain
ordinary guest memory, as on the other platforms.

## Java execution and platform state

`internal/jvm/bytecode_state.go` records Java frames, locals, operand values,
primitive bits, invocation PCs, execution identity and instruction budgets.
Explicit heap roots preserve aliases and objects reachable only through frames.
A restored native result or exception returns through the saved caller exactly
once. The forwarding `Thread.run` retains its already selected target.

`thread_checkpoint.go` parks JVM-owned workers at instruction boundaries. Sleep,
join, monitor wait/notification/reentry, and platform waits wake into this barrier.
Relative waits exclude time spent encoding a checkpoint. Object/class monitor
ownership and recursion are checked before worker startup. Contended synchronized
native entry is stored immediately before its invoke instruction, with the
already-popped arguments restored to the logical stack; the body has not run.
A second capture before a restored worker's first grant retains its continuation.
The standalone heap API remains strict and the KTF/AOT heap schema is unchanged.

The MIDP timer worker uses newly authored embedded Java bytecode. Its task,
remaining delay, period and callback frame therefore follow the ordinary worker
path. A blocking vendor audio call carries a native wait token. The token names
its playback generation so an old stopped waiter cannot wait for or stop a newer
playback on the same clip. Resume finishes waiting and cleanup without calling
Play again. Logical audio is reconstructed after the Host resets its output epoch.

Regenerate the authored worker and its test task with Java 8-compatible output:

```sh
javac -source 1.8 -target 1.8 -nowarn -g:none \
  internal/api/midp/java/net/wfeature/TimerThread.java \
  internal/api/midp/java/net/wfeature/TimerCheckpointTask.java
```

`newRuntime` installs the same classes/services used by startup, without creating
the application. The platform heap codec preserves hidden LCDUI data, native
images/graphics/fonts, media objects, editors, RMS handles and file streams in
one alias-preserving graph. Buffers being drawn, pending refresh buffers, and the
last presented frame are separate records. Platform roots retain display/card
stacks, pending serial callbacks, paint state, keypad ownership, device values
and file/record caches. Named FIFO events are rebuilt without serializing Go
closures. Display refresh and editor timestamps use the saved guest clock.

Host key ownership changes and their guest callbacks share the dispatch lock.
Capture takes that lock and then the worker barrier. A request cannot observe a
new pad state whose key callback has not run. Restore first prepares an isolated
runtime, validates all records and linked native data, and reads current saves.
Only after successful validation and persistence does it silently retire the old
VM, attach live devices and start the restored workers.

## Files and RMS

XFile records carry path or archive-entry provenance, mode, open state and cursor.
RMS records retain names, execution metadata and listener references. Both open
and closed cached handles are rebuilt using current saves, including current RMS
index/removal state and packaged defaults. A missing current file becomes empty;
a load never reconstructs its older bytes from a slot. Later guest writes use the
live store.

Failed writes are retained by key even after the originating handle closes or
is removed. This includes RMS index/deletion writes. A dirty file is flushed
before capture; storage failure refuses capture/load and preserves pending writes
in the running session. Detached preparation uses a placeholder store and makes
no ordinary save access. Commit reads through a bounded, read-only cache before
attempting the old session's writes, and rebuilds again if those writes changed
anything. Unique reads and reconstructed cache copies each have a 128 MiB budget;
repeated handles cannot multiply one large save into unbounded allocations.

WIPI streams retain file provenance. Input streams keep an independent cursor
and mark; output streams write the original file buffer and flush through the
save boundary. One input and one output stream may be open per file, following
[the WIPI File contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/io/File.md).
The prior output-stream association had no consumer, so its writes never reached
the file. Stream close leaves the parent file open, and failed flushes remain
retryable. XFile seek positions are bounded to the nonnegative signed Java-int
range reported by the API.

## SGS state

SGS captures between complete synchronous events. Its record preserves the VM
instruction boundary, variables/constants, resources, shared byte-buffer views,
queued bitmap aliases (including storage retained after resource resize), PCG
random state, timers, indexed graphics and last presentation. Audio, vibration,
clock, speed and pause state accompany the VM. Replacement does not call the
termination entry. SGS save writes are synchronous and it has no persistent
file handles to rebuild.

## Refusals and validation

A busy Host dispatch, in-progress class initialization, an unknown native
remainder/payload, or an opaque queued serial closure refuses capture. Workers
that cannot reach a supported boundary are limited by a one-second barrier
request; cancellation releases workers already parked. Supported native waits
must explicitly validate their token and callsite. Active cheat freezes/patches
are refused, as on the other platforms. A save store is required.

Archive identity, version/policy, bounds, frame shapes, root types, monitor
ownership, native references, menu selections, timestamps and save paths are
checked before adoption. Invalid data or failed reads do not replace the running
session. Changing the authentication compatibility setting between capture and
load is refused because it can change the bytecode underlying a continuation.
This does not make independently scheduled Java workers deterministic:
restored workers preserve execution state, but their subsequent interleaving
still depends on scheduling.

## Acceptance

Newly authored bytecode, worker, timer and JAR fixtures cover nested returns,
exceptions, wide arguments/locals, cycles, sleep/join/wait/notify, contended
monitors, native entry and timer task continuation. Platform tests cover hidden
native aliases, pending drawing versus presented pixels, WIPI roots/streams,
playback generations, current file/RMS data, bounded cache reconstruction and
failed-write/read isolation. Shared tests exercise active/paused restoration,
malformed records and fresh-process debug/release interchange.

CLI tests and real protocol-2 WebSocket tests cover Java and SGS capability,
slot save/load, paused startup, refused loads, complete-frame/output reset and
stale-input rejection. `web/acceptance/checkpoint-skt.mjs` drives the rendered
keypad editor and buttons in Chromium and WebKit, compares saved/changed/restored
pixels and ordinary save hashes, and checks damaged-slot feedback plus input
on the new epoch. Browser artifacts remain under ignored `var/acceptance`.

`TestLocalSKTCheckpoint` is opt-in and reads ignored archives only. It runs each
archive over a private temporary save store, captures, changes a later ordinary
save, restores, delivers keys, continues execution and captures again. The
local corpus is acceptance evidence, not distributable test data. Run with:

```sh
WFEATURE_SKT_CHECKPOINT_ACCEPTANCE=1 go test ./internal/session \
  -run '^TestLocalSKTCheckpoint$' -parallel=3 -count=1 -timeout=90s -v
```

`WFEATURE_SKT_CHECKPOINT_DIR`, `WFEATURE_SKT_CHECKPOINT_LIMIT` and
`WFEATURE_SKT_CHECKPOINT_VARIANT=java|script` select a bounded local route.

On 2026-10-07, the bounded local route passed for all 15 Java archives in the
primary SKT directory and three SGS archives selected from the larger local
corpus. At speed 4 each route ran 120 Host ticks before capture, then exercised
load, continued ticks, key press/release, preservation of a newer ordinary-save
probe and another capture. This establishes that route, not complete gameplay.
Chromium 153.0.8010.12 and WebKit 26.6 each passed all nine rendered-client checks.
The complete Go/default and debug suites, 281 page tests, internal race suite
and `go vet ./...` also passed.
