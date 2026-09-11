# SGS extended text evidence

This note records read-only inspection of the original runtime bundled with a
local archive. Addresses are original PE virtual addresses. No executable code,
font bytes or other assets were copied into the project. This private instruction
set is outside the WIPI specification. The note describes observed instructions,
not a claim that every effect is implemented or visually verified.

## Invocation and state

Opcode `0xcf` calls handler `0x41cd40`; `0xd0` calls `0x41cdd0`. Both consume
four signed words in order `(x, y, resource, flags)` and push no result. Both
use the current text style, foreground and alignment. `0xcf` disables background
painting; `0xd0` enables it with the current background color. They call the
shared text function at `0x40d360`.

Only the low three flag bits affect rendering:

| Mask | Effect established by the raster instructions |
| --- | --- |
| `1` | Horizontal stroke expansion (bold) |
| `2` | Per-row horizontal displacement (italic) |
| `4` | A horizontal underline |

The flags are sign-extended by the opcode handler, but each effect tests only
these low bits. Negative values therefore select the same effects as their
low-three-bit mask; `-1` selects all three. Higher bits do not change rendering.

The resource is scanned to NUL. An empty string returns without painting a
background or underline. The text state and current drawing color are preserved;
the shared routine restores the previous drawing color before returning.

## Alignment and advances

Let `N` be the encoded string length in bytes. The style tables at `0x4947d8`
and `0x4947f0` give base cells `4x6`, `6x8`, `6x12`, `6x12`. Style 3 doubles its
base cell, giving a `12x24` byte advance. Korean consumes two bytes in styles
2 and 3. The small styles have Latin-only glyph tables.

The total nominal width `W` is `N * byteAdvance`. Alignment changes the supplied
x coordinate by zero, `W/2` with integer truncation, or `W`, for left, center or
right alignment. Bold and italic do not change alignment or character advances.
Consequently ink can extend beyond the nominal right edge.

## Styles 0 through 2

The raster helper is `0x40d640`. A glyph consumes its fixed cell minus the last
row and column, matching the ordinary text raster's spacing. An ink pixel at
local `(column, row)` is plotted at `(x + column + shift(row), y + row)`.
Bold also plots the adjacent pixel to its right. Each plotted pixel is clipped
independently against the current inclusive clip bounds.

For italic text, define `initialShift = H/4 - 1`, where `H` is the cell height.
The row shift is:

```
max(0, initialShift - (row + 1)/4)
```

Division above is integer division. The initial shifts are 0, 1 and 2 pixels
for styles 0, 1 and 2. The first decrease occurs after row 2, and later decreases
after rows 6 and 10. No italic flag means zero displacement throughout.

The background path begins with `extension = flags & 1`. If background painting
is enabled and italic is selected, it adds `initialShift`. Its inclusive
rectangle is:

```
(x - 1, y - 1) through (x + W + extension - 1, y + H - 1)
```

The underline is one pixel high at `y + H`, inclusive from `x - 1` through
`x + W + extension - 1`. A subtle observed detail: the italic addition to
`extension` occurs only inside the background branch. Thus transparent italic
underlining uses only the bold extension, while background-enabled italic
underlining includes the italic extension as well. This is visible in the
branch structure at `0x40d51c`–`0x40d5be`.

## Style 3: established effects and verified raster coordinates

The large-style path increments the aligned x and supplied y by one before
calling raster helper `0x40d830`. Advances remain twice the base byte advance.
The helper emits two-pixel-high rectangles for source ink pixels. Their width
is two pixels normally and four pixels with bold, as established by
`((flags & 1) << 1) | 1` used as an inclusive endpoint difference.

Italic starts at `H/2 - 2`, or four pixels for the base height 12. It decreases
by one after every second source row until zero. This differs from merely
scaling the smaller-style italic displacement.

Using the original, pre-increment coordinates, the background inclusive bounds
are `(x - 1, y - 1)` through `(x + W + extension, y + 24)`. Extension begins
as the bold bit and adds four only inside the background-enabled italic branch.
The underline runs from `x - 1` through `x + W + extension`, at `y + 24`.

The horizontal loop in `0x40d830` is suspicious. The caller at `0x40d4d9`–
`0x40d4fd` passes `baseCellWidth * consumedBytes - 1` as its fifth argument.
The helper's setup at `0x40d884`–`0x40d8ad` subtracts the supplied x from that
argument before adding the displaced x back to form its horizontal endpoint.
Taken literally, the loop endpoint is the fifth argument plus displacement,
rather than the glyph's x plus its width. That can suppress pixels at ordinary
positive x positions. The bitmap cursor also advances according to this loop.

The helper was subsequently executed in an isolated x86 interpreter using an
independently authored all-ink glyph and bounded mapped memory. Arguments were
`(3, 1, x, 20, 5, 21, glyph, 3, flags)`. At x=0 it emits 33 rectangles
(three columns across eleven rows), at x=1 it emits 22, at x=4 it emits eleven,
and at x>=5 it emits none. Bold changes widths from two to four; italic adds
the established four-to-zero row shift without changing column counts. Thus
the surprising horizontal endpoint is observable behavior, not a disassembly
mistake. The source bit cursor remains continuous across the shortened rows.

Local evidence is `build/sgs_large_probe.py` and `build/sgs-large-native.log`.
No original font bytes were used by the probe or incorporated in the emulator.
The original negative-x path may read beyond a glyph; the implementation must
retain an explicit source bound instead of reproducing a native memory read.
These extended-command quirks do not change ordinary text services 0x6a–0x6d.
