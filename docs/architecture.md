# Architecture

`wfeature` keeps the original Host / Runtime / Execution layering and draws its
boundaries as Go packages. The Execution packages import no other layer, the
Runtime imports only the SMAF decoder, and the platform packages compose the
two. The server and the CLI's `run` command reach a platform through
`internal/session`; the per-platform diagnostic commands (`runktf`, `runlgt`,
`runskt`) call the platform packages directly. No entry point carries
platform-specific logic of its own.

## Layers

### Host

A Host owns the browser canvas, audio and save APIs, or the CLI's input and
output. It supplies the framebuffer dimensions and receives complete RGBA8888
frames through the `internal/backend` presentation boundary; the core refers to
no concrete browser or operating-system API. A release is one server binary per
operating system (`make dist` builds five), used as a browser PWA, and the phone
apps wrap the same server. There is no desktop shell.

| Package | Role |
| --- | --- |
| `cmd/server` | Serves the PWA, the game archives, the save and debug-log APIs and the emulation sessions; its `status` and `stop` subcommands manage a running server. See [running](running.md). |
| `cmd/cli` | Drives a game from a terminal for development and diagnostics. See [CLI](cli.md). |
| `internal/webhost` | The server's HTTP routes and session protocol, testable without a process. See [sessions](session.md). |
| `internal/session` | The platform-agnostic session driver: inspect, start, tick, input, frames and checkpoints, so no Host switches on platform. |
| `internal/wsproto` | WebSocket framing ([below](#session-transport)). |
| `internal/vp8l` | Lossless WebP encoder for protocol 2 pictures. |
| `internal/filter/hqx` | hq2x, hq3x and hq4x magnification for protocol 1, `Session.Frame` and the CLI. See [hqx](hqx.md). |
| `internal/gameroot` | The depth at which a game library is discovered ([below](#archive-detection)). |
| `internal/launcher` | Answers whether a server is running and stops it through `/api/status` and `/api/shutdown`; a port held by another process is reported and left alone. |
| `internal/appserver` | Runs the server inside another program on loopback and returns its port, without owning a signal handler, a logger or the exit. |
| `internal/licenses` | The project licence and third-party notices, embedded in every binary. |
| `web` | The browser client, embedded in the server binary. Every sentence a player reads is in `web/text.properties`; `internal/webhost` fills the page from it and words its own answers with it ([web host](../web/README.md#text)). |
| `mobile/android`, `mobile/ios` | The phone apps: a web view in front of the same server. Android runs the server binary as a child process. iOS does not let an app start a process, so `mobile/ios/lib` compiles `internal/appserver` into a `c-archive` that the app links and calls. See [mobile](mobile.md). |

### Runtime

`internal/backend` provides events, time, the virtual file system and save
store, task execution, audio timelines, frame presentation, the checkpoint
envelope and logging policy. Debug and release are build profiles of the same
code, not separate runtime environments ([build profiles](#build-profiles)).
Services that every platform uses below its call boundary sit beside it, so
each is written once:

| Package | Role |
| --- | --- |
| `internal/backend` | The Runtime boundary above, and the [shared service contracts](#shared-runtime-services). |
| `internal/audio/smaf` | Parses SMAF (`.mmf`) and translates it into timed MIDI and PCM events. See [audio](audio.md). |
| `internal/keypad` | Turns Host key holds into a handset's single direction pad and runs the handset's key-repeat clock. |
| `internal/textinput` | Keypad multi-tap text entry for MIDP `TextBox` and `TextField` and KTF lwc text components. |
| `internal/wipic` | What the platforms share below the call boundary: format strings, handset bitmaps and tolerant picture decoders. |
| `internal/curve` | Arcs and rounded rectangles for every drawing surface. |
| `internal/glyph` | Runtime-authored 5x7 glyphs; an unsupported character becomes a codepoint-marked box. |
| `fonts` | The runtime's embedded font assets. |
| `internal/cheat` | Memory search and patching over a guest's memory: the ARM address space on KTF and LGT, a synthetic address space over the JVM heap on SKT. The Hosts expose it as the cheat panel and the CLI's `-cheat` and `-patch`. |

### Execution

| Package | Role |
| --- | --- |
| `internal/armcore` | Pure-Go ARMv4T (ARM and Thumb) interpreter for KTF and LGT. Shared memory belongs to the core, and each cooperative guest thread owns its register context. Instruction quanta are serialized and SVC handlers run outside that lock, so Host waits do not block other guest threads. See [ARM](armcore.md). |
| `internal/armcore/conformance` | The corpus every armcore backend has to answer identically, with expectations derived from the architecture. |
| `internal/jvm`, `internal/jvm/classfile` | Java class-file parsing and bytecode execution: SKT MIDP, SKVM and WIPI titles, and the Go objects behind KTF's AOT Java. See [JVM](jvm.md). |
| `internal/sgsvm` | The SGS script format of SKT GNEX/GVM titles. See [SGS](sgs.md). |

### Platform composition

`internal/platform/*` detects input formats and composes the Execution layer,
the Runtime and an API surface for each carrier.

| Package | Role |
| --- | --- |
| `internal/platform/detect` | Names the platform an archive belongs to ([below](#archive-detection)). |
| `internal/platform/ktf` | KTF: WIPI Java/AOT titles and older descriptor modules on the ARM core with Go JVM objects, and the earlier native package. See [KTF](ktf.md). |
| `internal/platform/lgt` | LGT: Clets and AOT Java titles on the ARM core. See [LGT](lgt.md). |
| `internal/platform/skt` | SKT: SKVM/MIDP and WIPI Java on the JVM, and GNEX/GVM scripts on `internal/sgsvm`. See [SKT Java](skvm.md). |
| `internal/platform/compatibility` | The embedded registry of code-revision exceptions. See [code-revision compatibility](platform-compatibility.md). |
| `internal/api/midp`, `internal/api/skvm`, `internal/api/wipi` | Java class libraries declared in Go and installed on a VM by `Define`: MIDP, SKT's extensions (`com.skt.m`, `com.skt.m3d`, `com.xce`) and WIPI Java (`org.kwis.msp`, `org.kwis.msf`). See [JVM](jvm.md#the-class-library-is-declared-not-compiled). |
| `internal/zipentry` | Entry-name conventions the loaders share ([below](#archive-entry-names)). |

### Tools and diagnostics

These packages are not layers: they drive or inspect a session from outside, or
support tests.

| Package | Role |
| --- | --- |
| `cmd/ktfdump` | Writes a KTF game's image after relocation, with an address-to-method map, for a disassembler. See [CLI](cli.md#ktfdump). |
| `internal/route` | Reads and replays a scripted way back to a scene; KTF, LGT and SKT take `-route`. What it needs of a session (advance a tick, fingerprint the screen, send a key, report whether a stalled guest can resume) arrives as functions and a key table, because the platform sessions spell those differently. See [CLI](cli.md#routes). |
| `internal/serve` | Drives a running game one JSON command per line (`-serve`); a route replays, a serve session explores. See [CLI](cli.md#driving-a-run-from-outside). |
| `internal/gdbstub` | The GDB remote serial protocol over the ARM core (`runktf -gdb`). |
| `internal/guestprofile` | Ranks the ARM core's address samples by guest symbol (`-profile`). See [CLI](cli.md#guest-profiling). |
| `internal/ladder` | The settled-screen and key-response judgment the local acceptance rungs share. |
| `internal/storageinventory` | The arithmetic the opt-in storage inventory probes share. |
| `internal/testfixture` | Newly authored archives for tests; no game or original-runtime bytes. |
| `internal/tools/acceptance` | Runs the local archive probes for all three platforms and records the answers (`make acceptance`). See [testing](testing.md#local-acceptance). |
| `internal/tools/apiscan` | Reports, per archive, the platform surface a title links against that the runtime does not answer, without playing it. |
| `internal/tools/distcheck` | Checks that the archives `make dist` wrote are what the repository meant to publish (`make dist-check`). |
| `internal/tools/javastub` | Writes the runtime's class surface as Java source stubs, so a fixture compiles with `javac`. |

### Archive entry names

`internal/zipentry` holds the conventions every platform archive's entry names
follow, so the loaders do not answer them separately. It currently holds one
rule: **a directory that every entry shares is removed.** A copy that was
unpacked and zipped again often gains the game's name as a containing folder,
which leaves `__adf__` or `app_info` one level below where the loader looks it
up by exact name. Detection then claims the archive for nobody and a loader
reports a missing marker; neither says "this is packed differently".

The rule is applied without asking because it cannot damage an archive that
works: when every entry is inside one directory the marker is not at the root,
and no loader reads such an archive. A JAR is unaffected without being excluded,
because `META-INF/` always sits beside the rest, and the loaders apply the rule
only to the outer archive. Only one level comes off, so an archive nested twice
is reported rather than searched, and a rename that would collide with another
entry abandons the unwrap. On 2026-08-16 no local archive was wrapped (all 64
carried their marker at the root), so the rule is written against a described
shape, which the tests build.

The SKT loader does not use the rule because it does not need it: it finds its
descriptor by extension wherever it sits, derives the JAR name from the
descriptor, and files installed files by base name. That is the shape the KTF
and LGT loaders would need to stop depending on the rule.

### Archive detection

`detect.Archive` answers a platform or `unknown`. Detection reads entry names
from the central directory after the shared directory is removed and matches
case-insensitively. The three named markers have no priority among themselves:
entries are visited in central-directory order, and the first entry that wears
any of them decides. Only when no entry does are the shapes below it asked, in
table order:

| Marker | Platform |
| --- | --- |
| an entry whose last path element is `__adf__` | KTF |
| an entry whose last path element is `app_info` | LGT |
| an entry with the extension `.msd` | SKT |
| a JAR whose class constant pools reference `com/skt/m/`, `com/skt/m3d/` or `com/xce/` | SKT |
| exactly one `.mif` beside exactly one `.mod` (the earlier KTF package) | KTF |
| an `.sgs` payload beside a descriptor naming the GNEX or GVM runtime | SKT |

The constant-pool scan decompresses class files, so it is bounded (4 MiB per
entry, 64 MiB in total).

**"Nothing claimed this file" has several causes, and a Host should say which.**
`detect.Classify` answers the same platform with a reason beside it:
`not-an-archive`, `known-format-unsupported`, `drm-wrapped`,
`archive-of-archives` or `no-marker`. Only `no-marker` can be work for this
project: a package whose shape is not yet recognized. The reason is a second
return value rather than a second set of platform values, so a caller that loads
a game keeps asking `detect.Archive` and only a caller that counts asks for the
reason. The acceptance records carry it per file; see [testing](testing.md).

**A zip whose entries are all zips** is refused by its shape rather than by the
descriptor it has no room for: each inner archive is a whole game, so the Host
names the shape and says to unpack it. `detect.ArchiveOfArchives` is the test
and `session.ErrArchiveOfArchives` the answer; the page turns it into the
sentence a person reads.

**`drm-wrapped` is read from a container header and nothing else.**
`detect.DCFHeader` recognizes both layouts a locked package uses: the media type
and content identifier declared in the clear at the front, and a box structure
whose brand names the wrapper, in a brand box or at offset zero. Nothing is
decrypted and no key is read; the key belongs to the network that issued it. A
person holding a locked file is told to find another copy, and a person holding
a damaged zip is told to download it again.

The wrapper is one level in: a locked package keeps its outer archive,
descriptor, icons and data directory, and only the payload JAR beside them is an
encrypted container. Its marker still names a platform, so after a platform
claims an archive `detect.Classify` reads the front of its `.jar` entries (at
most four, 1 KiB each) and answers `drm-wrapped` instead. All three vendors
package a descriptor beside a JAR, so one rule covers them; a `.jar` in a zip
nobody claimed is not a declared payload and is not asked. A box declares its
size against the file it is in, so the walk is measured against the length the
zip directory gives and the encrypted content is never inflated. Reading the
header still costs a deflate reader's start-up, because the reader fills its
window before returning a first byte: measured on 2026-09-06 at 182µs a file
against 4µs, small beside anything done with an archive after classifying it. On
that date the corpus held exactly one such file, in the older KTF collection.

`internal/gameroot` names the depth at which a game library is discovered: the
root and one platform group below it. The picker, `checkgames` and the save-owner
collision checks read through it, so they agree on which files are in the
library; a deeper archive is not one a Host offers, which is what an ignored
diagnostic corpus filed under the root is made of. See [CLI](cli.md).

<a id="phase-1-startup-path"></a>

## Java startup path

The implemented MIDP path is:

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
raster operations, font metrics and deterministic text rendering share this path
in both Hosts, and Canvas game-action mapping, full-screen state, pointer
callbacks and touch controls use the same event boundary. The JVM's scope and
deliberate limitations are in [`jvm.md`](jvm.md).

<a id="phase-2-execution-boundary"></a>

## Native execution boundary

The KTF execution path is:

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

Repository-authored fixtures exercise every step, and opt-in probes over real
archives kept out of the repository relocate, initialize, resolve the ADF main
class through the real `GetClass` export, construct and start it, and present a
first frame. Startup callbacks register bounded AOT metadata and bind native
strings, classes and newly allocated AOT objects and arrays to Go JVM objects.
Guest exceptions are allocated in both representations, uncaught objects keep
their guest addresses across typed JVM errors, and handler heads are per logical
ARM thread and inherited by nested calls. Java methods the runtime owns are
emitted as validated raw KTF metadata with both the register-argument `fn_body`
and the native argument-container entry forms.

LGT Clets and AOT Java titles, and the earlier KTF native package, run on the
same ARM core through their own loaders and platform-call tables; see
[`lgt.md`](lgt.md) and [`ktf.md`](ktf.md).

Titles run rather than only start: input, timers, guest threads, archive
resources, sound and persistence are on the path a played game takes. This
project publishes no per-title answer; the kept claim is what a sweep of a whole
local set did on a dated run. The probes and their counts are in
[`testing.md`](testing.md); the instruction subset and the KTF format's
boundaries are in [`armcore.md`](armcore.md) and [`ktf.md`](ktf.md).

## What an archive may declare

Every platform's loader bounds the same four things about a zip, the same way:

| | the input | how many entries | one entry | everything expanded |
|---|---|---|---|---|
| KTF outer zip and inner JAR | 128 MB | 8192 | 128 MB | 512 MB |
| LGT zip | 128 MB | 8192 | 64 MB | 512 MB |
| SKT container and inner JAR | 128 MB | 8192 | 128 MB | 512 MB |

The per-entry bound alone bounds one file, and a zip that declares eight
thousand of them is eight thousand times that. The count is checked before
anything is read, because it is already in the directory the reader parsed. The
total counts bytes actually read rather than declared sizes, since a zip may lie
about either. The SKT container reads its entries in three passes (the
descriptor, the JAR, then the title's installed files), so its total is carried
across them in a `budget`. Before 2026-09-02 the SKT container bounded only one
entry, and its JAR one entry and the total.

Each loader takes its limits as a value, so a test can reach the far side of
every bound without building half a gigabyte. On 2026-09-02 the largest local
archive was under 4 MB, so these bounds limit damage and constrain no real
title.

**A Host reads the file into memory before a loader sees it.** The bound is
therefore a bound on what the loader parses, not on what the process allocates,
and the input it protects is a file the user put in their own game root. Bounding
allocation as well would need every Host entry point to measure before reading.

## Build profiles

- debug: `-tags debug`; includes detailed logs and browser diagnostic hooks.
- release: the default build tags; retains only normal logs and applies
  `-trimpath -ldflags="-s -w"`.

Each profile builds into its own directory, `build/debug` and `build/release`,
so the two coexist and neither build replaces the other. The server is built per
profile like every other binary; no flag selects the other profile's save tree,
because a process serving it would disagree with the binary that is running.

Both profiles read the same `var/games` and `var/ext`, use the same save and
checkpoint formats and speak the same save API, but each owns its saves under
`var/savedata/<profile>/<platform>/<owner>/`. Playing a debug build must not move
a release build's progress, and a debug session is where a half-finished API is
most likely to write a save the game cannot read back. Both binaries resolve the
path from their own build tag, so the CLI and the server of one profile share a
tree. To move saves between profiles or keep them elsewhere,
`WFEATURE_SAVE_ROOT` (the server's `-saves`) names the KTF tree outright; the
other platforms' trees are its siblings. See [running](running.md).

## Debug run logs

Debug builds collect a run report; release builds transmit no diagnostics.

- Core: the platform layer takes `TraceLimit` and `Logger` and has no
  build-profile knowledge. Hosts decide: they pass `ktf.DefaultTraceLimit`
  (16,384 events) through `session.Options.TraceLimit` in debug and zero in
  release, and a debug server also enables the guest profile.
  `Session.Diagnostics()` returns the counted boundary events and the ordered
  trace of the most recent ones.
- Host (server): the session composes the report from the same counts and trace
  and writes it under the ignored `var/logs/`, because the server holds the
  numbers.
- Host (web): `web/debug-log.js` retains the page's console output and uncaught
  errors. The debug-log settings button asks the server for its report and
  posts the page's log to `POST /api/debug-log`, so both halves of a run land
  side by side.
- Host (CLI): `runktf <zip> -diag report.json` writes the same counts and trace
  next to the run summary.

A release collects nothing on either side of the socket. The page starts
collecting at load, because the useful lines are those from before anything
knows the profile (a module that failed to import, a socket that never opened).
The profile arrives with `ready`, and a release page then calls
`stopLogCapture()`: the console methods are restored, the buffer is dropped, and
nothing further is retained or posted. A release server answers
`POST /api/debug-log` with 404. Neither half depends on the other being right.

A debug server bounds its reports, because the route has no authentication and
the server binds every interface so that a phone can reach it: one report is at
most `maxDebugReport` (2 MiB), at most `debugLogBurst` (10) may arrive in a
rolling `debugLogWindow` (one minute), and every write prunes the directory —
reports older than `debugLogLife` (14 days) first, then the oldest while the
directory exceeds `debugLogBudget` (64 MiB), never the newest and never a file
this server did not name. Session reports go through the same writer, so
`var/logs/` stays bounded whichever side wrote last.

## Session transport

The page once ran the emulator itself, compiled to WebAssembly. A phone could
not run it fast enough to play: the same build and game measured about fifteen
times slower than on a desktop browser, and the cost was Go's WebAssembly backend
rather than the emulator (see
[the session history](history/session.md#implementation-server-sessions)). The
Host layer therefore has one browser shape: the emulator runs natively on a
server that is left running, and the phone is a thin client that draws the
frames it is sent and posts input back. The protocol is
[`session.md`](session.md).

`internal/wsproto` implements the WebSocket framing:

- It is the whole dependency: the handshake, masked and unmasked data frames,
  and the close and ping control frames. The project already writes its own ELF
  loader, gdb stub and SMAF decoder rather than take a dependency for each; the
  module requires only `golang.org/x/text`, `x/image` and `x/sys`.
- The codec works on any `io.ReadWriter`, and both ends exist in Go, so a test
  drives a real client against a real server over an in-memory transport
  without a browser or a port. Only the handshake (`Upgrader`/`Accept`, and the
  test client `wsproto.Dial`) knows about `net/http`.
- Every frame is untrusted input. Lengths are refused against a bound before
  anything is allocated (a ten-byte header may claim four gigabytes), reserved
  bits and malformed fragmentation are rejected rather than ignored, and a
  server refuses the unmasked client frames RFC 6455 forbids.
- A frame's header and payload reach the transport in one write, and
  `WriteBatch` sends several messages in one. A socket that does not wait to
  fill a packet sends each write as its own packet, and behind a TLS proxy as its
  own record, so a message written in two parts paid for two sets of headers.
  See [writes and statistics](session.md#writes-and-statistics).
- A WebSocket handshake is not subject to CORS, so `Upgrader` by default checks
  `Origin` against the host the request arrived on; otherwise any page the user
  visits could drive a session on their machine. A deployment reached by LAN IP
  while the page was loaded from a hostname replaces the policy with
  `Upgrader.CheckOrigin`. A reverse proxy that does not pass the browser's
  `Host` through leaves every session refused;
  [`running.md`](running.md#behind-a-reverse-proxy-on-a-unix-socket) has the
  configuration that works.
- The HTTP routes follow the same rule: a request other than GET or HEAD whose
  `Origin` names another host is refused with 403, so a page on another site
  cannot submit to them. A request with no `Origin` (the `stop` command,
  `curl`) is unaffected.

## Where a stopped game keeps its state

A parked session retains live objects and suspended Go call stacks in server
memory. It survives a disconnected browser, but not server shutdown. Guest save
files and `.wfs` exports do not capture that execution state. A server that
stops closes parked and attached sessions alike before it exits, so the writes a
title had issued reach its save files; see
[retention and control](session.md#retention-and-control).

Quick save and quick load carry execution state across a restart. KTF, LGT and
SKT share one envelope, one slot, one set of Host commands and one
`internal/session` API; what differs per platform is the runtime record inside
the envelope and how a parked guest thread is described. A checkpoint is
execution state only: it holds no ordinary save and a load replaces none, which
[Quick load and ordinary saves](#quick-load-and-ordinary-saves) states in full.
The KTF records are [below](#ktf-checkpoint-records), followed by
[the LGT ones](#lgt-checkpoints); [SKT checkpoints](skt-checkpoints.md) describe
JVM worker continuations and SGS event-boundary restoration through the same
Host path. The [validation record](testing.md#quick-save-and-checkpoints)
describes authored and local-archive coverage.

### Envelope and slot

`internal/backend` writes the envelope little-endian, identically in debug and
release:

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 8 | `WFSTATE\0` |
| 8 | 2 | Version, 3 |
| 10 | 2 | Execution variant: 1 KTF Java, 2 KTF module, 3 KTF native, 4 LGT Clet, 5 LGT Java, 6 SKT Java, 7 SKT script |
| 12 | 32 | SHA-256 of the whole archive |
| 44 | 4 | Session section length, at most 64 KiB |
| 48 | 4 | Runtime section length, at most 128 MiB |
| 52 | 4 | Reserved, zero; version 1 kept the length of an embedded save here |
| 56 | 32 | SHA-256 of bytes 0–55 and both sections |
| 88 | — | Session section, then runtime section |

The version is read before the size is bounded, so an envelope of an earlier
format is refused as unsupported whatever its size. The checksum detects damage;
it is not authentication, and every inner record remains untrusted. Before typed
JSON allocation, a schema-aware pass bounds depth, value count and estimated
allocation, and rejects missing, duplicate, unknown or wrongly typed fields. A
pointer field is the one a record may leave out: absent reads as null, which
is how a record adds a field and still reads the records written before it.
Byte payloads keep their raw bytes.

A directory store keeps one Host checkpoint slot per exact archive at
`.wfeature-quicksave/owners/<owner>/<archive SHA-256>.v3.wfq`, beside the guest
owner directory. Reads are bounded and confined to the reserved directory, and
links and nonregular files are refused. Writes validate the envelope and use the
ordinary synced temporary-file replacement. Slots stay outside ordinary save
exports. The file name carries the envelope version, so no build writes over
another format's slot; the earlier names are listed under
[leftovers](#leftovers-from-earlier-builds). Version 3 keeps version 2's layout;
it exists because the audio, JVM heap and media-listener records inside it
changed, and without the bump a 0.5.2 slot would be read as current and refused
as damaged. A future incompatible state or ABI change must bump the version the
same way. A slot is outside what an upgrade promises to keep working: an
incompatible one is refused with its reason and left in place.

### Session API and Host controls

`internal/session` captures, loads into an existing session, or restores from
archive and checkpoint bytes after a process restart. Its shared record holds
pause state, the key-repeat phase and its clock anchor, the pad, every held Host
key (at most 64) and a held pointer, and is captured with the platform clock
before any heap copy or file I/O. Restoring preserves input ownership;
`ReleaseHeldInput` is a separate Host action taken after the output and input
epochs reset, and it can run guest callbacks, which capture and detached
preparation never do. Shared settings and input records validate before the
commit.

These operations follow the single-owner discipline of `Tick` and `SendKey`: the
caller serializes whole rounds, input, lifecycle, cheats, clock changes and
external save operations. A busy client is refused immediately, and capture runs
no pause callback (`pauseApp`, `pauseClet`) and queues no delayed request. Active
cheat freezes and patches refuse capture on every platform.

The server, the page and the CLI drive this API between whole rounds; the
protocol, the page's controls and the connection epoch are in
[session checkpoints](session.md#ktf-checkpoints). The CLI's `run` command uses
it for live commands and startup restoration, and subprocess restoration passes
in both directions between debug and release for Java and native packages. A
visible browser resumes a saved paused session through the ordinary lifecycle;
the CLI preserves pause until its `park` command resumes it. Chromium checks
cover live restoration and disk restoration after a server restart, including
two local gameplay routes.

### Save locks and claims

Complete reads, writes and recovery serialize through a kernel file lock in the
reserved directory beside the owner's folder,
`.wfeature-quicksave/owners/<owner>/`, across store objects and processes. The
reserved directory keeps the live owner's filename, so case and Unicode aliases
share a lock wherever the filesystem aliases those names. A separate
nonblocking Host claim (`backend.ClaimSaveDirectory`) excludes competing sessions
and imports, allows read-only backup, and is kept while a session is parked. File
data is synced before it is renamed into place; directory syncing keeps the
store's advisory OS and filesystem guarantees. This is not a claim of verified
power-loss durability on every target.

Both the transaction lock and the claim are taken twice: in a registry of this
process keyed by the resolved owner root, and as a kernel lock on a file in the
reserved sibling directory. A symbolic link shares its target's identity even
before the first save creates that directory: a linked owner takes the target's
kernel lock first and also keeps the lock beside its original spelling for
earlier processes. Aliases of one lock file are locked once. Slots and recovery
journals keep their original paths beside the link.

When no location can hold a kernel lock (its reserved directory or lock file
cannot be created, or its filesystem has no locks), only the registry remains.
That is at least the exclusion of releases before the lock, which was one mutex
per store object, but another process is no longer kept out, and each Host logs
that once per directory. When only one of a linked owner's locations is
unavailable, every usable kernel lock is held for the whole operation or claim;
the warning still reports incomplete exclusion, and a failure at the legacy
location cannot release a usable target lock. Permission errors keep their OS
classification. A lock somebody holds is contention and never falls back, and a
reserved path that is a link or not a directory is refused.

A per-game owner directory may itself be a symbolic link. Ordinary reads,
writes, batches, listing, export, import and the claim go through it. So do
quick save and quick load: a slot is a file in the reserved directory beside the
link, named after it, and a step reaches the saves only through ordinary reads
and writes. Settling a replacement an earlier build left half done still refuses
a linked root, because it renames the directory itself, and so do generation
snapshots.

### KTF checkpoint records

The ARM core records parked derived calls and resumes them in a fresh core; the
platform supplies each pending supervisor operation's remainder, and unknown
remainders are refused. Capture requires the caller to park the whole logical
thread first. The KTF component handles Java jumps, native return cleanup and
the sleep and wait adapters. Older module invocation records keep both return
registers and drop the helper's two spilled arguments only after a successful
child return; their direct waits complete without repeating the sleep, and
exceptions keep the module's handler layout and unwind boundary.

- **ARM state.** Separate records keep sparse memory, mapping permissions and
  each logical thread's private words. Their detached constructor checks sizes,
  ranges and the Host's execution policy before adoption. Translation caches and
  Host callbacks are rebuilt.
- **JVM execution.** The AOT worker's owner and spent bytecode budget are kept,
  including the core library's forwarding `Thread.run` remainder. Active
  bytecode frames, class initializers, JVM-owned goroutines and unknown native
  remainders are refused. Workers resume through the ordinary completion path.
- **JVM heap** (version 2). Explicit roots, object sharing, core native
  payloads, statics, AOT metadata and bindings, cooperative Thread records and
  the bounded interned-string pool, so restored literals, native constants and
  `String.intern()` keep reference identity. Version 1 records are refused.
  KTF restores its allocator and array views over saved ARM memory without
  copying elements through their adapters. Unknown payloads and held monitors
  are refused. `Memory.ValidateRange` checks an adapter's mapped range and
  permissions without reading, allocating or changing its bytes.
- **Graphics and images.** Graphics records keep drawing state and target memory
  view. Image records keep bounds and both premultiplied RGBA and unassociated
  NRGBA colors, including RGB hidden by zero alpha. Images and guest
  framebuffers keep shared mutable transparency masks.
- **Platform objects.** Platform object roots, queued callbacks, key ownership,
  events and pending timer deadlines. Weak image and clip owners stay weak; an
  owner Go has already collected needs ordinary resource cleanup before capture.
- **Text editors.** Caret, character mode, UTF-16 limit and the pending
  multi-tap cycle. The relative key time and timer deadlines rebase to the
  destination clock. Hosts serialize input with capture and adoption.
- **Storage.** A Java database handle is recorded as the name it was opened
  under; a `File` as its name, position, open mode and, when its open asked for
  an empty file, the number of bytes it then held. The storage component records
  the names in the C file and record-database catalogs, the open handles with
  their cursors, the handle counters, each record database's record size, and
  the names of the files the session wrote. No file or record bytes and none of
  the deletion and directory lists are recorded: those are the saves, and a load
  reads them from the store. Handles of one name share one store after a load.
- **Local relay.** Service phase, identity, slot and label, the incomplete
  outgoing frame, unread response bytes, individual close flags and shared
  stream objects. A temporary heap root carries a socket only the runtime holds;
  it gets no guest identity or ARM allocation, and adoption keeps only its native
  payload. Stream class and native ownership are checked before heap adoption.
  This covers relay state; a parked read still needs a supported call
  remainder.
- **Control.** The guest clock's original epoch and exact relative anchor
  (keeping the unscaled offset preserves fractional time across a change of Host
  epoch), painting and event ownership, the C input mode, controller, revision
  and queued character, pending network failure callbacks, and C media clips with
  their eviction order. An active Host input guard, paint callback or
  result-binding operation prevents capture until its remainder has completed.
- **Media and audio.** C media records keep encoded bytes and handles; active
  playback uses the Host output restoration below. A separate backend audio
  record keeps loaded event sequences, playback cursors and repeat origins,
  device volume, active-note ownership and used channels. Its bounded
  constructor emits no past events and copies PCM and SysEx buffers. The session
  must validate the saved guest clock against this timeline before advancing it;
  the record does not represent synthesizer voices or physical output queues.
- **Vibrator.** A separate validated record keeps timed, expired and indefinite
  requests against the motor's Host clock, without issuing a new request or
  applying guest speed. Adoption must reset the Host's output epoch so that a
  restored, older request counter is observed.
- **Runtime metadata.** Native dispatch IDs, code and class arena cursors, class
  links, aliases and initialization flags, collector allocations and release
  state, WIPI C allocation ownership, user memory pools, interface and context
  addresses, and pixel-operation results. Keeping the pixel cache avoids
  repeating guest calls and charging their instructions again. Released address
  sets and their eviction order are separate, because reuse can leave duplicate
  entries in the order. Decoding validates bounds, mapped permissions, duplicate
  records and allocation overlap before adopting any metadata. Debug arena
  checks restart from the restored bytes; historical diagnostic counters are
  Host state.
- **Client.** The client record joins these components with every worker in
  queue order, including workers waiting for their first grant, private stacks
  and TLS, Timer ownership, painted cards and the LIFO stack free list. It keeps
  the client thread, pacing averages and deadlines, speed, the executable
  descriptor, and the last displayed LCD independently of unflushed back-buffer
  pixels. Heap, editor and scheduler offsets share one capture instant. Logical
  audio and vibration records accompany the guest clock, and audio catch-up is
  bounded before execution. Every worker record validates before any restored
  goroutine starts, and an invalid detached client is discarded without touching
  the source session.

The client record stays separate from external saves and Host output. Authored
tests replace the source process and check nested returns, exceptions, wait
deadlines, multiple parked workers, initial grants, stack reuse, and recapture
before and after a restored grant. The destination registers native
implementations on a fresh VM without preparing guest interface tables or
recreating the fixture's ARM code and objects. Startup and restoration share the
archive and Host attachment code. The KTF session API joins the client and
authentication records under the shared envelope; per-run authentication save
adapters travel as what they made for the run and never had on disk: an issued
certificate, a removal made in this run, the subscriber recovery inputs.

**Detached preparation and commit.** Preparation reconstructs against a
placeholder store that answers every read as absent, refuses every write and
counts what reaches it, with silent outputs. Commit hands the restored runtime
the live store, fills what it has open from that store with reads only, and then
cuts the displaced client off: its saves go to an empty memory store of its own,
its outputs are detached, and its workers are aborted without `destroyApp`. A
failed commit leaves the original session usable and the saves unchanged. The
final adoption rebases guest clocks, timers, worker and pacing deadlines, every
editor and the vibrator, so time spent parsing or staging files is not guest
time.

**Portable audio output** keeps the page synthesizer's bounded voice set, and
[loaded-sound admission](audio.md#loaded-sound-resources) bounds the retained
event, PCM and SysEx payload and reserves possible held keys before accepting
handles. The output record keeps original note programs, raw velocities and
samples, current per-clip channels and independent guest gain. Audio component
version 9 records paused cursors, frozen output, note envelope ages, finite
remaining passes, completion counts, media position, pitch sensitivity, RPN and
NRPN selectors, released keys held by sustain, independent PCM group controls,
original wave byte charges and fractional sample-frame positions. Genuine
version 6, 7 and 8 records remain readable at their saved whole frames; versions
6 and 7 derive charges from the available tails, and version 6 invents no PCM
routing. Earlier versions and mixed shapes are refused. New captures write
version 9 in both profiles; rollback needs a pre-upgrade checkpoint or normal
startup. See [PCM state compatibility](audio.md#live-pcm-track-controls). SKT
Java checkpoints also keep the bounded Player registry and queued listener events
as heap roots, without callback closures. The
[ownership repair](audio-ownership.md) also reconstructs output after a reconnect
and after dropped audio batches. PCM position uses an unscaled Host clock that
excludes detached staging time. Paused clips freeze their output clock, and
capable pages reconstruct notes at their elapsed envelope age. Oscillator phase,
release tails and network or device latency are not saved. A Host reset stops
all old sources before replay and clears audio definitions and frame decoder
state.

**Native packages** use a separate versioned record under the same envelope. It
keeps ARM memory, root registers, allocator ownership, built-in interfaces,
application identity, screen and image pixels, the display colours, the second
drawing interface's pen, point size and clip (a pointer, absent from records
taken before that interface was answered, which start it at its defaults),
listeners, queued events and resumes, frame and timer deadlines and logical
audio. An open file is recorded as
its name, save key, position, whether it may be written and, when its open
emptied the file, the number of bytes it then held. The record holds no file
bytes: the table of names the session wrote, the marks on names not yet stored
and the parsed resource cache are not restored, and a load opens each recorded
file again, from the save store and then from the package. Native playback
creates one-shot clips at nonnegative guest times; restoration rejects repeat
flags and negative playback origins before the running session is displaced, so
a modified checkpoint cannot make the next tick replay old audio cycles. Cached
image bytes and decoded pixels stay distinct from guest bytes changed after
decoding. Restoration installs fresh built-in bindings without running the
package's entry, factory or startup event. Custom Host bindings and active
tracing refuse capture. A native package's writes wait in the platform until a
file is closed or a frame ends, so a capture and a load both store what is
waiting first; adoption then detaches the old runtime from the store, and the
restored runtime takes over the names of the files the old one made, so an
empty file the play made is still a file after the load. Elapsed time and
deadlines rebase at adoption, excluding detached staging time.

<a id="lgt-checkpoints"></a>

### LGT checkpoints

LGT has two execution variants under the same envelope: a Clet, which the
platform calls and which returns, and an AOT Java title, whose guest threads are
goroutines parked inside platform calls. Both are recorded between two ticks,
the one place where nothing of the guest's is in flight on the Host's own
goroutine. Capture runs no guest code and does not call `pauseClet`; a load runs
neither the module's entry, its initializer nor `startClet`.

Java media adds an explicit version 3 runtime envelope that contains the
unchanged strict version 2 session and client record plus a media ownership and
notification record. The latter keeps Clip identities, exact completion
watermarks and queued PlayListener recipients, and validates after guest
bindings are restored and before workers start. The version 2 reader stays
strict and reconstructs issued Clip ownership without inventing prior events;
sessions with no Java media still write that shape. Earlier readers refuse the
new envelope, so rollback requires a supported older checkpoint or ordinary
startup. Delivery and GC rules are in
[Java WIPI listeners](audio.md#java-wipi-playback-listeners). Ordinary save
contents do not change.

**The guest clock needs no rebasing.** It is virtual: elapsed guest time plus the
work since an instruction baseline, and the instruction count travels with the
ARM state. Timer deadlines, thread wake-ups and sound origins are readings of
that clock and are stored as they are. The only Host-clock anchors are the
pacing deadline, which a restored session takes again at its first tick, the
vibrator's deadline, and the remaining PCM and percussion tails of the audio
output; the last two are re-anchored when a load commits, so time spent
validating a record is not time the motor or a sample ran.

The record holds the ARM memory image and the platform thread, then every table
the client keeps on the Go side:

- the three allocators (platform data, surfaces and the title's heap), each as a
  cursor, the spans taken back and every outstanding block. A restored allocator
  is accepted only if the blocks and spans cover exactly what the cursor has
  handed out, so it hands out the same address next;
- the platform stubs and their cursor, the Clet entry points, the handle
  counter, resource identities and the list of resolved imports;
- every surface with its transparency mask. A surface's pixels are stored only
  when the runtime's copy differs from guest memory at the same address: a
  Clet's surfaces are already in the memory image, and storing them twice
  doubled the largest part of a record. The frame last presented is stored apart
  from the display surface, because a title draws its next frame into the
  surface before presenting it;
- timers, pending dial results, queued events, the input mode and a pending Host
  text commit;
- the answers each pixel operation has already given. Without them a restored
  title would be asked again and charged the instructions again, which moves the
  guest clock and makes a restored session a different session;
- open files, each as a handle, a path, a cursor, whether the handle may write
  and, when its open asked for an empty file, how many bytes it then held. No file
  bytes are recorded, nor either of the two path lists a title's file calls
  keep: a load reads all of them from the store;
- media clips, levels and mutes, the logical audio timeline and the vibrator;
- the C library's generator and each `java/util/Random`. A seed names a sequence
  of the standard library's generator, which cannot report its state. It is an
  additive lagged sequence (each value is the sum of the values 607 and 273
  places back), so its whole state is its last 607 values. The runtime takes the
  first 607 from the library, the only part a seed decides, and produces every
  later one itself; the record is those 607 numbers and a position, and a
  restore is exact however long the title has been drawing.

An AOT Java title adds the tables behind its objects, which are guest memory
themselves: the class table with the link surface and layout the module was
answered with, string text (as bytes, because a title may hold half a surrogate
pair), open streams, images and the decode cache, vectors, calendars, sinks,
record stores, widgets, Graphics state, monitors, and the collector's object
table and schedule. A resource stream names the archive entry it reads rather
than repeating it, because a title that never closes its streams holds every
resource it has read. A stream opened on a `File` is recorded as the offset its
window starts at and how far it has read, and a `DataBase` as its name and record
size; a load fills both from the store. What a title has written into a stream
and not flushed travels in the record, because it has not reached a file yet.

**A parked guest thread is recorded as what it still owes.** The ARM core
supplies the registers of every call the thread is inside. For each call stopped
at a platform call, the record names the remainder:

| remainder | where the thread is parked | what is left |
| --- | --- | --- |
| result | `Thread.sleep`, `Thread.yield` | the answer |
| wait | `Object.wait`, with or without a deadline | take the lock again at the depth it was given up from, then the answer |
| monitor | a contended monitor enter | take the lock, then the answer |
| initializer | a class initializer the platform entered for the thread | finish the nested call, then the answer |
| function | the C library's run-function call | finish the nested call, then the answer |

A thread whose slice ended on its instruction budget has no platform call above
its innermost frame and resumes at that instruction with a fresh budget. A
thread not yet granted its first slice is recorded with no calls and enters
`run` from the top. Its open try regions, lock count and renewal count travel
with it. The remainder is derived from the same dispatch tables the call was
served from, at capture and again at restore, so a record cannot claim a
remainder its platform call does not have.

**Anything else is refused by name.** A thread parked beneath a platform call
whose remainder lives in Go locals (a class declaration thunk, a stream read
through the title's own `InputStream`, a paint entered from the thread) fails
the capture with the call it was inside, and the session carries on. Over the
local library, every thread at every tick boundary was in one of the rows above;
see [testing](testing.md#lgt-checkpoints).

A restored call releases no collector pins: the pin list is emptied at the end
of every round, capture refuses a boundary where it is not, and none of the
calls above builds an object before it parks.

Authentication adapters travel as their moved state only. Which exchange a
module is a client of is read from the module's code again, as at session start,
and a record that claims an adapter the module does not match is refused.
Validating one reads and writes no save; what an adapter derived from a save is
derived again from the store when the load commits.

Restoration loads the archive afresh, requires the record's memory map to be the
one that load produces plus the guest thread stacks the record declares, and
builds every table before a single goroutine starts. It is prepared against a
placeholder store that answers nothing, with no audio device. Commit fills what
the record has open from the live store, stores the writes the running session
had issued and not yet stored, and only then displaces that session: its store
and audio sink are detached and its guest threads are ended one at a time. **It
is not closed**, because closing calls `destroyClet`, which is guest code that
may write a save, and a load runs no exit callback.

A record is checked against the platform it is restored into, not only against
itself, wherever the runtime would otherwise act on a number the record states.
A panic in a restored session is contained like any other guest fault; these
checks cover what a panic boundary does not, a loop that does not end or an
allocation the Host is asked for:

- The guest clock is two numbers, and the first tick adds one to the other: the
  elapsed time, and the work the instructions retired since the baseline stand
  for. Both are bounded, the work to an hour, and the audio timeline is checked
  against their sum. A repeating sound is caught up cycle by cycle with no
  budget, so a record that put its instruction count years past its baseline
  would otherwise spend the first tick replaying it.
- A surface's pixels must be one block of the surface allocator, large enough
  for its size. The display is the one surface a record may carry outside that
  allocator, so each of its sides is held to 1,024, the largest either Host
  starts a session at.
- An allocator states no more blocks than its region has room for and no more
  spans taken back than lie between them.
- Every object the collector tracks, and the block its fields live in, must be
  blocks the data allocator has outstanding, each named once: a collection reads
  every block whole, so this bounds one.
- A superclass chain must end, whether followed through the links the classes
  record or through the name a platform class's superclass is looked up under
  the first time a type check passes through it. The layout's chains, walked by
  name, follow the same rule.
- A laid-out class may not state a size no class has, because a platform class
  first reached after a load is allocated from what its layout says.
- A thread that has ended holds no lock, and a thread parked for a lock is not
  the one holding it. Neither can happen in a session, and either leaves a lock
  nobody releases.
- A pending dial that a local service accepted is answered by that service when
  it comes due, so a record that has one must have the service.

Three behaviors of the running platform changed with this work, because a
session and its checkpoint have to take the same path: timers that come due in
one tick fire in the order they came due rather than in map order; a file opened
for writing gets its own copy of a packaged file's bytes instead of writing
through the archive's; and guest threads are ended one at a time and waited for,
at a close as well as at a load, because waking them together had them unwinding
through one client's bookkeeping at once.

Limits: active cheat freezes and patches refuse capture. Diagnostic counters
(uncaught-callback counts, collection time, the platform-call trace) restart at a
load. Handles inside the Java tables (a Graphics' surface, a widget's parent, a
thread's runnable) are not checked against the objects they name; every consumer
answers a wrong one with an error or a contained panic. A pending calendar field
assignment is restored in the Host's zone, the only zone the runtime sets. The
subscriber identity a title was started with is restored with it, so a title
that compares the number against one it stored sees the one it stored.

<a id="quick-load-and-ordinary-saves"></a>

### Quick load and ordinary saves

**The game's own saves come first.** A checkpoint is execution state: guest
memory, threads, the screen, sound and input. It holds no save. A quick load
brings the game back to the moment of the quick save and leaves every save where
it is; the restored game reads the saves as they are then, and what it writes
afterwards lands on top of them. Nothing is backed up or kept aside, and no
second path restores saves: the later write wins. This rule covers every
supported KTF, LGT and SKT checkpoint variant, and the CLI and the server reach
it through the same `internal/session` calls.

Two things stand between the rule and a runtime, and every variant handles them
the same way.

**A write the game issued may still be in the Host.** On LGT and SKT a write
waits in the buffer the handles on its path share until a flush or close, and a
native KTF package's write waits in the platform until a file is closed or a
frame ends. A checkpoint
holds no save bytes, so such a write would be lost once the session that made it
is gone. A quick save and a quick load therefore store it first, through a write
whose failure is reported: when the store refuses, the step is refused with
`backend.ErrCheckpointSaveWrite`, the game carries on, and the write is still
pending in it. The descriptor KTF runtime stores every write before the call
that made it returns, so it has nothing pending. Nothing the game has not issued
is written. A write through an output stream is a write to its file on every
platform, so it is pending like any other.

On SKT and LGT every handle on a path shares one buffer, so a quick step stores
each path's current content once and has nothing older to refuse from another
handle. LGT still refuses a buffer a `DataBase` stored a container behind under
the same name, a buffer and a `DataBase` on one save that both hold a write, and
a buffer whose name has no save key, because storing those would put older
content over newer or write where no key is; the title's own close does that
write and is the title's to make. LGT preloads any path lists pending writes need in an
isolated view, so a failed checkpoint read cannot clear dirty buffers or poison
the live session's caches. A prior guest read failure blocks a load while issued
writes remain pending; with none pending, a load can still recover the session.

**The Host keeps copies of what a game has open**: an open file's buffer, the
records behind a database handle, the lists of removed and created names, the
table of names a session wrote. Each answers the game before the store is asked
and is written back whole. Restored from a record, a copy would answer with what
the file held at the quick save and then be written over the file as it is now.
So a record names these objects and holds nothing of them, and committing a load
fills each from the live store through the lookup a first open of that name
makes: the removal list, the save key, then the packaged copy.

The order inside a load, on every variant:

1. The record is validated into a detached runtime over a placeholder store that
   answers every read as absent, refuses every write and counts what reaches it.
   The shared session logs a warning if validation asked it for anything; no
   variant does.
2. What can be refused without the saves is refused: the archive's identity, a
   running session that is not at a boundary, a cancelled request.
3. The restored runtime's storage is filled from the live store, with reads
   only. A save that cannot be read, a container that does not decode, or more
   bytes than the load's budget refuses the load with
   `backend.ErrCheckpointSaveRead`. A directory where a file was is not a failed
   read: a key that is a directory has no entry, so it is a save that is not
   there.
4. The running session's issued writes are stored. If that stored anything,
   step 3 runs again, so the restored game reads them like any other save.
5. The running session is cut off from the store and the Host's outputs, its
   guest threads are ended without an exit callback, and the restored runtime is
   adopted over the live store. Nothing in this step can fail.

A load therefore writes no save of its own, and with nothing pending it makes no
store write at all. A refused load leaves the running game running and reverts
or removes nothing. A load into a new process (`run -quickload`, a start with
`quick_load`) runs the same steps without a running session.

Every rebuilt object follows the same rules:

- A cursor stays where the record has it, even past the end of a file that is
  shorter now. A read there answers end of file, and a write fills the gap with
  zeros.
- A name the store has nothing for leaves its object empty and still open. The
  Host creates nothing; the game's next write through the object creates the
  name through the ordinary write path.
- An object whose open asked for an empty file (a truncating open) takes at most
  the bytes it held at the quick save, from the front of the file as it is now:
  the title is rewriting that file and never receives the tail of a newer,
  longer one. An object whose open created a file that was not there asked for no
  such thing and is given the whole file, because a title that keeps its save
  open for a session is in that position on its first run, and what it saved
  through the object after the quick save must not be cut off by its next write.
  KTF's WIPI C file table has no object of the first kind: a truncating open
  there empties the file in the store at the open, and its handles are cursors
  on the file as it is.
- Every list the runtime keeps beside the files is read during the load, so a
  list that cannot be read refuses the load rather than the game's first call
  after it.
- What is read is bounded and each key is read once, because the names come from
  a slot, which is untrusted input.

**Accepted limits.** These are how a game behaves once its memory is older than
its saves, and the emulator does not correct them:

- A game that keeps save data in its own memory shows what it remembered. A save
  list can look as it did at the quick save until the game reads the list again,
  usually when it is next started; the files are as they are.
- A game that then writes part of its save puts that part over the save as it is
  now, so the save can hold two points in time.
- A quick save taken while the game was in the middle of saving repeats the rest
  of that sequence on every load, over the save as it is then.
- On KTF, `FileSystem.list` and `DataBase.listDataBases` answer the packaged
  names plus the names this run knew, and do not read the disk.
- An authentication adapter's issued certificate travels in the slot, because it
  was made for the run and never read from a save.
- The LGT adapter that keeps a 100-byte file takes that file's original flag and
  certificate from the file as it is when its header validates, and from the
  record otherwise.

<a id="leftovers-from-earlier-builds"></a>

**Leftovers from earlier builds.** A build between 0.5.1 and the change that
removed saves from checkpoints wrote quick saves that embedded the saves and replaced the whole save folder at a
load: it staged the full set, verified it, recorded an intent, moved the live
directory to `previous` and installed the staged one. That writer has been
removed. Its recovery remains, and every store operation runs it first: a
replacement an earlier build left prepared or half-swapped rolls back, and a
completed one keeps its new set and its `previous`. On 2026-10-01 a concurrent
recovery defect was found in that path: an independent reader could delete an
active replacement's staging and intent, leaving the original generation only in
its backup. Shared filesystem locking closed the interleaving, and every store
operation still takes that lock; see
[the record of the repair](testing.md#checkpoint-adoption-blocker). Recovery from
what an earlier build left is tested from literal on-disk states. Earlier
measurements and rejected approaches are in the
[snapshot investigation](history/maintenance.md#snapshot-feasibility).

Such a build can have left three things in
`.wfeature-quicksave/owners/<owner>/`, and this build writes, reads and removes
none of them:

- `<archive SHA-256>.wfq`, a version 1 slot. It is reported as present so that
  the load is offered, and loading it is refused with the reason. A new quick
  save is written beside it under the current name.
- `previous/`, the saves a version 1 load set aside. They can be the newest saves
  a person made before that load, and nothing knows whether they are newer than
  the saves in use, so no code restores or removes them.
- `next/`, the staging directory of a replacement that never recorded its
  intent.

0.5.2 wrote version 2 slots, `<archive SHA-256>.v2.wfq`. They hold no saves, and
this build treats them as it treats a version 1 slot: reported as present,
refused with `ErrCheckpointLegacy` and the reason when loaded, and never written,
renamed or removed; a new quick save goes beside them under the version 3 name.

The server writes one Info line per start that names the save folder when an
earlier-format slot or `previous/` is there. To put a `previous` tree back by hand: stop the
server, move the contents of the owner's save folder somewhere else, copy the
contents of `previous/` into the owner's save folder, and start the server again.
Any of the three can be removed by hand once the game has been started once under
this build: every store operation first settles a replacement an earlier build
left half done, and after that the saves in use are never inside the reserved
directory.

## Documentation

[The documentation index](README.md) lists maintained references and subject
histories. The platform overviews describe current behavior; the histories keep
detailed traces, layouts and dated measurements.

<a id="shared-runtime-services"></a>
<a id="services-shared-runtime-service-contracts"></a>

## Shared runtime service contracts

The backend boundaries share these services across the platforms. The contracts
state caller responsibilities; they add no runtime layer.

### One save rule

Every file and record surface a title saves through — KTF's WIPI C file table,
its Java `File` and the earlier native package's files; LGT's C file table,
Java `File`, streams and `DataBase`; SKT's `XFile`, WIPI `File`, `com.xce.io`
streams and record stores — keeps the same rule:

1. **A write is seen at once.** A write — through a handle, or through an
   output stream on one, which holds nothing back — or a truncation is what
   every later read of that path in the session sees, through any handle, a
   new open, an existence check, a size or a stream.
2. **A write is stored by the close.** What a title wrote reaches the save
   store no later than its flush or close of that file, and in any case when
   the game ends, whether the title exits, fails, or the Host closes it.
   Storing earlier is allowed and some surfaces do.
3. **A truncation is stored at the open.** A truncating open empties the file
   in the store at the open, as a handset's file system does.
4. **Opening for writing creates the file.** Opening a missing file for
   writing makes an empty file that every later read sees at once, stored by
   the close as any write is; opening a missing file only for reading fails.
5. **A removal lasts.** A removed file is absent to every later read and stays
   absent in later sessions through the platform's removal list; writing the
   path again brings it back.
6. **A name is the same file on every host.** Names that differ only by case
   or by Unicode normalization are different files on a host file system that
   folds them as well, and a name Windows refuses or changes is stored as any
   other; [the save store](session.md#save-keys-on-disk) writes an upper-case
   letter in a path as `#` and the letter, as a handset wrote it, and what a
   host would fold or refuse by its number.
7. **Nothing else saves.** No background, timer or lifecycle event writes a
   save, and leaving a game in the background saves nothing. Saving is the
   player's to do in the game. Quick save stores the title's pending writes
   first and changes nothing else; a quick load changes no save.

When a surface stores within rule 2 differs by platform:

| Surface | Stored |
| --- | --- |
| KTF C table, KTF Java `File`, record stores of every platform | at every write |
| KTF earlier native package | at the close, at the end of each frame, at the end of the game |
| LGT C table, Java `File` and streams | at the close or flush, and at the end of the game |
| SKT `XFile`, WIPI `File`, `com.xce.io` streams | at the close or flush, and at the end of the game |

Three platform contracts stand beside the rule. A KTF C create-mode open of a
database the archive itself ships does not empty it. The earlier KTF native
package's platform names its files by lower-cased base name, so names that
differ only by case are one file there by the platform's own contract. And that
package is BREW's: its read-write mode (2) opens a file that is there and is
refused for one that is not, in place of rule 4, and only its create mode (4)
and its append mode (8) make a missing file. Both generations of its modules
are written against that — one creates a name before it opens it read-write
and retries a refused 2 or 8 with 4, the other takes its defaults when a
read-write open of its settings is refused — and a title of it that was handed
an empty file there read nothing out of it. BREW refuses a missing file in
append as well; this platform makes it, which ends as the retry with 4 does
and keeps the first save of a module that appends without retrying
([history](history/ktf.md#ktf-native-settings-2026-10-20)).

<a id="services-audio-time"></a>

### Audio time

Each `backend.Audio` instance has one guest-time domain. `Play` and `Advance`
receive durations with the same origin and rate. SMAF event times are millisecond
offsets from `Play`'s timestamp, not wall-clock timestamps. Host speed changes
affect the progression of guest time, and Audio does not apply speed again. A new
session clock needs a new timeline; a backwards timestamp does not rewind events
already emitted. Paused guest time must not advance because the browser
disconnects or wall time passes.

KTF uses `guestElapsed()` for both playback and advancement, LGT uses
`client.clock.now()`, and SKT uses `GuestElapsed()` through `audioNow()` and
`AdvanceAudio()`. The script runtime uses its own `clock` for both. Keeping each
pair together prevents a sound from being scheduled in another clock domain and
remaining indefinitely in the future.

<a id="services-data-ownership-and-callbacks"></a>

### Data ownership and callbacks

`Framebuffer.Present` borrows `Frame.RGBA` for the call. A framebuffer that keeps
pixels must copy them before returning, and a consumer must not infer ownership
from whether a particular framebuffer currently copies.

Audio sink PCM and SysEx slices are also borrowed and read-only during the call.
A sink that keeps or asynchronously transmits them must copy them. This holds at
reduced volume too: the implementation may allocate scaled samples, but
allocation is not an ownership transfer.

`Audio.LoadEvents` admits a sequence before loading it: the events must be of a
supported type and ordered by millisecond offset, and the sequence is charged
against the [loaded-sound budget](audio.md#loaded-sound-resources). It copies the
event slice and each event's sample and SysEx bytes, so the caller keeps
ownership of what it passed. Closing the handle releases the timeline's
reference.

Audio serializes sink callbacks under its own mutex. Stop, close, restart and
volume changes can emit callbacks as well as `Advance`. A sink must not reenter
the same Audio instance, because its mutex remains held. Serialized callbacks do
not imply that every caller is the Host's frame-loop goroutine.

### Owned presentation frames

`FrameUpdate` is an ownership transfer, unlike borrowed `Frame` pixels: a Host
may keep its RGBA bytes. `FrameSink.Offer` does not block; the Host must detach
the producer or stop the runtime before closing the channel. KTF sampling and the
browser encoder use separate retained pixel storage. Ordinary server frames use
`Session.FrameUpdate` with the same ownership and scale contract. `Force`
requests an explicit redraw across lifecycle or display-setting boundaries;
otherwise the encoder can omit an identical consecutive picture. See
[session presentation](session.md#presentation-and-audio).
