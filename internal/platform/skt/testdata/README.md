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
[`docs/testing.md`](../../../../docs/testing.md#compiling-a-java-fixture), compile
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
[`docs/testing.md`](../../../../docs/testing.md#compiling-a-java-fixture), build it
with a private output directory:

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-license-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -nowarn -g:none -classpath "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/LicenseMIDlet.java
jar cfm internal/platform/skt/testdata/license.jar \
  internal/platform/skt/testdata/LICENSE.MF -C "$fixture_dir" .
```
