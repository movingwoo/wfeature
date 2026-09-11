# SGS framebuffer scroll contract

Opcode `0xce` scrolls an entire indexed framebuffer. It is not a rectangle
transform. The implementation follows analysis of the original runtime;
this private SGS operation is not a WIPI API. No original code or assets are incorporated.

## Arguments and selected buffer

The callback handler at `0x41cd00` consumes four signed 16-bit arguments in
this order, pushes no result, and calls the renderer at `0x40eee0`:

| Argument | Meaning |
| --- | --- |
| `buffer` | `0`: drawing buffer; `1`: backup buffer |
| `dx` | Horizontal displacement; positive moves pixels right |
| `dy` | Vertical displacement; positive moves pixels down |
| `mode` | `0`: replace exposed pixels with white; `1`: wrap |

Both `buffer` and `mode` must be exactly zero or one. Other values, including
negative values, do nothing. The callback still consumes all four arguments.
The renderer checks `mode` at `0x40eeea`, checks `buffer` at `0x40ef01`, and
selects the drawing or backup pointer at `0x40ef0c`–`0x40ef16`.

The operation uses the full framebuffer width and height. It does not consult
the drawing clip, current color, palette bank, or text state, and it does not
present a frame. Both backing buffers have the same dimensions. Except for
the oversized blank-scroll behavior below, only the selected buffer changes.
Horizontal movement happens before vertical movement.

## Blank mode

For `mode == 0`, offsets within the inclusive ranges `[-width, width]` and
`[-height, height]` shift the selected buffer. At destination `(x, y)`, the
result is the old selected-buffer pixel at `(x-dx, y-dy)` if that source
coordinate is inside the screen, or raw framebuffer byte `0xff` otherwise.
This is white independently of the palette bank. An offset equal to either
full dimension therefore whites the entire selected buffer. Zero movement
leaves it unchanged.

The horizontal branches at `0x40ef65`–`0x40f011` move each row with overlapping
copies and fill the exposed side. The vertical branches at
`0x40f018`–`0x40f0b8` move the remaining rows and fill the exposed rows.
The copy helper at `0x44d5a0` selects forward or backward copying for overlap.

There is an observable original-runtime quirk for an offset whose magnitude
is **strictly greater** than its corresponding screen dimension. The checks
at `0x40ef24`–`0x40ef5f` jump directly to `0x40f0c3`, which calls the ordinary
white-clear helper `0x40eb80`. That helper always addresses the **drawing
buffer**, even when `buffer == 1`. Thus an oversized blank scroll targeting
the backup buffer clears the drawing buffer and leaves the backup unchanged.
The jump occurs before either axis has moved. Equality does not take this
branch and continues to affect the selected buffer.

## Wrap mode

For `mode == 1`, pixels leaving an edge reappear at the opposite edge of the
selected buffer. At destination `(x, y)`, read the old pixel at
`((x-dx) mod width, (y-dy) mod height)`, with nonnegative coordinate modulo.
There is no blank fill.

Offsets outside the inclusive dimension ranges are reduced using signed
remainder at `0x40f0d3`–`0x40f121`. Offsets equal to a positive or negative
full dimension are retained internally but produce the same image as zero
movement on that axis. Horizontal rotation uses a saved row segment at
`0x40f121`–`0x40f239`; vertical rotation uses saved column segments at
`0x40f240`–`0x40f3d7`. Both positive and negative displacements preserve the
wrapped pixels in their original order.

## Implementation boundary and regression cases

The implementation computes from a bounded snapshot of the selected
buffer and charges work proportional to framebuffer area before mutation.
It need not reproduce the original fixed-size native temporary storage. The
original UI caps dimensions at 256; safe host-selected larger dimensions
should use validated Go storage rather than inherit that native buffer size.
Invalid selector/mode no-ops and the oversized blank-scroll quirk are
behavioral contracts, not reasons to perform unchecked memory access.

Authored regression cases cover both buffers, both displacement signs,
both modes, diagonal movement, zero movement, exact-dimension shifts, shifts
beyond either dimension, and invalid selectors/modes. Verify that clipping
and palette state do not alter the result, that scrolling never flushes, and
that rejected work or cancelled execution leaves both buffers unchanged.
