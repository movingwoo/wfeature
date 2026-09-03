# JVM implementation status

This is the bytecode half of the emulator: the JVM that runs an SKT title's
classes, and the one KTF reaches for the Java objects its AOT-compiled guest
hands back. (LGT does not use it — that platform's Java titles are AOT-compiled
too, but their metadata never becomes a class file, so `internal/platform/lgt`
carries its own object model.) It is not a complete JVM and is not meant to
become one — what is here is what titles have actually called, grown fixture by
fixture and then title by title. What that leaves out is at the end of this
file.

## Implemented

- class-file versions 45–70 and modified UTF-8 constant pools
- JAR-backed lazy class loading and class-name validation
- field/method descriptors and category 1/2 local and operand stacks
- class initialization, `ConstantValue`, static/instance field
- static, virtual, special, and interface method dispatch
- object creation and primitive, reference, and multidimensional arrays
- integer/long/float/double arithmetic, conversions, comparisons, branches, and
  both switch formats
- legacy `jsr`/`ret` and wide local instructions
- Java exception tables, `athrow`, and a basic runtime-exception hierarchy.
  Those runtime-owned Throwable types have no class file, and bytecode may
  still `new` one: the hierarchy table is what makes them instantiable and what
  their constructors resolve through, because the code that raises one is
  runtime-owned library code as often as it is the game — `Vector.elementAt`
  raising `ArrayIndexOutOfBoundsException` was the case that found it
- `checkcast`, `instanceof`, synchronized methods, and monitor instructions
- instruction, frame, and array-size limits applied across nested calls
- Go native-method registration boundary
- direct construction of runtime-owned `Object`, UTF-16-aware `String` and
  `StringBuffer` operations, and the game-used `Integer` conversions.
  `String.compareTo` orders by UTF-16 unit, which is what the language defines
  and what a title's own sort depends on; `Integer` boxes an int through its
  constructor and reads it back at any width
- runtime-owned `Thread`/`Runnable` metadata backed by Go goroutines, bounded
  guest executions, sleep/yield/interrupt state, and monitor wait/notify
- runtime-owned byte/data input streams, modified UTF-8 reads and writes, and
  safe JAR resource lookup through `Class.getResourceAsStream`. `writeUTF` is
  written against `readUTF` rather than against Go's encoder, because the pair
  is one round trip: a title stores a name with one and reads it back with the
  other
- `java.io.Reader` and `java.io.InputStreamReader`, which decode a byte stream
  into characters **without knowing the charset**: the reader feeds the
  platform's installed decoder one more byte at a time until the decoder
  answers with a character rather than a replacement, which makes the same code
  right for a single-byte and a two-byte encoding. Reading the whole stream up
  front would have been simpler and would have made a reader over a connection
  wait for the far end to close
- the game-used `Hashtable`, `Vector`, `Random`, `Calendar` and `TimeZone`
  subset. `Calendar.set` moves the same component `get` reads back and
  normalizes an out-of-range value the way the lenient mode a title relies on
  does. **`TimeZone` knows two zones**: GMT, and the one the guest clock runs
  in — Calendar and Date read that clock as local time, so a zone object that
  disagreed with them would make a title's own arithmetic wrong, and shipping
  the IANA table would mean embedding tzdata in a cross-compiled release.
  `Vector`'s capacity increment is honoured rather than treated as a hint: a
  vector grows by exactly the increment it was given and doubles only when it
  has none, because `capacity()` is observable and one title walks a vector
  with it as the bound
- `System.out` and `System.err` as real `PrintStream` fields, so the debug
  printing left in a shipped title resolves; what it prints goes to the logging
  boundary at debug level, one line per call rather than per newline
- Host APIs for creating guest objects and invoking constructors
- superclass traversal with depth limits and cycle detection
- composite class sources that search the runtime class library before the JAR
- bounded KTF AOT class, method, field, and vtable metadata registration with
  guest addresses kept as opaque identifiers
- pinned guest-address bindings for runtime-owned AOT String/Class objects and
  bounded AOT member lookup across registered superclass metadata
- uninitialized AOT object and primitive/reference-array construction from
  registered class addresses, with JVM array limits and zero-value semantics
- KTF `CallNative` normal-return execution through the shared ARM core, with a
  validated guest argument container, shared nesting-depth bound, and preserved
  outer register context
- bounded KTF raw exception-handler traversal, AOT/runtime hierarchy matching,
  and restore-PC unwind through the JavaJump/CallNative supervisor boundary
