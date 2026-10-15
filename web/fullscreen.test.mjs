import assert from "node:assert/strict";
import { test } from "node:test";

import { FULLSCREEN_KEY, createFullscreen } from "./fullscreen.js";

const rootOf = () => {
  const classes = new Set();
  return {
    classes,
    classList: {
      toggle: (name, on) => {
        if (on) classes.add(name);
        else classes.delete(name);
      },
    },
  };
};

const storageOf = (entries = {}) => {
  const map = new Map(Object.entries(entries));
  return {
    map,
    getItem: key => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => map.set(key, String(value)),
  };
};

test("the page starts in the layout it was left in", () => {
  const root = rootOf();
  const fullscreen = createFullscreen({ root, storage: storageOf({ [FULLSCREEN_KEY]: "on" }) });
  assert.equal(fullscreen.on, true);
  assert.ok(root.classes.has("fullscreen"), "the class is on before anything is drawn");
  const plain = rootOf();
  assert.equal(createFullscreen({ root: plain, storage: storageOf() }).on, false);
  assert.ok(!plain.classes.has("fullscreen"));
});

test("switching draws the layout, remembers it, and says so once", () => {
  const root = rootOf();
  const storage = storageOf();
  const heard = [];
  const fullscreen = createFullscreen({ root, storage, changed: on => heard.push(on) });
  assert.equal(fullscreen.toggle(), true);
  assert.ok(root.classes.has("fullscreen"));
  assert.equal(storage.getItem(FULLSCREEN_KEY), "on");
  assert.equal(fullscreen.set(true), false, "asking for what is already so changes nothing");
  assert.equal(fullscreen.toggle(), true);
  assert.ok(!root.classes.has("fullscreen"));
  assert.equal(storage.getItem(FULLSCREEN_KEY), "off");
  assert.deepEqual(heard, [true, false]);
});

test("a storage that throws costs only the memory of the choice", () => {
  const root = rootOf();
  const storage = {
    getItem: () => { throw new Error("denied"); },
    setItem: () => { throw new Error("denied"); },
  };
  const fullscreen = createFullscreen({ root, storage });
  assert.equal(fullscreen.on, false);
  assert.equal(fullscreen.toggle(), true);
  assert.ok(root.classes.has("fullscreen"));
});
