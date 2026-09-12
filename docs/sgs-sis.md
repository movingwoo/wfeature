# SGS SIS header and decoder investigation

SIS has two distinct encodings selected by the gate described in
[extended images](sgs-images.md). This document records exact inspected header
fields and the verified payload structure. It does **not** yet specify a complete
SIS decoder: type 2 payload branches remain unresolved. Addresses refer to the
original runtime;
no original implementation, lookup storage, permutation table or asset is
reproduced.

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
| 19 | 1 | Complete-canvas inversion; `0x5391f7` |
| 20 | 5 | Object count minus one; decoded count must be at most 20; `0x5391f3` |
| 25 | 3 | Reference-transform tile-index width; `0x5391f8` |
| 28 | 1 | Stored field with no type 1 extraction consumer; `0x5391f9` |
| 29 | 4 | Common delay field returned by metadata parser; `0x539202` |
| 33 | 3 | Frame-record variant selector; `0x5391fa` |
| 36 | 4 | Stored fields with no type 1 extraction consumer; `0x539408`; highest bit also stored at `0x5391fb` and exported by the native metadata helper |

The header therefore ends at byte 8. Width ranges from 8 to 248 and height
from 8 to 120. Zero is rejected by this parser, so the outer wrapper's
zero-to-256 normalization does not extend these type 1 dimensions.

A whole-executable static scan found only the parser store for bit 28 and only
the parser store for the complete four-bit field. The copied highest bit has
two type 1 reads, both in the metadata helper where it is written to an output
pointer. The type 1 extraction and rendering paths do not read any of these
five stored values. Calling them extraction-inert does not discard the highest
bit's separate metadata meaning.

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

## Metadata query comparison checkpoint

On 2026-09-12, original helper `0x410310` was executed again in an isolated x86
interpreter over 102 newly authored SIS headers. Every call returned to its
sentinel within the 100,000-instruction limit. The matrix and exact results
were:

| Encoding | Authored combinations | Native result |
|---|---|---|
| Type 1 | Frame selectors 1 and 20; minimum and maximum dimensions; object counts 1, 20 and 21 | 8 returned 0; the four object-count-21 cases returned -1 |
| Type 2 | Outer selectors 0, 29 and 30; common and per-frame delays; zero, sub-eight and rounded dimensions; actual subtypes 0, 1, 2, 8 and 9 | 72 returned 0; the eighteen subtype-8 cases returned -1 |

For every successful call, the five signed words matched the Go header parser:
actual subtype, one object, frame count, rounded width and rounded height. Every
failure left the fresh five-word native output zeroed. The same result file was
then applied to the real Go `0xe8` dispatch, not only to the parser helper; all
102 return values and 510 output words matched, and the caller's earlier stack
value was retained. `TestLocalScriptSISMetadataComparison` keeps this direct
comparison repeatable with an ignored JSONL result file named by
`WFEATURE_SGS_SIS_METADATA_COMPARISON`.

This closes the systematic SIS header-metadata comparison without widening the
decoder claim. Truncated input remains deliberately rejected by Go before a
read, and SAF metadata, SIS object decoding, frame composition and host image
operations remain separate work.

## Type 1 objects: raw tiles and coded runs

Frame extraction at `0x43a1a0` reparses the header, reads all object definitions,
then reads frame composition records. The first object's next eight bits must
not all be zero. For later objects, eight zero bits select the reference-object
reader at `0x43ada0`; a nonzero lookahead selects `0x43aa90`. The lookahead is
rewound before dispatch. No byte alignment is inserted between objects.

The reference reader consumes nine zero bits, followed by a one-bit mode. Mode
zero is an exact reference to the previous resolved object and consumes no
further payload. Mode one snapshots the previous resolved object and then reads
a tile-replacement stream. The first object cannot be a reference.

Header bits 25 through 27 give the replacement tile-index width `n`. For `n=0`,
the stream is immediately complete and consumes no further bits. Otherwise it
repeats these records until the all-ones `n`-bit index:

1. An `n`-bit tile index in row-major tile order.
2. A one-bit coding selector.
3. One complete 64-pixel tile using the same literal or coded representation as
   an independent object.