- guest-layout KTF exception objects pinned to their Go JVM objects, typed
  uncaught transport retaining the guest address, and per-logical-ARM-thread
  handler heads inherited by nested guest calls
- stable reverse lookup from pinned JVM objects to their KTF guest addresses,
  with duplicate address/object pairings rejected
- explicit `InvokeSpecial` for runtime-owned superclass methods invoked with an
  AOT subclass receiver
- a KTF AOT outer-call wrapper for constructors, instance methods, and static
  methods, including 32/64-bit primitive and bound-reference argument/results
- runtime-owned Java methods emitted as raw KTF metadata with bounded direct
  register-argument and native argument-container SVC entry points
- MIDlet active/pause/resume/destroy transitions through a bounded FIFO backend
  event queue
- `getAppProperty`, `notifyPaused`, `notifyDestroyed`, and `resumeRequest` native
  services
- error states for non-runtime lifecycle failures and host diagnostics for all
  callback failures
- runtime-owned `MIDletStateChangeException` and the minimal `Exception`
  constructor boundary
- state preservation and retry for conditional destroy refusal, plus
  unconditional destroy handling
- paused-state handling and retry on the next resume when `startApp()` reports a
  transient state-change exception
- forced `destroyApp(true)` cleanup after an uncaught `RuntimeException` from
  `startApp()` or `pauseApp()`, with the original failure retained for host
  diagnostics
- ignored `RuntimeException` failures from `destroyApp()`, followed by the MIDP
  Destroyed state
- runtime-owned minimal LCDUI `Display` and `Displayable` classes
- one stable `Display` per MIDlet, delayed and coalesced `setCurrent()` events,
  current-screen queries, and active/paused `isShown()` state
- runtime-owned minimal `Canvas` and `Graphics` classes, automatic initial
  paint, clipped and coalesced repaint events, and synchronous `serviceRepaints`
- Graphics color overloads, clip and translation state, solid rectangle fills,
  lines, and rectangle outlines with coordinates clipped before rasterization
- mutable and immutable MIDP images, white off-screen initialization,
  PNG/JPEG/GIF decoding from JAR resources or byte arrays, ARGB construction
  and retrieval, all eight MIDP region transforms, and source-over alpha drawing
- `drawImage`, `drawRegion`, and `drawRGB`, including image anchors, translated
  clips, negative scan lengths, and bounds validation before guest-array access
- runtime-owned bitmap fonts with face/style/size metrics, font selection, text
  anchors, and `drawChar`/`drawChars`/`drawString`/`drawSubstring` rasterization
- Host-provided framebuffer dimensions and complete RGBA8888 frame presentation
  through the same runtime path for both Hosts
- bounded FIFO delivery of Host key press, release, and repeat events to the
  current active Canvas, including repaint requests posted by key callbacks
- Canvas game-action/key-code/key-name mapping, double-buffer/repeat/pointer
  capabilities, full-screen state, and repaint on a visible mode change
- bounded FIFO delivery of Host pointer press, release, and drag callbacks to
  the current active Canvas

The default native hooks provide an initial subset of `Object`,
`System.currentTimeMillis`, `System.arraycopy`, `Math.abs/min/max`, and
`String.length/charAt/equals/hashCode`.

## The class library is declared, not compiled

The classes this runtime owns — CLDC's core, MIDP, SKVM — are declared in Go as
`jvm.ClassDefinition` values and installed with `DefineClass`. There are no
class files behind them and no JDK in the build.

Which of the three a class belongs to is the profile it came from rather than
its package name. `java.util.Timer` and `java.util.TimerTask` are the case:
they are in `java.util` and they are not in CLDC, so they are declared in
`internal/api/midp` and only a VM that installs that profile answers them —
see `skvm.md`, "A timer runs on a thread, because the title keeps drawing".

A definition carries what a class file carried: the name, the superclass and
interfaces a game's `instanceof` reads, the fields and methods it links
against, the constants it reads straight out of the class, and the checked
exceptions each method declares. What it adds is the Go body of every method
the runtime implements itself. A method declared without a body is either
abstract, or one a platform fills in with `RegisterNative` because its answer
depends on the Host.

Two rules keep a Go body behaving like the bytecode it replaced.

A field the class declares lives on the object under that field's name rather
than in Go state beside it, because a game may subclass these classes and read
what they wrote — `java.io.ByteArrayOutputStream`'s protected `buf` is the
case.

