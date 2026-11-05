# Platform coverage audits

This file preserves implementation investigations and dated validation records.
Statements about current behavior, missing APIs, corpus size, and planned work
apply to their original revision; later records may supersede them.
Start with [testing.md](../testing.md) for the maintained overview.
This consolidation adds no execution or acceptance evidence.


## Contents

- [LGT audit (2026-09-12, 2b56bad)](#lgt-lgt-coverage-audit-after-pr-143)
- [Subsequent work](#lgt-subsequent-work)
- [Confirmed missing Java methods](#lgt-confirmed-missing-java-methods)
- [Partial behavior behind implemented entries](#lgt-partial-behavior-behind-implemented-entries)
- [The coverage scanner can hide Java gaps](#lgt-the-coverage-scanner-can-hide-java-gaps)
- [Validation and limits](#lgt-validation-and-limits)
- [KTF and SKT audit (2026-09-12, 2b56bad)](#ktf-skt-ktf-and-skt-coverage-audit)
- [Subsequent work](#ktf-skt-subsequent-work)
- [KTF](#ktf-skt-ktf)
- [SKT Java](#ktf-skt-skt-java)
- [Scanner limits](#ktf-skt-scanner-limits)
- [Validation and limits](#ktf-skt-validation-and-limits)
- [Cross-platform parity and what the specification settles (2026-11-05)](#cross-platform-parity-2026-11-05)

<a id="lgt-lgt-coverage-audit-after-pr-143"></a>

## LGT audit (2026-09-12, 2b56bad)

This investigation covers revision `2b56bad`, after
[PR #143](https://github.com/movingwoo/wfeature/pull/143) was merged. No runtime
implementation changed during the audit. The PR repaired measured startup and
initial gameplay paths; it did not complete the LGT Java library.

<a id="lgt-subsequent-work"></a>

### Subsequent work

This snapshot predates the managed follow-up. The scanner now uses Java
dispatch coverage, and custom stream mark/reset operations reach guest
overrides; see the [platform implementation](lgt.md#implementation-the-lgt-platform).
Focused Java LWC fields support [native keyboard input](../native-text-input.md),
with session, WebSocket and browser proof from an authored AOT archive. These
changes do not complete the Java library or prove that the missing methods below
execute in current archives.

<a id="lgt-confirmed-missing-java-methods"></a>

### Confirmed missing Java methods

The dispatch tables in `internal/platform/lgt/java_api.go` remain partial.
The following are examples, not an exhaustive count of missing APIs:

| Class | Missing methods | Evidence |
|---|---|---|
| `DataInputStream` | `readUnsignedShort`, `readFloat`, `readDouble` | Slots 26, 30, 31 return `served=false` through the virtual dispatcher. |
| `DataOutputStream` | `writeFloat`, `writeDouble` | Slots 21, 22 return `served=false`; `writeLong` is already implemented. |
| `StringBuffer` | `append(char[])`, `append(long)`; further overloads and `reverse` | Slots 19 and 24 return `served=false`; remaining examples are absent from the registered method and baked-slot tables. |
| `Stack` | `peek`, `search` | Slots 34, 36 return `served=false`; PR #143 added `empty`, not these methods. |
| `Vector` | `contains`, `setElementAt`; enumeration and further operations | Slots 18 and 26 return `served=false`; the table implements only selected operations. |
| `Calendar`, `Date`, `TimeZone` | Time-zone selection and access, `Date.setTime`, most `TimeZone` methods | Source audit: only no-argument `Calendar.getInstance`, selected Calendar operations, `Date.getTime`, and `TimeZone.getRawOffset` are registered. |

Method contracts were checked against the published CLDC pages for
[DataInputStream](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataInputStream.md),
[DataOutputStream](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataOutputStream.md),
[StringBuffer](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/lang/StringBuffer.md),
[Stack](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/util/Stack.md),
[Vector](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/util/Vector.md),
[Calendar](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/util/Calendar.md),
[Date](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/util/Date.md), and
[TimeZone](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/util/TimeZone.md).
The specification defines behavior, not vendor slot numbers. The slot labels
above follow the existing declaration-order model and adjacent anchors; this
audit did not discover native call sites for those missing methods. A new
vendor layout still needs caller evidence before assigning its slots.

These gaps can stop a title that reaches them. The first-frame corpus check
below did not reach them, so they are not eleven newly observed game crashes.
Superseded in part: Vector slot 26 now serves `setElementAt`; see
[LGT execution and Java](../lgt.md#execution-and-java).

<a id="lgt-partial-behavior-behind-implemented-entries"></a>

### Partial behavior behind implemented entries

- **Custom stream mark/reset forwarding:** `java_stream.go` checks the
  platform's `Markable` flag instead of forwarding these operations to a guest
  stream. A temporary probe built a guest `InputStream` subclass whose slot 18
  returns true, wrapped it with `DataInputStream`, and observed `markSupported`
  return false. The byte-array path added in #143 works; custom guest overrides
  did not. No new real-title blocker was established.
- **LWC text UI:** `java_widget.go` retains text, children, visibility and input
  mode, but does not lay out or paint the widgets. Text-field, text-box and shell
  `keyNotify` handlers return false; input listeners are stored without being
  fired. A title relying on these widgets has no complete editing interaction.
  See the published
  [TextFieldComponent](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/TextFieldComponent.md)
  and [InputMethodHandler](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodHandler.md)
  contracts.
- **WIPI-C input:** `wipic_im.go` advertises `EN/L`, `EN/S`, `KO`, and `N123`,
  but only numeric key completion is implemented. English multi-tap and Hangul
  composition remain absent at this boundary.
- **Platform static fields:** `java_runtime.go` initializes only `System.out`
  and `System.err`. Other platform-owned fields remain zero-filled. This is a
  compatibility watch, not proof that a current title reads a missing field;
  compile-time constants may already be embedded in guest code.

The WIPI-C database block is also intentionally partial: database listing can
answer an empty list, while opening and manipulating a C database is absent.
This is distinct from the implemented Java database service. Network refusal
is an intentional offline policy, with narrowly recognized authentication
adapters; an unavailable remote service is not automatically a missing API.

<a id="lgt-the-coverage-scanner-can-hide-java-gaps"></a>

### The coverage scanner can hide Java gaps

`imports.go:importImplemented` treats a Java slot as implemented when
`javaSlotName` returns a nonempty name. `trace.go:javaSlotName` can produce
`Class.slotN` for an unsupported virtual slot, or a metadata name for a method
with no handler. Naming a call does not prove that dispatch can serve it.

A temporary Go overlay probe prepared the classes in the first table, then
called `callJavaPlatformVirtual` for the eleven listed slots. All eleven
returned `served=false` and no error, while `importImplemented` returned true
for the same encoded slot. This reproduces the predicate defect independently
of archive content. Fixing the predicate alone would still leave a coverage
limit: `apiscan` records resolved imports at startup, not all future virtual
dispatches or lazily resolved imports. The separate API-demand probe also
checks named map entries rather than modeling every inherited baked slot.

The corpus import scan at that revision processed 31 archives and reported five
unserviced imports, each resolved by four archives: Java auxiliary entries
`0x1f8/0x17`, `0x1fc/3`, `0x1ff/3`, `0x201/3`, and C-library slot `0x32`.
It reported no WIPI-C gap at startup. Resolution alone does not establish that
any of these five imports is called, and this result does not mean Java has
only five missing methods.

<a id="lgt-validation-and-limits"></a>

### Validation and limits

- `go test ./internal/platform/lgt`: passed.
- `go test -tags debug ./internal/platform/lgt`: passed.
- Temporary Go overlay probes: reproduced eleven unsupported dispatches and
  their scanner false positives, plus the lost guest mark-support override.
- `WFEATURE_LGT_ACCEPTANCE=1 go test ./internal/platform/lgt -run
  '^TestLocalLGTArchivesBootAndPaint$' -json -count=1 -timeout=5m`: 31 passed,
  two failed before execution with `LGT archive has no app_info`, out of the
  33 ZIP files currently in `var/games/lgt`. The command therefore failed;
  this is not a clean whole-folder acceptance result. The two input failures
  are separate from missing runtime APIs, and their packaging was not repaired.
- `go run ./internal/tools/apiscan -games var/games/lgt`: 31 scan results and
  two input errors, with the limitations above.

Raw audit output and the temporary Go overlay are in
`/tmp/wfeature-lgt-audit/`; they are disposable local evidence, not fixtures.
No game archives or user saves were changed. The probe uses isolated test
saves. No full playthrough, new browser run, audible playback check, or
save/progression route was performed. Repository-wide race, vet and build
gates were not rerun for this documentation-only investigation.

The audit recommended correcting the scanner's implementation predicate and
making its coverage boundary explicit, then tracing denser menu/save/load routes
before selecting missing methods to implement. Historical counts and
limitations in `lgt.md` are not a current corpus measurement.


<a id="ktf-skt-ktf-and-skt-coverage-audit"></a>

## KTF and SKT audit (2026-09-12, 2b56bad)

This investigation extends the [LGT audit](#lgt-lgt-coverage-audit-after-pr-143)
at revision `2b56bad`. No runtime code changed. Missing APIs, deliberately
limited behavior and unverified gameplay paths are separate findings. SGS
development was paused, and this audit did not resume it.

<a id="ktf-skt-subsequent-work"></a>

### Subsequent work

The tables below keep the initial audit findings at `2b56bad`. The managed
follow-up repaired KTF shutdown and SKT CLDC stream interfaces, added supported
[native keyboard input](../native-text-input.md), and recorded save and
stream-call evidence. Current results and limits are in the
[KTF follow-up](ktf.md#followup-ktf-compatibility-follow-up) and
[SKT follow-up](skt.md#followup-skt-compatibility-follow-up-2026-09-12). The historical widget description
does not request handset keypad composition; the user selected Host keyboard/IME
input instead.

<a id="ktf-skt-ktf"></a>

### KTF

| Area | Current gap and evidence |
|---|---|
| Animated WIPI images | `Image.loadImage(String, ImageObserver)` is deliberately unregistered in `internal/platform/ktf/testdata/wipi_java_gaps.txt`. Animated decoding and completion callbacks are absent. `play` and `stop` already handle the nonanimated case. |
| Graphics alpha | `runtimeGraphicsSetAlpha` in `runtime_graphics.go` stores the value, but drawing does not use it for blending. |
| LWC and KFC widgets | The fixed-value stub inventory includes geometry, layout, form display and layer operations. Existing text editing is not a complete widget renderer; shared Hangul composition is absent. |
| Media callbacks | `Clip.setListener` is a no-op; playback completion is not delivered through it. Recording is refused and several guest volume APIs retain fixed answers, although ordinary clip playback works. |
| Java filesystem | `FileSystem.mkdir/rmdir` are no-ops, while `isDirectory` and creation-time queries return zero. These are distinct from the WIPI-C filesystem. |
| Shutdown lifecycle | `Session.Close` stops audio and guest workers without invoking guest `destroyApp`. Base-class callback declarations do not prove Host shutdown calls a guest override. |
| Earlier native/BREW packages | The documented loading stall remains unresolved. The local execution probe skipped because the current KTF directory contains no native package; this audit supplies no new reproduction or completion evidence. |

Superseded in part: `Clip.setListener` now delivers playback events; see
[Java WIPI playback listeners](../audio.md#java-wipi-playback-listeners).

The gap inventory also includes bridge constructors, a class initializer and
an abstract callback omitted because of the runtime architecture; those are
not all missing user features. The fixed-value inventory likewise mixes actual
limitations with truthful constants and valid empty base methods. Existing
surface and stub-inventory tests passed.

The image contract was checked against the published
[WIPI Image specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Image.md).
The unresolved vendor callback mapping still needs caller evidence.

<a id="ktf-skt-skt-java"></a>

### SKT Java

| Area | Current gap and evidence |
|---|---|
| MIDP graphics | `Graphics.copyArea`, `fillTriangle`, and `setStrokeStyle` are missing. A temporary probe on a started fixture runtime confirmed `HasMethodBody=false`. |
| Stream-based image construction | `Image.createImage(InputStream)` is missing; the live-runtime probe confirmed it. Resource-name, byte-array and several other overloads exist. |
| Core stream interfaces | `java/io/DataInput` and `java/io/DataOutput` are absent from the core class definitions. Local archive references include their methods; the runtime probe found no body for their `readInt`/`writeInt` entries. Concrete `DataInputStream` and `DataOutputStream` support is a different surface and exists. |
| SIS Java images | `SISImage.getFrame/getObject` cannot return decoded content; paint handlers use `ignoreVoid`. This is the Java API, distinct from the SGS SIS path. |
| 3D | `m/XO_World.draw` uses `micro3DIgnore`; SKVM `Graphics3D` has state handling but no rasterizer. Existing vector/transform arithmetic does not draw a model. |
| Text input | Shared Latin multi-tap and numeric editing exist; Hangul composition does not. |
| Vendor browser/network classes | `com/xce/jam/XBrowser` and `com/xce/net/Socket` are absent, including referenced `setNetworkMode` and `setPPPPreserveTime`. Adding those contracts is distinct from supplying a remote service; networking is intentionally refused. |

Public contracts were checked against the published MIDP
[Graphics](https://mirusu400.github.io/wipi-wiki/midp/java-api/javax/microedition/lcdui/Graphics.md)
and [Image](https://mirusu400.github.io/wipi-wiki/midp/java-api/javax/microedition/lcdui/Image.md)
pages, and CLDC
[DataInput](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataInput.md)
and [DataOutput](https://mirusu400.github.io/wipi-wiki/cldc/java-api/java/io/DataOutput.md).
Vendor SIS/3D findings come from repository handlers, not a guessed WIPI contract.

**Some older limitation lists were stale.** SKT runtime Canvas pointer events pass
through `Runtime.SendPointer` to the active canvas and have regression tests.
The shared session still rejects SKT pointer input and advertises pointer support
only for KTF, so this does not establish browser touch support. Arcs and
rounded rectangles are registered and covered by curve tests. Vibration requests
reach the shared Host boundary. These were not wholly unimplemented despite
older statements in `skvm.md` and `jvm.md`.

<a id="ktf-skt-scanner-limits"></a>

### Scanner limits

KTF `apiscan` searches class-name strings and descriptor references, not full
method contracts. It reported `PluginJlet` in 35 archives and WIPI `Socket` in
11. A string or return-type reference does not prove that a guest resolves or
calls that class. The scan cannot detect no-op bodies, absent callbacks or
unsupported overloads on an otherwise published class.

SKT's bare scan reported 24 distinct class/method entries. Supplying actual
native registrations from a started repository fixture with `-natives` removed
nine method entries, leaving 15: four classes and eleven methods on them.
Each remaining entry was referenced by one archive; the entries overlap by
class and are not fifteen independent game failures. The removed entries
included working device backlight and audio-volume methods. The documented
`-natives` input is necessary here.

The remaining four classes are the two core interfaces and two vendor classes
listed above. Static references alone do not show when those branches execute.
All fifteen SKT archives then in the corpus reached a first frame. Neither
scanner proves complete semantics: a declared no-op is accepted as a method.

<a id="ktf-skt-validation-and-limits"></a>

### Validation and limits

- Ordinary and debug package suites passed for `internal/platform/ktf`,
  `internal/platform/skt`, `internal/api/midp`, `internal/api/skvm`, and
  `internal/jvm` (cached results).
- Fresh KTF first-frame probe: 35 passes and one failure among 36 ZIP files.
  The failure was `KTF archive has no __adf__`. A separate content-classification
  probe established **35 KTF and one LGT** archive in that directory. This is a
  misplaced-platform input, not a demonstrated KTF runtime defect; nothing was
  moved. The whole-folder command still exited nonzero.
- Fresh SKT boot-and-paint probe: 15/15 passed, at 300 ticks per archive.
  Content classification confirmed all fifteen are SKT.
- Fresh KTF native-package probe: skipped with
  `no local KTF native package present`.
- Temporary Go overlay probes confirmed the selected missing SKT method bodies,
  absent class declarations, working arc/backlight controls, and classifications.
  Fixture diagnostics supplied the informed SKT scan.

The existing probes were `TestLocalKTFArchivesRenderFirstFrame`,
`TestLocalSKTArchivesBootAndPaint`, and `TestLocalKTFNativePackageRuns`, with
their documented opt-in environment variables and `-count=1`. Scans used
`go run ./internal/tools/apiscan -games var/games/<platform>`, plus the SKT
variant with `-natives <fixture-diagnostics.json>`. Disposable logs, diagnostics
and overlays are under `/tmp/wfeature-platform-audit/`. No user save store was
attached to the KTF probe; SKT corpus tests used temporary saves.

No full gameplay, in-game save/reload routes, new browser checks, audio listening
checks, or repository-wide race/vet/build gates were run. The current folders
are smaller than historical corpora. These Java probes exercised no SGS archive;
its paused UI/communication, extended-image and real-playback/progression work
remains as recorded in [sgs-completion.md](../sgs-completion.md#paused-at-user-request).

The audit recommended prioritizing save/menu/exit routes and missing class
resolution over implementing every standard member. The unresolved native KTF
loading case needed its archive located before it could be remeasured.

<a id="cross-platform-parity-2026-11-05"></a>

## Cross-platform parity and what the specification settles — 2026-11-05

Before 0.6.0 the open specification gaps (the 2026-10-21 comparison kept in the
local task list) were sorted by one question: which of them can be implemented
without guessing. A member qualified when three things held:

- its category is not one the 2026-10-22 scope decision left out (exception
  members, floating point and the `MC_math` family, calendar internals,
  multi-program management, recording, handset services, LEDs);
- where a call arrives is settled: KTF and SKT resolve every Java member by
  class, name and descriptor; LGT resolves every `org/kwis` member by the name
  the module hands over, and places a `java/*` virtual call by the
  declaration-order rule its watched slots spell
  ([the class library's core](lgt.md#lgt-class-library-2026-10-28));
- what it answers is settled: another platform here already answers it, or
  CLDC 1.1 gives one answer.

**The comparison was taken from the tables, not from the 2026-10-21 lists.**
Temporary package tests (not kept) wrote out every member each platform serves
at `b377335`: KTF's `runtimeJavaClasses` (891 entries); LGT's name tables
(`javaPlatformMethods`, `javaGraphicsMethods`, `javaWidgetMethods`,
`javaDatabaseMethods`) and `javaBakedVirtualSlots` (584 lines with the
superclass map); and SKT's `jvm.CoreLibraryDefinitions` with the MIDP, SKVM and
WIPI definitions (1,501). A member counted as served when its class or any
superclass in that platform's own hierarchy declares it: SKT's WIPI `Graphics`,
`Font`, `Image`, `Card` and `Display` extend the MIDP classes, and LGT walks
`javaPlatformSuper` for a baked slot.

What the comparison changed in the earlier lists:

- **No LGT `org/kwis` member uses a baked slot.** Every one LGT serves is in a
  name table, and `javaPlatformVirtualDispatch` resolves a vtable stub through
  `javaVirtualMember` to the name the module listed for it. The caution the
  2026-10-28 plan attached to LGT's `org/kwis` virtual methods (a slot needs a
  watched anchor) applies to `java/*` classes only.
- **CLDC 1.1 members missing from the WIPI 1.2.1 pages were not counted.** LGT's
  class library is CLDC 1.1's (`Thread.interrupt` at slot 12 settled that), so
  `Random.nextInt(int)` was a member it lacked.
- **Members after `isXORMode` on the truncated `Graphics` page were not
  counted**: `setPixels`, `setStrokeStyle`, `setXORMode`, `setRGBPixels` and
  `setGrayScale`, which KTF implements.
- **`ByteArrayOutputStream.toString()` answered Object's identity string on all
  three platforms**, not the bytes as text.

Left out because the specification does not settle the answer:

- `DataComparatorInteger`, `DataComparatorString`, `DataFilterInteger`: the
  pages say "compare as an integer or a string at this offset" and name a
  minimum and maximum, but not the integer's width or byte order, where a string
  ends, whether the bounds are inclusive, or what a record shorter than the
  offset does.
- `Graphics.encodeImage`: no output format.
- Animated images (`isAnimated`, `play`, `stop`, `stopImage`, `loadImage`,
  `ImageObserver`): no notification timing or status codes.
- Event hooks and key grabs (`addJletEventListener`, `setJletEventListener`,
  `grabKey`, `ungrabKey`, `setDockedCard`, `Display.where`,
  `EventQueue.hookEvent`, SKT's and LGT's `EventQueue`): the handset's event
  flow.
- The rest of `InputMethodHandler`: the handset's input-method UI; text entry
  goes through the page's text input instead.
- SKT `HandsetProperty.getSystemProperty`: the keys and values are a handset's.
- The remaining media members, and every remaining C gap: a left-out category,
  or a slot that would be estimated. The KTF kernel's slot 5, which the spec
  order put at `MC_knlExecute`, turned out to be `MC_knlLoad` in
  [#260](https://github.com/movingwoo/wfeature/pull/260).

Settled but more than a member fill, and done as changes of their own in the
same pass: LGT `Timer`/`TimerTask` (its threads and quick-save record), LGT
cards with a position (`Card(x, y, w, h)`, `move`, `resize`, `getX`, `getY`,
`isShown`), the LGT Jlet's pause, resume and destroy (its launcher only
entered `startApp`), and `Thread.interrupt` on LGT (waking a sleeping guest
thread).

**What was filled**, with the detail in each platform's record (SKT's below):

- KTF and SKT, in the shared class library: the `String`, `StringBuffer`,
  `Vector`, `Hashtable`, `Reader`, `InputStreamReader`, `Runtime` and `Class`
  members LGT already answered, `Writer` and `OutputStreamWriter`, the static
  `DataInputStream.readUTF(DataInput)`, and a `ByteArrayOutputStream`,
  `Vector`, `Stack` or `Hashtable` that answers its own text; KTF also
  `Timer`'s Date forms ([KTF](ktf.md#ktf-parity-fill-2026-11-05)).
- LGT: every `org/kwis` member above that KTF or SKT answers, `Boolean`,
  `Character`, `Writer`, `OutputStreamWriter`, the `Reader` and `OutputStream`
  bodies a title's subclass inherits, `Random.nextInt(int)`, the C image
  error `M_E_BADFORMAT` at -6, and `EOFException` at the end of a data stream
  ([LGT](lgt.md#lgt-parity-fill-2026-11-05)).
- SKT's WIPI `Graphics` and `Image`: the members KTF answers, XOR mode over
  every drawing call ([below](#cross-platform-parity-2026-11-05-skt-graphics)).
- LGT, as changes of their own: `Timer` and `TimerTask`, `Thread.interrupt`,
  cards with a position, and the Jlet's `pauseApp`, `resumeApp` and
  `destroyApp`, which no LGT Java title had ever been sent
  ([LGT](lgt.md#lgt-parity-fill-2026-11-05)).

Two members were withheld where the platforms or the page disagree on the
answer itself: `getGrayScale` (KTF answers the packed colour, the page the grey
level) and SKT's byte pixel transfers (a handset's own LCD format). Where KTF is
lenient and the page would throw or clip, the new members are lenient too, so
the three platforms answer alike until a title shows otherwise; where the page
states a value outright and KTF differs, the page is followed and the
difference recorded (`createImage(Image)` turns the mask white).

**Validation.** `gofmt`, `go vet` and `go test` with and without the debug
tag, `go test -race ./internal/...`, the 569 page tests and the eight audio
harnesses in both engines all pass. The 607 local archives were run 500 ticks
from an empty save under the build before (`b377335`) and this one: every KTF
and LGT archive ends on the same digest; 26 SKT archives differ, every one of
them also between runs of a single build, because SKT runs on the wall clock,
and three compared by their last frame show the same scene at a different
moment of its animation. A quick save taken at 300 ticks under the build
before loads under this one for each of the 587 archives that made one, and
100 ticks later reaches the digest the build before reaches from it, except
six SKT archives from that same wall-clock list. After the LGT timer, card and
lifecycle changes the gates passed again, and the 148 LGT archives were run once
more under both builds: the 500-tick digests, a quick save the build before
took at 300 ticks loaded here, and a two-second park at tick 300 through
`run -serve` followed by 300 ticks all end on the same digest, with no warning
or error the build before did not log.

**A review on 2026-11-06 played deeper** and found what those runs, which end
on title screens, could not reach. The 481 KTF and LGT archives were played
about 1,900 ticks through `run -serve`, a key every 42 ticks and a quick save
halfway, under both builds:

- 472 match at every step. Six of the rest are a quick save refused as busy
  while eight runs shared the machine — three under each build — which shifts
  the replies after it, and two are titles whose runs differ under one build.
- **One KTF title reaches a member this pass changed**: it splits the
  `toString()` of a byte array stream it read a text resource into. With
  Object's identity text its dialog box stayed empty and its paint threw
  ArrayIndexOutOfBoundsException every frame; it now shows its lines
  ([KTF](ktf.md#ktf-parity-fill-2026-11-05)).
- A quick save the build before took halfway loads here and plays on to the
  same digests for every archive. One taken here loads under the build before
  for every LGT archive and is refused there for 180 KTF archives, whose heap
  holds `buffer-v2` buffers; that is a step back to an earlier build only.
- **The 24 LGT Java archives parked in the middle of play** showed what a park
  on a title screen does not: one title stood still after every park, its
  frame chain running twice a tick, until the serial calls it had queued were
  served after `pauseApp` ([LGT](lgt.md#lgt-parity-fill-2026-11-05)).
- SKT: 595 WIPI Graphics calls made through the VM on an off-screen image —
  every fill, line, curve, text and picture form under clips, translations,
  alpha, every anchor and bad arguments — leave the same pixels and throw the
  same exception classes under both builds. Forwarding to MIDP costs 0.03 to
  0.26 µs a call, and CPU time moved by no more than 1% on three titles of each
  platform.

<a id="cross-platform-parity-2026-11-05-skt-graphics"></a>

### SKT's WIPI Graphics

The comparison found WIPI `Graphics` members KTF serves and SKT did not. SKT
now answers `copyArea`, `drawPolygon`, `setPixel`, `getPixel`,
`getRed/Green/BlueComponent`, `setXORMode`/`isXORMode` and
`setStrokeStyle`/`getStrokeStyle`, and `Image.createImage(Image)`, as KTF does
for the same calls (`internal/api/wipi/graphics.go`). No local SKT title is
known to call them.

- **They are built from calls the platform already registers**: MIDP's fills,
  lines, curves, text and blits, the clip and translation getters, `getRGB`
  and the WIPI RGB pixel pair. SKT's own drawing state is unexported, so the
  library composes them rather than reaching into it.
- **XOR mode reaches every drawing call.** In XOR mode the platform's own call
  is drawn onto a scratch picture and each pixel it covered is inverted once,
  by its coverage where a picture's edge is blended, as KTF blends a glyph's
  edge. KTF inverts on every write instead, so where two strokes of one call
  meet — a rectangle's corners, a polygon's shared vertex — KTF leaves the
  pixel and SKT inverts it. A call whose bounds miss the clip is still made on
  the Graphics, so its arguments are checked as outside XOR mode.
- **`reset`, `drawChar` and the source-rectangle `drawImage` moved from the
  platform to the library** (`internal/platform/skt/wipi.go` no longer
  registers them), because a native the platform registers cannot also have a
  library body: `drawChar` and the 8-argument `drawImage` now honour XOR mode,
  and `reset` puts XOR off and the stroke back to SOLID as KTF's does.
- **A paint starts with XOR off and the solid stroke.** SKT reuses one screen
  Graphics for the session and puts its clip, translation and font back at the
  start of each paint; a probe showed XOR mode and a stroke style set in one
  paint still set at the next, where KTF hands each paint a new Graphics, so
  the paint start now puts those two back as well.
- `createImage(Image)` follows the specification's page: an editable copy,
  the masked part turned white and no mask kept. KTF's copy keeps the
  transparency.
- **Not served**: `getGrayScale`, because KTF answers the packed colour where
  the page says the grey level (and SKT's `setGrayScale` refuses anything over
  255, so KTF's answer would not even round-trip); and the byte forms
  `getPixels`/`setPixels`, whose layout is a handset's LCD format — KTF's two
  RGB565 bytes are its 16-bit panel's, and SKT's display answers 24 bits a
  pixel.
- Where KTF is lenient and the specification would throw or clip — a stroke
  style other than SOLID or DOTTED, polygon arrays of different lengths — SKT is
  lenient too; the stroke style is kept and answered and lines stay solid.
- XOR mode and the stroke style are entries in the Graphics object's field map
  under undeclared `host:` keys, so the heap record keeps them and the class
  layout the cheat panel reads is unchanged; an earlier quick save restores
  them off and SOLID.

Found and left: WIPI `Image.createImage(Image, int, int, int, int)` is declared
native but nothing registers it.
