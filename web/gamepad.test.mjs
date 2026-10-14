import assert from "node:assert/strict";
import { test } from "node:test";

import {
  assignPad,
  createPadInput,
  createPadRumble,
  defaultPadBindings,
  inputLabel,
  isPadAction,
  loadPadBindings,
  loadRawPadBindings,
  padActions,
  padFamily,
  rawInputs,
  standardInputs,
  storeRawTable,
  targetLabel,
} from "./gamepad.js";
import { createKeyHolds } from "./key-holds.js";
import { keyOrder } from "./keybindings.js";
import { SETTINGS } from "./keypad-layout.js";
import { RAPID_FIRE } from "./rapid-fire.js";
import { QUICK_LOAD, QUICK_SAVE } from "./checkpoint.js";

// A controller as the browser lists one: a picture of every button and axis.
// The tests change the picture and read the page's holds, which are the real
// ones, so what is checked is what the game would have been sent.
const controller = ({
  index = 0,
  id = "Xbox Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 0b13)",
  mapping = "standard",
  buttons = 17,
  axes = 4,
} = {}) => ({
  index,
  id,
  mapping,
  connected: true,
  buttons: Array.from({ length: buttons }, () => ({ pressed: false, value: 0 })),
  axes: new Array(axes).fill(0),
});

const hold = (pad, n, value = 1) => {
  pad.buttons[n] = { pressed: value > 0, value };
};
const letGoOf = (pad, n) => hold(pad, n, 0);
const stick = (pad, first, x, y) => {
  pad.axes[first] = x;
  pad.axes[first + 1] = y;
};
const degrees = (angle, length = 1) => [
  Math.cos((angle * Math.PI) / 180) * length,
  Math.sin((angle * Math.PI) / 180) * length,
];

const harness = ({ bindings = loadPadBindings(null), raw = {}, allows = () => true } = {}) => {
  const sent = [];
  const holds = createKeyHolds({
    press: name => sent.push(`+${name}`),
    release: name => sent.push(`-${name}`),
  });
  const acts = [];
  const seen = [];
  let flowing = true;
  const input = createPadInput({
    latch: holds.latch,
    lift: holds.lift,
    moveTo: holds.moveTo,
    act: name => acts.push(name),
    bindingsFor: pad => (pad.standard ? bindings : raw[pad.id] ?? {}),
    game: () => flowing,
    allows,
    activity: info => seen.push(info),
  });
  return {
    input,
    holds,
    acts,
    seen,
    flow: on => { flowing = on; },
    // What the game was sent since the last look.
    take: () => sent.splice(0),
  };
};

test("the default layout reaches every phone key and leaves quick save and load off", () => {
  assert.deepEqual(Object.keys(defaultPadBindings).sort(), [...standardInputs].sort());
  const reached = new Set(Object.values(defaultPadBindings).filter(Boolean));
  for (const name of keyOrder) assert.ok(reached.has(name), `no control sends ${name}`);
  assert.ok(reached.has(SETTINGS) && reached.has(RAPID_FIRE));
  assert.ok(!reached.has(QUICK_SAVE) && !reached.has(QUICK_LOAD));
  // The face and the corners, as the layout's comment explains them.
  assert.equal(defaultPadBindings.b0, "OK");
  assert.equal(defaultPadBindings.b1, "CLR");
  assert.equal(defaultPadBindings.b4, "MENU");
  assert.equal(defaultPadBindings.b5, "SOFT2");
  assert.equal(defaultPadBindings.b6, "*");
  assert.equal(defaultPadBindings.b7, "#");
  // The right stick is the number block around 5.
  assert.deepEqual(
    ["rs-upleft", "rs-up", "rs-upright", "rs-left", "rs-right", "rs-downleft", "rs-down", "rs-downright"]
      .map(input => defaultPadBindings[input]),
    ["1", "2", "3", "4", "6", "7", "8", "9"],
  );
  // The home button is the operating system's and is not offered at all.
  assert.ok(!standardInputs.includes("b16"));
});