A call a library method makes on its own receiver goes back through the VM
rather than calling the Go function directly, because that is what
`invokevirtual` did: a game that overrides `InputStream.read` is calling its own
method from the inherited `read(byte[])`.

`synchronized` survives the move. The interpreter takes the receiver's monitor
for a bytecode body, and `invokeInstance` now does the same before it enters a
Go body whose declaration says synchronized, which is what keeps
`java.util.Vector` thread-safe for a title that shares one between threads.

A declared class outranks a class of the same name in the game's archive, the
way the library source preceded the JAR before. `jvm.CoreLibraryDefinitions`,
`midp.Definitions` and `skvm.Definitions` hand the same declarations to tools;
`internal/tools/javastub` turns them back into Java signatures so a test fixture
can still be compiled against this runtime with `javac` (see
[`testing.md`](testing.md)).

### The classes a title names

A native table and a class are not the same thing, and a runtime can carry one
without the other for a long time before it matters. `Integer.parseInt` was
answered here for sessions while `java/lang/Integer` did not exist as a class:
every call resolved, because a native is looked up by name and descriptor
before anything resolves a class at all. The day a title writes `new
Integer(3)`, `catch (Throwable t)` or `x instanceof RuntimeException`, the
class is what is asked for, and the answer was `class not found` on a method
the runtime had implemented all along.

Twelve archives arriving at once made that the largest single cause of a title
refusing to start, so the boxed numbers, `Math`, `Runtime` and the whole
throwable hierarchy are declared now. Three of those declarations are worth
their own line:

- **The exception classes are generated from the chain the runtime already
  had.** `runtimeClassParents` is what a `catch` has always been resolved
  through; the declarations are built from that same table rather than from a
  second one beside it, so the class a `catch` resolves and the class a `new`
  resolves cannot disagree. `instanceof` and `checkcast` work on them now as a
  side effect, which was the open item this closed.
- **A declaration whose method has no body is answered by the native under the
  same key.** That is how `String` has always worked here, and the boxed
  numbers now do the same: the declaration is the surface, `builtins.go` is the
  behavior.
- **Declaring a method the runtime does not implement is worse than not
  declaring it.** Resolution finds the declared method, finds no body, and
  fails with `method has no code` — where before it would have inherited a
  working one from `java/lang/Object`. `Integer.equals` is the case: declaring
  it meant implementing it, and value equality is what a title searching a
  `Vector` of boxed numbers was always asking for.

Two smaller rules came out of the same pass. **A parse of a null string is
`NumberFormatException`**, not a failed native — a title parsing a system
property the platform does not answer is asking a question with a Java answer,
and it catches it. And **a charset name is either UTF-8, the handset's, or
refused**: `String(byte[], String)` and `getBytes(String)` recognise the
EUC-KR spellings a Korean title uses and route them through the platform's own
encoder, because every handset this runtime serves has that default charset;
anything else is refused rather than guessed at, since decoding text with the
wrong table produces a screen full of plausible-looking mistakes.

**A stream that can be read twice has to say so.** `InputStream.reset` throws,
which is right for the abstract class and wrong for both of the streams a title
actually holds: a stream over a byte array can always go back, and a
`DataInputStream` can do whatever the stream it wraps can. Neither declared
`mark`, `markSupported` or `reset`, so both inherited the refusal. What that
cost is not a missing feature but a wrong answer: one title reads a header out
of a resource, resets to hand the same bytes to its own decoder, catches the
`IOException` its decoder never raises, keeps a null image and paints it — and
the report was a `NullPointerException` in `drawImage`, four hundred successful
`createImage` calls after the cause. The array stream now keeps a mark where
reading started, `reset` returns to it, and the wrapper passes all three calls
on to what it wraps. Three KTF titles reach a first frame on that alone.

The mark of a windowed stream is the start of its window, not the start of the
array. The class documentation says a reset there reaches the whole array; no
implementation of it does that, and a title reading one record out of a shared
buffer would walk into the record before its own.

**A calendar answers every field CLDC declares.** `AM_PM` and a `set` of
`DAY_OF_WEEK` were the two the switch did not reach, and a field it does not
reach is an `IllegalArgumentException` in the middle of a title's startup
rather than a wrong date. One local title asks for `AM_PM` during its own
startup and stopped there. Setting a day of the week names a
day inside the week the calendar is already in, which is what moving by the
difference from the current weekday does; the same normalization that carries
an out-of-range day into the next month carries this one.

