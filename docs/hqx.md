# hqx magnification

Most of these games render 240x320. At that size a modern display shows a
postage stamp; at an integer multiple it shows a grid of large squares. hqx is
the filter emulators settled on for this problem: for each pixel it looks at
the eight neighbours, decides which belong to the same shape, and draws the
boundaries between shapes as diagonals rather than staircases. Flat areas stay
flat, so pixel art keeps its character instead of being blurred.

## The port

The algorithm is its decision table: 256 cases per filter, one per combination
of which neighbours differ, each naming which of ten blends writes each output
pixel. `pattern2x.go`, `pattern3x.go` and `pattern4x.go` in
`internal/filter/hqx` hold it, about 8,000 lines mechanically translated from
the reference implementation's macros and match arms rather than retyped.

Two checks make that translation verifiable rather than trusted:

- **All 256 patterns appear exactly once** in each of the three, with no gaps
  and no duplicates, so a case dropped by the translator shows up.
- **`reference_test.go` compares against the original, pixel for pixel.** It
  catches the failure counting cannot: a case present but routed to the wrong
  blend. It needs a Rust build of the reference named by
  `WFEATURE_HQX_REFERENCE`; the harness reads width, height and scale as
  arguments and streams little-endian `0xAARRGGBB` pixels through stdin and
  stdout. It was verified across two-color noise, full-range noise (which
  exercises the alpha blending), diagonals, and single-pixel and single-row
  images (which pin the edge clamping) at all three scales.

`hqx_test.go` then locks the verified behaviour in with digests, so an ordinary
test run catches a regression without a Rust toolchain.

```sh
WFEATURE_HQX_REFERENCE=/path/to/hqxref go test ./internal/filter/hqx
```

## Two departures from the reference

**No 64 MB lookup table.** The reference precomputes RGB→YUV for all sixteen
million colors. Sixty-four megabytes to save an arithmetic conversion is a poor
trade wherever this runs, so this port converts directly, and converts the
source image once into a YUV plane rather than converting each neighbour on
every visit. Every pixel is a neighbour of eight others, so this reduces nine
conversions per pixel to one: 5.6 ms to 3.9 ms per 240x320 frame natively, and
16.0 ms to 13.7 ms in wasm.

**The page magnifies with its own copy of the tables.** The server used to
magnify each picture and send the result, which kept one implementation; over
recorded play, a session at hq2x cost 3.4 to 3.8 times the traffic of the same
session at the original size, and hq3x 6.3 times. A page speaking session
protocol 2 now receives the guest's own picture. `web/magnify.js` draws it at
the scale the server names with each complete picture, calling `web/hqx.js` to
redo only the blocks an update can reach (a pixel's block and the blocks around
it). Because the server names the scale, a MIDlet surface, which is never
magnified, stays at its own size. A big-endian browser gets a plain
enlargement instead, because the page reads
`ImageData` one little-endian word per pixel. The first protocol and the CLI's
`-scale` still magnify in Go; each first-protocol connection's encoder and each
`session.Session` holds its own `hqx.Scaler`.

`web/hqx-patterns.js` is generated from `pattern2x.go`, `pattern3x.go` and
`pattern4x.go` by `TestJavaScriptPatternsMatchTheGoTables`, which fails when the
file and the tables differ; `WFEATURE_WRITE_HQX_JS=1` regenerates it. Each Go
case becomes one small function and the table an array of them, because a
single function holding all 256 cases was optimized by V8 before most cases had
run, deoptimized each time a new one did, and was two to three times slower.
`TestTheSharedFixtureDigest` and `web/hqx.test.mjs` magnify the same authored
picture (every neighbour pattern, twice) and must produce the same three
digests, which holds the two implementations to the same pixels. That
picture's colours all have luma and chroma well away from whole numbers: the
conversion is floating point, and Go may fuse a multiply-add where JavaScript
does not, which rounds a value on a boundary differently.

The page reads pixels as signed words (`web/hqx-blend.js`): an opaque pixel
read unsigned is above 2^31, which a JavaScript engine stores boxed, and the
filter ran nearly twice as slow that way. A pixel whose eight neighbours all
share its colour is written as a flat block without consulting the tables,
because every blend of one colour with itself is that colour.

## Cost

Per 240x320 frame, on full-range noise, which is the worst case: every pixel
differs from every neighbour, so the most expensive cases run and the blends
never take their equal-colors shortcut. Real frames have large flat areas that
cost almost nothing.

The native column is the Go filter, which the first protocol and the CLI use.
The wasm column dates from when the whole emulator ran in the page.

| | native | wasm |
|---|---|---|
| hq2x | 3.9 ms | 13.7 ms |
| hq3x | 4.0 ms | 14.8 ms |
| hq4x | 4.2 ms | 15.7 ms |

The page's JavaScript filter, measured on 2026-09-24 in Node 25 on an Apple M1
over two recorded 240x320 game frames, magnifying the whole frame:

| | JavaScript, whole frame |
|---|---|
| hq2x | 4.4–5.1 ms |
| hq3x | 6.0–7.4 ms |
| hq4x | 11.3–16.0 ms |

A phone is slower than that desktop, and a frame that changed a little costs a
small part of it. These games ask for about 50 ms per frame, and the filter runs
only on frames the guest flushes. Magnification is still the user's choice and
not the default. Its cost on a physical phone has not been measured.

### What it stopped allocating

Magnifying a frame used to build three working planes and an output image, all
new every frame: 3.1 MB at hq2x and 10.5 MB at hq4x, twenty-odd times a second,
on a machine that is also running the emulator. The collector that reclaims it
takes that time from the guest.

`hqx.Scaler` holds the working planes between frames. This is safe because
every plane is written from end to end before it is read: the source planes per
pixel, and the destination because every destination pixel belongs to exactly
one source pixel's block. The functions `Scale` and `ScaleRGBA` still exist and
allocate what they always did.

The picture a `Scaler` answers with is not pooled. It leaves for a Host to hold
(the server hands it to another goroutine to compress), so it is fresh per
frame; reusing it is the one change here that would corrupt a frame in flight,
and `hqx_test.go` pins that it is not reused.

| per 240x320 frame | before | after |
|---|---|---|
| hq2x | 3.09 MB, 5 allocations | **1.28 MB, 2** |
| hq4x | 10.46 MB, 5 allocations | **5.06 MB, 2** |

hq2x also became 18% faster; hq4x is unchanged in time, being dominated by the
filter itself. What remains is the output image.

## Using it

- Browser: the 🔍 image-quality setting offers original, hq2x, hq3x and hq4x
  and is remembered per browser under `wfeature:frameScale`. The page sends it
  with the session's `start` message and as a `scale` message when changed; the
  server names the scale with each complete picture and the page magnifies.
- CLI: `runktf <game> -frame out.png -scale 3` (2, 3 or 4), which also applies
  to `-framedir`.
