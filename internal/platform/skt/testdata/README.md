# SKT fixtures

`src/WIPIListenerMIDlet.java`, `wipi-listener.jar` and `WIPI_LISTENER.MF`
exercise integer playback events, single-listener replacement/removal,
deferred reentrant restart and bounded callback history. Go supplies an authored
400 ms score and tests forced collection and full checkpoints with active,
paused, idle and queued Clip owners. Listener instances retain only their ID;
the fixture can drop its Clip and clear history to prove collection. The
package-private `WIPIListenerExtension` extends `PlayListener`, checking
transitive interface identity during listener installation and restoration.
Compile against the MIDlet signature and the WIPI signatures below with
Java 8 target settings and `-g:none`; package `WIPIListenerMIDlet.class` and
`WIPIListenerExtension.class` with `WIPI_LISTENER.MF`. No generated signatures or sound assets belong in
the JAR.

`src/MediaLifecycleMIDlet.java`, `media-lifecycle.jar` and `MEDIA_LIFECYCLE.MF`
exercise finite repeats, media time, ordered asynchronous notifications and
boxed event payloads. Bounded event history records callback ownership and
whether delivery happened inside a native operation; listeners compare standard
event names by reference and can request
one restart or a volume change. Go supplies the authored 400 ms score and
checks full checkpoint restoration with undelivered events and a previously
retained event name. Compile against
the generated MIDP signatures using Java 8 settings and `-g:none`; package
only `MediaLifecycleMIDlet.class` with `MEDIA_LIFECYCLE.MF`.

`src/MediaVolumeMIDlet.java`, `media-volume.jar` and `MEDIA_VOLUME.MF` exercise
MIDP control lookup, interface casts/calls, stable object identity, fresh arrays,
volume/mute getters and change notifications. Go supplies authored mixed
MIDI/PCM bytes and restores the player/control/listener graph through a full
checkpoint. The callback queries the control and can queue another mute change.
Compile against the generated MIDP/SKVM signatures with Java 8 target settings
and `-g:none`; package only `MediaVolumeMIDlet.class` with `MEDIA_VOLUME.MF`.
No generated signatures or sound assets belong in this JAR.

`src/AudioGainMIDlet.java`, `audio-gain.jar` and `AUDIO_GAIN.MF` exercise
SKVM device volume, both WIPI Clip volume signatures and transient MIDP tones.
The Go tests supply authored SMAF bytes, change levels while two clips play,
and restore the clips or an active tone through a complete checkpoint.

Compile with the generated MIDP/SKVM signatures described below and these
additional compile-only WIPI signatures (the generator does not emit WIPI):

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

Place them in `org/kwis/msp/media/Clip.java`, `Player.java` and
`PlayListener.java` beside the other
generated signatures, compile that classpath, and compile the fixture with
Java 8 target settings and `-g:none`. Package only `AudioGainMIDlet.class`
with `AUDIO_GAIN.MF`. No signature class belongs in the JAR; the runtime
supplies their behavior. The void volume extension is exercised directly by
the Go test because Java cannot overload a method by return type alone.

`src/BytecodeCheckpointMIDlet.java` and `bytecode-checkpoint.jar` exercise
restoration of an active bytecode call chain inside a lifecycle callback.
The JAR contains only `BytecodeCheckpointMIDlet.class` and the authored
`BytecodeCheckpointProbe.class` from `internal/jvm/testdata`, with
`BYTECODE_CHECKPOINT.MF`. Compile with Java 8 target settings against the
generated MIDP signatures; do not bundle those signatures. This is a JVM
continuation integration test, not a full SKT session checkpoint.

`src/NativeCheckpointMIDlet.java` and `native-checkpoint.jar` check that refusing
an incomplete JVM execution record leaves MIDlet resume and its bytecode caller
working. A second test restores the MIDlet's heap into a fresh VM without
constructing the MIDlet or calling `startApp` again. The JAR includes this MIDlet
and the authored `NativeCheckpointProbe.class` and `HeapCheckpointProbe.class`
from `internal/jvm/testdata/`. Compile the MIDlet against the generated MIDP
signatures and those probes, using Java 8 target settings.
The heap fixture also resumes Calendar operations with a fixed test timezone.
Package only those three classes with `NativeCheckpointMIDlet` as `MIDlet-1`;
the compiler stubs are not runtime dependencies and are not bundled.

`src/RestartingMIDlet.java` and `restarting.jar` reproduce repeated startup
replacing a Canvas and creating another worker. Compile with Java 8 target
settings against the generated library signatures, then package only
`RestartingMIDlet.class` and `RestartingMIDlet$Scene.class` with `RESTARTING.MF`.
The Go tests enable the selected resume behavior directly; the authored class
set is not a production compatibility entry.

