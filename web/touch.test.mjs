import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { createTouchStream, guestPoint } from "./touch.js";

const rectOf = (left, top, width, height) => ({ left, top, width, height });

// The ordinary case: a 240x320 screen, magnified three times by the server's
// filter, shown in an element of exactly the picture's shape.
const plain = {
  rect: rectOf(0, 0, 480, 640),
  frameWidth: 720,
  frameHeight: 960,
  screenWidth: 240,
  screenHeight: 320,
};

test("a touch in the middle is the middle of the game's screen", () => {
  const point = guestPoint({ ...plain, clientX: 240, clientY: 320 });
  assert.deepEqual(point, { x: 120, y: 160 });
});

test("the corners of the element are the corners of the screen", () => {
  assert.deepEqual(guestPoint({ ...plain, clientX: 0, clientY: 0 }), { x: 0, y: 0 });
  // The last pixel rather than one past it: the guest indexes an array with
  // whatever it is handed.
  assert.deepEqual(guestPoint({ ...plain, clientX: 479.9, clientY: 639.9 }), { x: 239, y: 319 });
});

test("the element's own offset in the page is taken off", () => {
  const point = guestPoint({ ...plain, rect: rectOf(100, 50, 480, 640), clientX: 340, clientY: 370 });
  assert.deepEqual(point, { x: 120, y: 160 });
});

test("a frame taller than its hole is letterboxed left and right", () => {
  // A 2:3 handset shown in the page's 3:4 hole: the picture is as tall as the
  // hole and narrower, so the space beside it is bezel.
  const tall = {
    rect: rectOf(0, 0, 480, 640),
    frameWidth: 240,
    frameHeight: 400,
    screenWidth: 240,
    screenHeight: 400,
  };
  // 640/400 = 1.6, so the picture is 384 wide and starts 48 in.
  assert.equal(guestPoint({ ...tall, clientX: 47, clientY: 320 }), null);
  assert.deepEqual(guestPoint({ ...tall, clientX: 48, clientY: 0 }), { x: 0, y: 0 });
  assert.deepEqual(guestPoint({ ...tall, clientX: 240, clientY: 320 }), { x: 120, y: 200 });
  assert.equal(guestPoint({ ...tall, clientX: 433, clientY: 320 }), null);
});

test("a frame wider than its hole is letterboxed top and bottom", () => {
  const wide = {
    rect: rectOf(0, 0, 480, 640),
    frameWidth: 480,
    frameHeight: 320,
    screenWidth: 480,
    screenHeight: 320,
  };
  // 480/480 = 1, so the picture is 320 tall and starts 160 down.
  assert.equal(guestPoint({ ...wide, clientX: 240, clientY: 159 }), null);
  assert.deepEqual(guestPoint({ ...wide, clientX: 0, clientY: 160 }), { x: 0, y: 0 });
  assert.deepEqual(guestPoint({ ...wide, clientX: 240, clientY: 320 }), { x: 240, y: 160 });
  assert.equal(guestPoint({ ...wide, clientX: 240, clientY: 481 }), null);
});

test("a touch outside the element is not on the screen", () => {
  assert.equal(guestPoint({ ...plain, clientX: -1, clientY: 320 }), null);
  assert.equal(guestPoint({ ...plain, clientX: 240, clientY: -1 }), null);
  assert.equal(guestPoint({ ...plain, clientX: 480, clientY: 320 }), null);
  assert.equal(guestPoint({ ...plain, clientX: 240, clientY: 640 }), null);
});

test("a canvas with no size yet answers nothing rather than dividing by zero", () => {
  assert.equal(guestPoint({ ...plain, rect: rectOf(0, 0, 0, 0), clientX: 0, clientY: 0 }), null);
  assert.equal(guestPoint({ ...plain, frameWidth: 0, clientX: 0, clientY: 0 }), null);
  assert.equal(guestPoint({ ...plain, screenHeight: 0, clientX: 0, clientY: 0 }), null);
  assert.equal(guestPoint({ ...plain, rect: null, clientX: 0, clientY: 0 }), null);
});

const streamOf = () => {
  const sent = [];
  const stream = createTouchStream({
    press: (x, y) => sent.push(["press", x, y]),
    drag: (x, y) => sent.push(["drag", x, y]),
    release: (x, y) => sent.push(["release", x, y]),
  });
  return { sent, stream };
};