**Two messages now carry the numbers that explain them.** A refused
`substring` names the range and the length it was given, and an unsupported
calendar field names the field. Both are read off a failure report by someone
who has no other view of what the title computed: the range `10..11 of a 10
character string` is what identified a truncated `long` in the platform above,
and the message before it said only that a range was wrong.

**`Class.forName` has three kinds of name to answer, and the loader knows one
of them.** A title that names its own class reaches a platform's ahead-of-time
registry rather than a class file, and asking the loader alone answered "not
found" for the name a title had just been started under. One KTF title asks
for its own main class on the way into its first card, caught the
`ClassNotFoundException`, and repainted an empty card for the rest of the run
— eight hundred flushes with no lit pixel, and nothing in the trace but the
same three calls. An array type is the third kind: it has no class file at all
and exists whenever its element type does, with a primitive element always
existing. All three are one question now, asked in one place, and the exception
is what is left when none of them answers.

**A body with no declaration is reachable from a native dispatch and from
nowhere else.** Eleven members of this library were in that state at once:
`String.replace(char,char)`, its char-array reads and its encoding-named
`getBytes`, `StringBuffer`'s long, boolean, object and char-array appends, its
capacity constructor and `setLength`, and `Class.forName`. Every one had a
working Go body, and every one was unreachable from compiled code, because a
class file resolves a member through its class rather than through a native
table. Four local archives stopped on four of them, which is how the group was
found; the rest were the same defect waiting for a title that used them.
`TestEveryCoreLibraryBodyIsDeclared` is now the tripwire — it walks the
registered bodies and fails on one no declaration names — and its inverse
checks that a declaration marked native has a body or is one of the two a
platform answers.

**Which titles a missing member would stop is a measurable number on the
compiled platform.** A KTF client image's name pool holds its method entries as
`<descriptor>+<name>`, so a member with a distinctive pair can be counted
across a corpus by reading the pool. The owning class is still not in there —
`internal/tools/apiscan` says why — but for `(J)Ljava/lang/String;+valueOf`
there is only one class it can belong to. Five of the 261 local KTF archives
name it, and this library had every other form of `String.valueOf` and not that
one, so each of those five would have stopped at the call rather than at
anything to do with the number it was formatting. The same scan says
`StringBuffer.append(long)` is named by forty of them, which is what the
measurement is worth: it turns "a gap somewhere out there" into a list of
titles, and it is how this one was decided rather than argued about.

**A field belongs to the class that declares it, whatever the code touching it
calls that class.** A compiler names a field reference after the type the
source expression had, not after the declaring class, so a subclass reading an
inherited field emits its own name — `Sub.buf` for a field `java.io` declares,
`Sub.v` for one its own superclass declares — while the superclass that wrote
the field emitted the superclass's name. Storing under the name as written put
the write and the read in two different slots, and the read answered a zero
nothing had written. Field access resolves the declaring class now, the way the
specification's field resolution does, walking superinterfaces before the
superclass because a static final may be declared on an interface; the answer
is cached per reference and thrown away whenever a class is defined. **This was
never about the library**: it is every guest class that inherits a field, which
is the shape a title's own hierarchy has. It was found by a stream subclass
reaching for the buffer its superclass had filled.

**The boxed flag is the fourth box.** `java/lang/Boolean` was the one CLDC box
this library never declared, and a title that puts a flag in a `Vector`
resolves the class before it can box anything. `TRUE` and `FALSE` are built in
the class initializer rather than answered by a Go singleton, because a title
compares a boxed flag against the field with `==` and has to get the same
object every time it reads it; the hash is the specification's 1231 and 1237
rather than the value.

**The byte source carries the specification's field names.** `buf`, `pos`,
`count` and `mark` are what `java.io.ByteArrayInputStream` declares protected,
and this runtime had named them `data`, `position` and `limit` behind private
access. A title that subclasses the stream to hand its buffer on — the same
thing it does to the sink — asks for `buf`, and a private field under another
name answers nothing.

**`System.exit` is a Host decision, so it is a hook.** `Options.Exit` is what
the call reaches, and a platform installs the teardown its own destroy path
uses. A MIDlet is not supposed to call it — `notifyDestroyed` is the
lifecycle's way out — but titles of this era ship it on the path out of an
error dialog, and with no hook the call is a missing method that ends the
session as a failure rather than as the shutdown the title asked for.

