# MIDP class library

`java/` contains runtime-owned Java API sources authored for `wfeature`.
Compiled class files in `classdata/` are embedded into the Go runtime so users
do not need a JDK installed.

In addition to lifecycle callbacks, the current `MIDlet` class exposes
`getAppProperty`, `notifyPaused`, `notifyDestroyed`, and `resumeRequest` as native
methods. `internal/platform/skt` connects them to the backend event queue and
manifest. `MIDletStateChangeException` is also provided for MIDP 2.0 conditional
lifecycle refusal. The platform runtime also applies the MIDP unchecked-failure
policy: start/pause runtime exceptions receive one forced destroy cleanup call,
while runtime exceptions raised by destroy itself are ignored.

The minimal LCDUI library currently includes `Display`, `Displayable`, `Canvas`,
`Graphics`, `Image`, and `Font`. `Display.getDisplay`, delayed `setCurrent`, `getCurrent`, and
`isShown` are implemented by `internal/platform/skt` on the shared backend
event queue. `callSerially` queues a Runnable that the Host's next pass runs;
see `docs/skvm.md` for why one per pass rather than all of them. A current `Canvas` receives coalesced `paint(Graphics)` callbacks;
the initial `Graphics` surface supports color overloads, clip and translation
state, shapes, decoded or ARGB images, transformed regions, alpha blending, and
bitmap text drawing into a Host-owned RGBA framebuffer or mutable off-screen
image. Coordinates and untrusted array ranges are validated before
rasterization. Host key press, release, and repeat events plus pointer press,
release, and drag events are delivered to the current active Canvas as protected
callbacks on the same queue. Game-action/key-name mapping, full-screen state,
and input capability queries are implemented. The high-level surface — `Command`, `Item` and its subclasses, `Screen`,
`Form`, `List`, `TextBox`, `Alert`, `Ticker`, and `game.GameCanvas` — is
drawn and navigated by the runtime rather than by the application; see
`docs/lcdui.md`. `javax/microedition/media` holds `Manager`, `Player`, and
`MediaException`. The runtime-owned font has authored printable-ASCII glyphs and uses a
deterministic marked box for other Unicode codepoints until an external font can
be approved separately.

`javax/microedition/rms/` is the record store surface: `RecordStore` with its
five exceptions, the `RecordFilter`/`RecordComparator`/`RecordListener`
interfaces, `RecordEnumeration`, and the runtime's `RecordSet` implementation
of it. `internal/platform/skt/rms.go` holds the records and persists them
through the Host save boundary. See `docs/rms.md`.

Regenerate the current minimal library with Java 8 class format:

```sh
javac -source 1.8 -target 1.8 -g:none -d internal/api/midp/classdata \
  internal/api/midp/java/javax/microedition/midlet/MIDletStateChangeException.java \
  internal/api/midp/java/javax/microedition/midlet/MIDlet.java \
  internal/api/midp/java/javax/microedition/lcdui/Displayable.java \
  internal/api/midp/java/javax/microedition/lcdui/Canvas.java \
  internal/api/midp/java/javax/microedition/lcdui/Font.java \
  internal/api/midp/java/javax/microedition/lcdui/Graphics.java \
  internal/api/midp/java/javax/microedition/lcdui/Image.java \
  internal/api/midp/java/javax/microedition/lcdui/Display.java
javac -source 1.8 -target 1.8 -g:none -d internal/api/midp/classdata \
  internal/api/midp/java/javax/microedition/rms/*.java
```

Or regenerate the whole library at once:

```sh
javac -source 1.8 -target 1.8 -g:none -d internal/api/midp/classdata \
  $(find internal/api/midp/java -name '*.java')
```