test("a stored table keeps what the user set and takes defaults for the rest", () => {
  assert.deepEqual(loadPadBindings(null), defaultPadBindings);
  assert.deepEqual(loadPadBindings([1, 2]), defaultPadBindings);
  const loaded = loadPadBindings({ b0: "5", b1: "", b2: "NOT_A_KEY", b16: "OK", "rs-up": QUICK_SAVE });
  assert.equal(loaded.b0, "5");
  assert.equal(loaded.b1, "", "a control cleared on purpose stays clear");
  assert.equal(loaded.b2, "", "a key this build does not have is not sent");
  assert.equal(loaded["rs-up"], QUICK_SAVE);
  assert.equal(loaded.b3, defaultPadBindings.b3);
  assert.ok(!Object.hasOwn(loaded, "b16"), "a control this build does not offer is dropped");
  assert.deepEqual(Object.keys(loaded), standardInputs);
});

test("assigning changes one control and refuses what this build does not know", () => {
  const table = loadPadBindings(null);
  const moved = assignPad(table, "b0", "5");
  assert.equal(moved.b0, "5");
  assert.equal(moved.b2, "5", "two controls may send one key");
  assert.equal(assignPad(table, "b0", "").b0, "");
  assert.equal(assignPad(table, "b0", "NOPE"), table);
  assert.equal(assignPad(table, "b40", "OK"), table);
  assert.equal(assignPad(table, "b16", "OK").b16, "OK", "a raw controller's button number is a control");
});

test("an unrecognised controller's table is kept under its name, bounded", () => {
  const loaded = loadRawPadBindings({
    "USB Gamepad": { b0: "OK", "a1-": "UP", b99: "OK", b2: "NOPE", b3: "" },
    "": { b0: "OK" },
    Empty: { b0: "" },
  });
  assert.deepEqual(loaded, { "USB Gamepad": { b0: "OK", "a1-": "UP" } });
  assert.deepEqual(loadRawPadBindings("junk"), {});

  let tables = {};
  for (let n = 0; n < 20; n++) tables = storeRawTable(tables, `pad ${n}`, { b0: "OK" });
  assert.equal(Object.keys(tables).length, 16);
  assert.ok(!Object.hasOwn(tables, "pad 0") && Object.hasOwn(tables, "pad 19"));
  // Writing a table again makes it the newest, so it outlives the others.
  tables = storeRawTable(tables, "pad 4", { b1: "CLR" });
  assert.equal(Object.keys(tables).at(-1), "pad 4");
  assert.deepEqual(tables["pad 4"], { b1: "CLR" });
  tables = storeRawTable(tables, "pad 4", { b1: "" });
  assert.ok(!Object.hasOwn(tables, "pad 4"), "a table with nothing bound is removed");
});

test("an unrecognised controller is read as numbered buttons and two ways per axis", () => {
  assert.deepEqual(rawInputs({ buttons: 3, axes: 2 }), ["b0", "b1", "b2", "a0+", "a0-", "a1+", "a1-"]);
  assert.equal(rawInputs({ buttons: 99, axes: 99 }).length, 32 + 16 * 2);
});

