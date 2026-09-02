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
