# Code-revision compatibility

`internal/platform/compatibility/registry.json` is the source-controlled registry
of exceptions for specific code revisions on SKT, KTF and LGT. Go embeds it in
every build, so the CLI and the server read the same registry through the
platform runtimes. There is no external database, runtime JSON override or
separate download; a registry change requires rebuilding and restarting the
executable.

The registry identifies original code revisions. Recognition of shared
authentication code patterns is separate; see [authentication](authentication.md).

## Entry format

The document has schema `version: 1` and an `entries` array. Each entry contains:

- `id`: a unique, behavior-based identifier, without an individual game name.
- `platform`: `skt`, `ktf` or `lgt`.
- `match.kind`: the fingerprint algorithm, `java_class_set` or `native_module`.
- `match.sha256`: the exact lowercase SHA-256 code fingerprint.
- `fixes`: names of reviewed implementations in Go.
- `evidence`: a repository documentation path, optionally with a section anchor,
  recording the reproduction, reason, validation and limits.

The two fingerprints:

- `java_class_set` sorts all `.class` entry names, then hashes each name's UTF-8
  bytes and its complete class bytes, each preceded by its byte length as an
  unsigned 64-bit big-endian integer. Any added, removed or changed class changes
  the fingerprint.
- `native_module` hashes the complete, unmodified native executable. LGT uses
  `binary.mod`, including its ELF headers and sections; SKT's native package uses
  its `.daf`, including its header and relocation table. Changing any module byte
  changes the fingerprint.

Archive filenames, display names, descriptors, ZIP metadata and resources select
nothing, so identical code repackaged with different resources receives the same
exception.

Matching always includes the platform and fingerprint kind, not only the hash.
The shared package owns metadata validation and matching; each platform owns
fingerprint calculation and correction behavior. Fix names are
platform-prefixed and accepted only with their platform and kind: SKT's Java
fixes require `java_class_set` and its native fixes `native_module`, and LGT
fixes require `native_module`.

### Registered fixes

The registry holds 18 entries (last changed 2026-10-30). The `evidence` field of
each entry is the authoritative index; this table groups them by fix.

