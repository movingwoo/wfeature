# SGS extended image contracts

The extended decoder accepts proprietary `SIS` and `SAF` streams, not PNG.
These findings come from static inspection of the original runtime; addresses
are evidence references, not copied implementation. Header recognition alone
does not establish support for the compressed payload codecs.

## Accepted headers

The gate at `0x421210` requires one of these prefixes:

| Prefix | Additional gate | Decoder type |
|---|---|---|
| ASCII `SIS` | `(byte[3] & 0xf8) >> 3` is 1 through 20 | 1 |
| ASCII `SIS` | Same selector is 0, 29, or 30 | 2 |
| ASCII `SAF` | Further validation by the SAF parser | 8 |

Other selectors and prefixes fail. In particular, a PNG signature cannot pass
this gate. Types 1 and 2 call `0x43a470` and `0x438830`; type 8 enters the
callback-driven parser through `0x4246f0`. These decoder type numbers are
separate from the compact bitmap resource format's type byte.

The SAF header reader at `0x4247a0` requires seven bytes: `SAF`, a zero byte,
version 1 or 2 at offset 4, and a big-endian two-byte declared length at offsets
5–6. It returns header size 7. This is a necessary header condition; the complete
record stream must also validate. The object scanner at `0x420770` then reads
records beginning with a one-byte tag and big-endian two-byte length. Ordinary
records advance by length plus three. Tag 7 has an extra big-endian length at
record offset 3; tag 4 describes an image object and has further header parsing
through `0x420a00`. Treating all tags as ordinary length-prefixed payloads would
misalign the stream.

For type 2 SIS, `0x438830` reads width and height at offsets 6 and 7. Nonzero
values are rounded down to multiples of eight; zero means 256. Offset 9 is the
frame count and must be nonzero. Remaining SIS fields and compressed payloads
need their own complete bounds-checked specification before decoding is added.

## Five-word metadata

`0x410310(stream, output)` returns zero on success and minus one on parsing
failure. Its output consists of five consecutive little-endian words:

| Word | Meaning | Evidence |
|---|---|---|
| 0 | SIS parser's actual subtype, or SAF bit depth | Parser output stored at `0x4103c5` |
| 1 | Embedded object count when word 0 equals 8; otherwise 1 | `0x420770` reads `0x53d573`; `0x4103bc` stores it |
| 2 | Frame count | Parser first output; `0x4103cd` |
| 3 | Width | Parser second output; `0x4103d6` |
| 4 | Height | Parser third output; `0x4103da` |

The wrapper normalizes zero width or height to 256. Object count and animation
frame count are distinct: SAF stores the latter at `0x53d572`. The object scan
also computes aggregate decoded storage by summing object width times height
times bit depth, divided by eight. Its third output is this byte count, not
another frame count.

For SAF, the internal decoder family is 8, but the metadata parser returns the
bit depth supplied by the basic header, through `0x424b70` and `0x4215ac`.
Thus a one-bit SAF returns word 0 equal to 1, not 8. This distinction is verified
with authored input below and matters when deciding whether to scan objects.

## SAF nested metadata records

The outer tag 7 record has a big-endian two-byte record length. In the verified
minimal metadata form that length is 2, and its payload is another big-endian
two-byte length followed by that many bytes of nested headers. The state
machine first reads the outer payload (`0x4234c4`), reads its nested length at
`0x4238ee`, then requests the nested bytes and parses them at `0x4239be`.
Consequently its total encoded size is `3 + 2 + nestedLength`, not simply the
outer record length plus three.

The nested parser `0x424880` dispatches tags 1, 2, 3, 9 and 10. Ordinary nested
records have a one-byte tag, a one-byte payload length, and that many payload
bytes. Unknown nested tags skip `length + 2`. Palette tag 2 has additional
length rules described below. These nested lengths must not be confused with
the outer stream's two-byte lengths.

| Nested tag | Fields after tag and length | Evidence |
|---|---|---|
| 1 | Width byte, height byte, bit-depth byte, field byte, field byte, flag byte | `0x4229f0` |
| 2 | Object-count byte, palette-flags byte, optional palette payload | `0x422bb0` |
| 3 | Frame-count byte, playback-field byte, field byte | `0x422ce0` |
| 10 | Big-endian delay word; if zero, one big-endian delay word per frame follows | `0x422d50` |

Tag 1 has a six-byte basic payload. If its length exceeds six, the original
reader accesses a further four-byte big-endian field and uses that as a storage
limit; otherwise that field defaults to 65536. A safe parser must therefore
require at least ten payload bytes for this extended form. The width and height
are unsigned bytes here, with zero normalized by the outer query wrapper.
The metadata path accepts bit depths no greater than its configured limit of
8 (`0x424991`); this check alone does not prove every such depth is drawable.

