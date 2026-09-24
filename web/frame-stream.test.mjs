import assert from "node:assert/strict";
import { test } from "node:test";
import { FrameReceiver } from "./frame-stream.js";

// The mock PNG is the signature, a header naming the width and a height of
// one, and then the pixels themselves; the mock decoder reads them back. Real
// PNG decoding is covered by the opt-in browser route.
const png = pixels => {
  const bytes = new Uint8Array(24 + pixels.length);
  bytes.set([137, 80, 78, 71, 13, 10, 26, 10]);
  const view = new DataView(bytes.buffer);
  view.setUint32(16, pixels.length);
  view.setUint32(20, 1);
  bytes.set(pixels, 24);
  return bytes;
};

// message builds a protocol 2 picture message.
const message = ({ operation = 0, scale = 1, dx = 0, x = 0, pixels = null, reserved = 0 }) => {
  const body = pixels ? png(pixels) : new Uint8Array(0);
  const bytes = new Uint8Array(16 + body.length);
  bytes.set([0x57, 0x46, 0x50, 0x32, operation, scale]);
  const view = new DataView(bytes.buffer);
  view.setInt16(6, dx);
  view.setUint16(10, x);
  view.setUint16(14, reserved);
  bytes.set(body, 16);
  return bytes.buffer;
};
const complete = (pixels, scale = 1) => message({ operation: 0, scale, pixels });
const replace = (pixels, x) => message({ operation: 1, x, pixels });
const masked = (pixels, x, dx = 0) => message({ operation: 2, x, dx, pixels });
// A bare PNG is what a server speaking only the first protocol sends.
const bare = pixels => png(pixels).buffer;

const settle = () => new Promise(resolve => setImmediate(resolve));

// The canvas model is one row of pixels, with zero as a transparent pixel:
// drawing is source-over, so a transparent pixel keeps what was under it.
const harness = t => {
  const priorDocument = globalThis.document, priorDecoder = globalThis.createImageBitmap;
  const decoded = [], frames = [], failures = [];
  const newCanvas = () => {
    const canvas = { width: 0, height: 0, pixels: [] };
    const context = {
      canvas,
      clearRect: (x, y, width) => {
        for (let i = x; i < x + width; i++) canvas.pixels[i] = 0;
      },
      drawImage: (image, x) => {
        for (let i = 0; i < image.width; i++) {
          const target = x + i;
          if (target >= 0 && target < canvas.width && image.pixels[i]) canvas.pixels[target] = image.pixels[i];
        }
      },
    };
    canvas.getContext = () => context;
    return canvas;
  };
  globalThis.document = { createElement: newCanvas };
  globalThis.createImageBitmap = async blob => {
    assert.equal(blob.type, "image/png");
    const data = new Uint8Array(await blob.arrayBuffer());
    const bitmap = { width: data.length - 24, height: 1, pixels: [...data.slice(24)], closed: false,
      close() { this.closed = true; } };
    decoded.push(bitmap);
    return bitmap;
  };
  const receiver = new FrameReceiver(
    (frame, presentation) => frames.push({ pixels: [...frame.pixels].slice(0, frame.width), ...presentation }),
    error => failures.push(error));
  t.after(() => { receiver.close(); globalThis.document = priorDocument; globalThis.createImageBitmap = priorDecoder; });
  return { receiver, decoded, frames, failures };
};

test("complete, masked and replacing updates compose in order", async t => {
  const { receiver, decoded, frames, failures } = harness(t);
  receiver.receive(complete([1, 2, 3, 4], 3));
  receiver.receive(masked([8, 0], 1));
  receiver.receive(replace([0, 9], 2));
  await settle();
  assert.deepEqual(frames.map(frame => frame.pixels), [[1, 2, 3, 4], [1, 8, 3, 4], [1, 8, 0, 9]]);
  // A transparent pixel in a masked update keeps the old one; in a replacing
  // one it erases it.
  assert.deepEqual(frames.map(frame => frame.scale), [3, 1, 1]);
  assert.deepEqual(frames.map(frame => frame.dirty), [null,
    { left: 1, top: 0, right: 3, bottom: 1 }, { left: 2, top: 0, right: 4, bottom: 1 }]);
  assert.deepEqual(failures, []);
  assert.ok(decoded.every(bitmap => bitmap.closed));
});

test("a bare PNG from an older server is a complete picture it already magnified", async t => {
  const { receiver, frames } = harness(t);
  receiver.receive(bare([5, 6]));
  receiver.receive(masked([7], 0));
  await settle();
  assert.deepEqual(frames.map(frame => [frame.pixels, frame.scale, frame.dirty]),
    [[[5, 6], 1, null], [[7, 6], 1, { left: 0, top: 0, right: 1, bottom: 1 }]]);
});