test("controls are named the way the controller in hand prints them", () => {
  assert.equal(padFamily("Xbox 360 Controller (XInput STANDARD GAMEPAD)"), "xbox");
  assert.equal(padFamily("Wireless Controller (STANDARD GAMEPAD Vendor: 054c Product: 09cc)"), "playstation");
  assert.equal(padFamily("DualSense Wireless Controller"), "playstation");
  assert.equal(padFamily("Pro Controller (STANDARD GAMEPAD Vendor: 057e Product: 2009)"), "nintendo");
  assert.equal(inputLabel("b0", { family: "xbox" }), "A (아래)");
  assert.equal(inputLabel("b0", { family: "playstation" }), "✕ (아래)");
  // The bottom button of a Nintendo controller is B: the layout is by place.
  assert.equal(inputLabel("b0", { family: "nintendo" }), "B (아래)");
  assert.equal(inputLabel("b1", { family: "nintendo" }), "A (오른쪽)");
  assert.equal(inputLabel("b6", { family: "playstation" }), "L2");
  assert.equal(inputLabel("b9", { family: "xbox" }), "Menu");
  assert.equal(inputLabel("b12"), "십자 ↑");
  assert.equal(inputLabel("ls-left"), "왼스틱 ←");
  assert.equal(inputLabel("rs-upleft"), "오른스틱 ↖");
  assert.equal(inputLabel("b0", { standard: false }), "버튼 1");
  assert.equal(inputLabel("a2-", { standard: false }), "축 3 −");
  assert.equal(targetLabel(""), "없음");
  assert.equal(targetLabel("CLR"), "취소");
  assert.equal(targetLabel(RAPID_FIRE), "연사");
  assert.deepEqual(padActions.filter(isPadAction), padActions);
  assert.ok(!isPadAction("OK"));
});

test("a button holds its key for as long as it is down", () => {
  const h = harness();
  const pad = controller();
  h.input.poll([pad]);
  hold(pad, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+OK"]);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "a held button is one press, not a stream");
  letGoOf(pad, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-OK"]);
});

test("a release goes before the press it makes room for", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 0);
  h.input.poll([pad]);
  h.take();
  letGoOf(pad, 0);
  hold(pad, 1);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-OK", "+CLR"]);
});

test("the direction pad and the left stick are one direction key between them", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 12);
  h.input.poll([pad]);
  stick(pad, 0, 0, -1);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+UP"], "a key held two ways is one press");
  letGoOf(pad, 12);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "the stick still holds it");
  stick(pad, 0, 0, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-UP"]);
});

test("the left stick goes one way at a time", () => {
  const h = harness();
  const pad = controller();
  stick(pad, 0, ...degrees(-80));
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+UP"]);
  // Swung round to the right through the diagonal: one key let go, one pressed.
  stick(pad, 0, ...degrees(-10));
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-UP", "+RIGHT"]);
});

test("the right stick is the number block, and turning it slides from key to key", () => {
  const h = harness();
  const pad = controller();
  stick(pad, 2, 0, -1);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+2"]);
  // Just past the edge of the up sector, within the margin: still 2.
  stick(pad, 2, ...degrees(295));
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "a thumb resting on an edge does not flicker");
  // Clearly into the next sector.
  stick(pad, 2, ...degrees(305));
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-2", "+3"]);
  stick(pad, 2, ...degrees(135));
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-3", "+7"]);
  stick(pad, 2, 0, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-7"]);
  // Approached straight from the middle, a diagonal is its own key.
  stick(pad, 2, ...degrees(295));
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+3"]);
});

test("a stick needs a firm push to arrive and lets go only near the middle", () => {
  const h = harness();
  const pad = controller();
  stick(pad, 2, 0, 0.45);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), []);
  stick(pad, 2, 0, 0.6);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+8"]);
  stick(pad, 2, 0, 0.4);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), []);
  stick(pad, 2, 0, 0.3);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-8"]);
});

test("a trigger is a lever with two thresholds", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 6, 0.3);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "a finger resting on the trigger is not a press");
  hold(pad, 6, 0.6);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+*"]);
  hold(pad, 6, 0.4);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "easing off a little does not release");
  hold(pad, 6, 0.2);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-*"]);
  // A digital button that reports only `pressed` is still a press.
  pad.buttons[2] = { pressed: true };
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+5"]);
});

test("a page control runs once on the press and holds nothing", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 9);
  h.input.poll([pad]);
  h.input.poll([pad]);
  letGoOf(pad, 9);
  h.input.poll([pad]);
  assert.deepEqual(h.acts, [SETTINGS]);
  assert.deepEqual(h.take(), [], "a page control sends no handset key");
  hold(pad, 10);
  h.input.poll([pad]);
  assert.deepEqual(h.acts, [SETTINGS, RAPID_FIRE]);
});

