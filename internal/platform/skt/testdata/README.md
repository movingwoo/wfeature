# SKT fixtures

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
