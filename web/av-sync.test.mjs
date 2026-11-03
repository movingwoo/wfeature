import assert from "node:assert/strict";
import { test } from "node:test";

import {
  AV_SYNC_KEY,
  AV_SYNC_MAX,
  AV_SYNC_MIN,
  DelayedPictures,
  clampAVSync,
  pictureDelay,
  rememberAVSync,
  soundDelay,
  storedAVSync,
} from "./av-sync.js";
import { isSetting } from "./settings-file.js";

const storage = entries => {
  const values = new Map(Object.entries(entries ?? {}));
  return {
    values,
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, String(value)),
  };
};

// A picture is a canvas the receiver lends: a size and, here, a frame number
// standing for what is drawn on it. A copy takes the number it showed.
const picture = (number, width = 4, height = 3) => ({ width, height, number });
const canvasFactory = () => {
  const made = [];
  const createCanvas = (width, height) => {
    const canvas = {
      width, height, number: null,
      getContext: () => ({ drawImage: source => { canvas.number = source.number; }, globalCompositeOperation: "" }),
    };
    made.push(canvas);
    return canvas;
  };
  return { made, createCanvas };
};

const harness = (options = {}) => {
  let time = 0;
  let pending = null;
  const shown = [];
  const { made, createCanvas } = canvasFactory();
  const held = new DelayedPictures({
    show: (canvas, presentation) => shown.push({ number: canvas.number, ...presentation }),
    now: () => time,
    schedule: callback => { pending = callback; return 1; },
    cancel: () => { pending = null; },
    createCanvas,
    ...options,
  });
  const frame = at => {
    time = at;
    const callback = pending;
    pending = null;
    callback?.();
  };
  return { held, shown, made, frame, at: value => { time = value; }, scheduled: () => pending !== null };
};

test("the setting is a whole step within range, and either picture or sound waits", () => {
  assert.equal(clampAVSync("123"), 120);
  assert.equal(clampAVSync(-4), 0);
  assert.equal(Object.is(clampAVSync(-4), -0), false);
  assert.equal(clampAVSync(5000), AV_SYNC_MAX);
  assert.equal(clampAVSync(-5000), AV_SYNC_MIN);
  for (const nothing of [null, undefined, "", "soon", NaN]) assert.equal(clampAVSync(nothing), 0);

  assert.equal(pictureDelay(250), 250);
  assert.equal(soundDelay(250), 0);
  assert.equal(pictureDelay(-250), 0);
  assert.equal(soundDelay(-250), 0.25);
});

test("the setting is remembered for this browser and travels with the settings file", () => {
  const backing = storage();
  assert.equal(storedAVSync(backing), 0);
  assert.equal(rememberAVSync(181, backing), true);
  assert.equal(backing.values.get(AV_SYNC_KEY), "180");
  assert.equal(storedAVSync(backing), 180);
  assert.equal(storedAVSync(storage({ [AV_SYNC_KEY]: "loud" })), 0);
  assert.equal(storedAVSync({ getItem: () => { throw new Error("blocked"); } }), 0);
  assert.equal(rememberAVSync(10, { setItem: () => { throw new Error("full"); } }), false);
  assert.equal(isSetting(AV_SYNC_KEY), true);
});

test("a held picture is a copy shown when it falls due, not the borrowed canvas", () => {
  const { held, shown, frame } = harness();
  held.setDelay(100);
  const borrowed = picture(1);
  held.push(borrowed, { scale: 2, dirty: { left: 0, top: 0, right: 2, bottom: 2 } });
  // The receiver draws the next update into the same canvas.
  borrowed.number = 2;
  frame(50);
  assert.deepEqual(shown, []);
  frame(100);
  assert.deepEqual(shown, [{ number: 1, scale: 2, dirty: { left: 0, top: 0, right: 2, bottom: 2 } }]);
});

test("pictures due together show the newest with everything the skipped ones changed", () => {
  const { held, shown, frame, at, scheduled } = harness();
  held.setDelay(100);
  at(0);
  held.push(picture(1), { dirty: { left: 0, top: 0, right: 1, bottom: 1 } });
  at(10);
  held.push(picture(2), { dirty: { left: 3, top: 2, right: 4, bottom: 3 } });
  at(20);
  held.push(picture(3), { dirty: { left: 1, top: 1, right: 2, bottom: 2 } });
  frame(115);
  assert.deepEqual(shown, [{ number: 2, scale: 1, dirty: { left: 0, top: 0, right: 4, bottom: 3 } }]);
  assert.equal(held.holding, 1);
  assert.equal(scheduled(), true);
  frame(120);
  assert.deepEqual(shown.at(-1), { number: 3, scale: 1, dirty: { left: 1, top: 1, right: 2, bottom: 2 } });
  assert.equal(scheduled(), false);
});

test("a whole-picture update makes the joined update whole", () => {
  const { held, shown, frame } = harness();
  held.setDelay(10);
  held.push(picture(1), { dirty: null });
  held.push(picture(2), { dirty: { left: 0, top: 0, right: 1, bottom: 1 } });
  frame(10);
  assert.equal(shown[0].dirty, null);
});

test("past the byte bound the oldest waiting copy goes and its change stays", () => {
  // Each 4x3 picture is 48 bytes: two fit.
  const { held, shown, frame, at } = harness({ maxBytes: 96 });
  held.setDelay(100);
  at(0);
  held.push(picture(1), { dirty: { left: 0, top: 0, right: 1, bottom: 1 } });
  at(5);
  held.push(picture(2), { dirty: { left: 2, top: 2, right: 3, bottom: 3 } });
  at(9);
  held.push(picture(3), { dirty: { left: 1, top: 1, right: 2, bottom: 2 } });
  assert.equal(held.holding, 2);
  frame(105);
  // The first picture never shows; the second carries what it changed.
  assert.deepEqual(shown, [{ number: 2, scale: 1, dirty: { left: 0, top: 0, right: 3, bottom: 3 } }]);
});

test("changing the delay shows what was waiting at once, and reset forgets it", () => {
  const { held, shown, frame, scheduled } = harness();
  held.setDelay(200);
  held.push(picture(1));
  held.push(picture(2));
  held.setDelay(0);
  assert.deepEqual(shown.map(entry => entry.number), [2]);
  assert.equal(held.holding, 0);
  assert.equal(scheduled(), false);

  held.setDelay(200);
  held.push(picture(3));
  held.reset();
  frame(1000);
  assert.deepEqual(shown.map(entry => entry.number), [2]);
});

test("a steady delay reuses its copies instead of allocating each picture", () => {
  const { held, made, frame, at } = harness();
  held.setDelay(20);
  for (let index = 0; index < 50; index++) {
    at(index * 10);
    held.push(picture(index));
    frame(index * 10 + 1);
  }
  assert.ok(made.length <= 6, `${made.length} canvases for a two-frame delay`);
  const waiting = held.holding;
  held.push(picture(99, 0, 0));
  assert.equal(held.holding, waiting, "an empty picture is not held");
});
