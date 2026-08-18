import assert from "node:assert/strict";
import { test } from "node:test";

import { createKeyHolds } from "./key-holds.js";

// The tracker's job is the order and the count: what the game is told, and how
// often. Each test records the run of press/release calls and reads it back.
const record = () => {
  const sent = [];
  const holds = createKeyHolds({
    press: name => sent.push(`press ${name}`),
    release: name => sent.push(`release ${name}`),
  });
  return { holds, sent };
};

test("a finger dragged across the pad presses each key it crosses, in order", () => {
  const { holds, sent } = record();
  holds.moveTo(1, "2");
  for (const name of ["6", "8", "4"]) holds.moveTo(1, name);
  holds.lift(1);
  assert.deepEqual(sent, [
    "press 2",
    "release 2",
    "press 6",
    "release 6",
    "press 8",
    "release 8",
    "press 4",
    "release 4",
  ]);
});

test("a key is released before the next one is pressed", () => {
  // A game that reads the two in the order they arrive would otherwise see both
  // keys held at once, which a single finger never means.
  const { holds, sent } = record();
  holds.moveTo(1, "2");
  holds.moveTo(1, "6");
  assert.deepEqual(sent.slice(1), ["release 2", "press 6"]);
});

test("moving within the same key says nothing", () => {
  const { holds, sent } = record();
  holds.moveTo(1, "5");
  holds.moveTo(1, "5");
  holds.moveTo(1, "5");
  assert.deepEqual(sent, ["press 5"]);
});

test("a finger that leaves the keys lets go, and pressing again on return", () => {
  const { holds, sent } = record();
  holds.moveTo(1, "5");
  holds.moveTo(1, null);
  holds.moveTo(1, "5");
  holds.lift(1);
  assert.deepEqual(sent, ["press 5", "release 5", "press 5", "release 5"]);
  assert.equal(holds.tracking(1), false);
});

test("two fingers on one key are one press, and it is held until the last leaves", () => {
  const { holds, sent } = record();
  holds.moveTo(1, "5");
  holds.moveTo(2, "5");
  assert.deepEqual(sent, ["press 5"]);
  holds.lift(1);
  assert.deepEqual(sent, ["press 5"], "the key is still held by the other finger");
  holds.lift(2);
  assert.deepEqual(sent, ["press 5", "release 5"]);
});

test("fingers on different keys do not disturb each other", () => {
  const { holds, sent } = record();
  holds.moveTo(1, "2");
  holds.moveTo(2, "6");
  holds.moveTo(1, "8");
  holds.lift(2);
  holds.lift(1);
  assert.deepEqual(sent, [
    "press 2",
    "press 6",
    "release 2",
    "press 8",
    "release 6",
    "release 8",
  ]);
});

test("a finger that goes down beside the keys presses the first one it reaches", () => {
  // A slide begins on the screen or in the margin as readily as on the pad:
  // nothing on a handset says a thumb has to land on a button to press one.
  const { holds, sent } = record();
  holds.track(1);
  assert.equal(holds.tracking(1), true);
  assert.deepEqual(sent, [], "holding nothing yet");
  holds.moveTo(1, "4");
  holds.moveTo(1, "8");
  holds.lift(1);
  assert.deepEqual(sent, ["press 4", "release 4", "press 8", "release 8"]);
});

test("a finger that goes down beside the keys and never reaches one is quiet", () => {
  const { holds, sent } = record();
  holds.track(1);
  holds.moveTo(1, null);
  holds.lift(1);
  assert.deepEqual(sent, []);
});

test("tracking a finger twice does not lose the key it already holds", () => {
  const { holds, sent } = record();
  holds.moveTo(1, "5");
  holds.track(1);
  holds.lift(1);
  assert.deepEqual(sent, ["press 5", "release 5"]);
});

test("a latched key is held wherever the finger wanders, and let go on the lift", () => {
  // The keypad's top row. Those keys are aimed at one at a time, so the pointer
  // stops being followed the moment one of them is pressed.
  const { holds, sent } = record();
  holds.latch(1, "CLR");
  assert.equal(holds.tracking(1), false, "a latched pointer is not followed");
  holds.lift(1);
  assert.deepEqual(sent, ["press CLR", "release CLR"]);
});

test("a latch does not disturb a slide running under another finger", () => {
  const { holds, sent } = record();
  holds.moveTo(1, "2");
  holds.latch(2, "CALL");
  holds.moveTo(1, "8");
  holds.lift(2);
  holds.lift(1);
  assert.deepEqual(sent, [
    "press 2",
    "press CALL",
    "release 2",
    "press 8",
    "release CALL",
    "release 8",
  ]);
});

test("a pointer that never touched a key is not tracked, and lifting it is quiet", () => {
  const { holds, sent } = record();
  assert.equal(holds.tracking(7), false);
  holds.lift(7);
  assert.deepEqual(sent, []);
});

test("lifting twice releases once", () => {
  // pointerup and pointercancel can both arrive for the same touch.
  const { holds, sent } = record();
  holds.moveTo(1, "5");
  holds.lift(1);
  holds.lift(1);
  assert.deepEqual(sent, ["press 5", "release 5"]);
});