test("a shift moves the held picture and the strip it uncovers keeps its pixels", async t => {
  const { receiver, frames, failures } = harness(t);
  receiver.receive(complete([1, 2, 3, 4]));
  receiver.receive(masked([9], 3, -1));
  // A shift that already produced every pixel carries no PNG.
  receiver.receive(masked(null, 0, 2));
  await settle();
  assert.deepEqual(failures, []);
  assert.deepEqual(frames.map(frame => frame.pixels), [[1, 2, 3, 4], [2, 3, 4, 9], [2, 3, 2, 3]]);
  assert.deepEqual(frames.map(frame => frame.dirty), [null, null, null]);
});

test("a complete picture resets the base across sizes and restarts", async t => {
  const { receiver, frames } = harness(t);
  for (const buffer of [complete([1, 2, 3]), complete([0, 0, 8]), complete([6, 7]), replace([9], 0)]) receiver.receive(buffer);
  await settle();
  assert.deepEqual(frames.map(frame => frame.pixels), [[1, 2, 3], [0, 0, 8], [6, 7], [9, 7]]);
});

test("decodes serialize and closing releases an in-flight bitmap", async t => {
  const { receiver, decoded, frames } = harness(t);
  const decode = globalThis.createImageBitmap;
  let release;
  globalThis.createImageBitmap = async blob => { await new Promise(done => { release = done; }); return decode(blob); };
  receiver.receive(complete([1, 2]));
  receiver.receive(masked([8], 0));
  await settle();
  receiver.close();
  release();
  await settle();
  assert.equal(decoded.length, 1);
  assert.ok(decoded[0].closed);
  assert.deepEqual(frames, []);
});

test("malformed messages or missing bases fail once instead of corrupting later updates", async t => {
  const malformed = [
    masked([1], 0),                                   // no base yet
    new Uint8Array([1, 2, 3]).buffer,                 // not a picture
    message({ operation: 3, pixels: [1] }),           // unknown operation
    message({ operation: 0, scale: 0, pixels: [1] }), // no scale
    message({ operation: 0, scale: 5, pixels: [1] }), // scale out of range
    message({ operation: 0, reserved: 1, pixels: [1] }),
    message({ operation: 0, dx: 1, pixels: [1] }),    // a shift on a complete picture
    message({ operation: 1 }),                        // a replacement with nothing in it
    new Uint8Array([0x57, 0x46, 0x50, 0x32, 0]).buffer,
  ];
  for (const [index, input] of malformed.entries()) {
    await t.test(String(index), async child => {
      const { receiver, failures } = harness(child);
      receiver.receive(input);
      receiver.receive(complete([9]));
      await settle();
      assert.equal(failures.length, 1);
    });
  }
});

test("updates are checked against the held picture", async t => {
  for (const [index, update] of [replace([3, 4], 1), masked(null, 0), masked([1], 0, 2), masked([1], 0, -2)].entries()) {
    await t.test(String(index), async child => {
      const { receiver, frames, failures } = harness(child);
      receiver.receive(complete([1, 2]));
      receiver.receive(update);
      await settle();
      assert.deepEqual(frames.map(frame => frame.pixels), [[1, 2]]);
      assert.equal(failures.length, 1);
    });
  }
});

test("decode backlog is bounded and failure cancels queued frames", async t => {
  const { receiver, frames, failures } = harness(t);
  for (let i = 0; i < 40; i++) receiver.receive(complete([i + 1]));
  await settle();
  assert.deepEqual(frames, []);
  assert.equal(failures.length, 1);
});

test("a slow bitmap decoder cannot reorder dependent updates", async t => {
  const { receiver, frames, failures } = harness(t);
  const decode = globalThis.createImageBitmap;
  let release, calls = 0;
  globalThis.createImageBitmap = async blob => {
    if (++calls === 1) await new Promise(done => { release = done; });
    return decode(blob);
  };
  receiver.receive(complete([1, 2, 3]));
  receiver.receive(masked([7], 0));
  receiver.receive(masked([8], 2));
  await settle();
  assert.equal(calls, 1);
  release();
  await settle();
  assert.deepEqual(frames.at(-1).pixels, [7, 2, 8]);
  assert.deepEqual(failures, []);
});

test("a decoder failure discards dependent updates and releases the stream", async t => {
  const { receiver, frames, failures } = harness(t);
  globalThis.createImageBitmap = async () => { throw new Error("PNG decode failed"); };
  receiver.receive(complete([1, 2]));
  receiver.receive(masked([3], 1));
  await settle();
  assert.deepEqual(frames, []);
  assert.equal(failures[0].message, "PNG decode failed");
  assert.equal(receiver.queue.length, 0);
});

test("frame dimensions and queued bytes are bounded before decoding", async t => {
  const { receiver, decoded, failures } = harness(t);
  receiver.receive(complete(new Array(4097).fill(1)));
  await settle();
  assert.equal(decoded.length, 0);
  assert.equal(failures.length, 1);
  const other = new FrameReceiver(() => assert.fail("oversized frame was accepted"), error => failures.push(error));
  other.receive({ byteLength: 97 * 1024 * 1024 });
  assert.equal(failures.length, 2);
});