Tag 2 extracts palette mode from `(flags >> 5) & 3`. Mode 0 adds no palette
payload. Mode 1 reads a big-endian byte count after the flags and copies that
many palette bytes. Mode 2 generates 256 grayscale RGB triples. Mode 3 reads a
big-endian byte count and skips the corresponding payload. For modes 1 and 3,
the returned record span is `declaredLength + paletteByteCount + 4`; the extra
two-byte count and palette bytes are outside the ordinary nested length. A
host must validate both spans and palette capacity before reading or copying.
These palette modes come from `0x422bb0`; the extended renderer's later palette
handling is a separate compatibility concern.

Tag 3 stores frame count at `0x53d572`; tag 2 stores object count at `0x53d573`.
Tag 10 stores a common nonzero delay or a per-frame array when the common word
is zero. The public header parser divides SAF delay values by 100 before
returning them. Neither delay form appears in the five-word query result.

## SAF object lengths used by metadata scanning

At an outer tag 4 record, `0x420770` passes `record + 3` to `0x420a00`.
The verified basic object-header length is five, with fields:

| Offset from `record + 3` | Meaning |
|---|---|
| 0 | Low seven bits: object ID; high bit: object flag |
| 1 | Object width, strictly positive |
| 2 | Object height, strictly positive |
| 3 | Unresolved object field |
| 4 | Payload codec selector |
| 5–6 | Big-endian encoded-payload byte count |
| 7 onward | Encoded payload bytes |

The object parser returns the encoded-payload byte count. The scanner advances
by `outerHeaderLength + encodedPayloadLength + 5`, giving eleven bytes for a
five-byte object header and one-byte payload. It increments the object count
for each tag 4 and stops after the count declared by nested tag 2. No frame
composition records are needed for this metadata scan. Scanning does not
decompress or validate the payload against the object's decoded dimensions.
It sums `width * height * bitDepth / 8` as decoded storage and has a 65536-byte
object-storage check, but the original caller does not consistently reject all
negative helper results before advancing. A safe implementation must do so.

## Authored metadata-query evidence

The following forty bytes were newly authored for a metadata-only fixture:

```text
53 41 46 00 01 00 28
07 00 02 00 11
01 06 08 01 08 00 00 00
02 02 01 00
03 03 01 00 00
04 00 05 00 08 01 00 00 00 01 00
```

They contain a version 1 SAF header declaring 40 bytes, seventeen nested
metadata bytes, and one object record. The one-byte payload is intentionally
only a scanner fixture: these observations make no claim that it decodes an
eight-bit, eight-pixel object or constitutes a complete animation.

An isolated x86 interpreter ran original function `0x410310` with these authored
bytes and a fresh memory image per case. All five cases returned zero and
reached the return sentinel within a one-million-instruction limit:

| Variation | Returned five words |
|---|---|
| As shown | `8, 1, 1, 8, 1` |
| Basic bit depth changed to 2 | `2, 1, 1, 8, 1` |
| Basic bit depth changed to 1 | `1, 1, 1, 8, 1` |
| Canvas width and height changed to zero | `8, 1, 1, 256, 256` |
| Frame count changed to 3 | `8, 1, 3, 8, 1` |

This confirms field order, bit depth versus internal decoder family, dimension
normalization, and independent frame count. It is sufficient for an authored
metadata-query test; complete SAF execution still requires valid object data
and frame composition.

Initialization at `0x4103f0` caches the five fields in globals `0x5269c0`
(type), `0x5269bc` (objects), `0x5269b8` (frames), `0x5269b4` (width), and
`0x5269b0` (height). It configures decoder storage through `0x420e80`, obtains
palette information through `0x4211d0`, and advances `0x421b50` until completion
or failure. Subsequent services depend on this shared initialized state.

## Service argument order

Each service below consumes seven words and pushes one result. Arguments are
listed oldest first. The last two words are consumed but not read by these
original handlers. This detail follows from operand base `0x52770c`: a load at
`top*2 + 0x527708` accesses argument 4, not argument 6.

| Opcode | Arguments | Operation |
|---|---|---|
| `0xe6`, suboperation 1 | `1, 1, sourceResource, unused, unused, unused, unused` | Initialize extended decoder from the source stream through `0x4103f0` |
| `0xe6`, suboperation 2 | `1, 2, sourceResource, objectIndex, unused, unused, unused` | Decode selected SAF object through `0x410560` and `0x420b30` |
| `0xe6`, suboperation 3 | `1, 3, pixelResource, x, y, unused, unused` | Render decoded pixels using current dimensions and palette through `0x4104f0` |
| `0xe7` | `1, 1, sourceResource, destinationResource, frameIndex, unused, unused` | Decode a frame into the destination through `0x4104b0` |
| `0xe2` | `1, 1, objectIndex, x, y, unused, unused` | Render a previously decoded SAF object through `0x410600` |

