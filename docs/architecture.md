# Architecture

`wfeature` preserves the original Host / Runtime / Execution layering while
redefining its boundaries as Go packages.

## Layers

1. Host
   - `cmd/cli` drives a game from a terminal, and `cmd/server` serves the
     client and hosts the emulation sessions a phone plays through.
     `internal/webhost` holds the server's routes so they can be tested
     without a process, and `internal/vp8l` writes the lossless WebP
     pictures it sends a page that asks for them.
   - Owns browser canvas/audio/save APIs and native CLI input and output.
   - Supplies the framebuffer dimensions and receives complete RGBA8888 frames
     through the `internal/backend` presentation boundary.
   - The core does not refer directly to concrete browser or operating-system
     APIs.
   - The long-term local deployment runs the same server on Ubuntu, macOS, and
     Windows and accesses it as a browser PWA. There is no OS-specific desktop
     shell.
2. Runtime
   - `internal/backend` provides events, time, the virtual file system, task
     execution, and logging policy.
   - Debug and release are build profiles of the same code, not separate runtime
     environments.
3. Execution
   - `internal/jvm` executes SKT Java bytecode.
   - `internal/sgsvm` executes SKT GNEX/GVM SGS bytecode; the SKT platform
     composes it with shared backend services. See [sgs.md](sgs.md).
   - `internal/armcore` executes the initial ARMv4T subset for KTF/LGT. Shared
     memory belongs to the core, while each cooperative guest thread owns its
     register context. Instruction quanta are serialized and SVC handlers run
     outside that lock so Host waits do not block other guest threads.

`internal/platform/*` detects input formats and composes the required Execution
layer and API surface. `cmd/cli` and `cmd/server` are thin Host entry points
that call the same platform packages.

`internal/zipentry` is shared the same way and for the same reason: it holds
the conventions a platform archive's entry names follow, and the three loaders
would otherwise each answer them separately. Today that is one rule. **A copy
that was unpacked and zipped up again often gains the game's name as a
containing folder**, which leaves `__adf__` or `app_info` one level below where
the loader looks them up by exact name — so detection claims the archive for
nobody and a loader handed it reports a missing marker. Neither says "this is
packed differently"; both say "this is not a game".

Removing a directory that every entry shares cannot damage an archive that
works, which is what makes the rule safe to apply without asking. If every
entry is inside one directory then the marker is not at the root, and an
archive whose marker is not at the root is one no loader here reads today. It
fires exactly on archives that are already failing. A JAR is unaffected without
being excluded, because `META-INF/` always sits beside the rest — and the
loaders ask only when reading the outer archive, never the JAR.

Two things bound it. Only one level comes off, so an archive nested twice is
still reported rather than dug through until something matches. And **no local
archive is wrapped** — all 64 carry their marker at the root — so this is
written against a shape that has been described rather than one measured here,
and the tests build it rather than finding it.

**A zip whose entries are all zips is the other shape a person can hand over
that is not a game**, and it is answered the same way — by what it is rather
than by what it is missing. Somebody bagged three episodes of the same title
into one file; each of the three is a whole archive that runs on its own, so
the Host names the shape and says to unpack it, rather than refusing over the
descriptor the outer zip has no room for. `detect.ArchiveOfArchives` is the
test and `session.ErrArchiveOfArchives` is the answer; the page turns it into
the sentence a person reads.

The SKT loader was already immune and stays untouched, which is the more
interesting half: it finds its descriptor by extension wherever it sits,
derives the JAR name from the descriptor's own so the two move together, and
files installed files by base name. Nothing there looks up a fixed path. That
is the shape the other two would have to grow to stop needing the rule at all.

**"Nothing claimed this file" is four answers, and they need telling apart.**
`detect.Archive` answers a platform or `unknown`, and the comment beside that
value has always said what is wrong with it: "I cannot tell what this is" and
"this is damaged" are different problems and a Host should be able to say
which. Only one of the four is work this project can do — a package of a shape
this does not yet recognise — and a sweep over a corpus that counts all four
together reports a number nobody can act on.

`detect.Classify` answers the same platform with the reason beside it:
`not-an-archive`, `known-format-unsupported`, `drm-wrapped`,
`archive-of-archives`, or `no-marker`. It is a second return value rather than
a second set of platform values, so every caller that loads a game keeps asking
`detect.Archive` and only a caller that wants to count reaches for the rest.
The acceptance records carry it per file; see [`testing.md`](testing.md).

`drm-wrapped` is read from the container's header and nothing else.
`detect.DCFHeader` recognises both layouts a locked package uses — the one with
the media type and content identifier declared in the clear at the front, and
the box structure whose brand names the wrapper, whether that brand is in a
brand box or at offset zero — and reports what they declare. **Nothing decrypts and no key is read**: the payload is encrypted, the
key belongs to the network that issued it, and the only question worth asking
here is whether a file is a container that was locked or one this project has
never seen the shape of. A person holding the first is told to find another
copy; a person holding a damaged zip is told to download it again.

**The wrapper is one level in, and looking only at the outer file found none
of it.** A locked package is not a locked zip: the outer archive is untouched
and still opens, the descriptor and the icons and the data directory are all
still there, and what changed is the payload JAR beside them, which stopped
being an archive and became an encrypted container. So the marker still named
its platform, detection still answered it, and the file was counted not as
locked but as a package this project had failed to load. A `drm-wrapped` count
of zero over a whole corpus meant "not looked for" and read as "none here".

`detect.Classify` now reads the front of the payload before it answers a
platform. All three vendors package the same way — a descriptor beside a JAR
that is the game — so one rule covers them, and a wrapper that keeps the
payload's name is caught wherever it appears. The corpus holds exactly one such
file, in the older KTF collection; the LGT and SKT trees have none, and the
same check runs over them anyway because the shape is theirs too.

Two things it does not do. It asks only of an archive some platform has already
claimed, because an entry named `.jar` in a zip nobody claimed is not a payload
anybody declared. And it reads the front of the entry rather than the entry: a
box declares its size against the file it is in, so the walk is measured
against the length the zip's own directory gives, and megabytes of encrypted
content are never inflated to read a kilobyte of header. That still costs
what a deflate reader costs — measured at 182µs a file against 4µs, because
such a reader fills its window before returning a first byte, so asking for
sixteen bytes and asking for a thousand cost the same. It is a large multiple
of a small number and small beside anything done with an archive after
classifying it.

