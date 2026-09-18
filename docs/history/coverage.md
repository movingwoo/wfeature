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

<a id="lgt-lgt-coverage-audit-after-pr-143"></a>

## LGT audit (2026-09-12, 2b56bad)

This is an investigation of revision `2b56bad`, after
[PR #143](https://github.com/movingwoo/wfeature/pull/143) was merged.
No runtime implementation changed during this audit. The PR repaired measured
startup and initial gameplay paths; it did not complete the LGT Java library.

<a id="lgt-subsequent-work"></a>

### Subsequent work

This snapshot predates the managed follow-up. The scanner now uses actual Java
dispatch coverage, and custom stream mark/reset operations reach guest overrides;
see the [platform implementation](lgt.md#implementation-the-lgt-platform). Focused Java LWC fields now support
[native keyboard input](../native-text-input.md), with session, WebSocket, and
browser proof from an authored AOT archive. These changes do not complete the
Java library or prove the missing methods below execute in current archives.

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

These gaps can stop a title if it reaches them. The first-frame corpus check
below did not reach them, so they are not eleven newly observed game crashes.

<a id="lgt-partial-behavior-behind-implemented-entries"></a>

### Partial behavior behind implemented entries

- **Custom stream mark/reset forwarding:** `java_stream.go` checks the
  platform's `Markable` flag instead of forwarding these operations to a guest
  stream. A temporary probe constructed a guest `InputStream` subclass whose
  slot 18 returns true, wrapped it with `DataInputStream`, and observed
  `markSupported` return false. The byte-array path added in #143 works, but
  custom guest overrides still do not. No new real-title blocker was established.
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

The current corpus import scan processed 31 archives and reported five
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

The next useful work is to correct the scanner's implementation predicate and
make its coverage boundary explicit, then trace denser menu/save/load routes
before selecting missing methods to implement. Existing historical counts and
limitations in `lgt.md` should not be read as a current corpus measurement.


<a id="ktf-skt-ktf-and-skt-coverage-audit"></a>

## KTF and SKT audit (2026-09-12, 2b56bad)

This investigation extends the [LGT audit](#lgt-lgt-coverage-audit-after-pr-143) at revision
`2b56bad`. No runtime code changed. Missing APIs, deliberately limited behavior,
and unverified gameplay paths are separate findings. SGS development remains
paused; this audit does not resume it.

<a id="ktf-skt-subsequent-work"></a>

### Subsequent work

The tables below retain the initial audit findings at `2b56bad`. The managed
follow-up repairs KTF shutdown and SKT CLDC stream interfaces, adds supported
[native keyboard input](../native-text-input.md), and records actual save and
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

The gap inventory also includes bridge constructors, a class initializer and
an abstract callback omitted because of the runtime architecture. Those are
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

**Some old limitation lists are stale.** SKT runtime Canvas pointer events pass
through `Runtime.SendPointer` to the active canvas and have regression tests.
The shared session still rejects SKT pointer input and advertises pointer support
only for KTF, so this does not establish browser touch support. Arcs and
rounded rectangles are registered and covered by curve tests. Vibration requests
reach the shared Host boundary. These are not wholly unimplemented despite
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
All fifteen current SKT archives reached a first frame. Neither scanner proves
complete semantics: a declared no-op is accepted as a method.

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

Prioritize actual save/menu/exit routes and missing class resolution before
implementing every standard member. The unresolved native KTF loading case
needs its archive located before it can be remeasured.
