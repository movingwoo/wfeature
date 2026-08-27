import assert from "node:assert/strict";
import { test } from "node:test";

import { createSafeStorage } from "./storage.js";

// A storage that works, so the ordinary path is tested against the same shape
// the browser gives.
const workingStorage = (entries = {}) => {
  const map = new Map(Object.entries(entries));
  return {
    map,
    getItem: key => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => map.set(key, String(value)),
    removeItem: key => map.delete(key),
  };
};

// A browser told to block site data throws on the property itself, before any
// key is named. This is the case a `try` around `getItem` does not catch.
const deniedProperty = () => {
  throw new DOMException("The operation is insecure.", "SecurityError");
};

// A browser that allows storage and has no room left. Reads still work.
const fullStorage = () => {
  const map = new Map();
  return {
    map,
    getItem: key => (map.has(key) ? map.get(key) : null),
    setItem: () => {
      throw new DOMException("exceeded the quota", "QuotaExceededError");
    },
    removeItem: key => map.delete(key),
  };
};

test("a working storage is read and written through", () => {
  const backing = workingStorage();
  const store = createSafeStorage(() => backing);
  assert.equal(store.setItem("wfeature:scale", 2), true);
  assert.equal(store.getItem("wfeature:scale"), "2");
  assert.equal(backing.map.get("wfeature:scale"), "2");
  assert.equal(store.removeItem("wfeature:scale"), true);
  assert.equal(store.getItem("wfeature:scale"), null);
  assert.equal(store.available(), true);
});

test("a key nothing ever stored reads as absent rather than undefined", () => {
  const store = createSafeStorage(() => workingStorage());
  assert.equal(store.getItem("wfeature:never-set"), null);
});

test("a browser that throws on the storage property does not throw here", () => {
  const store = createSafeStorage(deniedProperty);
  assert.equal(store.getItem("wfeature:scale"), null);
  assert.equal(store.setItem("wfeature:scale", 2), false);
  assert.equal(store.removeItem("wfeature:scale"), false);
  assert.equal(store.available(), false);
});

test("a setting that could not be stored still reads back for this page", () => {
  // The whole reason the page keeps a shadow: a control whose value cannot be
  // saved should still work until the tab is closed, rather than snapping back
  // to the default the moment it is read.
  const store = createSafeStorage(deniedProperty);
  store.setItem("wfeature:scale", 2);
  assert.equal(store.getItem("wfeature:scale"), "2");
  store.removeItem("wfeature:scale");
  assert.equal(store.getItem("wfeature:scale"), null);
});

test("a full origin is the same case as a denied one", () => {
  const backing = fullStorage();
  const store = createSafeStorage(() => backing);
  assert.equal(store.setItem("wfeature:scale", 4), false);
  assert.equal(store.getItem("wfeature:scale"), "4");
  assert.equal(backing.map.size, 0);
  assert.equal(store.available(), false);
});

test("a write that failed wins over a stale value in storage", () => {
  // The key was written when there was room, and the value the page chose
  // since could not be stored. Reading the old one back would show a setting
  // the page is not using.
  const backing = workingStorage({ "wfeature:scale": "1" });
  let full = false;
  const store = createSafeStorage(() => ({
    ...backing,
    setItem: (key, value) => {
      if (full) throw new DOMException("exceeded the quota", "QuotaExceededError");
      backing.setItem(key, value);
    },
  }));
  full = true;
  assert.equal(store.setItem("wfeature:scale", 4), false);
  assert.equal(store.getItem("wfeature:scale"), "4");
  full = false;
  assert.equal(store.setItem("wfeature:scale", 2), true);
  assert.equal(store.getItem("wfeature:scale"), "2");
  assert.equal(backing.map.get("wfeature:scale"), "2");
});

test("storage that arrives late is used once it is there", () => {
  // Resolving the backing once at load would leave a page that was denied
  // storage denied for as long as it lives. It is resolved per call instead.
  let backing = null;
  const store = createSafeStorage(() => backing);
  assert.equal(store.getItem("wfeature:scale"), null);
  backing = workingStorage({ "wfeature:scale": "2" });
  assert.equal(store.getItem("wfeature:scale"), "2");
});

test("a storage missing removeItem does not take the page down", () => {
  // Not every object that answers like storage is storage: the page hands its
  // own map to a test, and an old browser's shim may be short a method.
  const store = createSafeStorage(() => ({ getItem: () => null, setItem: () => {} }));
  assert.equal(store.removeItem("wfeature:scale"), false);
});
