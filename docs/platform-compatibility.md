# Code-revision compatibility

`internal/platform/compatibility/registry.json` is the source-controlled registry of
exceptions for specific code revisions across SKT, KTF and LGT. Go embeds it in every build,
so the CLI and server use the same registry through the platform runtimes. There is
no external database, runtime JSON override, or separate download. Registry
changes require rebuilding and restarting the executable.

Authentication adaptation remains separate. This registry identifies original
code revisions; it does not replace recognition of shared authentication code
patterns.

## Entry format

The document has schema `version: 1` and an `entries` array. Each entry contains:

- `id`: a unique, behavior-based identifier, without an individual game name.
- `platform`: `skt`, `ktf` or `lgt`.
- `match.kind`: the fingerprint algorithm, `java_class_set` or `native_module`.
- `match.sha256`: the exact lowercase SHA-256 code fingerprint.
- `fixes`: names of reviewed implementations in Go.
- `evidence`: a repository documentation path, optionally with a section anchor,
  recording the reproduction, reason, validation and limits.

The `java_class_set` fingerprint preserves the existing matching algorithm: sort all `.class`
entry names, then hash each name's UTF-8 bytes and its complete class bytes,
preceding each with its byte length as an unsigned 64-bit big-endian integer.
Any added, removed or changed class changes the fingerprint. Archive filenames,
display names, ZIP metadata and resources do not select compatibility. Identical
code repackaged with different resources receives the same exception.

