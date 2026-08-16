# Architecture

`wfeature` preserves the original Host / Runtime / Execution layering while
redefining its boundaries as Go packages.

## Layers

1. Host
   - `cmd/cli` drives a game from a terminal, and `cmd/server` serves the
     client and hosts the emulation sessions a phone plays through.
     `internal/webhost` holds the server's routes so they can be tested
     without a process.
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

The SKT loader was already immune and stays untouched, which is the more
interesting half: it finds its descriptor by extension wherever it sits,
derives the JAR name from the descriptor's own so the two move together, and
files installed files by base name. Nothing there looks up a fixed path. That
is the shape the other two would have to grow to stop needing the rule at all.

`internal/route` is a Host-side tool rather than a layer: it reads a scripted
way back to a scene and drives one, and it belongs to no platform. What it
needs of a session — advance a tick, fingerprint the screen, send a key, say
whether a stalled guest can still resume — arrives as functions and a key
table, because the platform sessions spell those differently (a key event type
on one, a pressed flag on the other) while a route reads the same either way.
KTF and LGT both take `-route`; see [`cli.md`](cli.md).

## Phase 1 startup path

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

## Phase 2 execution boundary

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
each title actually reaches is a per-title question, tracked per title in the
support matrix ([`support.md`](support.md)) and deliberately not summarised
here, where it would go stale. The probes and their counts are in
[`testing.md`](testing.md); the
instruction subset and the KTF format's boundaries are in
[`armcore.md`](armcore.md) and [`ktf.md`](ktf.md).

## Build profiles

- debug: `-tags debug`; includes detailed logs and browser diagnostic hooks.
- release: the default build tags; retains only normal logs and applies
  `-trimpath -ldflags="-s -w"` to build artifacts.

Each profile builds into its own directory — `build/debug` and `build/release`
— so the two coexist and neither build silently replaces the other. The server
is built per profile like every other binary here. There is no flag for it: one
process serving the other profile's save tree is a way to disagree with the
binary that is running, not a feature.

Both profiles read the same `var/games` and speak the same save API, but each
owns its saves: `var/savedata/<profile>/ktf/`. Playing a debug build must not
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
- A WebSocket handshake is not subject to CORS, so `Upgrader` checks `Origin`
  against the host the request arrived on by default. Without that, any page
  the user visits could drive a session on their own machine. A deployment
  reached by LAN IP while the page was loaded from a hostname replaces the
  policy with `Upgrader.CheckOrigin`. It is also what a reverse proxy has to
  keep in mind: forwarding to the server without passing the browser's `Host`
  through leaves every session refused, and
  [`running.md`](running.md#behind-a-reverse-proxy-on-a-unix-socket) has the
  configuration that does.

## Where the rest of the documentation is

| Document | Covers |
|---|---|
| `armcore.md` | the ARM interpreter, its profiler, and why the browser was seven times slower |
| `jvm.md` | class loading and the bytecode interpreter |
| `ktf.md` | the KTF platform: client.bin, the AOT bridge, WIPI |
| `lgt.md` | the LGT platform: ELF loading and the import table |
| `skvm.md` | the SKT platform and the SKVM class surface |
| `lcdui.md` | the high-level MIDP screens the runtime draws itself |
| `rms.md` | MIDP record stores and where saves live |
| `audio.md` | SMAF/MIDI decoding and the Host audio timeline |
| `network.md` | why every platform refuses the network, and the surface that refusal covers |
| `hqx.md` | the upscaling filter |
| `session.md` | the server session: the protocol, pacing, frame skipping, saves |
| `running.md` | building and running on Ubuntu, macOS and Windows |
| `cli.md` | every CLI command and flag, repro routes, and `ktfdump` |
| `testing.md` | the test layout and the opt-in acceptance probes |