`src/SerialInputMIDlet.java` and `serial-input.jar` exercise an animation loop
whose paint consumes keys and whose serial callback clears them. Compile with
Java 8 target settings against the generated library signatures, then package
only `SerialInputMIDlet.class` and `SerialInputMIDlet$Loop.class` with
`SERIAL_INPUT.MF`. The same fixture checks synchronous repaint and recovery
after a serial callback throws.

`src/TextInputMIDlet.java` and `text-input.jar` are a packaged Host keyboard
and IME fixture. It starts on an empty 16-character TextBox; its `Next`
command switches to a second four-character TextBox so a session test can
check stale-target rejection as well as committed text and length limits.
Compile it against the generated library signatures and package it with
`TEXT_INPUT.MF`.

`src/StreamMIDlet.java` and `streams.jar` are a packaged lifecycle fixture for
the CLDC `DataInput` and `DataOutput` interfaces. It checks stream
assignability and primitive round trips after the SKT archive loader starts
the MIDlet. Build it against an authored minimal `MIDlet` signature and package
it with `STREAMS.MF`; the runtime supplies the actual MIDP class.

`src/AudioStopMIDlet.java` and `audio-stop.jar` are newly authored fixtures for
blocking audio calls. A guest thread distinguishes natural completion from
`UserStopException`; the Go test supplies an authored SMAF note and stops the
clip after its wait is registered. No sound asset is bundled in the JAR.

After compiling the generated library signatures described in
[fixture compilation guide](../../../../docs/history/testing.md#implementation-compiling-a-java-fixture), compile
this source against those classes and package its class files with this manifest:

```text
Manifest-Version: 1.0
MIDlet-1: Audio Stop Fixture, , AudioStopMIDlet
MIDlet-Name: Audio Stop Fixture
MIDlet-Version: 1.0
MIDlet-Vendor: wfeature
```

`Watched.java` and its Java 8 class-format output are test fixtures newly
authored for `wfeature`. It has two fields written from two methods, which is
what a write-watch test needs: a hit has to land on the address of the field
that was written and name the method that wrote it.

Regenerate it with:

```sh
javac -source 1.8 -target 1.8 -g:none internal/platform/skt/testdata/Watched.java
```

`src/LicenseMIDlet.java` and `license.jar` are newly authored fixtures for
session-local license adaptation. The checker uses a synthetic digest, an
observable update counter and ordinary RMS persistence. Its Java 8 compiler layout
matches a supported license-check shape without bundling external license code.

After generating and compiling the signatures described in
[fixture compilation guide](../../../../docs/history/testing.md#implementation-compiling-a-java-fixture), build it
with a private output directory:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-license-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -nowarn -g:none -classpath "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/LicenseMIDlet.java
jar cfm internal/platform/skt/testdata/license.jar \
  internal/platform/skt/testdata/LICENSE.MF -C "$fixture_dir" .
```

## Legacy clip tiles

`src/ClipTilesMIDlet.java` and `clip-tiles.jar` draw four solid tiles with
legacy inclusive clip extents. Compile against the generated signatures from
`docs/testing.md`, then package only the fixture classes with `CLIP_TILES.MF`:

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
automatically; the fixture does not appear in that allowlist.


## Browser persistence acceptance

`src/PersistenceMIDlet.java` and `persistence.jar` implement a visible RMS counter
for the two-version [PWA acceptance route](../../../../docs/pwa-acceptance.md).
Confirm cycles red/green/blue and writes the new counter. A fresh runtime reads
it back. Magenta indicates storage failure; a corner marker tracks key release.
Compile with the generated MIDP signatures and package only `PersistenceMIDlet`
and its nested class, using `PERSISTENCE.MF`. Do not package the generated stubs.

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-persistence-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -nowarn -g:none -classpath "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/PersistenceMIDlet.java
jar cfm internal/platform/skt/testdata/persistence.jar \
  internal/platform/skt/testdata/PERSISTENCE.MF -C "$fixture_dir" .
```

The browser Host detects SKT containers, so `persistence-skt.zip` wraps
`persistence.jar` and `PERSISTENCE.MF` renamed to `persistence.msd` at the ZIP
root. `text-input-skt.zip` similarly wraps the existing `text-input.jar` and
`TEXT_INPUT.MF` renamed to `text-input.msd`. Both ZIPs contain only authored
fixtures. Regenerate the outer ZIP after rebuilding either JAR.

## Full SKT checkpoints

`src/CheckpointMIDlet.java`, `checkpoint.jar` and `checkpoint-skt.zip` are newly
authored fixtures. They keep RMS and XFile/FileInputStream handles open, expose
startup/termination counters, draw a visible canvas and sleep inside a Java
worker. Tests restore that worker over newer ordinary saves and prove that only
the suffix runs. Compile with the generated MIDP/SKVM signatures into a private
output directory, then package only `CheckpointMIDlet` and its nested class with
`CHECKPOINT.MF`. The outer ZIP contains the JAR and that manifest renamed to
`checkpoint.msd`; no generated library stubs belong in either archive.