test("a page control the page refuses does not run", () => {
  const h = harness({ allows: name => name !== SETTINGS });
  const pad = controller();
  hold(pad, 9);
  h.input.poll([pad]);
  assert.deepEqual(h.acts, []);
});

test("a stick direction can be a page control", () => {
  const h = harness({ bindings: assignPad(loadPadBindings(null), "rs-up", QUICK_SAVE) });
  const pad = controller();
  stick(pad, 2, ...degrees(-45));
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+3"]);
  stick(pad, 2, 0, -1);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-3"]);
  assert.deepEqual(h.acts, [QUICK_SAVE]);
});

test("an unbound control does nothing", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 11);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), []);
  assert.deepEqual(h.acts, []);
});

test("the home button does nothing even in a table that names it", () => {
  const h = harness({ bindings: { ...loadPadBindings(null), b16: "OK" } });
  const pad = controller();
  hold(pad, 16);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), []);
});

test("keys stop while the page holds input back and do not press on their own after", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+OK"]);
  h.flow(false);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-OK"], "a key held when input stops is let go");
  hold(pad, 1);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "a press while input is held back goes nowhere");
  h.flow(true);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "nothing lands when input comes back");
  letGoOf(pad, 0);
  h.input.poll([pad]);
  hold(pad, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+OK"]);
});

test("a control held across a reset is dead until it is let go", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 0);
  stick(pad, 2, 0, -1);
  h.input.poll([pad]);
  // Two controls pressed in one picture arrive together; their order is not
  // anything a person did.
  assert.deepEqual(h.take().sort(), ["+2", "+OK"]);
  // The page's own order: the holds let go first, then the controllers.
  h.holds.clear();
  h.input.reset();
  assert.deepEqual(h.take().sort(), ["-2", "-OK"]);
  h.input.poll([pad]);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), []);
  letGoOf(pad, 0);
  stick(pad, 2, 0, 0);
  h.input.poll([pad]);
  hold(pad, 0);
  stick(pad, 2, 0, -1);
  h.input.poll([pad]);
  assert.deepEqual(h.take().sort(), ["+2", "+OK"]);
});

test("a reset without the holds cleared first still lets go", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 0);
  h.input.poll([pad]);
  h.take();
  h.input.reset();
  assert.deepEqual(h.take(), ["-OK"]);
});

test("the button that revealed a controller acts at once", () => {
  // A browser shows a controller to the page only after one of its buttons is
  // pressed, so that press is often still down at the first reading.
  const h = harness();
  const pad = controller();
  hold(pad, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+OK"]);
});

test("an unrecognised controller reads its own table, and a resting axis is not a press", () => {
  const id = "USB Gamepad (Vendor: 0079 Product: 0011)";
  const h = harness({ raw: { [id]: { b3: "OK", "a1-": "UP", "a3+": "#", "a3-": "*" } } });
  const pad = controller({ id, mapping: "", buttons: 10, axes: 4 });
  // A trigger that rests at one end of its travel.
  pad.axes[3] = -1;
  h.input.poll([pad]);
  assert.deepEqual(h.take(), [], "an axis already at its end when first seen is its rest");
  hold(pad, 3);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+OK"]);
  pad.axes[1] = -1;
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["+UP"]);
  pad.axes[1] = 0;
  pad.axes[3] = 1;
  h.input.poll([pad]);
  assert.deepEqual(h.take(), ["-UP", "+#"]);
  // The standard layout's defaults are not applied to it.
  hold(pad, 0);
  h.input.poll([pad]);
  assert.deepEqual(h.take(), []);
});

test("two controllers holding one key are one press", () => {
  const h = harness();
  const first = controller({ index: 0 });
  const second = controller({ index: 1 });
  hold(first, 0);
  hold(second, 0);
  h.input.poll([first, second]);
  assert.deepEqual(h.take(), ["+OK"]);
  letGoOf(first, 0);
  h.input.poll([first, second]);
  assert.deepEqual(h.take(), []);
  letGoOf(second, 0);
  h.input.poll([first, second]);
  assert.deepEqual(h.take(), ["-OK"]);
});