Each record replaces the indexed tile; it does not OR into the earlier pixels.
Record order is significant, and the last of several records for one index
wins. Consecutive mode-one references accumulate because each starts from the
previous resolved object. A later mode-zero reference resolves to that latest
state. Earlier independent and transformed objects retain their own snapshots.

Three authored original-runtime calls verify the exact-reference boundary. One
composed only the reference at (8,8), one ORed an independent object and its
reference at x offsets 0 and 1, and one selected the last member of a two-link
reference chain at (7,7). All returned 1 and reached the sentinel; their final
bit positions were 146, 165 and 157. The Go comparison repeats the exact packed
outputs through the real `0xe7` dispatch.

Twelve further original-runtime results verify the replacement contract: an
empty width-zero stream; literal replacement of one and two-tile objects;
reverse-order and duplicate-index records; a coded replacement; two chained
transforms; an exact reference after that chain; and all three separately
rendered snapshots of one independent object followed by two transforms. The
duplicate-index result retained only the last replacement. All native calls
reached their sentinel, returned the declared frame count, and are compared to
Go by exact packed output and final bit position. The native helper also
accepted an index beyond a one-tile object's logical range and left its visible
output unchanged. Go rejects that stream before changing the destination,
because allowing the native unchecked write would violate the untrusted-input
boundary.

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
does not. Both branches apply the same 64-position diagonal traversal before
packing rows. A fresh original-runtime probe selected literal input positions
0, 1, 2, 5, 8, 9 and 63; they appeared at raster coordinates (0,0), (1,0),
(0,1), (2,0), (1,2), (0,3) and (7,7). This is the conventional alternating
diagonal traversal and can be generated without retaining a lookup table. The
original helper is not reproduced as a private lookup table. The following
comparison checks the measured codes against a published standard.

