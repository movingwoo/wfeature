# hqx magnification

These games render 240x320. At that size on a modern display they are a
postage stamp; at an integer multiple they are a grid of large squares. hqx is
the filter emulators settled on for exactly this problem: for each pixel it
looks at the eight neighbours, decides which belong to the same shape, and
draws the boundaries between shapes as diagonals rather than staircases. Flat
areas stay flat, so pixel art keeps its character instead of being blurred.

## The port

The algorithm *is* its decision table: 256 cases per filter, one per
combination of which neighbours differ, each naming which of ten blends writes
each output pixel. `pattern2x.go`, `pattern3x.go`, and `pattern4x.go` hold it —
8,000 lines, mechanically translated from the reference implementation's
macros and match arms rather than retyped.

Two things make that translation checkable rather than trusted:

- **All 256 patterns appear exactly once** in each of the three, with no gaps
  and no duplicates. A case dropped by the translator would show up here.
- **`reference_test.go` compares against the original, pixel for pixel.** That
  is what catches the failure mode counting cannot: a case present but routed
  to the wrong blend. It needs a Rust build of the reference behind
  `WFEATURE_HQX_REFERENCE`; the harness reads width, height, and scale as
  arguments and streams little-endian `0xAARRGGBB` pixels through stdin and
  stdout. Verified across two-color noise, full-range noise (which exercises
  the alpha blending), diagonals, and single-pixel and single-row images (which
  pin the edge clamping) at all three scales.

`hqx_test.go` then locks the verified behaviour in with digests, so an ordinary
test run catches a regression without a Rust toolchain present.

## Two departures from the reference

**No 64MB lookup table.** The reference precomputes RGB→YUV for all sixteen
million colors. Sixty-four megabytes to save an arithmetic conversion is a poor
trade wherever this runs, so this converts directly — and then converts the
*source image* once into a YUV plane rather than converting each neighbour on
every visit. Every pixel is some other pixel's neighbour eight times over,
so that is nine conversions per pixel reduced to one: 5.6ms to 3.9ms per
240x320 frame natively, 16.0ms to 13.7ms in wasm.

**The frame is magnified in the engine, not the page.** The alternative would
have the page hold its own copy of the filter. Magnifying before presentation
keeps one implementation and hands the canvas pixels it can draw without
resampling. The framebuffer still enforces its own dimensions — that check is
what catches a genuine size mismatch — so changing the filter rebuilds the
surface at the new size rather than relaxing it.

## Cost

Per 240x320 frame, on full-range noise, which is the **worst case**: every
pixel differs from every neighbour, so the most expensive cases run and the
blends never take their equal-colors shortcut. Real frames have large flat
areas that cost almost nothing.

The wasm column is from when the filter ran in the page; the emulator and its
filter now run on the server, so the native column is the one in force.

| | native | wasm |
|---|---|---|
| hq2x | 3.9ms | 13.7ms |
| hq3x | 4.0ms | 14.8ms |
| hq4x | 4.2ms | 15.7ms |

Against the ~50ms these games ask for per frame, and only on frames the guest
actually flushes. Still, it is the user's choice and not the default: the page
offers original/hq2x/hq3x/hq4x and remembers it.

### What it stopped allocating

Time was not the whole cost. Magnifying a frame built three working planes and
an output image, all four new every frame: 3.1 MB at hq2x and 10.5 MB at hq4x,
on a machine that is also running the emulator, twenty-odd times a second. The
collector that cleans it up takes that time out of the guest's.

`hqx.Scaler` holds the working planes between frames, which is safe because
every one of them is written from end to end before it is read — the source
planes per pixel, and the destination because every destination pixel belongs
to exactly one source pixel's block. A session owns one; `Scale` and
`ScaleRGBA` still exist as functions and allocate exactly what they always did.

**The picture it answers with is not pooled.** That one leaves for a Host to
hold — the server hands it to another goroutine to compress — so it is fresh
per frame. Reusing it is the one buffer here that would corrupt a frame in
flight, and `hqx_test.go` pins that it is not.

| per 240x320 frame | before | after |
|---|---|---|
| hq2x | 3.09 MB, 5 allocations | **1.28 MB, 2** |
| hq4x | 10.46 MB, 5 allocations | **5.06 MB, 2** |

hq2x also came out 18% faster; hq4x is unchanged in time, being dominated by
the filter itself. What remains is the output image, which is the size it is.

## Using it

- Browser: the 🔍 image-quality setting, which the page sends to the session so
  the filter runs where the frame is made.
- CLI: `runktf <game> -frame out.png -scale 3`, which also applies to
  `-framedir`.
