# MIDP class library

`definitions.go` declares the MIDP surface in Go: the class metadata a game
links against, with the Go body of every method this layer implements itself.
`Define` (`library.go`) installs the whole surface on a VM, and a declared class
outranks one the game's own archive ships under the same name. There is no JDK
in the build. The one class file is the runtime's timer worker, described below.

The bodies in `lcdui.go`, `rms.go` and `timer.go` are the parts MIDP defines in
terms of other MIDP calls. Everything that touches the Host (pixels, key state,
the current screen, the save directory, sound) is declared native here and
registered by `internal/platform/skt`.

## Surface

- **`javax.microedition.midlet`.** Besides the lifecycle callbacks, `MIDlet`
  exposes `getAppProperty`, `notifyPaused`, `notifyDestroyed` and
  `resumeRequest` as natives, which the platform connects to the backend event
  queue and the manifest. `MIDletStateChangeException` provides MIDP 2.0
  conditional lifecycle refusal. The platform applies the MIDP unchecked-failure
  policy: a runtime exception from start or pause gets one forced destroy
  cleanup call, and one raised by destroy itself is ignored.
- **`javax.microedition.lcdui`, low level.** `Display`, `Displayable`,
  `Canvas`, `Graphics`, `Image` and `Font`. `Display.getDisplay`, delayed
  `setCurrent`, `getCurrent` and `isShown` run on the shared backend event
  queue. `callSerially` queues a Runnable that the Host's next pass runs, one
  per pass; see
  [the frame loop](../../../docs/history/skt.md#implementation-the-frame-loop)
  for why. A current `Canvas` receives coalesced `paint(Graphics)` callbacks.
  `Graphics` supports color overloads, clip and translation state, shapes
  including arcs and rounded rectangles, decoded or ARGB images, transformed
  regions, alpha blending and text, drawn into a Host-owned RGBA framebuffer or
  a mutable off-screen image. Coordinates and untrusted array ranges are
  validated before rasterization. Host key press, release and repeat events and
  pointer press, release and drag events reach the current active `Canvas` as
  protected callbacks on the same queue. Game-action and key-name mapping,
  full-screen state and input capability queries are implemented.
- **Text.** Characters the runtime-authored 5x7 table in `internal/glyph`
  covers are drawn from it; every other character, Hangul included, is drawn
  and measured with the embedded handset pixel face from the same package
  (`internal/platform/skt/font.go`). Only a character neither covers falls back
  to a deterministic codepoint-marked box.
- **`javax.microedition.lcdui`, high level.** `Command`, `Item` and its
  subclasses, `Screen`, `Form`, `List`, `TextBox`, `Alert`, `Ticker` and
  `game.GameCanvas` are drawn and navigated by the runtime rather than by the
  application; see [`docs/lcdui.md`](../../../docs/lcdui.md).
- **`javax.microedition.media`.** `Manager`, `Player`, `PlayerListener`,
  `MediaException`, `Control`, `Controllable` and `control.VolumeControl`,
  with the runtime's `net/wfeature/RuntimeVolumeControl` behind the last; see
  [`docs/audio.md`](../../../docs/audio.md).
- **`javax.microedition.rms`.** `RecordStore` with its five exceptions, the
  `RecordFilter`/`RecordComparator`/`RecordListener` interfaces,
  `RecordEnumeration`, and the runtime's `RecordSet` implementation of it.
  `internal/platform/skt/rms.go` holds the records and persists them through the
  Host save boundary; see [`docs/rms.md`](../../../docs/rms.md).
- **`javax.microedition.io`.** The Generic Connection Framework classes exist so
  a game that names one resolves it; `Connector` refuses every name. See
  [`docs/network.md`](../../../docs/network.md).
- **`java.util.Timer` and `TimerTask`.** They are MIDP's rather than CLDC's,
  so they are declared here. A task runs on a guest thread of its own, as the
  specification says, because a title that schedules a fixed-rate task keeps
  redrawing while it runs. The worker, `net/wfeature/TimerThread`, is the one
  class shipped as bytecode: `java/net/wfeature/TimerThread.java` is compiled to
  the embedded `TimerThread.class` and installed with `DefineClassFile`, so a
  parked task keeps every local and its exact return point for checkpoints.
  `TimerCheckpointTask` beside it is a test fixture for that path.

## Compiling a fixture against this surface

A test MIDlet is still Java, and `javac` needs signatures to compile it.
`internal/tools/javastub` writes them out of the core, MIDP and SKVM
declarations (WIPI Java is not included):

```sh
stub_dir="$(mktemp -d /tmp/wfeature-stubs.XXXXXX)"
go run ./internal/tools/javastub -out "$stub_dir/src"
mkdir -p "$stub_dir/classes"
(cd "$stub_dir/src" && javac -source 1.8 -target 1.8 -nowarn -d "$stub_dir/classes" \
  $(find javax com net m -name '*.java'))
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" -d "$fixture_dir" Fixture.java
```

The `java` stubs are not compiled, so `java.*` comes from the JDK. The `mkdir`
is needed with JDK 8, whose `javac` does not create the `-d` directory. The stubs are signatures only; the fixture runs on the
runtime, not on them.