An exhaustive original-runtime probe over every 12-bit lookahead found 64 valid
run prefixes for each initial color. Comparison with
[ITU-T Recommendation T.4, Tables 2 and 3a](https://www.itu.int/rec/dologin_pub.asp?id=T-REC-T.4-199607-S%21%21PDF-E&lang=e&type=items)
found that all 64 white runs match, while 57 of the 64 black runs match. The
seven black differences are:

| Run | T.4 code | Original-runtime code |
|---:|---|---|
| 14 | `00000111` | `00000001` |
| 17 | `0000011000` | `0000001000` |
| 18 | `0000001000` | `00001100111` |
| 19 | `00001100111` | `00001101000` |
| 20 | `00001101000` | `00001101100` |
| 21 | `00001101100` | `00001101110` |
| 64 | `0000001111` | `000001111` |

Direct raw-lookahead calls confirmed the table comparison. For example,
`000000010000` returns black run 14 with length 8, while the T.4 run-14 prefix
padded to 12 bits returns zero. `000000100000` returns native run 17 rather than
T.4 run 18. `000001111000` returns native run 64 with length 9, while the T.4
run-64 prefix padded to 12 bits returns zero. Four end-to-end coded-tile calls
also verified all-zero and all-one 64-pixel runs plus alternating 32-pixel runs
in both directions. The measured overlap does not establish how the native
codebook was derived, and the codebook is not the standard table plus one
exception.

The Go decoder uses the 128 independently observed color/run pairs and matches
prefixes incrementally for at most 12 bits. A local differential test checks
every pair rather than substituting the published table. Two further native
calls covered failure: a white run of 63 followed by a black run of 2 returned
zero at bit position 59, and a truncated black prefix with a zero-filled mapped
tail returned zero at bit position 51. Both calls reached the sentinel and left
the eight-byte destination unchanged. Go rejects both streams before changing
the destination.

Object parsing invokes the same tile reader in scan-only mode to locate the
next object; extraction invokes it again with output enabled. The object
descriptor retains both the byte pointer and bit position. A decoder that
forgets the residual bit offset will fail on non-byte-aligned objects.

After objects, `0x43bbb0` reads each frame: one inversion bit, one inclusion bit
per object, then a composition record through `0x43bd10` for each included
object. Header variant 1 omits the otherwise present three-bit pass selector;
those records use pass zero. The record then stores signed-magnitude x in eight
bits, signed-magnitude y in seven bits and four one-bit fields. Executable
nonsquare, asymmetric probes establish the first three fields by their output:
vertical mirror, horizontal mirror and 90-degree counterclockwise rotation.
Rotation happens first, followed by the two mirrors in the rotated dimensions.
The fourth field consumes two more bits when set. Values zero through three
produced identical output in the verified path, and the renderer does not read
the stored values.

The variant is the number of ordered rendering passes. Rendering visits passes
zero through variant minus one and includes an object only when its selector
equals the current pass. Variant zero therefore renders no objects, and a
selector greater than or equal to the variant is ignored. Each object is
rendered at most once. Pass zero ORs set object pixels into the packed one-bit
canvas. Later passes form a mask by filling the span between the first and last
set pixel in each nonempty object row, then trimming above and below set pixels
in each column. The native empty-column endpoint behavior leaves a filled span
only on row zero. Inside the mask, zero and one object pixels replace the
existing canvas pixel; set pixels outside it are ORed. Placement and clipping
use the transformed dimensions and clip against all four canvas edges.

Header bit 19 and the selected frame's leading bit each invert every byte in
the complete packed canvas after composition. This includes background outside
all objects. Setting both cancels. Frames retain separate composition records:
a two-frame probe rendered the first normally and the second inverted without
changing the first result.

Forty-six authored original-runtime calls recorded this composition boundary
on 2026-09-12. The vectors covered all eight transform
combinations, three clipped negative placements, four values of the trailing
two-bit field, variants 0, 2, 3 and 7 with selectors inside and outside their
ranges, full-canvas and empty-canvas inversion, two-frame selection, overlap,
later-pass replacement and the empty-column endpoint behavior. Every call
reached its sentinel and returned the declared frame count; final bit positions
were 181, 200, 203, 221, 343, 361 or 365 according to record shape. Exact packed
output is compared both with the Go decoder and the actual `0xe7` dispatch. Six
of these calls set header bit 28 or header bits 36 through 39 and were the first
evidence that these values did not alter simple extraction.
`TestLocalScriptSISCompositionComparison` validates the complete 46-row schema
from the ignored JSONL file named by
`WFEATURE_SGS_SIS_COMPOSITION_COMPARISON`.

A second matrix varied bit 28 and all sixteen values of the four-bit field over
four authored streams: a nonsquare asymmetric transformed object, clipped
later-pass overlap, a literal tile replacement followed by an exact reference,
and a coded tile. All 128 calls reached the sentinel and returned one. The
parser globals retained bit 28 and the complete nibble exactly, while the
separate metadata value equaled the nibble's highest bit. For each stream, all
32 combinations had identical packed output, source length and final cursor;
the four cursor positions were 203, 365, 338 and 95. Go repeats every vector
through actual `0xe7`, and a permanent regression checks the same field matrix
across literal composition, overlap, references and coded data. This dynamic
result and the absence of extraction consumers establish that every combination of these five bits
is accepted without changing extraction output.

Earlier literal composition evidence covered the seven traversal points above,
a full tile at offsets (0,0), (1,0), (-1,0), (0,1), (0,-1), (7,7) and (8,8), a
two-tile object, two overlapping independent objects and selection of the
second record in a two-frame stream. The Go comparison checks each packed
output through actual `0xe7` dispatch, including its result, caller stack,
packed prefix, zeroed requested tail and allocation rounding. A separate
authored bytecode regression executes the extraction instruction inside the VM.

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

The implemented subset accepts type 1 streams with independent literal or coded
tiles, mode-zero exact references, mode-one literal or coded tile replacements,
header variants zero through seven, ordered pass composition, the measured
geometric transforms, the consumed trailing record field and header/frame
inversion. Header bit 28 and the four fields at bits 36 through 39 are consumed
and accepted without changing extraction output. It reconstructs a requested
frame into temporary packed storage before a guest resource changes. The
pre-parse work reserve covers maximum coded expansion, reference fan-out,
object snapshots, geometric transforms, mask construction and scans,
composition, canvas inversion, and the maximum number of replacement records
permitted by the source length and index width. Type 2 and SAF extraction remain
unsupported. The safe frame range is zero through frame count minus one; the
original wrapper's inclusive upper-bound defect is not reproduced. Source
resources above 65,535 bytes are rejected, matching the script resource size
limit and keeping snapshot and decode work bounded.