`internal/gameroot` is the other Host-side tool of that kind: it names the
depth a game library is discovered at — the root and one group below it — so
that the picker and every command which reasons about "the library" agree on
which files are in it. A tool that walks further reports on archives no Host
will offer, which is what an ignored diagnostic corpus filed under the root is
made of. `checkgames` reads through it; see [`cli.md`](cli.md).

`internal/route` is a Host-side tool rather than a layer: it reads a scripted
way back to a scene and drives one, and it belongs to no platform. What it
needs of a session — advance a tick, fingerprint the screen, send a key, say
whether a stalled guest can still resume — arrives as functions and a key
table, because the platform sessions spell those differently (a key event type
on one, a pressed flag on the other) while a route reads the same either way.
KTF and LGT both take `-route`; see [`cli.md`](cli.md).

<a id="phase-1-startup-path"></a>

## Java startup path

The currently implemented path is:

```text
JAR bytes
  -> ZIP entry validation
  -> META-INF/MANIFEST.MF
  -> MIDlet-1 main class
  -> Java class-file parser
  -> cached class loader
  -> class initialization + static/instance storage
  -> bytecode frames + native method boundary
  -> runtime-owned CLDC Object/String/Thread/I/O/collection subset
  -> runtime-owned minimal MIDP class library
  -> bounded backend event queue
  -> MIDlet construction + active/paused/destroyed/error state
  -> app property + MIDlet lifecycle native services
  -> conditional destroy refusal + retry
  -> transient start refusal + paused retry
  -> unchecked start/pause failure + forced destroy cleanup
  -> MIDlet-scoped Display + coalesced current Displayable event
  -> coalesced Canvas repaint + clipped Graphics shapes, images, and text
  -> Host-owned RGBA framebuffer
  -> Host key press/release/repeat and pointer press/release/drag callbacks
  -> CLI / session API
```

Image creation, standard-library decoding, transformed region drawing, ARGB
raster operations, font metrics, and deterministic text rendering share this
runtime path for both Hosts. Canvas game-action mapping,
full-screen state, pointer callbacks, and touch phone controls use the same
event boundary. The web Host is an installable PWA served by the same binary
that runs the sessions behind it. The current JVM scope and deliberate
limitations are documented in [`jvm.md`](jvm.md).

<a id="phase-2-execution-boundary"></a>

## Native execution boundary

The current KTF execution path is:

```text
KTF archive bytes
  -> bounded outer ZIP + __adf__ descriptor
  -> bounded AID JAR + client.bin<BSS-size>
  -> native image and zero-filled BSS mapping
  -> sparse permission-checked 32-bit guest memory
  -> bounded client self-relocation
  -> validated WipiExe / ExeInterface / function-table pointers
  -> platform-owned initialization contexts + Thumb SVC callback tables
  -> bounded interface initialization + WIPI initialization
  -> bounded raw AOT class/member parsing + Go JVM metadata registration
  -> JVM-owned String/Class objects pinned behind opaque guest addresses
  -> bounded AOT object/array guest layouts + pinned Go JVM objects
  -> registered method/field lookup + depth-bounded nested JavaJump guest body calls
  -> validated CallNative container + depth-bounded guest body + result writeback
  -> bounded exception-handler chain + typed catch + restore-PC unwind
  -> guest-layout exception object + pinned Go JVM object + typed uncaught transport
  -> per-logical-thread exception-handler head shared by nested guest calls
  -> ExeInterface.GetClass(ADF MClass) + validated AOT class registry
  -> AOT constructor / instance / static outer-call wrapper
  -> JVM object-to-guest-address argument/result conversion
  -> runtime-owned Java method metadata + direct/native dual-entry SVC proxies
  -> per-thread r0-r15 + CPSR context
  -> bounded ARMv4T instruction quantum
  -> saved context at SVC
  -> asynchronous Runtime handler / Host event
  -> same context resumed at the next guest instruction
```

Every step of that path is exercised by repository-authored fixtures and, past
them, by opt-in probes over the real archives kept out of the repository: each
one relocates, initializes, resolves its ADF main class through the real
`GetClass` export, constructs it, starts it and presents a first frame. Startup
callbacks register bounded AOT metadata and bind native strings, classes and
newly allocated AOT objects and arrays to Go JVM objects. Guest exceptions are
allocated in both representations, uncaught objects keep their guest addresses
across typed JVM errors, and handler heads are per logical ARM thread and
inherited by nested calls. Java methods the runtime owns are emitted as
validated raw KTF metadata and expose both the register-argument `fn_body` and
the native argument-container entry forms.

**Titles run rather than merely start**: input, timers, guest threads, archive
resources, sound and persistence are all in the path a played game takes. What
each title actually reaches is a per-title question, and this project publishes
no per-title answer — the claim that is kept is what a sweep of a whole local
set did on a dated run, which is the form that does not go stale by standing
still. The probes and their counts are in [`testing.md`](testing.md); the
instruction subset and the KTF format's boundaries are in
[`armcore.md`](armcore.md) and [`ktf.md`](ktf.md).

## What an archive may declare

Every platform's loader bounds the same four things about a zip, and bounds
them the same way, because three loaders that disagree about it are three
places to have to check:

| | the input | how many entries | one entry | everything expanded |
|---|---|---|---|---|
| KTF outer zip and inner JAR | 128 MB | 8192 | 128 MB | 512 MB |
| LGT zip | 128 MB | 8192 | 64 MB | 512 MB |
| SKT container and inner JAR | 128 MB | 8192 | 128 MB | 512 MB |

The per-entry bound alone is not enough: it bounds one file, and a zip that
declares eight thousand of them is eight thousand times that. The count is
checked before anything is read, because it is already in the directory the
reader parsed and reading is the expensive half. What the total counts is bytes
actually read rather than the size the zip declares, since a zip may lie about
either.

