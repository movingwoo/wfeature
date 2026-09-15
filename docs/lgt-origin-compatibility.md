# LGT visible framebuffer origin (2026-09-15)

## Evidence

A reported 240x320 native Clet starts and accepts input, but some artwork is
drawn below its intended position. The saved session and page reports confirm
continued frame delivery and record the input sequence; they do not contain
pixel coordinates. The earlier investigation in
[lgt.md](lgt.md#one-build-draws-twenty-four-rows-below-everything-the-platform-draws)
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

## Correction boundary

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

## Validation

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
