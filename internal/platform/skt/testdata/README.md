# SKT fixtures

These are MIDlet JARs, because an SKT title is a MIDlet. Most are bare JARs
built to exercise one part of the runtime directly; `canvas-skt.zip` is the
Canvas fixture wrapped in the shape a handset was actually sent, which is what
the platform-neutral drivers use because a bare JAR is claimed by no vendor.

`arithmetic.jar` is a vertical execution-test JAR containing this repository's
`internal/jvm/testdata/Arithmetic*.class` files. It is not third-party game data.

`lifecycle.jar` contains the newly authored `LifecycleMIDlet.java` and uses the
runtime's minimal built-in MIDP `MIDlet` class to verify manifest app properties,
start/pause/resume/destroy, resume requests, callback failure states, conditional
destroy refusal and retry, and transient start refusal. `deferred-start.jar` uses
the same class with a separate manifest property so the first `startApp()` is
refused and the subsequent resume succeeds. `runtime-failure.jar` makes the
initial `startApp()` throw a runtime exception and verifies forced destroy
cleanup without using third-party game data.

`display.jar` contains the newly authored `DisplayMIDlet.java`. It verifies the
runtime-owned `Display` and `Displayable` classes, constructor-time display
lookup, delayed and coalesced current-screen changes, null requests,
pause/resume visibility, and the `callSerially` queue — including a Runnable
that hands itself straight back, which is the shape a frame loop takes.

`canvas.jar` contains the newly authored `CanvasMIDlet.java`. It verifies the
runtime-owned `Canvas` and `Graphics` classes, automatic initial paint, Host
framebuffer presentation, clipped and coalesced partial repaints, and synchronous
`serviceRepaints`. It also verifies clip and translation state, RGB color
components, clipped oversized fills, extreme-coordinate lines, rectangle
outlines, press/repeat/release callback ordering, repaints requested from key
callbacks, and paused-state input suppression. The same fixture covers drawing
through the Graphics a paint kept after it returned, mutable
and decoded images, alpha and transformed drawing, negative scan lengths, font
metrics, text anchors, and every implemented text entry point without
third-party game data. `FIXTURE.PNG.b64` is a newly generated two-pixel image
source used to reproduce the resource stored in the JAR. `DATA.BIN.b64` is a
newly authored big-endian integer and modified-UTF payload used to verify
`Class.getResourceAsStream` and `DataInputStream`.

`recordstore.jar` contains the newly authored `RecordStoreMIDlet.java`. It
verifies the record store surface end to end — creation, ids that survive
deletion, partial-range writes, listeners, filtered and sorted enumerations
that keep themselves updated, `listRecordStores`, and the closed-store and
missing-store failures — and reports one bit per check. See `docs/rms.md` for
the regeneration recipe.

`connector.jar` contains the newly authored `ConnectorMIDlet.java`. It drives
every `javax.microedition.io.Connector` entry point and checks that each is
refused, that the refusal is catchable as a plain `IOException`, that the
message names the requested target, and that a game's own state machine
reaches its offline state instead of staying in "connecting". See
`docs/network.md` for why refusing is the answer.

`ui.jar` contains the newly authored `UIMIDlet.java`, which covers the
high-level surface: commands and their listener, a List that reports its
selection, a Form with a StringItem, a ChoiceGroup and a TextField, a
TextBox, an Alert, and a GameCanvas buffer. See `docs/lcdui.md` for the
regeneration recipe.

Regenerate the arithmetic fixture with:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-skt-fixture.XXXXXX)"
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/MANIFEST.MF "$fixture_dir/META-INF/MANIFEST.MF"
cp internal/jvm/testdata/Arithmetic*.class "$fixture_dir/"
(cd "$fixture_dir" && zip -X -q /tmp/wfeature-arithmetic.jar \
  META-INF/MANIFEST.MF Arithmetic*.class)
mv /tmp/wfeature-arithmetic.jar internal/platform/skt/testdata/arithmetic.jar
```

The MIDP and SKVM classes a fixture compiles against are declared in Go rather
than shipped as class files, so `javac` needs signatures written out first.
Do this once per session and reuse `$stub_dir/classes` as the classpath below:

```sh
stub_dir="$(mktemp -d /tmp/wfeature-stubs.XXXXXX)"
go run ./internal/tools/javastub -out "$stub_dir/src"
(cd "$stub_dir/src" && javac -source 1.8 -target 1.8 -nowarn -d "$stub_dir/classes" \
  $(find javax com net -name '*.java'))
```

Regenerate the lifecycle fixtures with:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-midlet-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" -d "$fixture_dir" internal/platform/skt/testdata/src/LifecycleMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/LIFECYCLE.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/lifecycle.jar" META-INF/MANIFEST.MF LifecycleMIDlet.class)
cp "$fixture_dir/lifecycle.jar" internal/platform/skt/testdata/lifecycle.jar
cp internal/platform/skt/testdata/DEFERRED_START.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/deferred-start.jar" META-INF/MANIFEST.MF LifecycleMIDlet.class)
cp "$fixture_dir/deferred-start.jar" internal/platform/skt/testdata/deferred-start.jar
cp internal/platform/skt/testdata/RUNTIME_FAILURE.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/runtime-failure.jar" META-INF/MANIFEST.MF LifecycleMIDlet.class)
cp "$fixture_dir/runtime-failure.jar" internal/platform/skt/testdata/runtime-failure.jar
```

Regenerate the display fixture with:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-display-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" -d "$fixture_dir" internal/platform/skt/testdata/src/DisplayMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/DISPLAY.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/display.jar" META-INF/MANIFEST.MF DisplayMIDlet.class 'DisplayMIDlet$Counter.class' 'DisplayMIDlet$Loop.class' 'DisplayMIDlet$FirstScreen.class' 'DisplayMIDlet$SecondScreen.class')
cp "$fixture_dir/display.jar" internal/platform/skt/testdata/display.jar
```

Regenerate the Canvas fixture with:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-canvas-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" -d "$fixture_dir" internal/platform/skt/testdata/src/CanvasMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/CANVAS.MF "$fixture_dir/META-INF/MANIFEST.MF"
base64 -D -i internal/platform/skt/testdata/FIXTURE.PNG.b64 -o "$fixture_dir/fixture.png"
base64 -D -i internal/platform/skt/testdata/DATA.BIN.b64 -o "$fixture_dir/data.bin"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/canvas.jar" META-INF/MANIFEST.MF CanvasMIDlet.class 'CanvasMIDlet$PaintCanvas.class' fixture.png data.bin)
cp "$fixture_dir/canvas.jar" internal/platform/skt/testdata/canvas.jar
```

Regenerate the SKT-shaped Canvas archive — the JAR beside the `.msd` naming it,
which is how a title reaches a handset — with:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-canvas-skt.XXXXXX)"
cp internal/platform/skt/testdata/canvas.jar "$fixture_dir/canvas.jar"
cp internal/platform/skt/testdata/CANVAS.MF "$fixture_dir/canvas.msd"
(cd "$fixture_dir" && zip -X -q canvas-skt.zip canvas.msd canvas.jar)
cp "$fixture_dir/canvas-skt.zip" internal/platform/skt/testdata/canvas-skt.zip
```

Regenerate the connection fixture with:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-connector-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/ConnectorMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/CONNECTOR.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/connector.jar" META-INF/MANIFEST.MF ConnectorMIDlet.class)
cp "$fixture_dir/connector.jar" internal/platform/skt/testdata/connector.jar
```