test("one touch is a press, its drags and a release", () => {
  const { sent, stream } = streamOf();
  stream.down(1, { x: 10, y: 20 });
  stream.move(1, { x: 11, y: 21 });
  stream.move(1, { x: 12, y: 22 });
  stream.up(1, { x: 12, y: 22 });
  assert.deepEqual(sent, [
    ["press", 10, 20],
    ["drag", 11, 21],
    ["drag", 12, 22],
    ["release", 12, 22],
  ]);
  assert.equal(stream.holding(), false);
});

test("a thumb sitting still sends nothing after its press", () => {
  // A finger resting on a screen emits a stream of moves at the same guest
  // pixel; each one would otherwise be a message.
  const { sent, stream } = streamOf();
  stream.down(1, { x: 10, y: 20 });
  for (let index = 0; index < 50; index++) stream.move(1, { x: 10, y: 20 });
  assert.deepEqual(sent, [["press", 10, 20]]);
});

test("a finger that wanders off the picture keeps the point it left from", () => {
  // A handset's touch panel is the screen: there is nowhere beside it to drag
  // onto, and a release out in the bezel released where the finger last was.
  const { sent, stream } = streamOf();
  stream.down(1, { x: 10, y: 20 });
  stream.move(1, { x: 30, y: 40 });
  stream.move(1, null);
  stream.move(1, null);
  stream.up(1, null);
  assert.deepEqual(sent, [
    ["press", 10, 20],
    ["drag", 30, 40],
    ["release", 30, 40],
  ]);
});

test("a touch that begins on the bezel begins nothing", () => {
  const { sent, stream } = streamOf();
  assert.equal(stream.down(1, null), false);
  stream.move(1, { x: 10, y: 20 });
  stream.up(1, { x: 10, y: 20 });
  assert.deepEqual(sent, []);
});

test("a second finger is not a second touch", () => {
  // A handset had one panel, and a title that reads two fingers is one this
  // could not have run anyway. The second is ignored rather than interleaved.
  const { sent, stream } = streamOf();
  stream.down(1, { x: 10, y: 20 });
  assert.equal(stream.down(2, { x: 90, y: 90 }), false);
  stream.move(2, { x: 91, y: 91 });
  stream.up(2, { x: 91, y: 91 });
  stream.move(1, { x: 11, y: 21 });
  stream.up(1, { x: 11, y: 21 });
  assert.deepEqual(sent, [
    ["press", 10, 20],
    ["drag", 11, 21],
    ["release", 11, 21],
  ]);
});

test("a release with no press before it releases nothing", () => {
  const { sent, stream } = streamOf();
  assert.equal(stream.up(1, { x: 10, y: 20 }), false);
  assert.deepEqual(sent, []);
});

test("the stream says while a touch is in progress", () => {
  const { stream } = streamOf();
  assert.equal(stream.holding(), false);
  stream.down(1, { x: 1, y: 2 });
  assert.equal(stream.holding(), true);
  stream.up(1, { x: 1, y: 2 });
  assert.equal(stream.holding(), false);
});

// The rules above are the stream's; what follows is the page's use of them.
// app.js reaches the DOM as it loads and cannot be imported here, so it is read
// as text the way keypad.test.mjs reads it. The one thing worth pinning is
// which of the two questions the window's pointermove asks: a handler that asks
// only whether a touch is *in progress* answers every finger's move to the
// canvas, and the other finger's keypad slide stops for as long as a thumb
// rests on the screen. Asking the stream to take the move instead lets it
// answer false for a pointer it does not own, and that move falls through to
// the pad.
test("a move the touch does not own falls through to the keypad", () => {
  const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");
  const start = app.indexOf('"pointermove"');
  assert.ok(start > 0, "app.js no longer binds pointermove");
  const handler = app.slice(start, app.indexOf("{ passive: true }", start));
  assert.match(handler, /if \(touch\.holding\(\) && touch\.move\(event\.pointerId, touchPoint\(event\)\)\) return;/);
  assert.ok(
    !/if \(touch\.holding\(\)\)\s*\{/.test(handler),
    "the handler returns for every pointer while a touch is held",
  );
});