Handler entries are `0x41d550`, `0x41d6c0`, and `0x41d4c0`, respectively.
Unrecognized selectors return minus one. Initialization and drawing return zero
on success, minus one on failure. Suboperation 2 propagates a nonnegative decoder
result; the precise meaning of that successful value remains unestablished.
Resource IDs and source address ranges are checked before the `0xe6` selector
dispatch, including suboperation 3.

`0xe7` requests width times height bytes for its destination. Its allocator
returns success even when existing storage is retained, so the handler clears
exactly that requested span on every successful allocation; retained allocator
slack beyond the span is preserved. It then calls
`0x4104b0(source, destination, frameIndex)`. That
wrapper calls `0x421600(source, destination, frameIndex, 0)`. The final zero
selects single-frame output; it is not a transparency argument. The underlying
function also has a multi-frame branch, but this service does not expose it.
The type 1 path writes packed one-bit pixels into the prefix, so only
width-times-height divided by eight bytes contain decoded pixels even though
the destination allocation is eight times larger.
For SAF, frame reconstruction reaches `0x4218f0`. Original upper-bound tests
allow a frame equal to the frame count, an apparent boundary defect that must
not become unchecked indexing in a safe implementation.

`0xe2` requires current type 8. `0x420910` indexes decoded-object descriptors
of stride `0x22` at `0x53c198`, returning pixel pointer, dimensions, and another
render attribute. It performs no index validation itself. `0x410600` builds a
draw descriptor and invokes the existing rendering dispatch. This is an object
draw operation, not a rectangle copy or a PNG decode request.

Both drawing wrappers call `0x4106c0`, which selects packed monochrome, packed
two-bit, or eight-bit indexed rendering according to current decoder type. For
SAF it constructs either an identity palette or an RGB332 conversion of the
decoder's RGB palette. Full frame composition, object compression, the extra
object render attribute, and decoder completion limits remain to be specified.

## Implementation boundary

The implemented `0xe7` slice derives dimensions and frame bounds from the same
source it decodes instead of relying on process-global decoder state. It safely
handles aliased source and destination resources, charges a conservative work
bound before parsing, decodes into temporary storage, and changes the
destination only after the complete supported frame validates. Its exact type
1 subset is recorded in [SIS evidence](sgs-sis.md#extraction-contract-and-next-proof-boundary).

Remaining decoders must likewise bound stream reads, declared lengths, record
progress, indexes, aggregate decoded bytes, dimensions, and callback loops.
Unsupported compressed formats stay explicit until their complete contracts
and authored tests exist.

## Object compression 1 checkpoint

The body parsed by `0x420a00` has this layout, separately from the outer record
tag and length:

| Offset | Field |
|---|---|
| 0 | Low seven bits: object index; high bit: retained object flag |
| 1, 2 | Nonzero byte width and height |
| 3 | Compression selector, stored at descriptor offset `0x18` |
| 4 | Render attribute, stored at descriptor offset `0x14` |
| 5, 6 | Big-endian compressed payload length |
| 7 onward | Compressed payload |

The object-selection path at `0x420cdb` selects compression 0 or 1. Compression
1 calls `0x438180`, whose initialization at `0x4381e9` passes the version string
`1.1.3` and stream structure size `0x34` into `0x43d8b0`. That wrapper initializes
a 15-bit zlib stream. No original decompressor implementation was translated.

`decodeScriptSAFObject` implements this object-body layer with Go's standard
`compress/zlib`. It bounds the payload, preflights input/output work, limits
decompression to the expected byte count plus one, and verifies exact output
length and checksum before returning owned packed pixels. Failure returns no
object and changes no input or guest state. The current depth boundary is
1/2/4/8 bits with an integral packed-byte count; partial-byte dimensions fail
explicitly rather than retaining the original truncated allocation. Compression
0 remains unsupported. The opaque flag and render attribute are retained for
later rendering integration.

Six authored headers, including maximum byte dimensions and both flag values,
matched the original header helper's fields and payload-length result. An
isolated original zlib wrapper accepted authored 8-, 16-, and 128-byte outputs
at compression levels 0, 1, 6, and 9, matching every decoded byte. A larger probe
needs additional initialization of the original fast decoder tables; its missing
native pointer is a diagnostic limitation, not evidence about the Go decoder.
Tracked tests cover all four supported depths, independent output ownership,
truncated headers/payloads, checksum failure, short/oversized decoded data,
unsupported compression, invalid dimensions, and exhausted work budgets.

This helper is not yet connected to service dispatch. The outer SAF record
parser, decoder state, frame composition and display integration remain open;
the extended-image opcode rows therefore remain marked missing.
