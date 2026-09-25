import assert from "node:assert/strict";
import { test } from "node:test";
import { HqxScaler } from "./hqx.js";
import { Magnifier, unionRect } from "./magnify.js";

// picture stands in for the receiver's canvas at the game's own size.
const picture = (width, height, seed) => {
  const pixels = new Uint8ClampedArray(width * height * 4);
  let state = seed;
  for (let index = 0; index < width * height; index++) {
    state = (state * 1103515245 + 12345) >>> 0;
    // Few colours, so shapes and flat areas both occur.
    const colour = (state >>> 16) % 4;
    pixels.set([colour * 60, 200 - colour * 40, colour * 20, 255], index * 4);
  }
  return {
    width, height, pixels,
    getContext: () => ({
      getImageData: (x, y, w, h) => {
        const data = new Uint8ClampedArray(w * h * 4);
        for (let row = 0; row < h; row++) {
          data.set(pixels.subarray(((y + row) * width + x) * 4, ((y + row) * width + x + w) * 4), row * w * 4);
        }
        return { data };
      },
    }),
  };
};

// display stands in for the page's canvas; putImageData copies the dirty
// rectangle the way a browser does.
const display = () => {
  const canvas = { width: 0, height: 0, pixels: null, drawn: [] };
  const context = {
    canvas,
    putImageData: (image, dx, dy, x, y, w, h) => {
      if (!canvas.pixels || canvas.pixels.length !== canvas.width * canvas.height * 4) {
        canvas.pixels = new Uint8ClampedArray(canvas.width * canvas.height * 4);
      }
      canvas.drawn.push([x, y, w, h]);
      for (let row = y; row < y + h; row++) {
        const start = (row * image.width + x) * 4;
        canvas.pixels.set(image.data.subarray(start, start + w * 4), (row * canvas.width + x) * 4);
      }
    },
    clearRect() {},
    drawImage: (source, x, y, w, h) => { canvas.drawn.push(["drawImage", w, h]); },
  };
  return context;
};

const expected = (source, factor) => {
  const dst = new Int32Array(source.width * factor * source.height * factor);
  new HqxScaler().scale(new Int32Array(source.pixels.slice().buffer), source.width, source.height, factor,
    0, 0, source.width, source.height, dst);
  return new Uint8ClampedArray(dst.buffer);
};

test("the picture is magnified with hqx and only changed blocks are redone", t => {
  const priorImageData = globalThis.ImageData;
  globalThis.ImageData = class {
    constructor(width, height) { this.width = width; this.height = height; this.data = new Uint8ClampedArray(width * height * 4); }
  };
  t.after(() => { globalThis.ImageData = priorImageData; });
  const magnifier = new Magnifier();
  const context = display();
  const source = picture(24, 20, 7);
  magnifier.present(context, source, 3, null);
  assert.equal(context.canvas.width, 72);
  assert.equal(context.canvas.height, 60);
  assert.deepEqual(context.canvas.pixels, expected(source, 3));

  // Change a few pixels, as a masked update would, and hand over only them.
  for (const [x, y] of [[5, 5], [6, 5], [23, 19]]) source.pixels.set([250, 240, 20, 255], (y * 24 + x) * 4);
  context.canvas.drawn.length = 0;
  magnifier.present(context, source, 3, { left: 5, top: 5, right: 7, bottom: 6 });
  magnifier.present(context, source, 3, { left: 23, top: 19, right: 24, bottom: 20 });
  assert.deepEqual(context.canvas.pixels, expected(source, 3));
  // One pixel of blocks around each change, clamped to the picture.
  assert.deepEqual(context.canvas.drawn, [[12, 12, 12, 9], [66, 54, 6, 6]]);

  // A new scale is a new picture.
  magnifier.present(context, source, 2, { left: 0, top: 0, right: 1, bottom: 1 });
  assert.equal(context.canvas.width, 48);
  assert.deepEqual(context.canvas.pixels, expected(source, 2));
});

test("at the original size the picture is drawn as it is", () => {
  const magnifier = new Magnifier();
  const context = display();
  magnifier.present(context, picture(10, 8, 1), 1, null);
  assert.equal(context.canvas.width, 10);
  assert.equal(context.canvas.height, 8);
  assert.deepEqual(context.canvas.drawn, [["drawImage", 10, 8]]);
});

test("changed areas join, and all of the picture absorbs any area", () => {
  assert.deepEqual(unionRect({ left: 1, top: 2, right: 3, bottom: 4 }, { left: 0, top: 3, right: 2, bottom: 6 }),
    { left: 0, top: 2, right: 3, bottom: 6 });
  assert.equal(unionRect(null, { left: 0, top: 0, right: 1, bottom: 1 }), null);
  assert.equal(unionRect({ left: 0, top: 0, right: 1, bottom: 1 }, null), null);
});