## An object handed to valueOf is asked, not named

`String.valueOf(Object)` and `StringBuffer.append(Object)` answered a string's
text for a string and `class@identity` for everything else, where the
specification says both call the object's own `toString`. That was a deliberate
gap for a long time and it was measured rather than assumed: a run of every
local KTF archive with the call counter on said the object form is asked for by
seven titles — eleven hundred calls, one title making five hundred of them in a
single run — and probing the four heaviest for *what* they hand it, the answer
every time was a string, the one case the identity form already got right.
What the note asked for was a title that hands it something else.

**One does.** An SKT title builds every resource path it loads by appending
into a `StringBuffer` and then calling `String.valueOf(Object)` on the buffer,
so a sprite sheet it asks for by the name `/image/i_intro_0.png` was asked for
by the name `java/lang/StringBuffer@3f` instead. Nothing in the archive answers
that, the title catches its own `IOException`, keeps the null image it was left
with, and paints it — so the session ends in `Graphics.drawImage` several calls
away from the read that failed, which is why no amount of reading the failing
frame would have found it.

The cost the old note named is real and is paid only where it is owed:
`objectText` resolves `toString` first and **invokes nothing** when the
resolution lands on `java/lang/Object`, which is every object whose class does
not override it. A class that cannot be resolved is answered the same way,
because a name is a better answer than ending the session over a print. The
nesting is bounded — one object naming another is ordinary, a class whose
`toString` names itself is a guest archive deciding how much host stack to use,
and past `maxToStringDepth` it gets the identity it would have had before.

## Deliberately incomplete

- the Java core class library and most MIDP/WIPI API classes. What is present
  is what titles have actually called; the KTF WIPI Java surface — `Jlet`,
  annunciator, display, cards — has grown from a constructor probe's minimum to
  what a played game needs, and [`ktf.md`](ktf.md) is where it is described
- the complete verifier and access control
- complete assignability checks for interface inheritance and array covariance
- complete guest thread lifecycle (`join`, daemon) and full interruption
  semantics outside `Thread.sleep`. `Thread.currentThread` answers the running
  execution's thread, and the execution that is not a guest thread gets one
  object of its own and keeps it. **Priority is kept and not honoured**: guest
  threads here are driven in turn by the platform's own scheduler, so a title
  that raises its loader above its frame loop is expressing a preference this
  runtime cannot act on, and `getPriority` answering what was set is the honest
  half of that
- reflection, class loader object, weak reference
- ~~an object appended to a `StringBuffer` is named, not asked~~ — **the
  title that hands one something other than a string has arrived**, and both
  forms call `toString` now. See "An object handed to valueOf is asked, not
  named" below
- execution semantics for `invokedynamic`, method handles, and module constants
- the LCDUI presentation for uncaught exceptions already retained in host
  diagnostics
- `Image.createImage(InputStream)`; the blank, copy, region, resource-name and
  byte-array forms are implemented
- MIDP `Graphics` arcs, rounded rectangles, triangles, area copies, and stroke
  styles. The WIPI platforms implement several of these on their own
  `org.kwis.msp.lcdui.Graphics`, because titles there call them; no MIDlet here
  has

Commands, the high-level LCDUI widgets, `GameCanvas` and RMS `RecordStore` were
on this list and are not any more — they are what an SKT title turned out to be
made of. [`lcdui.md`](lcdui.md) and [`rms.md`](rms.md) describe them. Text is no
longer ASCII-only either: two pixel fonts are embedded and shared by every
platform through `internal/glyph`, and [`ktf.md`](ktf.md) has how a face is
chosen per screen size.

**How much of CLDC is missing is a measurable number, and measuring it is one
pass.** The specification site publishes every CLDC class page in one file
(`llms-full.txt`, see `AGENTS.md`), and each page carries its field, constructor
and method summaries in a fixed shape; turning those into descriptors and
subtracting `jvm.CoreLibraryDefinitions` reports what the classes this runtime
already publishes still do not answer. It was 153 members when this was first
run, and reading that list is how the group above stopped being four titles'
problem. What is left is now one kind: members that would be a guess rather
than an implementation
— `Class.forName`'s reflective neighbours, `Calendar.computeFields`,
`Vector`'s protected `elementData`, `Hashtable.rehash`. **A member is worth
declaring when its behavior is exactly specified and its body is a few lines,
and worth leaving out when answering it means inventing what it answers**, which
is the same rule the platform tables use.

