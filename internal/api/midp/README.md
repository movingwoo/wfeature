# MIDP class library

`definitions.go` declares the MIDP surface in Go: the class metadata a game
links against, with the Go body of every method this layer implements itself.
There are no Java sources and no class files here, and no JDK in the build.
`Define` installs the whole surface on a VM, and a declared class outranks one
the game's own archive ships under the same name.

The bodies in `lcdui.go` and `rms.go` are the part MIDP defines in terms of
other MIDP calls. Everything that touches the Host — pixels, key state, the
current screen, the save directory — is declared native here and registered by
`internal/platform/skt`.

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
`docs/lcdui.md`. The `javax.microedition.media` surface holds `Manager`, `Player`, and
`MediaException`. The runtime-owned font has authored printable-ASCII glyphs and uses a
deterministic marked box for other Unicode codepoints until an external font can
be approved separately.

`javax.microedition.rms` is the record store surface: `RecordStore` with its
five exceptions, the `RecordFilter`/`RecordComparator`/`RecordListener`
interfaces, `RecordEnumeration`, and the runtime's `RecordSet` implementation
of it. `internal/platform/skt/rms.go` holds the records and persists them
through the Host save boundary. See `docs/rms.md`.

## Compiling a fixture against this surface

A test MIDlet is still Java, and `javac` needs signatures to compile it
against. `internal/tools/javastub` writes them out of these same declarations:

```sh
stub_dir="$(mktemp -d /tmp/wfeature-stubs.XXXXXX)"
go run ./internal/tools/javastub -out "$stub_dir/src"
(cd "$stub_dir/src" && javac -source 1.8 -target 1.8 -nowarn -d "$stub_dir/classes" \
  $(find javax com net -name '*.java'))
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" -d "$fixture_dir" Fixture.java
```

The stubs are signatures only. The fixture runs on the runtime, not on them.