test("a controller that goes away lets go of what it held", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 0);
  h.input.poll([pad]);
  h.take();
  assert.equal(h.input.poll([null]), 0);
  assert.deepEqual(h.take(), ["-OK"]);
  assert.equal(h.seen.at(-1).gone, true);
});

test("a different controller in the same place starts again", () => {
  const h = harness();
  const pad = controller();
  hold(pad, 0);
  h.input.poll([pad]);
  h.take();
  const other = controller({ id: "Pro Controller (STANDARD GAMEPAD Vendor: 057e Product: 2009)" });
  h.input.poll([other]);
  assert.deepEqual(h.take(), ["-OK"]);
  assert.equal(h.seen.at(-1).family, "nintendo");
});

test("the panel hears which controls are lit and which controller was pressed", () => {
  const h = harness();
  const pad = controller();
  h.input.poll([pad]);
  assert.equal(h.seen.length, 1, "a controller is announced when it is first read");
  assert.equal(h.seen[0].rose, true);
  assert.equal(h.seen[0].standard, true);
  h.input.poll([pad]);
  assert.equal(h.seen.length, 1, "an unchanged picture says nothing");
  hold(pad, 2);
  h.input.poll([pad]);
  assert.deepEqual([...h.seen.at(-1).lit], ["b2"]);
  assert.equal(h.seen.at(-1).rose, true);
  letGoOf(pad, 2);
  h.input.poll([pad]);
  assert.deepEqual([...h.seen.at(-1).lit], []);
  assert.equal(h.seen.at(-1).rose, false);
});

// The controllers' motors, as the browser exposes them.
const motor = ({ effects = ["dual-rumble"], type, rejects = false, throws = false } = {}) => {
  const calls = [];
  const settle = value => (rejects ? Promise.reject(new Error("refused")) : Promise.resolve(value));
  return {
    calls,
    actuator: {
      ...(effects ? { effects } : {}),
      ...(type ? { type } : {}),
      playEffect(kind, options) {
        if (throws) throw new Error("refused outright");
        calls.push(["play", kind, options]);
        return settle("complete");
      },
      reset() {
        calls.push(["reset"]);
        return settle("complete");
      },
    },
  };
};

test("a controller's motor plays the guest's level as its strength", () => {
  const one = motor();
  const rumble = createPadRumble({ gamepads: () => [{ vibrationActuator: one.actuator }, null] });
  assert.equal(rumble.available(), true);
  rumble.play(60, 300);
  assert.deepEqual(one.calls, [["play", "dual-rumble", {
    duration: 300,
    startDelay: 0,
    strongMagnitude: 0.6,
    weakMagnitude: 0.6,
  }]]);
  one.calls.length = 0;
  rumble.play(0, 300);
  rumble.play(50, 0);
  rumble.stop();
  assert.deepEqual(one.calls, [["reset"], ["reset"], ["reset"]], "nothing of either is a stop");
});

test("a motor that cannot rumble is not asked, and a refusal is not an error", async () => {
  const none = motor({ effects: ["trigger-rumble"] });
  const old = motor({ effects: null, type: "dual-rumble" });
  const refusing = motor({ rejects: true });
  const throwing = motor({ throws: true });
  const rumble = createPadRumble({
    gamepads: () => [none, old, refusing, throwing].map(m => ({ vibrationActuator: m.actuator })),
  });
  rumble.play(100, 200);
  await new Promise(done => setTimeout(done, 0));
  assert.deepEqual(none.calls, []);
  assert.equal(old.calls.length, 1, "an older browser names its one effect as type");
  assert.equal(refusing.calls.length, 1);
  assert.equal(createPadRumble({ gamepads: () => { throw new Error("blocked"); } }).available(), false);
  assert.equal(createPadRumble({ gamepads: () => [{}] }).available(), false);
});