**The boxed numbers and the radix forms are no longer on the list, and neither
is `java/lang/Character`.** The class was the one CLDC class this library did
not publish at all, and **a missing class is worse than a missing method**: a
member nothing answers stops the call that wanted it, while a class nothing
declares stops the resolution, so a title that puts a char in a `Vector` — or
asks whether a key it was handed is a digit — dies before it reaches anything.
It is there now with the tests and conversions the specification names, and
**the character tests answer over ISO Latin-1**, which is what CLDC says a
handset provides by default: a runtime may know more of Unicode and this one
does not pretend to, because answering `isUpperCase` for a Hangul syllable one
way here and another on the handset is a divergence invented for no caller.

Beside it are the radix halves — `parseByte`, `parseShort`, `parseLong` and
`Integer.valueOf` with a base, `Integer.toString(int, int)` and
`Long.toString(long, int)` — the value equalities and hashes on `Byte`,
`Short` and `Long`, and the widening `floatValue`/`doubleValue`. **Nothing
local reaches one of them**: a scan of every KTF archive says the whole corpus
uses `parseInt`, `toString(int)`, `intValue`, `toHexString`, `byteValue`,
`parseByte` and the `Integer` constructor, and nothing else. That is the reason
to have them rather than not — the call that wants one is on a title nobody has
run, and it would stop rather than fail.

**`Math` is no longer on the list either.** CLDC 1.1's floating-point half —
`sqrt`, `ceil`, `floor`, `sin`, `cos`, `tan`, `toDegrees`, `toRadians`, and the
`float` and `double` overloads of `min` and `max` — is declared and implemented,
because every one of them is a value the specification names exactly rather than
a behaviour to invent. The two cases worth having a test for are the ones a
comparison would get wrong: a NaN poisons `max` and `min` rather than losing to
a number, and the two zeroes are ordered.

Text formatting of a float is deliberately not in that group. `Float` and
`Double` are absent from this library, and `StringBuffer.append(float)` with
them: Java's shortest-representation printing is not Go's, so a title that
formats a number would draw text that is subtly not what the handset drew, and
nothing local has asked for it yet.

Go's garbage collector also collects the guest object graph, so the project does
not implement a separate tracing GC. A compatibility layer will be added if a
game that depends on Java finalization or weak-reference semantics is found.

## The step limit is a ceiling or a window

`Options.MaxSteps` bounds the instructions one execution may spend, and an
execution is one entry into the interpreter: a Host call, or a guest thread for
its whole life. As a ceiling that is right for the first and wrong for the
second — a game's own thread decodes its images, loads its world and runs its
frames, so a fixed budget means every title dies partway through loading with an
error that names whatever method was unlucky enough to be running.

`Options.RenewSteps` makes the same number a window instead. When an execution
exhausts it the platform is asked whether to grant another; granting resets the
count, refusing ends the execution with the platform's error. Without the hook
the ceiling stands, which is what an unattended `InvokeStatic` should get.

`internal/platform/skt` installs it and grants windows while its MIDlet is
running, so destroying the MIDlet is what stops a runaway guest thread. That is
the same shape the WIPI platforms use for ARM guests, where one window's
exhaustion is a renewal request and cancelling the context is the only stop
(`docs/ktf.md`).

## What a guest instruction cost, and the two things that were not emulation

A player reported that the same title is far slower on this vendor than on
either WIPI platform, and it was — but most of the difference was not the
interpreter. Two lines were.

### A log line per bytecode instruction

The interpreter loop carried an ungated `Logger.Debug("jvm instruction", …)`.
The other execution core has no per-instruction log at all, so this was the
whole of the asymmetry, and it charged twice over:

- **A debug build wrote it.** Its level is `Debug`, so every instruction a title
  executed became a formatted line on stderr — 55,329 of them in three ticks of
  one archive, which is on the order of a million lines a second of play. The
  server a person runs at home is a debug build.
- **A release build built it and threw it away.** `slog` evaluates a call's
  arguments before the handler decides whether to keep them, so the
  `fmt.Sprintf` for the opcode and the boxing of six attributes happened for
  every instruction at every level.

One archive, 300 ticks, CPU:

| profile | before | after |
|---|---|---|
| release | 3.90s | 1.25s |
| debug | 12.74s | 0.70s |

