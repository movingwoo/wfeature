# SKT fixtures

Every fixture here is newly authored for `wfeature`. No game archive, sound
asset or generated library signature belongs in a JAR; the runtime supplies
the library behavior.

## Compiling

Generate the library signatures once per session with the
[fixture compilation guide](../../../../docs/history/testing.md#implementation-compiling-a-java-fixture);
`$stub_dir/classes` is then the classpath every recipe below uses. Compile with
Java 8 target settings (`-source 1.8 -target 1.8`) and `-g:none`, and package
only the fixture's own classes with its manifest.

The generator does not emit WIPI. Fixtures that use WIPI media add these
compile-only signatures as `org/kwis/msp/media/Clip.java`, `Player.java` and
`PlayListener.java` beside the generated signatures and compile that
classpath:

```java
package org.kwis.msp.media;
public class Clip {
    public Clip(String type, byte[] data) {}
    public native void setListener(PlayListener listener);
    public native String getType();
    public native boolean setVolume(int level);
    public native int getVolume();
}
```

```java
package org.kwis.msp.media;
public class Player {
    public static native boolean play(Clip clip, boolean repeat);
    public static native boolean stop(Clip clip);
    public static native boolean pause(Clip clip);
    public static native boolean resume(Clip clip);
}
```

```java
package org.kwis.msp.media;
public interface PlayListener {
    int ERROR=-1, END_OF_DATA=1, START=2, STOP=3, PAUSE=4, RESUME=5,
        RECORD=6, FULL_OF_DATA=7;
    void playUpdate(Clip clip, int event, int parm);
}
```

## Media

`src/WIPIListenerMIDlet.java`, `wipi-listener.jar` and `WIPI_LISTENER.MF`
exercise integer playback events, single-listener replacement/removal,
deferred reentrant restart and bounded callback history. Go supplies an
authored 400 ms score and tests forced collection and full checkpoints with
active, paused, idle and queued Clip owners. Listener instances retain only
their ID; the fixture can drop its Clip and clear history to prove collection.
The package-private `WIPIListenerExtension` extends `PlayListener`, checking
transitive interface identity during listener installation and restoration.
Compile against the MIDlet signature and the WIPI signatures above; package
`WIPIListenerMIDlet.class` and `WIPIListenerExtension.class` with
`WIPI_LISTENER.MF`.

`src/MediaLifecycleMIDlet.java`, `media-lifecycle.jar` and
`MEDIA_LIFECYCLE.MF` exercise finite repeats, media time, ordered asynchronous
notifications and boxed event payloads. Bounded event history records callback
ownership and whether delivery happened inside a native operation; listeners
compare standard event names by reference and can request one restart or a
volume change. Go supplies the authored 400 ms score and checks full
checkpoint restoration with undelivered events and a previously retained event
name. Compile against the generated MIDP signatures; package only
`MediaLifecycleMIDlet.class` with `MEDIA_LIFECYCLE.MF`.

`src/MediaVolumeMIDlet.java`, `media-volume.jar` and `MEDIA_VOLUME.MF`
exercise MIDP control lookup, interface casts/calls, stable object identity,
fresh arrays, volume/mute getters and change notifications. Go supplies
authored mixed MIDI/PCM bytes and restores the player/control/listener graph
through a full checkpoint. The callback queries the control and can queue
another mute change. Compile against the generated MIDP/SKVM signatures;
package only `MediaVolumeMIDlet.class` with `MEDIA_VOLUME.MF`.

`src/AudioGainMIDlet.java`, `audio-gain.jar` and `AUDIO_GAIN.MF` exercise
SKVM device volume, both WIPI Clip volume signatures and transient MIDP tones.
The Go tests supply authored SMAF bytes, change levels while two clips play,
and restore the clips or an active tone through a complete checkpoint. Compile
against the generated MIDP/SKVM signatures plus the WIPI signatures above;
package only `AudioGainMIDlet.class` with `AUDIO_GAIN.MF`. The void volume
extension is exercised directly by the Go test because Java cannot overload a
method by return type alone.

`src/AudioStopMIDlet.java` and `audio-stop.jar` cover blocking audio calls. A
guest thread distinguishes natural completion from `UserStopException`; the Go
test supplies an authored SMAF note and stops the clip after its wait is
registered. Compile against the generated signatures and package the class
files with this manifest:

```text
Manifest-Version: 1.0
MIDlet-1: Audio Stop Fixture, , AudioStopMIDlet
MIDlet-Name: Audio Stop Fixture
MIDlet-Version: 1.0
MIDlet-Vendor: wfeature
```

## Execution and checkpoints

`src/BytecodeCheckpointMIDlet.java` and `bytecode-checkpoint.jar` exercise
restoration of an active bytecode call chain inside a lifecycle callback. The
JAR contains only `BytecodeCheckpointMIDlet.class` and the authored
`BytecodeCheckpointProbe.class` from `internal/jvm/testdata`, with
`BYTECODE_CHECKPOINT.MF`. Compile against the generated MIDP signatures. This
is a JVM continuation integration test, not a full SKT session checkpoint.

`src/NativeCheckpointMIDlet.java` and `native-checkpoint.jar` check that
refusing an incomplete JVM execution record leaves MIDlet resume and its
bytecode caller working. A second test restores the MIDlet's heap into a fresh
VM without constructing the MIDlet or calling `startApp` again. The JAR
includes this MIDlet and the authored `NativeCheckpointProbe.class` and
`HeapCheckpointProbe.class` from `internal/jvm/testdata/`. Compile the MIDlet
against the generated MIDP signatures and those probes. The heap fixture also
resumes Calendar operations with a fixed test timezone. Package only those
three classes with `NativeCheckpointMIDlet` as `MIDlet-1`.

`src/RestartingMIDlet.java` and `restarting.jar` reproduce repeated startup
replacing a Canvas and creating another worker. Package only
`RestartingMIDlet.class` and `RestartingMIDlet$Scene.class` with
`RESTARTING.MF`. The Go tests enable the selected resume behavior directly; the
authored class set is not a production compatibility entry.

`src/SerialInputMIDlet.java` and `serial-input.jar` exercise an animation loop
whose paint consumes keys and whose serial callback clears them. Package only
`SerialInputMIDlet.class` and `SerialInputMIDlet$Loop.class` with
`SERIAL_INPUT.MF`. The same fixture checks synchronous repaint and recovery
after a serial callback throws.

`src/AsyncFailureMIDlet.java` and `async-failure.jar` cover `Thread.join`,
restart refusal after a completed thread, and a background thread's uncaught
failure reaching the session (`async_error_test.go`). Compile against the
authored MIDlet stubs and package `AsyncFailureMIDlet.class` plus its two
anonymous classes with `ASYNC_FAILURE.MF`.

`src/StreamMIDlet.java` and `streams.jar` are a packaged lifecycle fixture for
the CLDC `DataInput` and `DataOutput` interfaces. It checks stream
assignability and primitive round trips after the SKT archive loader starts
the MIDlet. Build it against an authored minimal `MIDlet` signature and
package it with `STREAMS.MF`.

`Watched.java` and its Java 8 class-format output give a write-watch test two
fields written from two methods: a hit must land on the address of the field
that was written and name the method that wrote it. Regenerate it with:

```sh
javac -source 1.8 -target 1.8 -g:none internal/platform/skt/testdata/Watched.java
```

## Text input

`src/TextInputMIDlet.java` and `text-input.jar` are a packaged Host keyboard
and IME fixture. It starts on an empty 16-character TextBox; its `Next` command
switches to a second four-character TextBox so a session test can check
stale-target rejection as well as committed text and length limits. Package it
with `TEXT_INPUT.MF`.

## Compatibility adaptations

`src/LicenseMIDlet.java` and `license.jar` cover session-local license
adaptation. The checker uses a synthetic digest, an observable update counter
and ordinary RMS persistence. Its Java 8 compiler layout matches a supported
license-check shape without bundling external license code. Build it with a
private output directory:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-license-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -nowarn -g:none -classpath "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/LicenseMIDlet.java
jar cfm internal/platform/skt/testdata/license.jar \
  internal/platform/skt/testdata/LICENSE.MF -C "$fixture_dir" .
```

`src/TutorialCacheMIDlet.java` and `tutorial-cache.jar` test the
revision-scoped `skt.tutorial_name_cache` adaptation, including the original
bounds exception and the corrected reload (`tutorial_compatibility_test.go`).
Compile with the authored MIDlet stub on the class path and package only
`TutorialCacheMIDlet.class`; its manifest selects `TutorialCacheMIDlet` with
MIDP-2.0 and CLDC-1.1.

## Legacy clip tiles

`src/ClipTilesMIDlet.java` and `clip-tiles.jar` draw four solid tiles with
legacy inclusive clip extents. Package only the fixture classes with
`CLIP_TILES.MF`:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-clip-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -nowarn -g:none -classpath "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/ClipTilesMIDlet.java
jar cfm internal/platform/skt/testdata/clip-tiles.jar \
  internal/platform/skt/testdata/CLIP_TILES.MF -C "$fixture_dir" .
```

The Go test first checks ordinary count-based clipping with the legacy
profile, which must not select compatibility. It then explicitly enables the
internal compatibility flag and repaints the same guest to verify inclusive
extents. Only the separately fingerprinted original code selects this flag
automatically; the fixture is not in that allowlist.

## Browser persistence acceptance

`src/PersistenceMIDlet.java` and `persistence.jar` implement a visible RMS
counter for the two-version [PWA acceptance route](../../../../docs/pwa-acceptance.md).
Confirm cycles red/green/blue and writes the new counter; a fresh runtime reads
it back. Magenta indicates storage failure; a corner marker tracks key release.
Package only `PersistenceMIDlet` and its nested class with `PERSISTENCE.MF`:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-persistence-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -nowarn -g:none -classpath "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/PersistenceMIDlet.java
jar cfm internal/platform/skt/testdata/persistence.jar \
  internal/platform/skt/testdata/PERSISTENCE.MF -C "$fixture_dir" .
```

The browser Host detects SKT containers, so `persistence-skt.zip` wraps
`persistence.jar` and `PERSISTENCE.MF` renamed to `persistence.msd` at the ZIP
root. `text-input-skt.zip` likewise wraps `text-input.jar` and `TEXT_INPUT.MF`
renamed to `text-input.msd`, `canvas-skt.zip` wraps `canvas.jar` beside
`canvas.msd`, and `hang-destroy-skt.zip` wraps `hang-destroy.jar` beside
`HANG_DESTROY.MF` renamed to `hang-destroy.msd`. Regenerate an outer ZIP after
rebuilding its JAR.

## Full SKT checkpoints

`src/CheckpointMIDlet.java`, `checkpoint.jar` and `checkpoint-skt.zip` keep RMS
and XFile/FileInputStream handles open, expose startup/termination counters,
draw a visible canvas and sleep inside a Java worker. Tests restore that worker
over newer ordinary saves and prove that only the suffix runs. Compile with the
generated MIDP/SKVM signatures into a private output directory, then package
only `CheckpointMIDlet` and its nested class with `CHECKPOINT.MF`. The outer
ZIP contains the JAR and that manifest renamed to `checkpoint.msd`.

## Other fixtures

These JARs are embedded by the named tests; each manifest beside it is the
JAR's `META-INF/MANIFEST.MF`.

| JAR | Source | Manifest | Used by |
| --- | --- | --- | --- |
| `arithmetic.jar` | `internal/jvm/testdata/Arithmetic.java` | `MANIFEST.MF` | `runtime_test.go` (JAR to bytecode execution) |
| `lifecycle.jar` | `src/LifecycleMIDlet.java` | `LIFECYCLE.MF` | `runtime_test.go`, `destroy_wait_review_test.go` (`hangNextDestroy("loop")` and `("wait")`), `cmd/cli/exit_test.go` |
| `deferred-start.jar` | `src/LifecycleMIDlet.java` | `DEFERRED_START.MF` (`Fixture-Defer-Start: true`) | `runtime_test.go` |
| `runtime-failure.jar` | `src/LifecycleMIDlet.java` | `RUNTIME_FAILURE.MF` (`Fixture-Fail-Start: true`) | `runtime_test.go`, `cmd/cli/exit_test.go` |
| `hang-destroy.jar` | `src/LifecycleMIDlet.java` | `HANG_DESTROY.MF` (`Fixture-Hang-Destroy: wait`, a `destroyApp` that waits for a notification nobody sends) | `internal/webhost/close_sessions_review_test.go` and `web/acceptance/android-lifecycle.mjs`, through `hang-destroy-skt.zip` |
| `display.jar` | `src/DisplayMIDlet.java` | `DISPLAY.MF` | `runtime_test.go` (display selection) |
| `canvas.jar` | `src/CanvasMIDlet.java`, resources `fixture.png` and `data.bin` from `FIXTURE.PNG.b64` and `DATA.BIN.b64` | `CANVAS.MF` | `runtime_test.go`, `internal/platform/detect` |
| `connector.jar` | `src/ConnectorMIDlet.java` | `CONNECTOR.MF` | `connector_test.go` |
| `recordstore.jar` | `src/RecordStoreMIDlet.java` | `RECORDSTORE.MF` | `rms_test.go`; recipe in [`rms.md`](../../../../docs/rms.md) |
| `ui.jar` | `src/UIMIDlet.java` | `UI.MF` | `lcdui_test.go`; recipe in [`lcdui.md`](../../../../docs/lcdui.md) |
| `skvm.jar` | `src/SKVMMIDlet.java` | `SKVM.MF` | `skt_test.go`; recipe in [SKT history](../../../../docs/history/skt.md#implementation-validation) |
| `jvm-contracts.jar` | `src/JVMContractsMIDlet.java` | `JVM_CONTRACTS.MF` | `jvm_contracts_review_test.go` (interpreter and core-library contracts through a MIDlet JAR) |
