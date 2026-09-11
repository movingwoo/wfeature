# SKT fixtures

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