`skt.inclusive_set_clip` enables the existing
MIDP clip-extent correction for the recognized session. Unrecognized code keeps
normal clipping. The original archive is never rewritten. The
[rendering investigation](#skt-skt-original-specific-clip-compatibility)
records the current entry's evidence and rendering tests.

Matching always includes the platform and fingerprint kind, not just the hash.
The shared package owns metadata validation and matching. Each platform owns
fingerprint calculation and correction behavior. Fix names are platform-prefixed
and validated against their supported platform and fingerprint kind.

`native_module` hashes the complete, unmodified native executable bytes. LGT
uses `binary.mod`, including its ELF headers and sections. Repackaging the same
module with different resources does not change its fingerprint; changing any
module byte does. `lgt.visible_framebuffer_origin` removes a recognized Clet's
24-row display-strip addition in mapped initialization code. See the
[LGT origin investigation](#lgt-lgt-visible-framebuffer-origin-2026-09-15) for evidence and limits.

KTF has no registered fixes. Add a fingerprint algorithm and an evidence-backed
fix together when an actual exception needs them. Do not hash an empty Java
class set to identify a native archive or add placeholder exceptions.

## Adding or removing an exception

First reproduce the defect and determine whether the platform contract can be
fixed generally. Use this registry when evidence requires a revision-specific
exception. Record the evidence in `docs/`, and add the verified revision's
fingerprint and required fix names to the JSON. Consolidate fixes for the same
platform, fingerprint kind and digest in one entry. A different code revision requires its own evidence
and entry; do not match by title or filename.

A new fix name also requires a Go implementation and tests proving both its
selected behavior and preservation of normal behavior. JSON cannot express
arbitrary bytecode patches, addresses or scripts. If a general platform fix
later makes an exception unnecessary, remove the entry or its obsolete fix.

The parser rejects unsupported schema versions, unknown fields, invalid
fingerprints, missing metadata, duplicate IDs or platform/kind/digest targets, and unknown or
repeated fix names. Embedded-data errors fail initialization and are caught by
the registry tests. Tests also check that evidence documents exist.

Run `go test ./internal/platform/compatibility ./internal/platform/skt ./internal/platform/lgt` after editing the registry. The optional
`TestLocalClipCompatibilityArchive` uses `WFEATURE_SKT_CLIP_ARCHIVE` with an
absolute local archive path to verify the current original and rejection after
class mutations. Real archives remain outside Git.

The initial registry migration passed the authored clipping regression, embedded
registry validation, local original fingerprint and class-mutation checks,
`make test`, `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`.
The migration preserves the previously verified clip behavior; it does not add
a new browser or full-game playthrough claim.


<a id="skt-skt-original-specific-clip-compatibility"></a>

## SKT inclusive clip evidence (2026-09-15)

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
[registry maintenance guide](). Authentication adaptation
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


<a id="skt-validation"></a>

### Validation

Authored clipping JAR, bounds, registry and local original fingerprint tests
passed. The isolated SKT branch passed `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...`. Local CLI map replay verified the grid removal;
this is not a full playthrough or new Safari check. Original assets and saves
remain outside Git.


<a id="lgt-lgt-visible-framebuffer-origin-2026-09-15"></a>

## LGT framebuffer origin evidence (2026-09-15)

<a id="lgt-evidence"></a>

### Evidence

A reported 240x320 native Clet starts and accepts input, but some artwork is
drawn below its intended position. The saved session and page reports confirm
continued frame delivery and record the input sequence; they do not contain
pixel coordinates. The earlier investigation in
[lgt.md](history/lgt.md#implementation-one-build-draws-twenty-four-rows-below-everything-the-platform-draws)
identified a 24-row addition in the guest's own display drawing state.

The supplied local module has SHA-256
`9d8f286d632b3a77b32250b6b7216b38fec2fa1f544a43171c2606dee2189bcb`.
Its initialization at guest address `0x1ef8c` contains:

```asm
adds r2, #0x90
ldr  r3, [r2]
adds r3, #24
str  r3, [r2]
```

The immediate instruction is at module file offset `0x1dfc4`, mapped to
`0x1ef90`. This is the display-strip addition described in the earlier
investigation. It is already in the guest's drawing coordinates before a
platform copy presents the image.

The WIPI 1.2.1 graphics specification describes the screen framebuffer and
initializes the graphics context's relative origin to `(0, 0)`; it does not
require every framebuffer to include a 24-row strip. The API sections
`MC_grpGetScreenFrameBuffer` and `MC_grpInitContext` were checked in the
[complete specification](https://mirusu400.github.io/wipi-wiki/llms-full.txt).
The strip convention remains a compatibility inference from this original,
not a general WIPI requirement. Other local Clets use row zero directly.

<a id="lgt-correction-boundary"></a>

### Correction boundary

Registry fix `lgt.visible_framebuffer_origin` uses the SHA-256 of the complete,
unmodified `binary.mod`, under fingerprint kind `native_module`. Archive names,
descriptors and resources do not select it. A changed module is unrecognized.

The shared LGT loader applies the correction after mapping the module and
before its first instruction runs. It verifies the four instructions above,
then replaces only `adds r3, #24` with `adds r3, #0` in guest memory. This retains
the existing origin without adding the strip. The original archive and parsed
section data remain unchanged. Drawing, clipping, fonts and framebuffer pointers
keep their ordinary contracts.

This deliberately adapts one initialization rather than translating a finished
frame: platform-drawn text and guest-drawn artwork can share a frame while
using different origins. Moving the whole output would move correct text too.
Addresses and instructions are implemented in Go, not configurable JSON patches.
An unexpected instruction sequence on a recognized module fails loading.

<a id="lgt-validation"></a>

### Validation

An authored Thumb regression executes the initialization and verifies an origin
of 24 without correction and zero with correction. It also checks surrounding
instructions and rejection of unexpected code. The opt-in
`TestLocalFramebufferOriginArchive` checks real-loader selection, unchanged
archive bytes, independence from descriptor/resource changes, and rejection of
a module with a changed instruction:

```sh
WFEATURE_LGT_ORIGIN_ARCHIVE=/absolute/path/to/archive.zip \
  go test ./internal/platform/lgt -run FramebufferOrigin -count=1
```

A fresh-save CLI replay captured the notice, title and opening scene before and
after the correction. Both runs completed 1,712 ticks, 392 flushes and
433,409,278 guest instructions. The notice text was unchanged. The corrected
title restored the bottom credit and removed the stale credit fragment at the
top; the opening artwork also moved up by 24 rows. This is direct rendering
evidence, not a claim of handset comparison or full-game completion.

A longer replay reached gameplay and held Right to move the character and
scroll the map. Both builds completed 7,824 ticks, 1,441 flushes and
2,114,774,337 guest instructions. The corrected frame places the character,
name label and lower HUD together; the old frame draws part of the HUD across
the top and misaligns the character and label. The notice frame is byte-identical.

An unregistered native Clet completed a separate 900-tick replay in both builds;
its final PNG was byte-identical. Registry, LGT and SKT tests passed, along with
`make test`, `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`.
The local original and module-mutation checks also passed in release, debug and
race runs. All seven named screenshots from the longer replay were byte-identical
between debug and release. Both server profiles built successfully. The running
server was not restarted; existing sessions need a server restart and a new game
session to use the new initialization. Full-game completion, long-term save restoration and handset visual
comparison remain outside this correction's validation.

Local screenshots, routes and logs remain ignored under
`var/diagnostics/lgt-origin-20260915/`. Probe save data uses separate directories
under `var/savedata/`; the user's existing saves are not used or modified.
