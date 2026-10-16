# JVM implementation status

`internal/jvm` is the bytecode half of the emulator: the JVM that runs an SKT
title's classes, and the one KTF uses for the Java objects its AOT-compiled
guest hands back. LGT does not use it. That platform's Java titles are also
AOT-compiled, but their metadata never becomes a class file, so
`internal/platform/lgt` carries its own object model.

It is not a complete JVM and is not meant to become one. What is here is what
titles have called, grown fixture by fixture and then title by title; what that
leaves out is listed under [Deliberately incomplete](#deliberately-incomplete).

## Implemented

### Class files, linking and dispatch

- class-file versions 45–70 (`classfile.MaxSupportedMajorVersion`) and modified
  UTF-8 constant pools. A constant may hold half a surrogate pair, which the
  format allows and javac writes for a literal such as `"\uD800"`; the class
  loads and the half reads as U+FFFD, because strings here are Go text (see
  [Deliberately incomplete](#deliberately-incomplete))
- JAR-backed lazy class loading and class-name validation, through composite
  class sources that search the runtime class library before the JAR
- field/method descriptors and category 1/2 local and operand stacks
- class initialization, `ConstantValue`, and static and instance fields. Field
  access resolves the declaring class; see "A field belongs to the class that
  declares it" below. `getstatic` and `putstatic` initialize that declaring
  class or interface and nothing else (JVMS 5.5): an interface's array constant
  read inside an implementing class runs the interface's initializer, and an
  inherited static read through a subclass does not run the subclass's.
  Initializing a class initializes its superclasses and not its
  superinterfaces, which is the rule for class files without default methods
- static, virtual, special and interface dispatch, and explicit `InvokeSpecial`
  for runtime-owned superclass methods invoked with an AOT subclass receiver
- object creation and primitive, reference and multidimensional arrays
- integer/long/float/double arithmetic, conversions, comparisons, branches and
  both switch formats
- legacy `jsr`/`ret` and wide local instructions
- superclass traversal with depth limits and cycle detection. The loader does
  not check a chain, so method resolution, the native lookup behind it and a
  `catch` clause's class test stop after 64 classes (`maxSuperclassDepth`): a
  class file whose superclass names itself fails resolution instead of spinning
- `checkcast` and `instanceof`. Class and interface checks follow transitive
  superinterfaces and interfaces inherited through a superclass, including
  diamonds, per the
  [JVM assignment rules](https://docs.oracle.com/javase/specs/jvms/se8/html/jvms-6.html#jvms-6.5.instanceof).
  A visited worklist bounds each query to `MaxFrames` distinct type loads and
  65,536 inheritance edges (`maxInstanceRelations`), so cycles and repeated
  branches cannot create unbounded work; an exhausted query answers false. The
  same relations govern native listener checks and checkpoint validation. An
  array is an instance of its own type, `Object`, `Cloneable` and
  `Serializable` only (no covariance)
- Go native methods (`RegisterNative`, `RegisterContextNative`) and Host APIs
  for creating guest objects and invoking constructors
- a store observer (`SetStoreObserver`) shown every guest field, static and
  array store. With none installed a store costs one nil check and no
  `StoreEvent` is built

### Exceptions

- Java exception tables and `athrow`. The runtime-owned throwables have no class
  file: the 27-class hierarchy in `runtimeClassParents` (`exception.go`) is what
  a `catch` resolves through, and the declared exception classes are generated
  from the same table, so the class a `catch` resolves and the class a `new`
  resolves cannot disagree. Bytecode may `new` one because the code that raises
  one is runtime-owned library code as often as it is the game;
  `Vector.elementAt` raising `ArrayIndexOutOfBoundsException` was the case that
  found it
- a Go body raises a catchable guest exception by returning
  `jvm.Throw(class, message)`; `IsGuestException` tests an error against a
  class, application classes included
- `Throwable.printStackTrace` logs the class and message at error level. No
  stack trace is recorded
- an interpreter failure is an `ExecutionError` naming the class, method,
  descriptor, PC and opcode. A guest thread's uncaught failure goes to
  `Options.AsyncError`, or to the logger when none is installed

### Limits and Host hooks

`Options` in `vm.go`:

| Option | Default | Effect |
|---|---|---|
| `MaxSteps` | 1,000,000 | instructions one execution may spend; `ErrStepLimit` when exhausted and not renewed |
| `MaxFrames` | 1,024 | frame depth across nested calls (`ErrFrameLimit`); also bounds type loads per assignability query |
| `MaxArrayLength` | 16,777,216 elements | largest array a guest can allocate; a larger request fails with the limit rather than `OutOfMemoryError`. One `multianewarray` is held to the same total, each array it creates counting as 16 elements beside its own, and `StringBuffer.setLength` to the same length |
| `RenewSteps` | unset | makes `MaxSteps` a window; see [below](#the-step-limit-is-a-ceiling-or-a-window) |
| `Clock` | wall clock | the guest clock every date-shaped builtin reads |
| `Speed` | game speed | scales `Thread.sleep` and timed `Object.wait`; a platform scales `Clock` by the same factor |
| `GuestThreadStarter` | one goroutine per thread | replaces `Thread.start` for a platform whose guest threads share one cooperative core |
| `ThreadYield` | unset (no-op) | what `Thread.yield` does |
| `ByteDecoder`, `ByteEncoder` | UTF-8 with replacement | the platform's default charset; KTF installs EUC-KR |
| `Exit` | unset | what `System.exit` does |
| `AsyncError` | logger | receives a guest thread's failure |
| `TraceInstructions` | off | one debug log line per bytecode instruction (`runskt -trace`) |

### Threads and monitors

- `Thread` and `Runnable` are runtime-owned. Without `GuestThreadStarter` a
  started thread is a goroutine with an execution of its own, whose
  `MaxSteps` covers the thread's whole life; with it, the platform queues the
  thread object for its own service loop.
- `sleep`, `yield`, `interrupt`, `isAlive`, `activeCount`, and untimed `join()`
  with multiple waiters, interruption and `VM.Close`. A live thread owned by a
  cooperative scheduler is the platform's to wait for, and the JVM refuses
  rather than block the one guest core (KTF parks the calling worker; see
  [the KTF history](history/ktf.md#implementation-publishing-the-rest-of-the-core-library)).
  Finished and failed-start threads leave the root set; their objects keep the
  restart prohibition (`IllegalThreadStateException`). An interrupt is consumed
  by the one sleep, wait or join that throws for it. A sleep or wait longer
  than a Go duration can hold (`Long.MAX_VALUE` is how a title says "for
  ever") is held at about 292 years rather than wrapping to a wait that
  returns at once, and `VM.Close` releases a sleep on the Host's own pass with
  `ErrClosed`.
- `Thread.currentThread` answers the running execution's thread. An execution
  that is not a guest thread gets one object of its own and keeps it.
- **Priority is kept and not honoured.** Guest threads are driven in turn by the
  platform's scheduler, so a title that raises its loader above its frame loop
  expresses a preference this runtime cannot act on; `getPriority` answers what
  was set.
- Monitors are recursive and owned by an execution. A synchronized bytecode
  method takes the receiver's monitor, or the class's for a static method;
  `monitorenter`/`monitorexit` take the object's; an instance Go body whose
  declaration says synchronized takes the receiver's, which keeps
  `java.util.Vector` thread-safe for a title that shares one between threads.
  `Object.wait` requires ownership
  (`IllegalMonitorStateException` otherwise), releases the whole recursion
  depth and restores it on re-entry. `notify` wakes waiters in FIFO order. A
  timed wait honours `Speed`, and `wait(long, int)` rounds its nanoseconds as
  the specification's body does, so `wait(0, n)` is a 1 ms wait rather than an
  untimed one. An interrupted wait raises `InterruptedException`, except one a
  `notify` had already chosen: that one returns normally with the interrupt
  still pending, because a notification may not be lost to an interrupt (JLS
  17.2.4). `VM.Close` releases blocked entries and waits with `ErrClosed`.

### Core library

The library is declared in Go (see
[the next section](#the-class-library-is-declared-not-compiled)):

- `java.lang`: `Object` (built in), `Class`, `Runnable`, `String`,
  `StringBuffer`, `StringBuilder`, `System`, `Thread`, `Runtime`, `Math`,
  `Boolean`, `Byte`, `Short`, `Integer`, `Long`, `Character`, and `Throwable`
  with its hierarchy
- `java.io`: `InputStream`, `OutputStream`, `ByteArrayInputStream`,
  `ByteArrayOutputStream`, `DataInput`, `DataOutput`, `DataInputStream`,
  `DataOutputStream`, `PrintStream`, `Reader`, `InputStreamReader`, and
  `IOException` with `EOFException`, `UTFDataFormatException` and
  `UnsupportedEncodingException`
- `java.util`: `Vector`, `Stack`, `Hashtable`, `Enumeration`, `Random`,
  `Calendar`, `Date`, `TimeZone`
- `net/wfeature/ArrayEnumeration`, the runtime's own `Enumeration`, because the
  standard names no implementation and a game only holds the interface

`java.util.Timer` and `TimerTask` belong to MIDP and are described there.
The Go bodies of declared native members are in `builtins.go` and
`builtins_lang.go`. `System.getProperty` and `Class.getResourceAsStream` are
declared here and answered by the platform, which owns the properties and the
archive.

Behaviour a title depends on:

- `String` and `StringBuffer` operations are UTF-16-aware. `String.compareTo`
  orders by UTF-16 unit, which is what the language defines and what a title's
  own sort depends on. `Integer` boxes an int through its constructor and reads
  it back at any width. A null where a `String` method takes a `String` is a
  `NullPointerException` the title can catch, and `equalsIgnoreCase(null)` is
  false. Every `StringBuffer` read and write takes the buffer's lock.
- **One class is one `Class` object**: `getClass`, a class literal and
  `Class.forName` answer the same object, so `getClass() != o.getClass()` in an
  `equals` and `synchronized (Foo.class)` mean what they say. The table is
  rebuilt from the heap on restore. A platform's `NewClassObject`, which KTF
  binds to its own class records, is separate.
- `Throwable` keeps an empty message: `new Exception("").getMessage()` is `""`,
  and only an exception built without one answers null.
- Byte and data streams read and write modified UTF-8. `writeUTF` is written
  against `readUTF` rather than against Go's encoder, because a title stores a
  name with one and reads it back with the other. Every `DataInput` read that
  runs out of input raises `EOFException`, which a title reading records until
  the end catches by name.
- A library method that calls back into the guest (`toString` for
  `String.valueOf` and an `append`, the stream reads behind `readUTF`, a key's
  `equals`) runs the callback on the caller's execution, so a synchronized
  caller's monitor and a class initializer the caller is running are the
  callback's too.
- `Reader` and `InputStreamReader` decode a byte stream **without knowing the
  charset**: the reader feeds the platform's decoder one more byte at a time
  until it answers with a character rather than a replacement, which is correct
  for single-byte and two-byte encodings alike. Reading the whole stream first
  would make a reader over a connection wait for the far end to close. When the
  decoder answers a replacement followed by more text, the bytes before were an
  unreadable sequence and the last byte starts the next character, so the read
  answers one U+FFFD and keeps that byte for the next read; a character outside
  the basic plane is answered as its two surrogate units, one per read.
- `Calendar.set` moves the component `get` reads back and normalizes an
  out-of-range value the way the lenient mode a title relies on does.
- **`TimeZone` knows two zones**: GMT, and the one the guest clock runs in.
  Calendar and Date read that clock as local time, so a zone object that
  disagreed with them would make a title's arithmetic wrong, and shipping the
  IANA table would mean embedding tzdata in a cross-compiled release.
  `getAvailableIDs` answers those two, and any other ID gets GMT, as the
  specification says an unrecognized ID does.
- `Vector` honours its capacity increment: a vector grows by exactly the
  increment it was given and doubles only when it has none, because
  `capacity()` is observable and one title walks a vector with it as the bound.
- `System.out` and `System.err` are real `PrintStream` fields, so the debug
  printing left in a shipped title resolves. Output goes to the logging
  boundary at debug level, one line per call rather than per newline.
- `Runtime.totalMemory` answers 2 MiB, the upper end of the 512 KiB–2 MiB the
  handsets gave a MIDlet. `freeMemory` answers 2 MiB less the host heap in use,
  never less than 256 KiB, because a title that reads zero free stops loading.

### String identity and the intern pool

String literals, class-file `ConstantValue` strings, native string constants and
CLDC 1.1 `String.intern()` share a bounded pool inside each VM. `intern()` adopts
its receiver when no equal string is present; Java constructors and the Host's
`NewString` remain fresh objects. Native constants can use `InternString`.
This follows [JVMS 5.1](https://docs.oracle.com/javase/specs/jvms/se7/html/jvms-5.html#jvms-5.1)
and the [CLDC String contract](https://docs.oracle.com/javame/config/cldc/ref-impl/cldc1.1/jsr139/java/lang/String.html#intern--).
The pool retains at most 65,536 objects and 16 MiB of text; a new entry
exceeding either budget raises guest `OutOfMemoryError`, while existing entries
remain available. Heap component version 2 preserves canonical objects,
including strings retained only by the pool, and validates unique, ordered
String roots before replacing the destination pool. Earlier heap records are
refused because they cannot distinguish literals from separately constructed
strings. Ordinary game save files are unchanged.

### KTF AOT bridge

- bounded registration of KTF AOT class, method, field and vtable metadata,
  with guest addresses kept as opaque identifiers
- pinned guest-address bindings for runtime-owned AOT `String`/`Class` objects,
  and bounded AOT member lookup across registered superclass metadata
- uninitialized AOT object and primitive/reference-array construction from
  registered class addresses, with JVM array limits and zero-value semantics
- `CallNative` normal-return execution through the shared ARM core, with a
  validated guest argument container, a shared nesting-depth bound and the
  outer register context preserved
- bounded raw exception-handler traversal, AOT/runtime hierarchy matching, and
  restore-PC unwind through the `JavaJump`/`CallNative` supervisor boundary
- guest-layout exception objects pinned to their Go JVM objects, typed uncaught
  transport retaining the guest address, and per-logical-ARM-thread handler
  heads inherited by nested guest calls
- stable reverse lookup from pinned JVM objects to their guest addresses, with
  duplicate address/object pairings rejected
- an AOT outer-call wrapper for constructors, instance methods and static
  methods, including 32/64-bit primitive and bound-reference arguments and
  results
- runtime-owned Java methods emitted as raw KTF metadata with bounded direct
  register-argument and native argument-container SVC entry points

### MIDP runtime

`internal/api/midp` declares the surface and `internal/platform/skt` answers
its natives; see [the MIDP library](../internal/api/midp/README.md),
[SKT](skvm.md), [LCDUI](lcdui.md) and [RMS](rms.md).

- MIDlet active/pause/resume/destroy transitions through a bounded FIFO backend
  event queue; `getAppProperty`, `notifyPaused`, `notifyDestroyed` and
  `resumeRequest` as native services
- runtime-owned `MIDletStateChangeException` and the minimal `Exception`
  constructor boundary. A conditional destroy refusal preserves state and is
  retried; an unconditional destroy is handled. A transient state-change
  exception from `startApp()` leaves the MIDlet paused and retries on the next
  resume
- an uncaught `RuntimeException` from `startApp()` or `pauseApp()` forces one
  `destroyApp(true)` cleanup and retains the original failure for Host
  diagnostics; a `RuntimeException` from `destroyApp()` is ignored and the
  MIDlet is Destroyed. Other lifecycle failures are error states, and every
  callback failure reaches Host diagnostics
- runtime-owned `Display` and `Displayable`: one stable `Display` per MIDlet,
  delayed and coalesced `setCurrent()` events, current-screen queries, and
  active/paused `isShown()` state
- `Canvas` with automatic initial paint, clipped and coalesced repaint events,
  synchronous `serviceRepaints`, game-action/key-code/key-name mapping,
  double-buffer/repeat/pointer capabilities, full-screen state, and a repaint
  on a visible mode change
- `Graphics` color overloads, clip and translation state, rectangle fills and
  outlines, lines, rounded rectangles and arcs, with coordinates clipped before
  rasterization; `drawImage`, `drawRegion` and `drawRGB` with image anchors,
  translated clips, negative scan lengths, and bounds validation before guest
  array access
- mutable and immutable images: white off-screen initialization, PNG/JPEG/GIF
  decoding from JAR resources or byte arrays, ARGB construction and retrieval,
  all eight MIDP region transforms, and source-over alpha drawing
- bitmap fonts with face/style/size metrics, font selection, text anchors and
  `drawChar`/`drawChars`/`drawString`/`drawSubstring` rasterization
- Host-provided framebuffer dimensions and complete RGBA8888 frame presentation
  through the same runtime path for both Hosts
- bounded FIFO delivery of Host key press, release and repeat events and pointer
  press, release and drag callbacks to the current active `Canvas`, including
  repaint requests posted by key callbacks
- `Controllable`, `Control` and `VolumeControl`, backed by per-player level and
  mute state and stable control identity across checkpoints; finite playback
  loops, guest-clock media time and ordered deferred `PlayerListener` callbacks
  (see [audio lifecycle](audio.md#midp-playback-lifecycle))

### Checkpoint state

The SKT checkpoint path joins bytecode continuations, a cancelable worker
barrier and the heap. It preserves nested frames, native wait remainders,
sleep/join, monitor wait/notification and contended entry, including
synchronized native entry before its body runs. Authored timer bytecode retains
task continuations; platform audio wait tokens retain the original playback
generation. Detached restoration runs no initializer or lifecycle callback. The
standalone heap and AOT execution contracts are unchanged. See
[SKT checkpoints](skt-checkpoints.md) for limits and tests.

The internal `NativeExecutionState` component preserves execution identity and
spent bytecode budget while a platform supplies the remaining AOT work,
including the core library's forwarding `Thread.run` bodies. Active bytecode
frames, initializers, JVM-owned threads and unknown native remainders are
refused. It does not provide session save/load; see
[execution state](architecture.md#where-a-stopped-game-keeps-its-state).

`HeapState` is a separate, bounded object-graph component. It retains aliases,
cycles, exact primitive bits, statics, completed class initialization, AOT
metadata and binding strength, interned string roots, identity counters, and
cooperative Thread flags with fresh notification channels. Platform roots must
be supplied explicitly. Native payloads and arrays outside the core library
require platform codecs. Held monitors, active or failed initializers,
independently scheduled threads, overlapping list views, and unbounded recurring
Calendar timezone rules are refused. Restoration builds detached objects and
validates references before VM adoption.

Calendar records retain the instant, location name, and finite timezone
transition history (at most 1,024 transitions, 1 MiB per record). They walk
both ends with [Go ZoneBounds](https://pkg.go.dev/time#Time.ZoneBounds) and
construct a location using [TZif v2](https://www.rfc-editor.org/rfc/rfc9636.html)
through Go's public API. The generated internal TZif has an empty recurrence
footer, so Go extends its final explicit zone indefinitely; it is not an
exported timezone file. Restoring needs no host timezone files and no timezone
assets are bundled. A recurring rule is refused rather than replaced with a
finite approximation.

## The class library is declared, not compiled

The classes this runtime owns are declared in Go as `jvm.ClassDefinition`
values and installed with `DefineClass`: CLDC's core here, MIDP in
`internal/api/midp`, SKVM in `internal/api/skvm`, and WIPI Java in
`internal/api/wipi`. There are no class files behind them and no JDK in the
build. The one exception is the MIDP timer worker, `net/wfeature/TimerThread`,
which is embedded bytecode installed with `DefineClassFile` so that a parked
task keeps every local and its exact return point.

A class belongs to the profile it came from, not to its package.
`java.util.Timer` and `java.util.TimerTask` are in `java.util` and not in CLDC,
so they are declared in `internal/api/midp` and only a VM that installs that
profile answers them; see
[a timer runs on a thread](history/skt.md#implementation-a-timer-runs-on-a-thread-because-the-title-keeps-drawing).
Scheduling a `TimerTask` that is already scheduled throws
`IllegalStateException`, as MIDP specifies, rather than starting a second worker.

A definition carries what a class file carried: the name, the superclass and
interfaces a game's `instanceof` reads, the fields and methods it links
against, the constants it reads out of the class, and the checked exceptions
each method declares. It adds the Go body of every method the runtime
implements itself. A method declared without a body is either abstract or one a
platform fills in with `RegisterNative` because its answer depends on the Host.

Three rules keep a Go body behaving like the bytecode it replaced:

- A field the class declares lives on the object under that field's name rather
  than in Go state beside it, because a game may subclass these classes and read
  what they wrote; `java.io.ByteArrayOutputStream`'s protected `buf` is the
  case.
- A call a library method makes on its own receiver goes back through the VM
  rather than calling the Go function directly, because that is what
  `invokevirtual` did: a game that overrides `InputStream.read` is calling its
  own method from the inherited `read(byte[])`.
- `synchronized` survives the move: `invokeInstance` takes the receiver's
  monitor before it enters a Go body whose declaration says synchronized, as
  the interpreter does for a bytecode body.

A declared class outranks a class of the same name in the game's archive.
`jvm.CoreLibraryDefinitions`, `midp.Definitions` and `skvm.Definitions` hand the
same declarations to tools; `internal/tools/javastub` turns them back into Java
signatures so a test fixture can still be compiled against this runtime with
`javac` (see [compiling a Java fixture](history/testing.md#implementation-compiling-a-java-fixture)).

### The classes a title names

A native table and a class are not the same thing. `Integer.parseInt` was
answered for sessions while `java/lang/Integer` did not exist as a class: a
native is looked up by name and descriptor before anything resolves a class, so
every call resolved. A title that writes `new Integer(3)`, `catch (Throwable
t)` or `x instanceof RuntimeException` asks for the class, and the answer was
`class not found` on a method the runtime had implemented all along.

When twelve archives arrived at once (2026-08-17), that was the largest single
cause of a title refusing to start, so the boxed numbers, `Math`, `Runtime` and
the whole throwable hierarchy were declared. Three rules came with them:

- **The exception classes are generated from the chain the runtime already
  had.** `runtimeClassParents` is what a `catch` has always resolved through;
  building the declarations from the same table means the class a `catch`
  resolves and the class a `new` resolves cannot disagree. `instanceof` and
  `checkcast` work on them as a consequence.
- **A declaration whose method has no body is answered by the native under the
  same key.** `String` has always worked this way, and the boxed numbers do the
  same: the declaration is the surface, `builtins.go` is the behaviour.
- **Declaring a method the runtime does not implement is worse than not
  declaring it.** Resolution finds the declared method, finds no body, and
  fails with `method has no code`, where it would otherwise have inherited a
  working one from `java/lang/Object`. Declaring `Integer.equals` therefore
  meant implementing it, and value equality is what a title searching a
  `Vector` of boxed numbers asks for.

**A parse of a null string is `NumberFormatException`**, not a failed native: a
title parsing a system property the platform does not answer catches it. **The
parses accept what CLDC's accept and nothing more**: an optional minus sign and
digits of the radix. A leading `+` and surrounding spaces are a
`NumberFormatException` in every `parseInt`, `parseLong`, `parseByte`,
`parseShort` and `valueOf`, radix forms included, and a radix outside
`Character.MIN_RADIX..MAX_RADIX` is refused the same way. The byte, short and
long parses used to trim their text first, and every parse took a plus sign.
**A charset name is UTF-8, the handset's, or refused.** `String(byte[], String)`
and `getBytes(String)` recognize the EUC-KR spellings a Korean title uses and
route them through the platform's encoder, because every handset this runtime
serves has that default charset. Any other name is refused
(`UnsupportedEncodingException`) rather than guessed at, since decoding with the
wrong table produces plausible-looking wrong text.

**A stream that can be read twice says so.** `InputStream.reset` throws, which is
right for the abstract class and wrong for the two streams a title holds: a
stream over a byte array can always go back, and a `DataInputStream` can do what
the stream it wraps can. Neither declared `mark`, `markSupported` or `reset`, so
both inherited the refusal. One title reads a header out of a resource, resets
to hand the same bytes to its own decoder, catches the `IOException` its decoder
never raises, keeps a null image and paints it; the report was a
`NullPointerException` in `drawImage`, four hundred successful `createImage`
calls after the cause. The array stream now keeps a mark where reading started,
`reset` returns to it, and the wrapper passes all three calls to what it wraps.
Three KTF titles reached a first frame on that change alone (2026-08-25).
The mark of a windowed stream is the start of its window, not of the array. The
class documentation says a reset there reaches the whole array; no
implementation does that, and a title reading one record out of a shared buffer
would walk into the record before its own.

**A calendar answers every field CLDC declares.** `AM_PM` and a `set` of
`DAY_OF_WEEK` were the two the switch did not reach, and an unreached field is
an `IllegalArgumentException` in the middle of a title's startup rather than a
wrong date; one local title asks for `AM_PM` during startup and stopped there.
Setting a day of the week names a day inside the calendar's current week, which
is moving by the difference from the current weekday; the normalization that
carries an out-of-range day into the next month carries this one.

**Two messages carry the numbers that explain them.** A refused `substring`
names the range and the length it was given, and an unsupported calendar field
names the field. The range `10..11 of a 10 character string` is what identified
a truncated `long` in the platform above; the earlier message said only that a
range was wrong.

**`Class.forName` answers three kinds of name in one place.** The loader knows
class files. A title that names its own class reaches a platform's
ahead-of-time registry instead, and asking the loader alone answered "not
found" for the name the title had been started under: one KTF title asks for its
own main class on the way into its first card, caught the
`ClassNotFoundException`, and repainted an empty card for the rest of the run,
eight hundred flushes with no lit pixel. An array type has no class file and
exists whenever its element type does; a primitive element always exists.
`ClassNotFoundException` is what is left when none of the three answers.

**A body with no declaration is reachable from a native dispatch and from
nowhere else**, because a class file resolves a member through its class rather
than through a native table. Eleven members were in that state at once:
`String.replace(char,char)`, its char-array reads and its encoding-named
`getBytes`, `StringBuffer`'s long, boolean, object and char-array appends, its
capacity constructor and `setLength`, and `Class.forName`. Four local archives
stopped on four of them, which is how the group was found.
`TestEveryCoreLibraryBodyIsDeclared` walks the registered bodies and fails on
one no declaration names; its inverse,
`TestEveryCoreLibraryNativeDeclarationHasABody`, fails on a native declaration
with no body other than the two a platform answers (`Class.getResourceAsStream`
and `System.getProperty`).

**Which titles a missing member would stop is measurable on the compiled
platform.** A KTF client image's name pool holds method entries as
`<descriptor>+<name>`, so a member with a distinctive pair can be counted across
a corpus by reading the pool. The owning class is not in there
(`internal/tools/apiscan` says why), but `(J)Ljava/lang/String;+valueOf` can
belong to only one class. On 2026-08-28, five of the 261 local KTF archives
named it, and this library had every other form of `String.valueOf`, so each of
the five would have stopped at that call. The same scan found
`StringBuffer.append(long)` named by forty. The measurement turns a suspected gap
into a list of titles, and it decided this case.

**A field belongs to the class that declares it, whatever the code touching it
calls that class.** A compiler names a field reference after the static type of
the source expression, so a subclass reading an inherited field emits its own
name (`Sub.buf` for a field `java.io` declares, `Sub.v` for one its own
superclass declares) while the superclass that wrote the field emitted the
superclass's name. Storing under the name as written put the write and the read
in different slots, and the read answered a zero nothing had written. Field
access now resolves the declaring class as the specification's field resolution
does, walking superinterfaces before the superclass because a static final may
be declared on an interface. The answer is cached per reference and discarded
whenever a class is defined. This affects every guest class that inherits a
field, not only the library; a stream subclass reading the buffer its superclass
had filled found it.

**`java/lang/Boolean` is declared.** It was the one CLDC box this library had
not declared, and a title that puts a flag in a `Vector` resolves the class
before boxing anything. `TRUE` and `FALSE` are built in the class initializer
rather than answered by a Go singleton, because a title compares a boxed flag
against the field with `==` and must get the same object every time; the hash
is the specification's 1231 and 1237.

**The byte source carries the specification's field names.**
`java.io.ByteArrayInputStream` declares `buf`, `pos`, `count` and `mark`
protected; this runtime had named them `data`, `position` and `limit` behind
private access. A title that subclasses the stream to hand its buffer on asks
for `buf`.

**`System.exit` is a Host decision, so it is a hook.** `Options.Exit` is what the
call reaches, and a platform installs the teardown its own destroy path uses. A
MIDlet is not supposed to call it (`notifyDestroyed` is the lifecycle's way
out), but titles of this era call it on the way out of an error dialog, and
without the hook the call is a missing method that ends the session as a failure
rather than as the shutdown the title asked for.

## An object handed to valueOf is asked, not named

`String.valueOf(Object)` and `StringBuffer.append(Object)` call the object's own
`toString`, as the specification says.

They used to answer a string's text for a string and `class@identity` for
everything else. That gap was measured before it was closed: a run of every
local KTF archive with the call counter on (recorded 2026-08-30) found the object form
asked for by seven titles, eleven hundred calls, one title making five hundred
of them in a single run, and probing the four heaviest showed a string every
time, the one case the identity form already got right. An SKT title then
handed it something else: it builds every resource path by appending into a
`StringBuffer` and calling `String.valueOf(Object)` on the buffer, so a sprite
sheet requested as `/image/i_intro_0.png` was requested as
`java/lang/StringBuffer@3f`. The archive has no such entry, the title catches
its own `IOException`, keeps the null image and paints it, and the session ended
in `Graphics.drawImage` several calls away from the failed read.

The cost is paid only where it is owed. `objectText` resolves `toString` first
and **invokes nothing** when the resolution lands on `java/lang/Object`, which
is every object whose class does not override it. A class that cannot be
resolved is answered the same way, because a name is a better answer than
ending the session over a print. Nesting is bounded: one object naming another
is ordinary, a class whose `toString` names itself is a guest archive deciding
how much host stack to use, and past `maxToStringDepth` (16) it gets the
identity form.

## Deliberately incomplete

- Most of the Java core class library and of the MIDP/WIPI API classes. What is
  present is what titles have called. The KTF WIPI Java surface (`Jlet`,
  annunciator, display, cards) is described in [`ktf.md`](ktf.md).
- the complete verifier and access control
- array covariance in assignability checks
- daemon threads, timed `join`, thread names, and interruption semantics outside
  `Thread.sleep`, `Thread.join()` and `Object.wait`
- reflection, class loader objects and weak references
- execution semantics for `invokedynamic`, method handles and module constants
- the LCDUI presentation of uncaught exceptions already retained in Host
  diagnostics
- `Image.createImage(InputStream)`; the blank, copy, region, resource-name,
  byte-array and ARGB forms are implemented
- MIDP `Graphics` triangles (`fillTriangle`), area copies (`copyArea`) and
  stroke styles. Arcs and rounded rectangles are implemented; see
  [the curve calls](lcdui.md#the-curve-calls-were-missing-entirely). The WIPI
  platforms implement several of the missing calls on their own
  `org.kwis.msp.lcdui.Graphics`, because titles there call them; no MIDlet here
  has.
- half a surrogate pair inside a string. Strings are held as Go text, which
  cannot carry one, so a lone surrogate from a class constant, `String(char[])`,
  `valueOf(char)` or `append(char)` reads back as U+FFFD; a proper pair is kept
- a heap budget. `MaxArrayLength` bounds one array and one `multianewarray`;
  a guest allocating in a loop is bounded only by the host
- `Float` and `Double`, and with them `StringBuffer.append(float)`. Java's
  shortest-representation float printing is not Go's, so a title formatting a
  number would draw text subtly different from the handset's, and nothing local
  has asked for it.
- a separate tracing garbage collector. Go's collector also collects the guest
  object graph. A compatibility layer will be added if a game that depends on
  Java finalization or weak-reference semantics is found.

Commands, the high-level LCDUI widgets, `GameCanvas` and RMS `RecordStore` were
on this list and are implemented; [`lcdui.md`](lcdui.md) and [`rms.md`](rms.md)
describe them. Text is not ASCII-only: two pixel fonts are embedded and shared
by every platform through `internal/glyph`, and [the KTF history](history/ktf.md)
has how a face is chosen from the screen size a descriptor declares.

**How much of CLDC is missing is measurable in one pass.** The specification site
publishes every CLDC class page in one file
(<https://mirusu400.github.io/wipi-wiki/llms-full.txt>), and each page carries
its field, constructor and method summaries in a fixed shape. Turning those into
descriptors and subtracting `jvm.CoreLibraryDefinitions` reports what the
classes this runtime publishes still do not answer. The first run
(2026-08-28) reported 153 members, and that list is how the undeclared group
above was found. What is left is members that would be a guess rather than an
implementation: `Class.forName`'s reflective neighbours,
`Calendar.computeFields`, `Vector`'s protected `elementData`,
`Hashtable.rehash`. **A member is worth declaring when its behaviour is exactly
specified and its body is a few lines, and worth leaving out when answering it
means inventing the answer**, which is the rule the platform tables use.

**`java/lang/Character` is declared.** It was the one CLDC class this library
did not publish, and a missing class is worse than a missing method: a member
nothing answers stops the call that wanted it, while a class nothing declares
stops resolution, so a title that puts a char in a `Vector`, or asks whether a
key is a digit, dies before it reaches anything. It has the tests and
conversions the specification names, and **the character tests answer over ISO
Latin-1**, which is what CLDC says a handset provides by default. Answering
`isUpperCase` for a Hangul syllable one way here and another on the handset
would be a divergence with no caller.

The radix forms are declared beside it (`parseByte`, `parseShort`, `parseLong`
and `Integer.valueOf` with a base, `Integer.toString(int, int)` and
`Long.toString(long, int)`), with the value equalities and hashes on `Byte`,
`Short` and `Long` and the widening `floatValue`/`doubleValue`. **Nothing local
reaches them**: a scan of every KTF archive found the corpus uses `parseInt`,
`toString(int)`, `intValue`, `toHexString`, `byteValue`, `parseByte` and the
`Integer` constructor, and nothing else. They exist for a title nobody has run,
which would otherwise stop at the call.

`Math` includes CLDC 1.1's floating-point half: `sqrt`, `ceil`, `floor`, `sin`,
`cos`, `tan`, `toDegrees`, `toRadians`, and the `float` and `double` overloads
of `min` and `max`, because each is a value the specification names exactly.
The tests cover the two cases a plain comparison gets wrong: a NaN poisons `max`
and `min` rather than losing to a number, and the two zeroes are ordered.

## The step limit is a ceiling or a window

`Options.MaxSteps` bounds the instructions one execution may spend, and an
execution is one entry into the interpreter: a Host call, or a guest thread for
its whole life. A ceiling is right for the first and wrong for the second. A
game's own thread decodes its images, loads its world and runs its frames, so a
fixed budget makes every title die partway through loading with an error naming
whichever method happened to be running.

`Options.RenewSteps` makes the same number a window. When an execution exhausts
it, the platform is asked whether to grant another: granting resets the count,
refusing ends the execution with the platform's error. Without the hook the
ceiling stands, which is what an unattended `InvokeStatic` should get.

`internal/platform/skt` installs it and grants windows while its MIDlet is
running, so destroying the MIDlet is what stops a runaway guest thread. The
WIPI platforms use the same shape for ARM guests, where one window's exhaustion
is a renewal request and cancelling the context is the only stop
([`ktf.md`](ktf.md)).

## What a guest instruction cost, and the two things that were not emulation

A player reported that the same title ran far slower on this vendor than on
either WIPI platform. Most of the difference was two lines outside the
interpreter. The measurements in this section were recorded on 2026-09-04.

### A log line per bytecode instruction

The interpreter loop carried an ungated `Logger.Debug("jvm instruction", …)`.
The ARM core has no per-instruction log, so this was the whole asymmetry, and it
cost twice:

- **A debug build wrote it.** At `Debug` level, every instruction became a
  formatted line on stderr: 55,329 of them in three ticks of one archive, on the order of a
  million lines per second of play. The server a person runs at home is a debug
  build.
- **A release build built it and discarded it.** `slog` evaluates a call's
  arguments before the handler decides whether to keep them, so the
  `fmt.Sprintf` for the opcode and the boxing of six attributes happened for
  every instruction at every level.

One archive, 300 ticks, CPU:

| profile | before | after |
|---|---|---|
| release | 3.90s | 1.25s |
| debug | 12.74s | 0.70s |

The gate is evaluated once, in `New`, because the logger and its level are
settled once; the loop reads a bool. The trace is a flag (`runskt -trace`, the
bargain `runlgt -trace N` already made) rather than something a debug build does
on its own: a trace that is on by default costs more than what it watches and
moves every timing it was opened to examine.

### Thread.yield was a scheduler round trip

`java.lang.Thread.yield()` was `runtime.Gosched()`. Titles call it often: one
local archive has **thirty-eight call sites**, the idiom of a handset whose
scheduler was cooperative, where a thread that did not yield let nothing else
run. A guest thread here is a goroutine that Go preempts regardless, so each call
bought nothing and cost a wake. A CPU profile of such a title spent **63% of its
samples in `runtime.wakep`** under that line. Removing it took the title's CPU
from 7.35s to 5.52s over the same 300 ticks with the same number of frames.
`Options.ThreadYield` remains for a platform that has a token to hand over; none
here does.

### One allocation per call instead of two

The interpreter popped a call's arguments into one slice, and `invokeInstance`
copied them into a second with the receiver in front; that second `make` was
**eighty per cent of everything a title allocated**. The interpreter now pops
into a slice with the receiver's slot already in front. The change took 21 to 19
allocations per call and about 1.8% of the time. **A guest method call still
allocated nineteen times and took 1.8µs**, half a million calls a second, which
was where the remaining budget was.

### Four allocations a guest call became one

Most of those nineteen allocations and 1.8µs were the measurement rather than
the call. **A benchmark that enters through `InvokeStatic` measures a *Host*
call**, and a Host call starts a fresh execution, so anything an execution
carries across its calls is cold every iteration. A title runs one guest thread
as one execution for a whole session and makes millions of calls inside it.
`testdata/CallLoop.java` is that shape (`BenchmarkGuestCallLoop*` in
`interpreter_bench_test.go`), and the numbers below are per guest call:

| | before | after |
|---|---|---|
| instance call | 597 ns, 4.0 allocs | **465 ns, 1.0 allocs** |
| static call | 470 ns, 4.0 allocs | **408 ns, 1.0 allocs** |
| call that also allocates an object | 1650 ns, 14.0 allocs | **1279 ns, 6.0 allocs** |

(medians of five; run-to-run spread on the machine was about 5%)

Three changes, in the order the profile named them:

- **Frames came from the heap, three allocations at a time** (frame, locals,
  operand stack), half of everything a title allocated. A frame lives exactly as
  long as the `execute` that made it, calls nest, and nothing keeps one
  afterwards (an `ExecutionError` copies the four fields it names). The
  execution lends them from a free list bounded at 64 (`framePoolLimit`); one
  execution is one guest thread, so the list needs no lock. Locals are cleared on
  the way in and the operand stack on the way out, because a stale `Value` of
  the right kind would read as an initialized local and a stale reference would
  keep an object alive while the list held the frame.
- **Every invoke instruction decoded its operand out of the constant pool
  again**, a quarter of the run. `constant` returned the entry *by value* (56
  bytes) and took its accepted tags as a variadic, so one `ReferenceAt` copied
  the struct five times and built five tag slices to read four strings the pool
  already held. It returns a pointer into a pool nothing writes after parsing,
  and the hot-path accessors check their tag inline.
- **A field access composed its own name.** `object.Fields` is keyed by
  `Class.name:Descriptor`, and that five-part concatenation ran on every
  `getfield` and `putfield`, and again on every `putfield` to fill a
  `StoreEvent` nobody read when no observer was installed. The composed key is
  cached beside the field resolution, and the event is built only when
  something is watching. That took the object loop from fourteen allocations a
  call to six.

The remaining allocation was the slice the interpreter pops arguments into. It
could not be pooled directly: for a bytecode callee it is dead once the frame
copies it into locals, but a native is arbitrary Go code that may keep it.

### The three lookups a call did not need, and its last allocation

That last allocation and the map lookups around it are closed. The benchmarks
gained two native members, because a native does not take the bytecode callee's
path:

| | before | after | |
|---|---|---|---|
| instance call | 442 ns, 1.01 allocs | **406 ns, 0.02 allocs** | -8% |
| static call | 390 ns, 1.01 allocs | **340 ns, 0.01 allocs** | -13% |
| call that also allocates an object | 1237 ns, 6.01 allocs | **1112 ns, 3.01 allocs** | -10% |
| static native | 330 ns, 2.01 allocs | **287 ns, 0.01 allocs** | -13% |
| instance native | 354 ns, 2.01 allocs | **341 ns, 1.01 allocs** | -4% |

- **Two tables of registered natives were both asked on every call**, for an
  answer that is almost always "neither": two hashes of a three-string key. They
  are one table with one entry type, and the two chain walks for an inherited
  native are one walk.
- **A frame parsed a whole method descriptor to keep the return type.** The parse
  is cached in a `sync.Map`, but a lookup still hashes the descriptor and walks;
  `ReturnTypeOf` is a `LastIndexByte` and one type, which is all a frame needs.
  The cached parse remains where the interpreter decides how much to pop.
- **The argument slice is borrowed from the execution**, as frames are, and **a
  native is lent it rather than given a copy.** The first version copied, on the
  reasoning that a native is arbitrary Go code. Over five seconds of one archive
  the copy was **82% of everything allocated**, because a title's calls into the
  platform are its drawing and drawing takes arguments. Whether any native keeps
  the slice it is handed was answered from the repository: none does; every one
  reads its arguments by index and keeps the values. The contract is written on
  `NativeMethod`: **the arguments are borrowed for the length of the call.** A
  body that broke it would read zero values, because the slice is emptied when
  it goes back, and fail its own argument check. Removing the copy took the
  archive's five seconds from 388MB allocated to 76MB.
- **One `defer` in the wrong function cost a fifth of a call.** Returning the
  slice on every exit is one deferred call, and `step` is the largest function in
  the package, large enough that the compiler stops open-coding defers and each
  becomes a heap record. With the defer in `step` a call took 512 ns instead of
  406 ns. The four invoke instructions are their own function, where the defer
  is cheap, which also shrinks `step`'s switch.
- **Byte arrays are read once.** Reading a guest byte array copied it twice,
  into a `[]Value` (24 bytes an element) and then into the `[]byte` the caller
  wanted, so a 64KB array cost 1.5MB to read. `ByteArraySnapshot` fills the
  bytes under one read lock, through `ByteArrayReader` where a storage can do it
  directly. The default heap storage skips the middle slice; **KTF's
  guest-memory storage skips the decode as well**, because a byte array in
  guest memory already is the bytes: one `Memory().Read` into the caller's
  slice. A storage that does not implement `ByteArrayReader` still goes through
  `LoadRange`, because one that keeps its elements elsewhere may fetch a run at
  once rather than an element at a time, as KTF's does for every other
  component width.

The same archive over the same five seconds, across the four passes:

| | allocated |
|---|---|
| before any of this | 615 MB |
| frames and the call path | 388 MB |
| natives lent their arguments | 76 MB |
| byte arrays read once | **21 MB** |

**What is left in a call is `step` itself.** The profile is the switch (18%),
the frame's push, pop and local access (11%), the remaining map lookups (5%),
and garbage collection driven by what a title allocates. The first two are what
a bytecode interpreter is, so **a resolution cached per call site would gain
five to eight per cent**, and a design has to be worth that number.

Where to keep one is answered: a call site is a constant-pool index in a class,
so `map[*classfile.Class][]atomic.Pointer[site]`, found **once per frame** in
`newFrame` and carried on the frame, turns three lookups per call site into one
per call, and `classfile` stays a parse result the execution layer does not
own. It would add three obligations: a monomorphic guard, because a virtual
call's answer depends on the receiver's class; an invalidation generation,
because a platform registers natives while a title is starting and a site cached
before that would be stale; and atomic publication, because guest threads run
the same class at once. It is recorded rather than built, because those costs
exceed the gain.

### Why this could not be measured end to end

A MIDlet has no tick of its own: its threads pace themselves against the wall
clock, and the Host tick presents what they drew. A fixed number of ticks is a
fixed number of *seconds*, and a faster engine spends them doing more work
rather than finishing sooner, so every end-to-end number misleads:

- wall time is flat, because it is the pacing;
- CPU time is ambiguous: a *slower* engine can burn less, because it does less
  in the same seconds;
- allocation totals go **up** when the engine improves. The same archive over
  the same five seconds allocated 208MB, then 355MB, then 615MB as each fix
  landed.

The judgement instrument is therefore a benchmark with fixed work, where the
clock is the answer: `BenchmarkGuestCallLoop*`, `BenchmarkGuestInstanceCall`,
`BenchmarkGuestInterfaceCall` and `BenchmarkGuestLoop` in `internal/jvm`, the
shape `internal/armcore` already uses. `TestLocalSKTArchiveCost` in
`internal/platform/skt` is the profiling harness beside them, and the macOS
warning in [`testing.md`](testing.md) applies to what it produces.

**The regression net for such a change is not the frame.** A title that animates
reaches a different point of its animation in the same wall second once the
engine is faster, so a frame diff reports every such title. The run's outcome is
read instead: over all ninety-one local archives (2026-09-04) the state and the
error text were unchanged, one title that destroys itself reached its own exit
eleven ticks sooner, and the one archive whose lit-pixel count moved is a
cross-fade both builds agree on when it is run alone rather than six at a time.