The gate is asked once, in `New`, because the logger and its level are settled
once; the loop reads a bool. **And the trace is now a flag rather than something
a debug build does on its own** — `runskt -trace`, the bargain `runlgt -trace N`
already made. A trace that is on by default is not a diagnostic: it costs more
than the thing it is watching and moves every timing it was opened to look at.

### Thread.yield was a scheduler round trip

`java.lang.Thread.yield()` was `runtime.Gosched()`. The titles that call it do
not call it once: one local archive has **thirty-eight call sites**, which is
the idiom of a handset whose scheduler was cooperative — a thread that did not
yield did not let anything else run, so a game sprinkled the call through
everything it did.

Here a guest thread is a goroutine and Go preempts it whether it asks or not, so
every one of those calls bought nothing and cost a wake. A CPU profile of such a
title spent **63% of its samples in `runtime.wakep`** under that line. Removing
it took a title's CPU from 7.35s to 5.52s over the same 300 ticks with the same
number of frames produced. `Options.ThreadYield` stays for a platform that
really does have a token to hand over; none here does.

### One allocation per call instead of two

The interpreter popped a call's arguments into one slice and `invokeInstance`
then copied them into a second with the receiver in front — and that second
`make` was **eighty per cent of everything a title allocated**. The interpreter
now pops into a slice with the receiver's slot already in front of it.

It is worth having and it is not the answer: 21 to 19 allocations per call, and
about 1.8% of the time. **A guest method call still allocates nineteen times and
takes 1.8µs**, which is half a million calls a second, and that is where the
remaining budget is.

### Four allocations a guest call became one

The item the numbers above left open was the call itself: nineteen allocations
and 1.8µs. Most of that turned out to be the measurement rather than the call.

**A benchmark that enters through `InvokeStatic` measures a *Host* call**, and a
Host call starts a fresh execution — anything an execution carries across the
calls it makes is cold every iteration. A title does not run that way: one guest
thread is one execution for a whole session and makes millions of calls inside
it. `testdata/CallLoop.java` is that shape, and the numbers below are per guest
call rather than per Host call:

| | before | after |
|---|---|---|
| instance call | 597 ns, 4.0 allocs | **465 ns, 1.0 allocs** |
| static call | 470 ns, 4.0 allocs | **408 ns, 1.0 allocs** |
| call that also allocates an object | 1650 ns, 14.0 allocs | **1279 ns, 6.0 allocs** |

(medians of five; the run-to-run spread on this machine is about 5%)

Three things, in the order the profile named them.

**A frame came from the heap, three allocations at a time** — the frame, its
locals and its operand stack, which was half of everything a title allocated. A
frame lives exactly as long as the `execute` that made it, calls nest, and
nothing keeps one afterwards: an `ExecutionError` copies the four fields it
names. So the execution lends them now, from a free list bounded at 64. One
execution is one guest thread, so the list needs no lock. The locals are cleared
on the way in and the operand stack on the way out, because a stale `Value` of
the right kind would read as an initialized local and a stale reference would
keep an object alive for as long as the list held the frame.

**Every invoke instruction decoded its own operand out of the constant pool
again**, and a quarter of the run was in there. Nothing was cached and nothing
needed to be: `constant` returned the entry *by value* — fifty-six bytes — and
took its accepted tags as a variadic, so one `ReferenceAt` copied the struct
five times and built five tag slices to read four strings the pool already held.
It returns a pointer now, into a pool nothing writes to after it is parsed, and
the accessors on the hot path check their own tag inline.

**A field access composed its own name.** `object.Fields` is keyed by
`Class.name:Descriptor`, and that five-part concatenation ran on every `getfield`
and every `putfield` — and once more on every `putfield` to fill in a
`StoreEvent` that, with no observer installed, nobody read. The composed key is
cached beside the field resolution that was already cached next to it, and the
event is built only when something is watching. That is what takes the object
loop from fourteen allocations a call to six.

**What is left is one allocation per call**, and it is the slice the interpreter
pops the arguments into. It cannot simply be pooled: for a bytecode callee it is
dead as soon as the frame copies it into locals, but a native is arbitrary Go
code that may keep it. Popping straight into the callee's locals would close it,
and that means resolving the method before popping rather than after.

### The three lookups a call did not need, and its last allocation

What the section above left open was the one allocation a call still made and
the map lookups around it. Both are closed, and the benchmarks gained two
members so the trade is visible: a native does not take the same path a
bytecode callee does.

