# SKT checkpoints

SKT Java/MIDP/WIPI, GNEX/GVM SGS and native WIPI-C programs (Clets) use the
shared checkpoint envelope, one slot per archive, CLI commands and browser keypad
controls. Restoration works in a
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
live store. A WIPI `DataBase` object keeps its name, record size and
modification time; its records are read from the current saves on a load, and
one deleted since the quick save is restored deleted. The directories WIPI
`FileSystem.mkdir` made are read from the current saves too.

An unchanged RMS handle does not write on close. Close retries only writes
already issued by the guest, so closing a handle restored over a later deletion
leaves that deletion intact. A later record mutation can create the store again.

Failed writes are retained by key even after the originating handle closes or
is removed. This includes RMS index/deletion writes. A dirty file is flushed
before capture; storage failure refuses capture/load and preserves pending writes
in the running session. Detached preparation uses a placeholder store and makes
no ordinary save access. Commit reads through a bounded, read-only cache before
attempting the old session's writes, and rebuilds again if those writes changed
anything. Unique reads and reconstructed cache copies each have a 128 MiB budget;
repeated handles cannot multiply one large save into unbounded allocations.

Every handle on one normalized key shares one file in the session (see
[the one save rule](architecture.md#one-save-rule)), so a quick step stores each
dirty path's current content once: there are no aliases to compare and no older
buffer to refuse. A store that refuses a write refuses the step with
`backend.ErrCheckpointSaveWrite` and leaves the file dirty for the running
guest. A load binds every restored handle onto one file per path, read from the
current saves; a checkpoint written while handles kept their own buffers
restores the same way, so the checkpoint format is unchanged.

WIPI streams retain file provenance. Input streams keep an independent cursor
and mark; output streams write the original file buffer and flush through the
save boundary. One input and one output stream may be open per file, following
[the WIPI File contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/io/File.md);
an earlier output-stream association had no consumer and lost its writes.
Stream close leaves the parent file open, and failed flushes remain
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

## Native Clet state

A native package's program is a Clet (see [SKT native](skt-native.md)): the
platform calls one of its entry points and it returns, so between two service
rounds nothing of it is running and there is no parked guest call to describe.
`internal/platform/skt/clet` records it there as variant 8 of the envelope
(`checkpoint_state.go`, `checkpoint_storage.go`, `session_checkpoint.go`):

- the ARM memory with its five mappings — the program's code, its data and BSS,
  the client stack, the platform's data arena and its stub space — and the
  client thread's registers. A record laid out any other way is not this
  program's and is refused, as is one whose code differs from what the archive
  loads under the session's authentication setting;
- the arena's cursor, free spans and every live `MC_knlAlloc` block, accepted
  only when the blocks and spans agree with the cursor, so the restored arena
  hands out the next address the original would have; the image sources, the
  recently released blocks a draw on a destroyed surface is told apart by, the
  user-memory pools, the pixel-operation cache and the platform stubs;
- the timers with their deadlines, the posted events, the pending network
  failures, the repaint flags, the input method's record, the media clips with
  their eviction order and mute states, the opacity masks shared between an
  image and its framebuffer, the retained frame, and the pacing, audio and
  vibration records;
- the file table by name, handle and cursor.

Deadlines are stored as the time left at the capture and laid against the
restored session's clock when the load commits, so the time a load takes is not
time a timer ran. The guest clock is stored as its age, and the audio catch-up
it implies is bounded before anything is built. Capture refuses a session that
is inside a call, delivering page text, destroyed, or past a failed save read,
and one without a save store.

The file table follows the shared rule. Every write the program makes is stored
before the call that made it returns, so a capture has nothing to store first
and a record holds no file's bytes: a load reads each named file from the store
as it is then, a handle keeps its cursor over the current file, and a name the
store no longer has leaves its handle on an empty file that the next write
makes again. The removal and directory lists are read again at the commit, so
an unreadable store refuses the load rather than the program's first call
after it. Neither program runs during a load: not the restored one's start
entry, and not the displaced one's destroy entry, which is guest code that
could write.

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
session. Java and SGS also bound pending audio catchup to 1,048,576 repeat cycles
and aggregate events, and 128 MiB of repeated PCM/SysEx payloads before adoption;
a separate 1,048,576-visit budget bounds note-off scans over both saved active
notes and notes accumulated during catchup. A forged old repeat cannot force
unbounded work on the first tick. Changing the authentication compatibility
setting between capture and load is refused because it can change the bytecode
underlying a continuation.
A Clet record is checked table against table before adoption — stubs inside
the stub space and not overlapping, blocks outside the free spans, masks and
files each held by something, deadlines and the clock anchor in range, callbacks
in executable memory — and against the restored memory it describes.
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

The native Clet is covered by `internal/testfixture.SKTCletArchive`, a native
package whose program is authored in Thumb instructions: a frame timer that
counts and draws, a field that composes through the input method, and keys that
rewrite a save, open a log and append to it through the open handle. The
`TestCheckpointSKTClet*` session tests restore it and require every later tick's
frame, flush count and guest clock to match the uninterrupted run, the files
written through a restored handle to match, and a save written after the
capture to survive a load. The engine's own tests account for every client and
runtime field (`TestCheckpointAccountsForEveryField`), refuse 25 kinds of
inconsistent record and a busy session, and read the store only when a load
commits.

`TestLocalSKTCheckpoint` is opt-in and reads ignored archives only. It runs each
archive at speed 4 over a private temporary save store, captures, changes a
later ordinary save, restores, delivers keys, continues execution and captures
again. The local corpus is acceptance evidence, not distributable test data.
Run with:

```sh
WFEATURE_SKT_CHECKPOINT_ACCEPTANCE=1 go test ./internal/session \
  -run '^TestLocalSKTCheckpoint$' -parallel=3 -count=1 -timeout=90s -v
```

`WFEATURE_SKT_CHECKPOINT_DIR` (default `var/games/skt`),
`WFEATURE_SKT_CHECKPOINT_LIMIT` (default six archives) and
`WFEATURE_SKT_CHECKPOINT_VARIANT=java|script|native` select a bounded local route. An
archive that fails to start is skipped as an existing startup limitation.

On 2026-10-07, the bounded local route passed for all 15 Java archives in the
primary SKT directory and three SGS archives selected from the larger local
corpus. At speed 4 each route ran 120 Host ticks before capture, then exercised
load, continued ticks, key press/release, preservation of a newer ordinary-save
probe and another capture. This establishes that route, not complete gameplay.
Chromium 153.0.8010.12 and WebKit 26.6 each passed all nine rendered-client checks.
The complete Go/default and debug suites, 281 page tests, internal race suite
and `go vet ./...` also passed.

On 2026-10-30 the native route passed for both local native packages, and a
probe on the CLI's manual clock compared every tick after a restore with the
uninterrupted run: one title captured every 47 ticks over 1,400 ticks of menus,
the world map and its dialogs (29 captures, 150 ticks each), the other every 83
ticks over 2,400 ticks from its start through its story scenes (27 captures, 120
ticks each), both into a fresh session and into a running one. Every compared
tick had the same frame, flush count and guest clock, and the saves matched.
