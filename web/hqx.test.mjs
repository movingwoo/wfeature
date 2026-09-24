import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { HqxScaler } from "./hqx.js";

// The picture internal/filter/hqx's javascript_test.go builds, byte for byte;
// see the comment there for why these colours and this layout.
const sharedFixture = () => {
  const width = 96, height = 96;
  const palette = [
    [18, 38, 200], [200, 60, 30], [60, 200, 90], [24, 44, 203],
    [203, 63, 33], [250, 240, 20], [120, 120, 121], [10, 10, 11],
  ];
  const centre = 0, other = 1, second = 2, near = 3;
  const pixels = new Uint8Array(width * height * 4);
  const set = (x, y, colour) => {
    const offset = (y * width + x) * 4;
    pixels.set(palette[colour], offset);
    pixels[offset + 3] = 255;
  };
  const neighbours = [[-1, -1], [0, -1], [1, -1], [-1, 0], [1, 0], [-1, 1], [0, 1], [1, 1]];
  for (let index = 0; index < 512; index++) {
    const pattern = index & 255, variant = index >> 8;
    const x = (index % 32) * 3 + 1, y = Math.floor(index / 32) * 3 + 1;
    set(x, y, centre);
    neighbours.forEach(([dx, dy], bit) => {
      const differs = (pattern >> bit) & 1;
      let colour = centre;
      if (differs && variant === 1 && bit % 2 === 1) colour = second;
      else if (differs) colour = other;
      else if (variant === 1 && bit % 3 === 0) colour = near;
      set(x + dx, y + dy, colour);
    });
  }
  let state = 0x9e3779b9;
  for (let y = 48; y < height; y++) {
    for (let x = 0; x < width; x++) {
      state = (state ^ (state << 13)) >>> 0;
      state = (state ^ (state >>> 17)) >>> 0;
      state = (state ^ (state << 5)) >>> 0;
      if (x > 0 && (state >>> 8) % 3 === 0) {
        pixels.copyWithin((y * width + x) * 4, (y * width + x - 1) * 4, (y * width + x) * 4);
        continue;
      }
      set(x, y, state % palette.length);
    }
  }
  return { pixels, width, height };
};

// The digests javascript_test.go holds for the same picture.
const sharedDigests = {
  2: "8ed8d84b0a10ab0aa89b4165293a18ac2af6fafdfd305af89ceeb1276d130ad8",
  3: "ed10b6149b3a51bb858e349dbaa11a982f7531d3068149bb9df3a59769862819",
  4: "ee08e64dc7bd1a01b48474094442a8f00c18cbcd423734b68b8c30474a8b00e1",
};

const magnify = (pixels, width, height, factor) => {
  const dst = new Int32Array(width * factor * height * factor);
  new HqxScaler().scale(new Int32Array(pixels.buffer), width, height, factor, 0, 0, width, height, dst);
  return dst;
};

test("the page magnifies exactly as the server does", () => {
  const { pixels, width, height } = sharedFixture();
  for (const [factor, want] of Object.entries(sharedDigests)) {
    const scaled = magnify(pixels, width, height, Number(factor));
    const digest = createHash("sha256").update(new Uint8Array(scaled.buffer)).digest("hex");
    assert.equal(digest, want, `hq${factor}x`);
  }
});

test("redoing only the blocks around a change matches magnifying everything", () => {
  const { pixels, width, height } = sharedFixture();
  const changed = pixels.slice();
  // A patch in the middle and one on the picture's edge, where the filter
  // repeats the edge pixel outward.
  for (const [x, y] of [[40, 60], [41, 60], [40, 61], [95, 0], [0, 95]]) {
    changed.set([250, 240, 20, 255], (y * width + x) * 4);
  }
  for (const factor of [2, 3, 4]) {
    const scaler = new HqxScaler();
    const src = new Int32Array(pixels.slice().buffer);
    const dst = new Int32Array(width * factor * height * factor);
    scaler.scale(src, width, height, factor, 0, 0, width, height, dst);
    src.set(new Int32Array(changed.buffer));
    // Each changed pixel's block and its neighbours' blocks are redone.
    for (const [left, top, right, bottom] of [[39, 59, 43, 63], [94, 0, 96, 2], [0, 94, 2, 96]]) {
      scaler.scale(src, width, height, factor, left, top, right, bottom, dst);
    }
    assert.deepEqual(dst, magnify(changed, width, height, factor), `hq${factor}x`);
  }
});

test("an unknown factor is refused", () => {
  assert.throws(() => new HqxScaler().scale(new Int32Array(1), 1, 1, 5, 0, 0, 1, 1, new Int32Array(25)));
});