| | before | after | |
|---|---|---|---|
| instance call | 442 ns, 1.01 allocs | **405 ns, 0.02 allocs** | -8% |
| static call | 390 ns, 1.01 allocs | **347 ns, 0.01 allocs** | -11% |
| call that also allocates an object | 1237 ns, 6.01 allocs | **1100 ns, 4.01 allocs** | -11% |
| static native | 330 ns, 2.01 allocs | **310 ns, 1.01 allocs** | -6% |
| instance native | 354 ns, 2.01 allocs | 358 ns, 2.01 allocs | +1% |

**There were two tables of registered natives and the interpreter asked both**,
on every call, for an answer that is almost always "neither" — two hashes of a
three-string key. They are one table with one entry type now, and the two
chain walks that looked for an inherited native are one walk.

**A frame parsed a whole method descriptor to keep the return type.** The parse
is cached, but the cache is a `sync.Map` and the lookup is a hash of the
descriptor and a walk; `ReturnTypeOf` is a `LastIndexByte` and one type, and it
is all a frame ever wanted. The cached parse stays where the parameters are
wanted, which is the interpreter deciding how much to pop.

**The argument slice is borrowed from the execution now**, the way frames
already were. It can be, because nothing keeps it: a bytecode callee's frame
copies it into locals before the first instruction runs, and a native is handed
a copy — the static path always made one, and the instance path makes one too,
so what a native may do with its arguments is now the same question on both
paths. That is the row where a native pays: it trades the slice the interpreter
used to allocate for a copy of its own, which is why the static native row
improves and the instance native row does not.

**And one `defer` in the wrong function cost a fifth of a call.** Returning the
slice on every way out is one deferred call, and `step` is the largest function
in this package — large enough that the compiler stops open-coding defers and
each one becomes a heap record. Measured, putting the defer in `step` cost more
than the allocation it was there to avoid: 406 ns to 512. The four invoke
instructions are their own function now, which is where the defer is cheap, and
which leaves `step`'s switch smaller besides.

**What is left is `step` itself.** The profile is the switch (18%), the frame's
own push, pop and local access (11%), the remaining map lookups (5%), and
garbage collection driven by what a title allocates rather than by what a call
does. The first two are what a bytecode interpreter is, so **a resolution
cached per call site would be reaching for five to eight per cent**, and that is
the number the design has to be worth.

Where to keep one is answered: a call site is a constant-pool index in a class,
so `map[*classfile.Class][]atomic.Pointer[site]` found **once per frame** in
`newFrame` and carried on the frame turns three lookups per call site into one
per call, and `classfile` stays a parse result that the execution layer does not
have to own. What it costs is three new debts — a monomorphic guard, because a
virtual call's answer depends on the receiver's class; an invalidation
generation, because a platform registers natives while a title is starting and a
site cached before that would be stale; and atomic publication, because guest
threads run the same class at once. Recorded rather than built: the debts are
larger than the number.

### Why this could not be measured end to end

A MIDlet has no tick of its own: its threads pace themselves against the wall
clock, and the Host tick presents what they drew. So a fixed number of ticks is
a fixed number of *seconds*, and a faster engine spends them doing more work
rather than finishing sooner. Every end-to-end number lies in a different
direction:

- wall time is flat, because it is the pacing;
- CPU time is ambiguous — a *slower* engine can burn less of it, because it gets
  less done in the same seconds;
- allocation totals go **up** when the engine improves. The same archive over
  the same five seconds allocated 208MB, then 355MB, then 615MB as each fix
  landed.

So the judgement instrument is a benchmark where the work is fixed and the clock
is the answer: `BenchmarkGuestInstanceCall`, `BenchmarkGuestInterfaceCall` and
`BenchmarkGuestLoop` in `internal/jvm`, which is the shape `internal/armcore`
already uses. `TestLocalSKTArchiveCost` in `internal/platform/skt` is the
profiling harness beside them, and the macOS warning in
[`testing.md`](testing.md) applies to what it produces.

**The regression net for a change like this is not the frame.** A title that
animates reaches a different point of its animation in the same wall second once
the engine is faster, so a frame diff reports every such title. What is read
instead is the run's outcome: over all ninety-one local archives the state and
the error text are unchanged, one title that destroys itself reaches its own
exit eleven ticks sooner, and the one archive whose lit-pixel count moved is a
cross-fade that both builds agree on when it is run on its own rather than six
at a time.
