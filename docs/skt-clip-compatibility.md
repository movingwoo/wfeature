# SKT original-specific clip compatibility

The reported archive's background loop places tiles on a 16-pixel grid and
passes `(x, y, 15, 15)` to a clipping wrapper. That wrapper has a branch adding
one to each extent, but its static boolean selects the branch passing extents
unchanged. Count-based clipping drops the last row and column of every tile,
exposing the background as a regular grid. This reproduces in the CLI framebuffer.

The archive declares `M_Profile-1.0, SKTP-1.0`, but this does not establish an
inclusive clipping contract for that profile. The initial profile-wide rule was
removed. The reporter withdrew the alleged second-platform edition; only the
reported SKT original is in scope.

The exception is registered in the embedded
[`registry.json`](../internal/platform/compatibility/registry.json); see the
[registry maintenance guide](platform-compatibility.md). Authentication adaptation
remains separate.

Compatibility requires an exact SHA-256 fingerprint of all original Java
classes, including their entry names and length-prefixed contents in sorted
order. Any changed, added or removed class disables the exception. Container
filenames, display names, ZIP timestamps and compression do not select it.
Resources are not part of the code fingerprint: an identical code revision
retains the same behavior when repackaged. No original class bytes are bundled.

For this code revision only, MIDP `Graphics.setClip` converts nonnegative
inclusive extents to pixel counts. A request for 15 by 15 covers 16 by 16 where
the surface and device clip allow it; a boundary still limits the drawable
region. Conversion uses 64-bit arithmetic, negatives remain empty, and zero
covers one pixel. Clip getters report the resulting region. WIPI Graphics,
`clipRect`, fills, image sizes and repaint rectangles retain their contracts.
The [MIDP Graphics contract](https://mirusu400.github.io/wipi-wiki/midp/java-api/javax/microedition/lcdui/Graphics.md)
remains count-based for every unrecognized code revision, regardless of profile.

The authored `ClipTilesMIDlet` JAR checks both normal clipping and the explicitly
enabled internal compatibility path. Its legacy profile alone must not enable
the exception. Unit cases cover translation, zero/negative extents, extreme
coordinates, device bounds and the WIPI exclusion. The optional local test
`TestLocalClipCompatibilityArchive` requires `WFEATURE_SKT_CLIP_ARCHIVE` to name
an absolute path to the reported archive. It checks recognition, display-name
independence and rejection after changing each class in turn.

The replay `var/logs/skt-tiles.route` (`-hold 5 -ticks 1100`) reaches the opening
map. `skt-tiles-before/tiles.png` shows the grid;
`skt-render-revised/tiles.png` shows continuous terrain with the restricted rule.
It is pixel-identical to the earlier successful capture using the broader rule.


## Validation

Authored clipping JAR, bounds, registry and local original fingerprint tests
passed. The isolated SKT branch passed `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...`. Local CLI map replay verified the grid removal;
this is not a full playthrough or new Safari check. Original assets and saves
remain outside Git.
