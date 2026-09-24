import assert from "node:assert/strict";
import { test } from "node:test";
import { FrameReceiver } from "./frame-stream.js";

// These payloads retain the wire header; the mock decoder reads pixel values
// after it. Actual PNG decoding is covered by the opt-in browser route.
const picture = (pixels, x = null, y = 0) => {
  const png = new Uint8Array(24 + pixels.length);
  png.set([137, 80, 78, 71, 13, 10, 26, 10]);
  const view = new DataView(png.buffer);
  view.setUint32(16, pixels.length);
  view.setUint32(20, 1);
  png.set(pixels, 24);
  if (x === null) return new Blob([png]);
  const header = new Uint8Array(12);
  header.set([87, 70, 80, 49]);
  new DataView(header.buffer).setUint32(4, x);
  new DataView(header.buffer).setUint32(8, y);
  return new Blob([header, png]);
};
const settle = () => new Promise(resolve => setImmediate(resolve));

const harness = t => {
  const priorDocument = globalThis.document, priorDecoder = globalThis.createImageBitmap;
  const decoded = [], frames = [], failures = [];
  let canvas;
  globalThis.document = { createElement: () => {
    canvas = { width: 0, height: 0, pixels: [], getContext: () => ({
      clearRect: (x, y, width) => {
        for (let i = x; i < x + width; i++) canvas.pixels[i] = 0;
      },
      drawImage: (bitmap, x) => {
        for (let i = 0; i < bitmap.width; i++) {
          // Source-over would retain the old value for a transparent pixel.
          if (bitmap.pixels[i]) canvas.pixels[x + i] = bitmap.pixels[i];
        }
      },
    }) };
    return canvas;
  } };
  globalThis.createImageBitmap = async blob => {
    assert.equal(blob.type, "image/png");
    const data = new Uint8Array(await blob.arrayBuffer());
    const bitmap = { width: data.length - 24, height: 1, pixels: [...data.slice(24)], closed: false,
      close() { this.closed = true; } };
    decoded.push(bitmap);
    return bitmap;
  };
  const receiver = new FrameReceiver(frame => frames.push([...frame.pixels].slice(0, frame.width)), error => failures.push(error));
  t.after(() => { receiver.close(); globalThis.document = priorDocument; globalThis.createImageBitmap = priorDecoder; });
  return { receiver, decoded, frames, failures };
};

test("patches compose in order even when display callbacks are coalesced", async t => {
  const { receiver, decoded, frames, failures } = harness(t);
  receiver.receive(picture([1, 2, 3, 4]));
  receiver.receive(picture([8], 1));
  receiver.receive(picture([0, 9], 2));
  await settle();
  assert.deepEqual(frames, [[1, 2, 3, 4], [1, 8, 3, 4], [1, 8, 0, 9]]);
  assert.deepEqual(failures, []);
  assert.ok(decoded.every(bitmap => bitmap.closed));
});

test("full PNGs reset the base across scale changes and same-size restarts", async t => {
  const { receiver, frames } = harness(t);
  for (const blob of [picture([1, 2, 3]), picture([0, 0, 8]), picture([6, 7]), picture([9], 0)]) receiver.receive(blob);
  await settle();
  assert.deepEqual(frames, [[1, 2, 3], [0, 0, 8], [6, 7], [9, 7]]);
});

test("decodes serialize and closing releases an in-flight bitmap", async t => {
  const { receiver, decoded, frames } = harness(t);
  const decode = globalThis.createImageBitmap;
  let release;
  globalThis.createImageBitmap = async blob => { await new Promise(done => { release = done; }); return decode(blob); };
  receiver.receive(picture([1, 2]));
  receiver.receive(picture([8], 0));
  await settle();
  receiver.close();
  release();
  await settle();
  assert.equal(decoded.length, 1);
  assert.ok(decoded[0].closed);
  assert.deepEqual(frames, []);
});

test("invalid headers or missing bases fail once instead of corrupting later patches", async t => {
  for (const [index, input] of [picture([1], 0), new Blob(["bad header"]), picture([1], 4096)].entries()) {
    await t.test(String(index), async child => {
      const { receiver, failures } = harness(child);
      receiver.receive(input);
      receiver.receive(picture([9]));
      await settle();
      assert.equal(failures.length, 1);
    });
  }
});

test("patch bounds are checked against the complete image", async t => {
  const { receiver, frames, failures } = harness(t);
  receiver.receive(picture([1, 2]));
  receiver.receive(picture([3, 4], 1));
  await settle();
  assert.deepEqual(frames, [[1, 2]]);
  assert.equal(failures.length, 1);
});

test("decode backlog is bounded and failure cancels queued frames", async t => {
  const { receiver, frames, failures } = harness(t);
  for (let i = 0; i < 40; i++) receiver.receive(picture([i]));
  await settle();
  assert.deepEqual(frames, []);
  assert.equal(failures.length, 1);
});

test("a slow bitmap decoder cannot reorder dependent patches", async t => {
  const { receiver, frames, failures } = harness(t);
  const decode = globalThis.createImageBitmap;
  let release, calls = 0;
  globalThis.createImageBitmap = async blob => {
    if (++calls === 1) await new Promise(done => { release = done; });
    return decode(blob);
  };
  receiver.receive(picture([1, 2, 3]));
  receiver.receive(picture([7], 0));
  receiver.receive(picture([8], 2));
  await settle();
  assert.equal(calls, 1);
  release();
  await settle();
  assert.deepEqual(frames.at(-1), [7, 2, 8]);
  assert.deepEqual(failures, []);
});

test("a decoder failure discards dependent patches and releases the stream", async t => {
  const { receiver, frames, failures } = harness(t);
  globalThis.createImageBitmap = async () => { throw new Error("PNG decode failed"); };
  receiver.receive(picture([1, 2]));
  receiver.receive(picture([3], 1));
  await settle();
  assert.deepEqual(frames, []);
  assert.equal(failures[0].message, "PNG decode failed");
  assert.equal(receiver.queue.length, 0);
});

test("frame dimensions and queued bytes are bounded before decoding", async t => {
  const { receiver, decoded, failures } = harness(t);
  receiver.receive(picture(new Array(4097).fill(1)));
  await settle();
  assert.equal(decoded.length, 0);
  assert.equal(failures.length, 1);
  const other = new FrameReceiver(() => assert.fail("oversized frame was accepted"), error => failures.push(error));
  other.receive({ size: 97 * 1024 * 1024 });
  assert.equal(failures.length, 2);
});
