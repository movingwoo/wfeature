import assert from "node:assert/strict";
import { test } from "node:test";
import { createRapidFire, RAPID_FIRE } from "./rapid-fire.js";
import { createKeyHolds } from "./key-holds.js";
import { createKeypadLayout, assignable } from "./keypad-layout.js";
import { keyOrder } from "./keybindings.js";

const setup = () => {
  let now = 0;
  let next;
  const sent = [];
  const fire = createRapidFire({
    send: (action, name) => sent.push([now, action, name]),
    schedule: (callback, delay) => { next = callback; assert.equal(delay, 50); return 1; },
    cancel: () => { next = null; },
  });
  const tick = (elapsed = 50) => { now += elapsed; const callback = next; next = null; callback?.(); };
  return { fire, sent, tick, pending: () => !!next };
};

test("off holds keys normally and schedules nothing", () => {
  const { fire, sent, tick, pending } = setup();
  fire.press("5"); tick(); fire.release("5");
  assert.deepEqual(sent, [[0, "press", "5"], [50, "release", "5"]]);
  assert.equal(pending(), false);
});

test("manual repeats only held 5 and OK at ten presses per second", () => {
  const { fire, sent, tick, pending } = setup();
  fire.cycle();
  assert.equal(fire.mode(), "manual");
  assert.equal(pending(), false);
  for (const name of ["5", "OK", "2"]) fire.press(name);
  for (let i = 0; i < 19; i++) tick();
  for (const name of ["5", "OK"]) {
    assert.equal(sent.filter(([, action, key]) => action === "press" && key === name).length, 10);
    assert.equal(sent.filter(([, action, key]) => action === "release" && key === name).length, 10);
    fire.release(name);
  }
  assert.equal(sent.filter(([, , key]) => key === "2").length, 1);
  assert.equal(pending(), false);
  fire.release("2");
});

for (const first of ["5", "OK"]) {
  test(`auto toggles ${first} and switches exclusively to the other key`, () => {
    const { fire, sent, tick, pending } = setup();
    const other = first === "5" ? "OK" : "5";
    fire.cycle(); fire.cycle(); tick();
    assert.deepEqual(sent, []);
    assert.equal(pending(), false);
    fire.press(first); fire.release(first);
    for (let i = 0; i < 18; i++) tick();
    assert.equal(sent.filter(([, action]) => action === "press").length, 10);
    fire.press(other);
    assert.deepEqual(sent.slice(-2), [[950, "release", first], [950, "press", other]]);
    fire.release(other);
    tick(); tick();
    assert.deepEqual(sent.at(-1), [1050, "press", other]);
    fire.press(other);
    assert.deepEqual(sent.at(-1), [1050, "release", other]);
    const stopped = sent.length;
    tick(); fire.release(other); tick();
    assert.equal(sent.length, stopped);
    assert.equal(pending(), false);
  });
}

test("auto ignores duplicate downs and does not resume a previously held target", () => {
  const { fire, sent, tick } = setup();
  fire.cycle(); fire.cycle();
  fire.press("OK"); fire.press("OK"); tick(); tick();
  assert.equal(sent.filter(([, action]) => action === "press").length, 2);
  fire.press("5"); fire.release("5");
  fire.press("5"); fire.release("5");
  const stopped = sent.length;
  fire.release("OK"); tick();
  assert.equal(sent.length, stopped);
  fire.press("2"); fire.release("2");
  assert.deepEqual(sent.slice(-2), [[150, "press", "2"], [150, "release", "2"]]);
});

test("reset releases a pulse and stops all future input with mode off", () => {
  const { fire, sent, tick, pending } = setup();
  fire.cycle(); fire.cycle(); fire.press("OK"); fire.release("OK");
  fire.reset(); fire.reset(); tick();
  assert.deepEqual(sent, [[0, "press", "OK"], [0, "release", "OK"]]);
  assert.equal(fire.mode(), "off");
  assert.equal(pending(), false);
});

test("entering auto with held keys waits for a new press and leaving auto restores ordinary holds", () => {
  const { fire, sent, tick, pending } = setup();
  fire.cycle(); fire.press("5");
  fire.cycle();
  assert.deepEqual(sent, [[0, "press", "5"], [0, "release", "5"]]);
  assert.equal(pending(), false);
  fire.release("5"); fire.press("OK"); fire.release("OK");
  fire.cycle();
  assert.deepEqual(sent.at(-1), [0, "release", "OK"]);
  tick();
  assert.equal(pending(), false);
  fire.press("5");
  assert.deepEqual(sent.at(-1), [50, "press", "5"]);
});

test("keyboard and multiple pointers share one repeat stream until the last release", () => {
  const { fire, sent, tick, pending } = setup();
  const holds = createKeyHolds({ press: fire.press, release: fire.release });
  fire.cycle();
  holds.moveTo(1, "5"); holds.moveTo(2, "5"); holds.latch("keyboard:KeyW", "5");
  holds.lift(1); holds.lift(2); tick(); tick();
  assert.deepEqual(sent, [[0, "press", "5"], [50, "release", "5"], [100, "press", "5"]]);
  holds.lift("keyboard:KeyW");
  assert.equal(pending(), false);
  assert.deepEqual(sent.at(-1), [100, "release", "5"]);
});

test("switch placement persists but never becomes a handset keyboard binding", () => {
  const data = new Map();
  const storage = { getItem: key => data.get(key), setItem: (key, value) => data.set(key, value) };
  const layout = createKeypadLayout(storage);
  layout.set("pad-r1c1", RAPID_FIRE);
  assert.equal(createKeypadLayout(storage).keyAt("pad-r1c1"), RAPID_FIRE);
  assert.ok(assignable.includes(RAPID_FIRE));
  assert.ok(!keyOrder.includes(RAPID_FIRE));
});


test("a delayed timer never replays missed presses in a burst", () => {
  const { fire, sent, tick } = setup();
  fire.cycle(); fire.cycle(); fire.press("OK"); fire.release("OK");
  tick(5000);
  assert.deepEqual(sent, [[0, "press", "OK"], [5000, "release", "OK"]]);
  tick();
  assert.deepEqual(sent.at(-1), [5050, "press", "OK"]);
});
