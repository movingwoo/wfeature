# SGS SIS header and decoder investigation

SIS has two distinct encodings selected by the gate described in
[extended images](sgs-images.md). This document records exact inspected header
fields and the verified payload structure. It does **not** yet specify a complete
SIS decoder: several composition fields, reference-object transforms and coded
payload branches remain unresolved. Addresses refer to the original runtime;
no original implementation, codebook, permutation table or asset is reproduced.

## Type 1: fixed eight-byte header

`0x43a470` checks ASCII `SIS`, then reads exactly 40 bits, most significant bit
first, beginning at byte 3. There is no padding between fields. The first field
also selects type 1 at the outer image gate, which restricts it to 1 through 20.

| Bit offset after signature | Bits | Meaning / observed destination |
|---|---|---|
| 0 | 5 | Frame count, nonzero; `0x5391f2` |
| 5 | 5 | Unresolved playback field; `0x5391f4` |
| 10 | 5 | Width in eight-pixel units, nonzero; `0x5391f5` |
| 15 | 4 | Height in eight-pixel units, nonzero; `0x5391f6` |
| 19 | 1 | Unresolved flag; `0x5391f7` |
| 20 | 5 | Object count minus one; decoded count must be at most 20; `0x5391f3` |
| 25 | 3 | Unresolved field; `0x5391f8` |
| 28 | 1 | Unresolved flag; `0x5391f9` |
| 29 | 4 | Common delay field returned by metadata parser; `0x539202` |
| 33 | 3 | Frame-record variant selector; `0x5391fa` |
| 36 | 4 | Flags; `0x539408`; highest bit also stored at `0x5391fb` |

The header therefore ends at byte 8. Width ranges from 8 to 248 and height
from 8 to 120. Zero is rejected by this parser, so the outer wrapper's
zero-to-256 normalization does not extend these type 1 dimensions.

## Type 2: variable byte header

`0x438830` checks `SIS` and walks these bytes. Let `N` be frame count and let
`E=N` when byte 12 has bit 7 set, otherwise `E=0`.

| Byte offset | Meaning / observed destination |
|---|---|
| 0–2 | ASCII `SIS` |
| 3–5 | Skipped here; byte 3's upper five bits selected the outer decoder branch |
| 6 | Width: zero means 256, otherwise round down to a multiple of eight |
| 7 | Height, same conversion |
| 8 | Unresolved field; `0x5391f7` |
| 9 | Frame count `N`, nonzero; `0x5391f2` |
| 10 | Unresolved playback field; `0x5391f4` |
| 11 | Unresolved field; `0x5391fc` |
| 12 | Common delay if bit 7 is clear; otherwise selects per-frame delays |
| 13 through `12+E` | `N` one-byte delays when selected; absent otherwise |
| `13+E` | Frame-record variant selector; `0x5391fa` |
| `14+E` | Flags: bits 7, 6, 5 become `0x5391fb`, `0x539409`, `0x53940a` |
| `15+E` | Actual decoder subtype, overwrites `0x5391f0` |
| `16+E` | Unresolved field; `0x5391f1` |
| `17+E` | Object count; `0x5391f3` |

The payload starts at `18+E`. The outer type 2 branch explicitly rejects an
actual subtype of 8 after parsing (`0x4213e7`). Object initialization at
`0x4383b0` restricts frame count to at most 20 and object count to at most 50.
Header parsing by itself does not perform those later limits. Tiny nonzero
dimension bytes can round to zero, so a host must validate final dimensions
before allocation or division.

## Type 1 objects: raw tiles and coded runs

Frame extraction at `0x43a1a0` reparses the header, reads all object definitions,
then reads frame composition records. The first object's next eight bits must
not all be zero. For later objects, eight zero bits select the reference-object
reader at `0x43ada0`; a nonzero lookahead selects `0x43aa90`. The lookahead is
rewound before dispatch. No byte alignment is inserted between objects.

An independent object at `0x43aa90` contains:

1. Five-bit tile-column count, nonzero.
2. Four-bit tile-row count, nonzero and at most 12.
3. One coding-selection bit for every tile, in tile order.
4. Tile payloads, continuing immediately at the current bit position.

Each tile represents 8 by 8 monochrome pixels. The tile reader `0x43b640`
uses the selection bit as follows:

- Zero: consume 64 literal bits, most significant bit first. This branch is
  established at `0x43ba59`.
- One: consume an initial color bit, then variable-length runs until exactly
  64 pixels have been produced. At `0x43b813` it looks ahead 12 bits and calls
  `0x43c0f0` with the current color. The result encodes run length in bits 7–1
  and output color in bit 0; a separate output gives consumed code length.
  A zero result fails, and a run extending beyond 64 pixels fails. The next
  run's expected color becomes the opposite of the just-produced color.

Only the consumed code length advances the real cursor; the 12-bit lookahead
does not. The complete color-dependent codebook has not been independently
specified here. It must not be replaced by a guessed unary or generic RLE
scheme. The coded branch also applies a 64-position traversal permutation
before packing rows (`0x43ba26`); its mathematical order remains unverified.
Thus literal and coded tiles cannot be assumed to share the same scan order.

Object parsing invokes the same tile reader in scan-only mode to locate the
next object; extraction invokes it again with output enabled. The object
descriptor retains both the byte pointer and bit position. A decoder that
forgets the residual bit offset will fail on non-byte-aligned objects.

After objects, `0x43bbb0` reads each frame: one frame flag, one inclusion bit
per object, then a composition record through `0x43bd10` for each included
object. That reader has a variant branch controlled by header field
`0x5391fa`. Rendering at `0x43cdb0` reconstructs the selected frame using those
records, object data, clipping and transforms. Full placement and transform
contracts are still required before an executable raw-tile-only subset is safe.

## Type 2 objects and payload dispatch

`0x4383b0` parses object descriptors before recording every frame's stream
pointer through `0x438d10`. An object starts with two bytes. The first splits
into a high-bit flag and seven-bit field. The second's high bit selects a
reference object; its low seven bits then identify an earlier object, converted
to zero-based indexing by subtracting one. Reference handling continues through
`0x438bc0` and is not fully specified here.

For an independent object, bits 6–4 of the second byte select an internal value:
0 gives -1, 1 gives -2, and 2 requests a following one-byte value. Other values
have no established safe contract. `0x438ad0` then reads two dimension bytes,
records their product, and records a payload pointer. The payload begins with
a big-endian two-byte byte length; the next object begins after that many
payload bytes plus the length word. This identifies a bounded encoded object,
not necessarily uncompressed pixel data.

`0x438750` selects a previously indexed frame and calls `0x439030`. Object
decoding branches through `0x439d40` and `0x4398e0`, with additional image
transform/composition helpers. Both coded paths use initialization at
`0x43f800`, state resets at `0x43f870` and `0x43f930`, and decoding at
`0x43f990`. The inspected state machine consumes bits, maintains adaptive
state, and reconstructs rows; its complete coding model is unresolved. A
length-prefixed object must therefore not be treated as raw packed pixels.

## Extraction contract and next proof boundary

`0x421600` selects type 1 extraction through
`0x43a1a0(source, destination, frameIndex)`. Type 2 initializes once through
`0x4383b0(source)` and extracts through
`0x438750(destination, frameIndex)`. Index -1 is supported by these lower
helpers for all frames, but the exposed `0xe7` wrapper rejects negative indexes.
The exposed wrapper's inclusive upper-bound defect remains documented in
[extended images](sgs-images.md).

The smallest promising implementation is type 1 with independent literal
tiles and a verified simple composition record. This still requires completing
`0x43bd10` placement semantics and confirming raw tile packing at the frame
boundary. It must explicitly reject coded tiles and reference objects until
those contracts are complete. Current evidence establishes the headers and
literal tile input bits, not an end-to-end executable SIS decoder.
