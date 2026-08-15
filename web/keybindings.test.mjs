import assert from "node:assert/strict";
import { test } from "node:test";

import {
  assign,
  bindable,
  clear,
  codeLookup,
  defaultBindings,
  heldBy,
  keyOrder,
  loadBindings,
} from "./keybindings.js";

// The rule the whole feature rests on is that a physical key is on one phone
// key at a time. Nothing at runtime would say otherwise if it broke — a press
// would simply send two keys, and a game that ignores one of them would look
// like a game that ignores the key.

test("the defaults are one physical key per phone key, in keypad order", () => {
  assert.deepEqual(Object.keys(defaultBindings).sort(), [...keyOrder].sort());
  const seen = new Map();
  for (const name of keyOrder) {
    const code = defaultBindings[name];
    assert.ok(code, `${name} has no default binding`);
    assert.ok(!seen.has(code), `${name} and ${seen.get(code)} both sit on ${code}`);
    seen.set(code, name);
  }
  // The panel is drawn from keyOrder because the table's own order is not this
  // one: "0" would come out ahead of "1".
  assert.equal(keyOrder[0], "1");
  assert.equal(keyOrder.indexOf("0"), 10);
});

test("taking a key that is already in use empties the key that had it", () => {
  const bindings = loadBindings(null);
  assert.equal(bindings.OK, "Space");

  const next = assign(bindings, "5", "Space");
  assert.equal(next["5"], "Space");
  assert.equal(next.OK, "", "the key that held Space kept it");
  // Everything else is where it was.
  assert.equal(next["1"], defaultBindings["1"]);
  assert.equal(Object.keys(next).length, keyOrder.length);
});

test("a key can be taken back from the key that took it", () => {
  const bindings = assign(loadBindings(null), "5", "Space");
  const back = assign(bindings, "OK", "Space");
  assert.equal(back.OK, "Space");
  assert.equal(back["5"], "");
});

test("re-assigning a key to where it already is changes nothing", () => {
  const bindings = loadBindings(null);
  assert.deepEqual(assign(bindings, "OK", "Space"), bindings);
});

test("clearing empties one key and leaves the rest", () => {
  const bindings = clear(loadBindings(null), "CALL");
  assert.equal(bindings.CALL, "");
  assert.equal(bindings.CLR, "Backspace");
  assert.equal(codeLookup(bindings).Backslash, undefined, "an empty key is still looked up");
});

test("keys the browser needs are refused", () => {
  for (const code of ["Tab", "Escape", "F5", "ShiftLeft", "MetaRight", "CapsLock", ""]) {
    assert.equal(bindable(code), false, `${code} was accepted`);
  }
  for (const code of ["KeyQ", "Space", "Numpad5", "ArrowUp", "F1"]) {
    assert.equal(bindable(code), true, `${code} was refused`);
  }
  // A refused key must not be able to arrive the long way round either.
  const bindings = loadBindings(null);
  assert.deepEqual(assign(bindings, "OK", "Tab"), bindings);
});

test("a stored table is read back as it was stored", () => {
  const stored = { ...defaultBindings, "5": "Space", OK: "" };
  const bindings = loadBindings(stored);
  assert.equal(bindings["5"], "Space");
  assert.equal(bindings.OK, "");
});

test("a stored table survives this build knowing a key it does not", () => {
  // The stored half is the explicit one: a key the user has moved keeps its
  // physical key, and a phone key the stored table predates takes its default
  // only if that default is still free.
  const stored = { ...defaultBindings, "5": "Space" };
  delete stored.OK;
  const bindings = loadBindings(stored);
  assert.equal(bindings["5"], "Space");
  assert.equal(bindings.OK, "", "the new key took a physical key that was spoken for");

  const roomy = { ...defaultBindings };
  delete roomy.OK;
  assert.equal(loadBindings(roomy).OK, "Space");
});

test("nonsense in storage falls back rather than throwing", () => {
  assert.deepEqual(loadBindings(null), loadBindings(undefined));
  assert.deepEqual(loadBindings("not a table"), loadBindings(null));
  assert.equal(loadBindings({ OK: 5 }).OK, "", "a key bound to a number is bound to nothing");
  assert.equal(loadBindings({ nosuchkey: "KeyP" }).OK, "Space");
  // Two names on one key cannot be reached through the panel, but a hand-edited
  // store can hold it and only one of them can win.
  const doubled = loadBindings({ ...defaultBindings, "5": "Space", OK: "Space" });
  assert.equal(Object.values(doubled).filter(code => code === "Space").length, 1);
});

test("the handler's direction drops what nothing is bound to", () => {
  const lookup = codeLookup(clear(loadBindings(null), "OK"));
  assert.equal(lookup.KeyQ, "4");
  assert.ok(!Object.hasOwn(lookup, "Space"));
});

test("the panel can say which key is about to lose one", () => {
  const bindings = loadBindings(null);
  assert.equal(heldBy(bindings, "Space", "5"), "OK");
  assert.equal(heldBy(bindings, "Space", "OK"), "", "a key does not take from itself");
  assert.equal(heldBy(bindings, "KeyP", "5"), "");
});