| Fix | Entries | Effect | Evidence |
| --- | --- | --- | --- |
| `skt.inclusive_set_clip` | 2 | MIDP `Graphics.setClip` treats extents as inclusive | [below](#skt-skt-original-specific-clip-compatibility), [SKT QA](history/skt.md#skt-qa-2026-09-29-inclusive-tile-clipping) |
| `skt.resume_without_start` | 2 | Preserves a live game whose start callback would recreate it | [SKT resume](history/skt.md#skt-resume-2026-09-30), [SKT QA](history/skt.md#skt-qa-2026-09-29) |
| `skt.tutorial_name_cache` | 1 | Clears a stale item-name cache at a tutorial transition | [tutorial reproduction](history/skt.md#gameplay-qa-2026-09-20-skt-tutorial-reproduction) |
| `lgt.visible_framebuffer_origin` | 1 | Removes a Clet's 24-row display-strip addition in initialization code | [below](#lgt-lgt-visible-framebuffer-origin-2026-09-15) |
| `lgt.direct_framebuffer_origin` | 1 | Removes the strip offset used by direct pixel writers | [LGT QA](history/lgt.md#lgt-qa-2026-09-21-direct-framebuffer-origin) |
| `lgt.image_framebuffer_origin` | 1 | Removes the strip offset from a native image path | [LGT QA](history/lgt.md#lgt-qa-2026-09-21-image-path-origin) |
| `lgt.color_key_sample_origin` | 1 | Aligns a native transparency calibration sample | [LGT QA](history/lgt.md#lgt-qa-2026-09-21-transparency-calibration-origin) |
| `lgt.wide_graphics_context` | 4 | Selects the 56-byte graphics context ABI instead of the compact default | [below](#lgt-wide-graphics-context) |
| `lgt.wide_exclusive_clip` | 1 | Keeps the wide context's clip ends exclusive | [LGT QA](history/lgt.md#lgt-qa-2026-09-21-second-color-report-wide-context-proof) |
| `lgt.save_subscriber_identity` | 1 | Lets a recognized save reader accept a foreign subscriber identity | [LGT QA](history/lgt.md#lgt-qa-2026-09-21-save-subscriber-comparison) |
| `lgt.certificate_message` | 2 | Selects the recognized two-stage local certificate exchange | [LGT QA](history/lgt.md#lgt-qa-2026-09-21-certificate-message-compatibility), [second revision](history/lgt.md#lgt-new-archives-2026-10-19-certificate-message) |
| `lgt.record_table_terminator` | 1 | Appends the end sequence a record-table search stops on, which the packaged table lacks | [LGT QA](history/lgt.md#lgt-qa-2026-10-28-unterminated-record-table) |
| `skt.native_discard_owner_state` | 1 | Withholds a recognized device dump's install directory so the game starts fresh | [SKT native](skt-native.md#skt-native-identity-discard) |
| `skt.native_save_identity` | 1 | Lets a recognized native executable accept its packaged save's handset digits | [SKT native](skt-native.md#skt-native-identity-save) |
| `skt.native_cert_subscriber` | 1 | Lets a recognized native executable accept its cert.c2s subscriber field (error 5001) | [SKT native](skt-native.md#skt-native-identity-cert) |

`skt.tutorial_name_cache` replaces a redundant phase assignment in an in-memory
class copy when one verified code revision returns from its tutorial equipment
menu to combat; JVM array checks are unchanged. The optional
`TestLocalTutorialCompatibilityFingerprint` uses the absolute local path in
`WFEATURE_SKT_TUTORIAL_ARCHIVE` to check selection and the rejection of a changed
class set.

KTF has no registered fixes. The parser accepts `ktf` as a platform, but no fix
name is valid for it. Add a fingerprint algorithm and an evidence-backed fix
together when an actual exception needs them; do not hash an empty Java class
set to identify a native archive, and do not add placeholder exceptions.

## Adding or removing an exception

First reproduce the defect and determine whether the platform contract can be
fixed generally. Use this registry only when evidence requires a
revision-specific exception. Record the evidence in `docs/`, then add the
verified revision's fingerprint and fix names to the JSON. Fixes for the same
platform, fingerprint kind and digest share one entry. A different code revision
requires its own evidence and entry; never match by title or filename.

A new fix name also requires a Go implementation and tests proving both the
selected behavior and the preservation of normal behavior. JSON cannot express
bytecode patches, addresses or scripts. When a general platform fix makes an
exception unnecessary, remove the entry or its obsolete fix.

The parser rejects an unsupported schema version, unknown fields, a second JSON
document, invalid fingerprints, missing metadata, duplicate IDs or
platform/kind/digest targets, and unknown or repeated fix names. Embedded-data
errors fail initialization and are caught by the registry tests, which also check
that every evidence document exists.

After editing the registry, run
`go test ./internal/platform/compatibility ./internal/platform/skt ./internal/platform/lgt`.
The optional `TestLocalClipCompatibilityArchive` uses `WFEATURE_SKT_CLIP_ARCHIVE`
with an absolute local archive path to verify the current original and its
rejection after class mutations. Real archives remain outside Git.

The initial registry migration passed the authored clipping regression, embedded
registry validation, the local original fingerprint and class-mutation checks,
`make test`, `make test-debug`, `go test -race ./internal/...` and `go vet ./...`.
It preserved the previously verified clip behavior and added no browser or
full-game playthrough claim.

<a id="skt-skt-original-specific-clip-compatibility"></a>

## SKT inclusive clip evidence (2026-09-15)

The reported archive's background loop places tiles on a 16-pixel grid and
passes `(x, y, 15, 15)` to a clipping wrapper. The wrapper has a branch that adds
one to each extent, but its static boolean selects the branch that passes
extents unchanged. Count-based clipping therefore drops the last row and column
of every tile and exposes the background as a regular grid. This reproduces in
the CLI framebuffer.

The archive declares `M_Profile-1.0, SKTP-1.0`, which does not establish an
inclusive clipping contract for that profile, so the initial profile-wide rule
was removed. The reporter withdrew the alleged second-platform edition; only the
reported SKT original is in scope. The exception is the
[`registry.json`](../internal/platform/compatibility/registry.json) entry
`skt-inclusive-clip-001`, selected by the `java_class_set`
[fingerprint](#entry-format). No original class bytes are bundled.

For this code revision only, MIDP `Graphics.setClip` converts nonnegative
inclusive extents to pixel counts. A request for 15 by 15 covers 16 by 16 where
the surface and device clip allow it; a boundary still limits the drawable
region. Conversion uses 64-bit arithmetic, negatives remain empty, and zero
covers one pixel. Clip getters report the resulting region. WIPI Graphics,
`clipRect`, fills, image sizes and repaint rectangles keep their contracts. The
[MIDP Graphics contract](https://mirusu400.github.io/wipi-wiki/midp/java-api/javax/microedition/lcdui/Graphics.md)
remains count-based for every unrecognized code revision, regardless of profile.

The authored `ClipTilesMIDlet` JAR checks normal clipping and the explicitly
enabled compatibility path; its legacy profile alone must not enable the
exception. Unit cases cover translation, zero and negative extents, extreme
coordinates, device bounds and the WIPI exclusion.
`TestLocalClipCompatibilityArchive` (above) also checks display-name
independence and rejection after changing each class in turn.

The replay `var/logs/skt-tiles.route` (`-hold 5 -ticks 1100`) reaches the opening
map. `skt-tiles-before/tiles.png` shows the grid;
`skt-render-revised/tiles.png` shows continuous terrain under the restricted rule
and is pixel-identical to the earlier capture under the broader rule.

<a id="skt-validation"></a>

### Validation

The authored clipping JAR, bounds, registry and local original fingerprint tests
passed. The isolated SKT branch passed `make test`, `make test-debug`,
`go test -race ./internal/...` and `go vet ./...`. A local CLI map replay
verified the grid removal; this is not a full playthrough or a new Safari check.
Original assets and saves remain outside Git.

<a id="lgt-lgt-visible-framebuffer-origin-2026-09-15"></a>

## LGT framebuffer origin evidence (2026-09-15)

<a id="lgt-evidence"></a>

### Evidence

A reported 240x320 native Clet starts and accepts input, but some artwork is
drawn below its intended position. The saved session and page reports confirm
continued frame delivery and record the input sequence; they contain no pixel
coordinates. The earlier
[investigation](history/lgt.md#implementation-one-build-draws-twenty-four-rows-below-everything-the-platform-draws)
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
`0x1ef90`. This is the display-strip addition from the earlier investigation,
already in the guest's drawing coordinates before a platform copy presents the
image.

The WIPI 1.2.1 graphics specification describes the screen framebuffer and
initializes the graphics context's relative origin to `(0, 0)`; it does not
require a framebuffer to include a 24-row strip. The sections
`MC_grpGetScreenFrameBuffer` and `MC_grpInitContext` were checked in the
[complete specification](https://mirusu400.github.io/wipi-wiki/llms-full.txt).
The strip convention is a compatibility inference from this original, not a
general WIPI requirement; other local Clets use row zero directly.

<a id="lgt-correction-boundary"></a>

### Correction boundary

Registry fix `lgt.visible_framebuffer_origin` is selected by the `native_module`
fingerprint of the complete, unmodified `binary.mod`. Archive names, descriptors
and resources do not select it, and a changed module is unrecognized.

The shared LGT loader applies the correction after mapping the module and
before its first instruction runs. It verifies the four instructions above, then
replaces only `adds r3, #24` with `adds r3, #0` in guest memory, which keeps the
existing origin without the strip. The original archive and parsed section data
are unchanged. Drawing, clipping, fonts and framebuffer pointers keep their
ordinary contracts.

The correction adapts one initialization rather than translating a finished
frame, because platform-drawn text and guest-drawn artwork can share a frame
while using different origins; moving the whole output would move correct text
too. Addresses and instructions are implemented in Go, not as configurable JSON
patches. An unexpected instruction sequence on a recognized module fails
loading.

<a id="lgt-validation"></a>

### Validation

An authored Thumb regression executes the initialization and verifies an origin
of 24 without the correction and zero with it, together with the surrounding
instructions and the rejection of unexpected code. The opt-in
`TestLocalFramebufferOriginArchive` checks real-loader selection, unchanged
archive bytes, independence from descriptor and resource changes, and rejection
of a module with a changed instruction:

```sh
WFEATURE_LGT_ORIGIN_ARCHIVE=/absolute/path/to/archive.zip \
  go test ./internal/platform/lgt -run FramebufferOrigin -count=1
```

A fresh-save CLI replay captured the notice, title and opening scene before and
after the correction. Both runs completed 1,712 ticks, 392 flushes and
433,409,278 guest instructions. The notice text was unchanged. The corrected
title restored the bottom credit and removed the stale credit fragment at the
top, and the opening artwork moved up by 24 rows. This is direct rendering
evidence, not a handset comparison or full-game completion.

A longer replay reached gameplay and held Right to move the character and scroll
the map. Both builds completed 7,824 ticks, 1,441 flushes and 2,114,774,337
guest instructions. The corrected frame places the character, name label and
lower HUD together; the old frame draws part of the HUD across the top and
misaligns the character and label. The notice frame is byte-identical.

An unregistered native Clet completed a separate 900-tick replay in both builds
with a byte-identical final PNG. Registry, LGT and SKT tests passed, as did
`make test`, `make test-debug`, `go test -race ./internal/...` and
`go vet ./...`. The local original and module-mutation checks passed in release,
debug and race runs. All seven named screenshots from the longer replay were
byte-identical between debug and release, and both server profiles built. The
running server was not restarted; an existing session needs a server restart and
a new game session to use the new initialization. Full-game completion,
long-term save restoration and handset visual comparison remain outside this
validation.

Local screenshots, routes and logs remain ignored under
`var/diagnostics/lgt-origin-20260915/`. Probe save data used separate
directories under `var/savedata/`; the user's existing saves were not used or
modified.

## LGT wide graphics context

`lgt.wide_graphics_context` selects the verified 56-byte context ABI for four
native code revisions (`lgt-wide-context-001` to `-004`); the compact ABI remains
the default. The fourth also takes `lgt.wide_exclusive_clip`: its context
setter computes `x + width` and `y + height` and its direct blitter treats them
as exclusive ends, while the earlier wide revisions keep their evidenced
inclusive convention. Field offsets,
direct callbacks, selection boundaries and acceptance status are in the
[first layout evidence](history/lgt.md#lgt-qa-2026-09-20-wide-graphics-context), the
[third revision](history/lgt.md#lgt-qa-2026-09-21-blank-rendering-and-context-layout) and the
[exclusive-clip proof](history/lgt.md#lgt-qa-2026-09-21-second-color-report-wide-context-proof).