SKT was the loader that had only two of the four — its container bounded one
entry and nothing else, and its JAR bounded one entry and the total but neither
the input nor the count. The SKT container reads its entries in three passes —
the descriptor, then the JAR, then the title's installed files — so its total is
carried across the calls in a `budget` rather than counted inside any one loop.

Each loader takes its limits as a value so a test can reach the far side of
every bound without building half a gigabyte to get there; a bound nobody can
afford to test is a bound nobody tests. The largest archive in the local
library is under 4 MB, so these are a ceiling on damage and not a constraint any
real title comes near.

**A Host reads the file into memory before a loader sees it.** The bound is
therefore a bound on what the loader will parse, not on what the process will
allocate, and the input it protects is a file the user put in their own game
root. Making it a bound on allocation too means the Host measuring before it
reads, which is a change to every entry point rather than to the loaders.

## Build profiles

- debug: `-tags debug`; includes detailed logs and browser diagnostic hooks.
- release: the default build tags; retains only normal logs and applies
  `-trimpath -ldflags="-s -w"` to build artifacts.

Each profile builds into its own directory — `build/debug` and `build/release`
— so the two coexist and neither build silently replaces the other. The server
is built per profile like every other binary here. There is no flag for it: one
process serving the other profile's save tree is a way to disagree with the
binary that is running, not a feature.

Both profiles read the same `var/games` and `var/ext` and speak the same save
API, but each owns its saves: `var/savedata/<profile>/ktf/`. Playing a debug build must not
move a release build's progress, and a debug session is where a half-finished
API is most likely to write a save the game cannot read back. Both binaries
resolve that path from their own build tag, so the two Hosts of one profile
share a tree and the profiles never mix. When they should — moving a save
between profiles, or keeping one somewhere else entirely — `WFEATURE_SAVE_ROOT`
names a tree outright. The local server for Ubuntu, macOS, and Windows uses the
same profiles and data formats.

## Debug run logs

A run that goes wrong has to leave something behind, so debug builds collect a
run report; release builds transmit no diagnostics at all.

- Core: `ktf.SessionOptions.TraceLimit` and `.Logger` are the only diagnostic
  knobs the platform layer takes. The core has no build-profile knowledge —
  Hosts decide, passing `ktf.DefaultTraceLimit` in debug and zero in release.
  `Session.Diagnostics()` returns the counted boundary events plus the ordered
  trace of the most recent ones.
- Host (server): the session composes the report from the same counts and
  trace and writes it under the ignored `var/logs/` itself, because the server
  is the side holding the numbers.
- Host (web): `web/debug-log.js` retains the page's own console output and
  uncaught errors. The debug-log settings button (🐞) asks the server for its
  report and posts the page's log to `POST /api/debug-log`, so the two halves
  of a run land side by side.
- Host (CLI): `runktf <zip> -diag report.json` writes the same counts and trace
  next to the run summary.

What a release does instead is nothing, on both sides of the socket. The page
starts collecting at load — the lines worth having are the ones from before
anything knows what this is, a module that failed to import or a socket that
never opened — and the profile arrives with the session's `ready`, so a release
collects for that moment and then calls `stopLogCapture()`: the console methods
are put back, the buffer is dropped, and nothing further is retained or posted.
The server closes the other end, answering `POST /api/debug-log` with a 404
rather than serving a route its own page never uses. Neither half depends on
the other being right.

The reports that a debug server does write are bounded, because the route has
no authentication and the server binds every interface so a phone can reach it:
one report is at most `maxDebugReport`, at most `debugLogBurst` may arrive in a
rolling `debugLogWindow`, and every write prunes the directory — reports past
`debugLogLife` first, then the oldest while the directory is over
`debugLogBudget`, never the newest and never a file this server did not name.
The session reports the server composes itself go through the same writer, so
`var/logs/` is bounded whichever side wrote last.

## Session transport

The page ran the emulator itself once, compiled to WebAssembly. A phone could
not run it fast enough to play — the same build and the same game measured
roughly fifteen times slower there than on a desktop browser, and the cost was
Go's WebAssembly backend rather than the emulator (see
[`armcore.md`](armcore.md)). The Host layer therefore has one browser shape: the
emulator runs natively on a server that is left running, and the phone is a thin
client that draws the frames it is sent and posts input back. What that session
looks like end to end is [`session.md`](session.md); `internal/session` is the
platform-agnostic driver behind it, so no Host switches on platform.

Client and server talk over a WebSocket, so `internal/wsproto` implements the
framing:

- It is the whole dependency: the handshake, masked and unmasked data frames,
  and the control frames that keep a connection honest. Nothing else in the
  protocol is needed here, and the project already writes its own ELF loader,
  gdb stub, and SMAF decoder rather than take a dependency for each.
- The codec works on any `io.ReadWriter`, and both ends exist in Go. A test
  drives a real client against a real server over an in-memory transport, so
  the framing is covered without a browser or a port. `wsproto.Accept` is the
  only part that knows about `net/http`; `wsproto.Dial` is a client for tests.
- Every frame is untrusted input. Lengths are refused against a bound before
  anything is allocated — a ten-byte header may claim four gigabytes — reserved
  bits and malformed fragmentation are rejected rather than ignored, and a
  server refuses the unmasked client frames RFC 6455 forbids.
