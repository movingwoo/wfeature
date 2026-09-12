# KTF and SKT coverage audit

This investigation extends the [LGT audit](lgt-audit-2026-09-12.md) at revision
`2b56bad`. No runtime code changed. Missing APIs, deliberately limited behavior,
and unverified gameplay paths are separate findings. SGS development remains
paused; this audit does not resume it.

## KTF

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

## SKT Java

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

**Some old limitation lists are stale.** SKT Canvas pointer events now pass
through `SendPointer` to the active canvas and have regression tests. Arcs and
rounded rectangles are registered and covered by curve tests. Vibration requests
reach the shared Host boundary. These are not wholly unimplemented despite
older statements in `skvm.md` and `jvm.md`.

## Scanner limits

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

## Validation and limits

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
remains as recorded in [sgs-completion.md](sgs-completion.md#paused-at-user-request).

Prioritize actual save/menu/exit routes and missing class resolution before
implementing every standard member. The unresolved native KTF loading case
needs its archive located before it can be remeasured.