- A frame's header and payload reach the transport in one write, and
  `WriteBatch` sends several messages in one. A socket that does not wait to
  fill a packet sends each write as its own, and behind a TLS proxy as its own
  record, so a small message written in two parts paid for two sets of
  headers. The session writer batches what is queued so a tick's picture and
  sound share one; see [session transport](session.md#writes-and-statistics).
- A WebSocket handshake is not subject to CORS, so `Upgrader` checks `Origin`
  against the host the request arrived on by default. Without that, any page
  the user visits could drive a session on their own machine. A deployment
  reached by LAN IP while the page was loaded from a hostname replaces the
  policy with `Upgrader.CheckOrigin`. It is also what a reverse proxy has to
  keep in mind: forwarding to the server without passing the browser's `Host`
  through leaves every session refused, and
  [`running.md`](running.md#behind-a-reverse-proxy-on-a-unix-socket) has the
  configuration that does.

## Where a stopped game keeps its state

A parked session retains live objects and suspended Go call stacks in server
memory. It survives a disconnected browser, but not server shutdown. Guest
save files and `.wfs` exports do not capture that execution state. A server
that stops closes parked and attached sessions alike before it exits, so the
writes a title had issued reach its save files; see
[retention and control](session.md#retention-and-control).

KTF and LGT quick save/load join the shared session, CLI, server and browser
controls. The envelope, the slot and the Host commands are one implementation;
what differs per platform is the runtime record inside the envelope and how a
parked guest thread is described. A checkpoint is execution state only: it holds
no ordinary save and a load replaces none, which
[Quick load and ordinary saves](#quick-load-and-ordinary-saves) states in full.
The KTF records are below, and [the LGT ones](#lgt-checkpoints) follow them.
The [validation record](testing.md) describes authored and local-archive coverage.
The ARM core can record parked derived calls and resume them in a fresh core;
the platform supplies each pending supervisor operation's remainder. The KTF
component handles Java jumps, native return cleanup, and the existing sleep/wait
adapters. Older module invocation records preserve both return registers and
drop the helper's two spilled arguments only after a successful child return.
Their direct waits complete without repeating the sleep. Exceptions retain the
module's distinct handler layout and unwind boundary. Unknown remainders are refused. Capture
requires the caller to park the entire logical thread first.

Separate ARM state records preserve sparse memory, mapping permissions, and
logical threads' private words. Their detached constructor checks sizes, ranges,
and the host's execution policy before adoption. ARM translation caches and Host
callbacks are rebuilt.

The JVM execution component preserves the AOT worker's owner and spent bytecode
budget, including the core library's forwarding `Thread.run` remainder. It
refuses active bytecode frames, class initializers, JVM-owned goroutines, and
unknown native remainders. Workers resume through the ordinary completion path.

The separate JVM heap component captures explicit roots, object sharing, core
native payloads, statics, AOT metadata/bindings, and cooperative Thread records.
KTF can restore its allocator and array views over saved ARM memory without
copying elements through their ordinary adapters. Unknown payloads and held
monitors are refused. `Memory.ValidateRange` checks an adapter's mapped range and
permissions without reading, allocating, or changing its bytes.

KTF Graphics records retain their drawing state and target memory view. Image
records preserve bounds and both premultiplied RGBA and unassociated NRGBA colors,
including RGB hidden by zero alpha. Images and guest framebuffers retain shared
mutable transparency masks. A Java database handle is recorded as the name it
was opened under, and a `File` as its name, position, open mode and, for one
whose open asked for an empty file, the number of bytes it then held. Neither record holds
save bytes: a load fills both from the save store. Handles of one name share one
store after a load. These heap records join the service tables through the
client construction path below.

The heap component also preserves platform object roots, queued callbacks, key
ownership, events, and pending timer deadlines. Weak image and clip owners remain
weak after restoration; an owner already collected by Go requires ordinary
resource cleanup before capture. Separate audio records retain playback and output
state. Text editor records preserve the caret, character mode, UTF-16 limit,
and pending multi-tap cycle. Their relative key time and timer deadlines rebase
to the destination clock. Hosts must serialize input with capture and adoption.
The storage component records the names in the C file and record-database
catalogs, the open handles with their cursors, the handle counters, each record
database's record size, and the names of the files the session wrote. It records
no file or record bytes and none of the deletion and directory lists: those are
the saves, and a load reads them from the store again.

Local relay records retain the service phase, identity/slot/label, incomplete
outgoing frame, unread response bytes, individual close flags, and shared stream
objects. A temporary heap root carries a socket retained only by the runtime;
it receives no guest identity or ARM allocation, and adoption retains its native
payload only. Stream class and native ownership are checked before heap adoption.
This covers relay state; parked reads still require a supported call remainder.

Control records retain the guest clock's original epoch and exact relative
anchor, painting/event ownership, C input mode/controller/revision and queued
character, pending network failure callbacks, and C media clips with their
eviction order. Keeping the unscaled clock offset preserves fractional time
through a change of Host epoch. An active Host input guard, paint callback or
result-binding operation prevents capture until its remainder has completed.
C media records retain encoded bytes and handles; active playback uses the
Host output restoration below. A separate backend audio record preserves loaded event
sequences, playback cursors and repeat origins, device volume, active-note
ownership and used channels. Its bounded constructor emits no past events and
copies PCM/SysEx buffers. The session must validate the saved guest clock against
this timeline before advancing it; the record does not represent synthesizer
voices or physical output queues.
The backend vibrator has a separate validated state record. It preserves timed,
expired and indefinite requests against the motor's Host clock, without issuing
a new request or applying guest speed. Session adoption must reset the Host's
output epoch so a restored, older request counter is observed.

Runtime metadata records preserve native dispatch IDs, code and class arena
cursors, class links/aliases and initialization flags, collector allocations and
release state, WIPI C allocation ownership, user memory pools, interface/context
addresses, and pixel-operation results. Keeping the pixel cache avoids repeating
guest calls and charging their instructions again after restoration. Released
address sets and their eviction order are separate: reuse can leave duplicate
entries in the order. Decoding validates bounds, mapped permissions, duplicate
records and allocation overlap before adopting any metadata. Debug arena checks
restart from the restored bytes; historical diagnostic counters are Host state.

The client record joins these components with every worker in queue order,
including workers waiting for their first grant, private stacks and TLS, Timer
ownership, painted cards, and the LIFO stack free list. It retains the client
thread, pacing averages and deadlines, speed, executable descriptor, and the
last displayed LCD independently of unflushed back-buffer pixels. Heap, editor,
and scheduler offsets share one capture instant. Logical audio and vibration
records accompany the guest clock; audio catch-up is bounded before execution.
All worker records are validated before any restored goroutine is started.
An invalid detached client is discarded without touching the source session.

The client record remains separate from external saves and Host output.
Authored tests replace the source process
and check nested returns, exceptions, wait deadlines, multiple parked workers,
initial grants, stack reuse, and recapture before and after a restored grant.
The destination registers native implementations on a fresh VM without preparing
guest interface tables or recreating the fixture's ARM code and objects. Startup
and restoration share archive/Host attachment code. Per-run authentication save
adapters travel as what they made for the run and never had on disk: an issued
certificate, a removal made in this run, the subscriber recovery inputs.
Whatever an adapter derived from a save is derived again from the store when a
load commits; constructing one for validation reads and writes nothing.

Complete reads, writes and recovery serialize through a kernel file lock in the
reserved directory beside the owner's folder,
`.wfeature-quicksave/owners/<owner>/`, across store objects and processes. The
reserved directory keeps the live owner filename so case and Unicode aliases
share locking wherever the filesystem aliases those names. A separate
nonblocking Host claim excludes competing sessions and imports while allowing
read-only backup, and is retained while a session is parked. File data is
synced before it is renamed into place; directory syncing retains the existing
store's advisory OS/filesystem guarantees. This is not a claim of verified
power-loss durability on every target.

Both the transaction lock and the claim are taken twice: in a registry of this
process, keyed by the owner root, and as a kernel lock on a file in the reserved
sibling directory. A location that cannot hold the kernel lock, because the
reserved directory or the lock file cannot be created there or because the
filesystem has no locks, keeps the registry alone. That is at least the
exclusion the releases before the lock had, which was one mutex per store
object, but another process is no longer kept out, and each Host says so once
per directory in its log. A lock that somebody holds is contention and never
falls back, and a reserved path that is a link or not a directory is still
refused.

A per-game owner directory may itself be a symbolic link. Ordinary reads,
writes, batches, listing, export, import and the claim go through it, as they
did before the lock existed. Checkpoint slots still refuse a linked root, and
so does settling a replacement an earlier build left half done, because that
renames the directory itself.

An earlier build's quick load replaced the whole save folder: it staged the
full set, verified it, recorded an intent, moved the live directory to
`previous` and installed the staged one. **That writer has been removed.** What
remains is its recovery, which every store operation runs first: a replacement
that an earlier build left prepared or half-swapped rolls back, and a completed
one keeps its new set and its `previous`. See
[Leftovers from earlier builds](#leftovers-from-earlier-builds).

The internal KTF session API joins client and authentication records. Its
envelope carries the full archive's SHA-256, an execution-variant number and a
version shared by debug and release. A SHA-256 covers the header and both
sections. The envelope bounds session data to 64 KiB and runtime data to
128 MiB, and has no section for saves: the four header bytes that held the
length of one in version 1 are reserved and must be zero. Before typed JSON
allocation, a schema-aware pass bounds depth, value count and estimated
allocation, and rejects missing, duplicate, unknown or wrongly typed fields.
Byte payloads retain raw bytes.

Detached preparation reconstructs against a placeholder store that answers
every read as absent, refuses every write and counts what reaches it, with
silent outputs. Commit hands the restored runtime the live store, fills what it
has open from that store with reads only, and then cuts the displaced client
off: its saves go to an empty memory store of its own, its outputs are
detached, and its workers are aborted without calling destroyApp. A failed
commit leaves the original session usable and the saves as they were.
The final adoption rebases guest clocks, timers, worker/pacing deadlines, every
editor and vibration, so time spent parsing or staging files is not guest time.
This API requires the caller to serialize whole rounds, input, lifecycle, cheats,
clock changes and external save operations. A busy low-level client is refused
immediately; capture does not run pauseApp or queue a delayed request. Active
cheat freezes and patches currently refuse capture.

The shared `internal/session` API now captures, loads into an existing session,
or restores from archive/checkpoint bytes after process restart. It retains
pause state, key-repeat phase and its clock anchor, the pad, all held Host keys
(bounded to 64), and a held pointer. Restoring preserves input ownership;
`ReleaseHeldInput` is a separate Host action after output/input epochs reset.
It can execute guest callbacks, whereas capture and detached preparation do not.
Shared settings and input records must validate before the commit.
These operations use the same single-owner discipline as Tick and SendKey.
The shared record and platform clock are captured before heap copies or file I/O.

Directory stores offer one Host checkpoint slot per exact archive at
`.wfeature-quicksave/owners/<owner>/<archive SHA-256>.v2.wfq`, beside the
guest owner directory. Reads are bounded and confined to the reserved directory;
links and nonregular files are refused. Writes validate the envelope and use the
ordinary synced temporary-file replacement. Slots stay outside ordinary save
exports. The file name carries the envelope version, so no build writes over
another format's slot: a file under the version 1 name, `<archive SHA-256>.wfq`,
is reported as present, refused with `ErrCheckpointLegacy` when it is loaded,
and never read, renamed or removed. Future incompatible state/ABI changes must
bump the relevant checkpoint version; debug and release use the same schema. A
slot is outside what an upgrade promises to keep working: an incompatible one
is refused with its reason and left in place.

Portable audio output now retains the page synthesizer's bounded voice set,
note-on channel settings and emitted volume, current channels and remaining PCM
samples. PCM position uses an unscaled Host clock; detached staging time is
excluded. Reconstructed notes restart their envelopes. Physical oscillator phase,
release tails and network/device latency are not saved. Host reset stops all
old sources before replay and clears audio definitions and frame decoder state.

The worktree contains server and page checkpoint controls. Commands run between
whole rounds; successful load advances a connection epoch, discards old queued
frames/sound and input commands, resets compression, releases held input and
reconstructs output. Protocol callers can explicitly restore a disk slot at
startup. The page exposes save/load as optional keypad assignments with immediate,
nonmodal feedback; its picker starts games normally. The CLI's
`run` command uses the same session API for live commands and startup restoration;
both hosts pass subprocess restoration in both debug/release directions for
Java and native packages. A visible browser resumes a saved paused session
through the ordinary lifecycle; the CLI preserves pause until its `park`
command resumes it. Chromium checks cover live restoration and disk restoration
after server restart, including two local gameplay routes.

Native packages use a separate versioned record under the same checkpoint
envelope. It retains ARM memory, root registers, allocator ownership, built-in
interfaces, application identity, screen/image pixels, listeners, queued
events/resumes, frame/timer deadlines and logical audio. An open file is
recorded as its name, save key, position, whether it may be written and, for
one whose open emptied the file, the number of bytes it then held.
The record holds no file bytes: the table of names the session wrote, the marks
on names not yet stored and the parsed resource cache are not restored, and a
load opens each recorded file again, from the save store and then from the
package.
Native playback creates one-shot clips at nonnegative guest times. Restoration
rejects repeat flags and negative playback origins before the running session
is displaced, so a modified checkpoint cannot force the next tick to replay old
audio cycles.
Cached image bytes and decoded pixels remain distinct from guest bytes that
changed after decoding. Restoration installs fresh built-in bindings without
running the package's entry, factory or startup event. Custom Host bindings and
active tracing refuse capture. A native package's writes wait in the platform
until a file is closed or a frame ends, so both a capture and a load store what
is waiting first; adoption then detaches the old runtime from the store.
Elapsed time and deadlines rebase at adoption, excluding detached staging time.

A concurrent recovery defect discovered on 2026-10-01 interrupted implementation
of the replacement path: an independent reader could delete an active
replacement's staging and intent, leaving the original generation only in its
backup. Shared filesystem locking closed that interleaving, and the lock is
what every store operation still takes. The
[record of that repair](testing.md#checkpoint-adoption-blocker) covers aliases
and process exclusion. A load no longer replaces a save folder and the writer
is gone; recovery of what an earlier build left is tested from literal on-disk
states.
Earlier measurements and rejected approaches are preserved in the
[snapshot investigation](history/maintenance.md#snapshot-feasibility).

<a id="lgt-checkpoints"></a>

### LGT checkpoints

LGT has two execution variants under the same envelope: a Clet, which the
platform calls and which returns, and an AOT Java title, whose guest threads are
goroutines parked inside platform calls. Both are recorded between two ticks,
which is the one place where nothing of the guest's is in flight on the Host's
own goroutine. Capture runs no guest code and does not call `pauseClet`; a load
runs neither the module's entry, its initializer nor `startClet`.

**The guest clock needs no rebasing.** It is virtual: elapsed guest time plus
the work since an instruction baseline, and the instruction count travels with
the ARM state. Timer deadlines, thread wake-ups and sound origins are all
readings of that clock, so they are stored as they are. The only Host-clock
anchors are the pacing deadline, which a restored session takes again at its
first tick, the vibrator's deadline, and the remaining PCM and percussion tails
of the audio output; the last two are re-anchored when a load is committed, so
time spent validating a record is not time the motor or a sample ran.

The record holds the ARM memory image and the platform thread, and then every
table the client keeps on the Go side:

- the three allocators — platform data, surfaces and the title's heap — as a
  cursor, the spans taken back and every outstanding block. A restored
  allocator is accepted only if the blocks and the spans cover exactly what
  the cursor has handed out, so it hands out the same address next;
- the platform stubs and their cursor, the Clet entry points, the handle
  counter, resource identities and the list of resolved imports;
- every surface, with its transparency mask. A surface's pixels are stored
  only when the runtime's copy differs from guest memory at the same address:
  a Clet's surfaces are already in the memory image, and keeping them twice
  doubled the largest part of a record. The frame last presented is stored
  separately from the display surface, because a title draws its next frame
  into the surface before it presents it;
- timers, pending dial results, queued events, the input mode and a pending
  Host text commit;
- the answers each pixel operation has already given. Without them a restored
  title would be asked again, and the instructions charged again — which moves
  the guest clock and makes a restored session a different session;
- open files, as a handle, a path, a cursor, whether the handle may write and,
  for one whose open asked for an empty file, how many bytes it then held. The record
  holds no file bytes and neither of the two path lists a title's file calls
  keep: a load reads all of them from the store again;
- media clips, levels and mutes, the logical audio timeline and the vibrator;
- the C library's generator, and each `java/util/Random`. The sequence a seed
  names is the standard library's, whose generator cannot be asked for its
  state. It is an additive lagged sequence — each value is the sum of the
  values 607 and 273 places back — so its whole state is its last 607 values.
  The runtime takes the first 607 from the library, which is the only part a
  seed decides, and produces every later one itself; the record is those 607
  numbers and a position, and a restore is exact however long the title has
  been drawing.

An AOT Java title adds the tables behind its objects, which are guest memory
themselves: the class table with the link surface and layout the module was
answered with, string text (as bytes — a title may hold half a surrogate pair),
open streams, images and the decode cache, vectors, calendars, sinks, record
stores, widgets, Graphics state, monitors, and the collector's object table and
schedule. A resource stream names the archive entry it reads rather than
repeating it: a title that never closes its streams holds every resource it has
read. A stream opened on a `File` is recorded as the offset its window starts at
and how far it has read, and a `DataBase` as its name and record size; a load
fills both from the store. What a title has written into a stream and not
flushed travels in the record, because it has not reached a file yet.

**A parked guest thread is recorded as what it still owes.** The ARM core
supplies the registers of every call the thread is inside. For each call that
is stopped at a platform call, the record names the remainder of that call:

| remainder | where the thread is parked | what is left |
| --- | --- | --- |
| result | `Thread.sleep`, `Thread.yield` | the answer |
| wait | `Object.wait`, with or without a deadline | take the lock again at the depth it was given up from, then the answer |
| monitor | a contended monitor enter | take the lock, then the answer |
| initializer | a class initializer the platform entered for the thread | finish the nested call, then the answer |
| function | the C library's run-function call | finish the nested call, then the answer |

A thread whose slice ended on its instruction budget has no platform call above
its innermost frame and resumes at that instruction with a fresh budget. A
thread that has not been granted its first slice is recorded with no calls and
enters `run` from the top. Its open try regions, lock count and renewal count
travel with it. The remainder is derived from the same dispatch tables the call
was served from, at capture and again at restore, so a record cannot claim one
its platform call does not have.

**Anything else is refused by name.** A thread parked beneath a platform call
whose remainder lives in Go locals — a class declaration thunk, a stream read
through the title's own `InputStream`, a paint entered from the thread — is a
capture that fails with the call it was inside, and the session carries on.
Over the local library every thread at every tick boundary was in one of the
rows above; see [testing](testing.md#lgt-checkpoints).

A restored call releases no collector pins. The pin list is emptied at the end
of every round, capture refuses a boundary where it is not, and none of the
calls above builds an object before it parks.

Authentication adapters travel as their moved state only. Which exchange a
module is a client of is read from the module's code again, the same way it was
at session start, and a record claiming an adapter the module does not match is
refused. Validating one reads no save and writes none; what an adapter derived
from a save is derived again from the store when the load commits.

Restoration loads the archive afresh, requires the record's memory map to be
the one that load produces plus the guest thread stacks the record declares,
and then builds every table before a single goroutine is started. It is
prepared against a placeholder store that answers nothing, with no audio
device. Commit fills what the record has open from the live store, stores the
writes the running session had issued and not yet stored, and only then
displaces that session: its store and audio sink are detached and its guest
threads are ended one at a time. **It is not closed**, because closing calls
`destroyClet`, which is guest code that may write a save, and a load runs no
exit callback.

A record is checked against the platform it is restored into, not only against
itself, wherever the runtime would otherwise act on a number the record states.
A panic in a restored session is contained like any other guest fault; these
checks are for what a panic boundary does not cover — a loop that does not end,
or an allocation the Host is asked for.

- The guest clock is two numbers, and the first tick adds one to the other: the
  elapsed time, and the work the instructions retired since the baseline stand
  for. Both are bounded, the work to an hour, and the audio timeline is checked
  against their sum. A repeating sound is caught up cycle by cycle with no
  budget, so a record that put its instruction count years past its baseline
  would otherwise spend the first tick replaying it.
- A surface's pixels must be one block of the surface allocator, large enough
  for its size. The display is the one surface a record may carry outside that
  allocator, so each side of it is held to 1,024, which is the largest either
  Host starts a session at.
- An allocator states no more blocks than its region has room for and no more
  spans taken back than lie between them.
- Every object the collector tracks, and the block its fields live in, must be
  blocks the data allocator has outstanding, each named once: a collection
  reads every block whole, so this is what bounds one.
- A superclass chain must end, whether it is followed through the links the
  classes record or through the name a platform class's superclass is looked up
  under the first time a type check passes through it. The layout's chains,
  which are walked by name, are held to the same rule.
- A laid-out class may not state a size no class has, because a platform class
  first reached after a load is allocated from what its layout says.
- A thread that has ended holds no lock, and a thread parked for a lock is not
  the one holding it. Neither can happen in a session, and either leaves a lock
  nobody will release.
- A pending dial that a local service accepted is answered by that service when
  it comes due, so a record that has one must have the service.

Three behaviors of the running platform changed with this work, each because a
session and its checkpoint have to take the same path. Timers that come due in
one tick fire in the order they came due rather than in map order. A file
opened for writing gets its own copy of a packaged file's bytes instead of
writing through the archive's. And guest threads are ended one at a time and
waited for, at a close as well as at a load: waking them together had them
unwinding through one client's bookkeeping at once.

Limits: active cheat freezes and patches refuse capture, as on KTF. Diagnostic
counters — uncaught-callback counts, collection time, the platform-call trace —
restart at a load. A pending calendar field assignment is restored in the
Host's zone, which is the only zone the runtime sets. The subscriber identity a
title was started with is restored with it, so a title that compares the number
against one it stored sees the one it stored.

<a id="quick-load-and-ordinary-saves"></a>

### Quick load and ordinary saves

**The game's own saves come first.** A checkpoint is execution state: guest
memory, threads, the screen, sound and input. It holds no save. A quick load
brings the game back to the moment the quick save was taken and leaves every
save where it is; the restored game reads the saves as they are then, and what
it writes afterwards lands on top of them. Nothing is backed up, nothing is
kept aside, and no second path restores saves: the later write wins.

That is one rule for the five checkpoint variants — KTF Java/AOT, the older KTF
descriptor module, KTF native packages, LGT Clets and LGT AOT Java — and the CLI
and the server reach it through the same `internal/session` calls.

Two things stand between the rule and a runtime, and both steps deal with them
the same way on every variant.

**A write the game issued may still be in the host.** On LGT a write waits in
the buffer of its open file until the file is closed, and a native KTF package's
write waits in the platform until a file is closed or a frame ends. A checkpoint
holds no save bytes, so such a write would be in no place at all once the
session that made it is gone. A quick save and a quick load therefore store it
first, through a write whose failure is reported: when the store refuses, the
step is refused with `backend.ErrCheckpointSaveWrite`, the game carries on, and
the write is still pending in it. The descriptor KTF runtime stores every write
before the call that made it returns, so it has nothing to store. Nothing the
game has not issued is written: bytes a title put into a stream and did not
flush stay in the stream.

On LGT the buffer of an open file is the file as it was when the handle was
opened plus what was written through it, and storing it stores all of it. When
the file has changed since, through another handle or a removal, the host would
be putting the older part of the buffer over newer content, so the step is
refused instead and names the file. The title's own close does exactly that
write and is the title's to make.

**The host keeps copies of what a game has open.** An open file's buffer, the
records behind a database handle, the lists of removed and created names, the
table of names a session wrote. Each answers the game before the store is asked
and is written back whole. Restored from a record, a copy would answer with what
the file held when the quick save was taken and then be written over the file as
it is now. So a record names these objects and holds nothing of them, and
committing a load fills each from the live store through the lookup a first open
of that name makes: the removal list, the save key, then the packaged copy.

The order inside a load, on every variant:

1. The record is validated into a detached runtime over a placeholder store
   that answers every read as absent, refuses every write and counts what
   reaches it. The shared session logs a warning if validation asked it for
   anything; no variant does.
2. What can be refused without the saves is refused: the archive's identity,
   a running session that is not at a boundary, a cancelled request.
3. The restored runtime's storage is filled from the live store, with reads
   only. A save that cannot be read, a container that does not decode, or more
   bytes than the load's budget refuses the load with
   `backend.ErrCheckpointSaveRead`. A directory where a file was is not a
   failed read: a key that is a directory has no entry, so it is a save that
   is not there.
4. The running session's issued writes are stored. If that stored anything,
   step 3 is done again, so the restored game reads them like any other save.
5. The running session is cut off from the store and from the Host's outputs,
   its guest threads are ended without an exit callback, and the restored
   runtime is adopted over the live store. Nothing in this step can fail.

A load therefore writes no save of its own: with nothing pending it makes no
store write at all. A refused load leaves the running game running and reverts
or removes nothing. A load into a new process (`run -quickload`, a start with
`quick_load`) runs the same steps without a running session.

The rules a rebuilt object follows are the same in every table:

- A cursor stays where the record has it, even past the end of a file that is
  shorter now. A read there answers end of file, and a write fills the gap with
  zeros.
- A name the store has nothing for leaves its object empty and still open. The
  host creates nothing; the game's next write through the object creates the
  name through the ordinary write path.
- An object whose open asked for an empty file — a truncating open — takes at
  most the bytes it held when the quick save was taken, from the front of the
  file as it is now. The title is rewriting that file, and it never receives
  the tail of a newer, longer one. An object whose open made a file that was
  not there asked for no such thing and is given the whole file, like any
  other: a title that keeps its save open for a session is in that position on
  its first run, and what it saved through the object after the quick save must
  not be cut off by its next write. KTF's WIPI C file table has no object of
  the first kind: a truncating open there empties the file in the store at the
  open itself, and its handles are cursors on the file as it is.
- Every list the runtime keeps beside the files is read during the load, so a
  list that cannot be read refuses the load rather than the game's first call
  after it.
- What is read is bounded and each key is read once: the names come from a
  slot, which is untrusted input.

**Accepted limits.** These are how a game behaves once its memory is older than
its saves, and the emulator does not correct them:

- A game that keeps save data in its own memory shows what it remembered. A
  save list can look as it did at the quick save until the game reads the list
  again, usually when it is started again; the files are as they are.
- A game that then writes part of its save puts that part over the save as it
  is now, and the save can hold two points in time.
- A quick save taken while the game was in the middle of saving repeats the
  rest of that sequence on every load, over the save as it is then.
- On KTF, `FileSystem.list` and `DataBase.listDataBases` answer the packaged
  names plus the names this run knew, and do not read the disk.
- An authentication adapter's issued certificate travels in the slot, because
  it was made for the run and never read from a save.
- The LGT adapter that keeps a 100-byte file takes that file's original flag
  and certificate from the file as it is when its header validates, and from
  the record otherwise.

<a id="leftovers-from-earlier-builds"></a>

**Leftovers from earlier builds.** A build between 0.5.1 and this change wrote
quick saves that embedded the saves and replaced them at a load. It can have
left three things in `.wfeature-quicksave/owners/<owner>/`, and this build
writes, reads and removes none of them:

- `<archive SHA-256>.wfq`, a version 1 slot. It is reported as present so that
  the load is offered, and loading it is refused with the reason. A new quick
  save is written beside it under the version 2 name.
- `previous/`, the saves a version 1 load set aside. They can be the newest
  saves a person made before that load, and nothing knows whether they are
  newer than the saves in use, so no code restores or removes them.
- `next/`, the staging directory of a replacement that never recorded its
  intent.

The server writes one Info line per start that names the save folder when the
first or the second is there. To put a `previous` tree back by hand: stop the
server, move the contents of the owner's save folder somewhere else, copy the
contents of `previous/` into the owner's save folder, and start the server
again. Any of the three can be removed by hand once the game has been started
once under this build: every store operation first settles a replacement an
earlier build left half done, and after that the saves in use are never inside
the reserved directory.

## Documentation

[The documentation index](README.md) lists maintained references and subject
histories. Use the platform overviews for current behavior and the histories
for detailed traces, layouts, and dated measurements.

<a id="shared-runtime-services"></a>
<a id="services-shared-runtime-service-contracts"></a>

## Shared runtime service contracts

The existing backend boundaries share services across the platforms. These
contracts describe caller responsibilities without adding a runtime layer.

<a id="services-audio-time"></a>

### Audio time

Each `backend.Audio` instance has one guest-time domain. `Play` and `Advance`
receive durations with the same origin and rate. SMAF event times are millisecond
offsets from `Play`'s timestamp; they are not wall-clock timestamps. Host speed
changes affect the progression of guest time. Audio does not apply speed again.
A new session clock needs a new timeline; a backwards timestamp does not rewind
already emitted events. Paused guest time must not advance just because the
browser disconnects or wall time passes.

KTF uses `guestElapsed()` for both playback and advancement, LGT uses
`client.clock.now()`, and SKT uses `GuestElapsed()` through `audioNow()` and
`AdvanceAudio()`. The script runtime uses its own `clock` for both operations.
Keeping those pairs together prevents a sound from being scheduled in a different
clock domain and remaining indefinitely in the future.

<a id="services-data-ownership-and-callbacks"></a>

### Data ownership and callbacks

`Framebuffer.Present` borrows `Frame.RGBA` for the call. A framebuffer retaining
pixels must copy them before returning. A consumer must not infer ownership from
whether a particular framebuffer currently copies.

Audio sink PCM and SysEx slices are also borrowed and read-only during the call.
A sink retaining or asynchronously transmitting them must copy them. This rule
also applies at reduced volume: the implementation may allocate scaled samples,
but allocation is not an ownership transfer.

`Audio.LoadEvents` copies the outer event slice. Nested sample and SysEx slices
remain shared with the caller and must remain immutable for the loaded handle's
lifetime. Events must already be ordered by nonnegative millisecond offsets.
Closing the handle releases the timeline's reference. These are explicit caller
preconditions, not validations added by this documentation change.

Audio serializes sink callbacks under its own mutex. Stop, close, restart and
volume changes can emit callbacks as well as `Advance`. A sink must not reenter
the same Audio instance, because its mutex remains held. Serialized callbacks do
not imply that every caller is the Host's frame-loop goroutine.

### Owned presentation frames

`FrameUpdate` is an ownership transfer, unlike borrowed `Frame` pixels. A Host
may retain its RGBA bytes. `FrameSink.Offer` is nonblocking; the Host must detach
the producer or stop the runtime before closing the channel. KTF sampling and
the browser encoder use separate retained pixel storage. Ordinary server frames
use `Session.FrameUpdate` with the same ownership and scale contract. `Force`
requests an explicit redraw across lifecycle or display-setting boundaries;
otherwise the encoder can omit an identical consecutive picture. See
[session presentation](session.md#presentation-and-audio).
