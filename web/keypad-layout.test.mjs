import assert from "node:assert/strict";
import { test } from "node:test";
import {
  createKeypadLayout,
  legacyPresets,
  legacyCells,
  migrateLegacy,
  defaultGrid,
  shapes,
  SETTINGS,
  assignable,
  keyName,
  keyFace,
} from "./keypad-layout.js";
import { keyOrder } from "./keybindings.js";

// Recorded old assignments keep migration tests independent of current defaults.
const asDrawn = {
  // Type4 is not one of the three: it is the empty pad, added with the editor
  // because "no keys at all" has to be a shape to be stored as one. It is here
  // so the loop below covers it, and it is empty but for the one key a pad
  // cannot lose.
  type4: { "band-c1": "SETTINGS" },
  type2: {
    "band-c1": "SETTINGS",
    "band-c3": "MENU",
    "band-c4": "RAPID_FIRE",
    "band-c5": "CALL",
    "band-c6": "SOFT2",
    "band-c7": "CLR",
    "pad-r1c2": "2",
    "pad-r2c1": "4",
    "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r1c5": "1",
    "pad-r1c7": "3",
    "pad-r2c6": "5",
    "pad-r3c5": "7",
    "pad-r3c7": "9",
    "pad-r4c3": "*",
    "pad-r4c4": "0",
    "pad-r4c5": "#",
  },
  type1: {
    "band-c1": "SETTINGS",
    "band-c3": "MENU",
    "band-c4": "RAPID_FIRE",
    "band-c5": "CALL",
    "band-c6": "SOFT2",
    "band-c7": "CLR",
    "pad-r1c2": "UP",
    "pad-r2c1": "LEFT",
    "pad-r2c2": "OK",
    "pad-r2c3": "RIGHT",
    "pad-r3c2": "DOWN",
    "pad-r1c5": "1",
    "pad-r1c6": "2",
    "pad-r1c7": "3",
    "pad-r2c5": "4",
    "pad-r2c6": "5",
    "pad-r2c7": "6",
    "pad-r3c5": "7",
    "pad-r3c6": "8",
    "pad-r3c7": "9",
    "pad-r4c3": "*",
    "pad-r4c4": "0",
    "pad-r4c5": "#",
  },
  type3: {
    "band-c1": "SETTINGS",
    "band-c3": "MENU",
    "band-c4": "RAPID_FIRE",
    "band-c5": "CALL",
    "band-c6": "SOFT2",
    "band-c7": "CLR",
    "pad-r1c1": "1",
    "pad-r1c2": "2",
    "pad-r1c3": "3",
    "pad-r2c1": "4",
    "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r2c6": "5",
    "pad-r3c5": "7",
    "pad-r3c7": "9",
    "pad-r4c3": "*",
    "pad-r4c4": "0",
    "pad-r4c5": "#",
  },
};

test("migration preserves the recorded handset arrangement of every preset", () => {
  for (const shape of shapes) {
    const assigned = Object.fromEntries(
      Object.entries(legacyPresets[shape]).filter(([, key]) => key),
    );
    assert.deepEqual(assigned, asDrawn[shape]);
    const migrated = migrateLegacy(asDrawn[shape]);
    for (const { id, region, row, column } of legacyCells) {
      const target = `r${region === "band" ? 1 : row * 2}c${column * 2 - 1}`;
      const group = migrated.groups.find((group) =>
        group.cells.includes(target),
      );
      assert.equal(group.key, asDrawn[shape][id] ?? "", `${shape} ${id}`);
    }
  }
});

test("settings is a local action with a stable accessible name", () => {
  assert.ok(assignable.includes(SETTINGS));
  assert.ok(!keyOrder.includes(SETTINGS));
  assert.equal(keyName(SETTINGS), "설정");
  assert.equal(keyFace("OK"), "OK");
});

test("legacy corruption retains exactly one settings action and drops unknown keys", () => {
  for (const source of [
    null,
    {},
    { "band-c1": "UNKNOWN" },
    { "band-c5": SETTINGS, "pad-r1c1": SETTINGS },
  ]) {
    const grid = migrateLegacy(source);
    assert.equal(
      grid.groups.filter((group) => group.key === SETTINGS).length,
      1,
    );
    assert.ok(
      grid.groups.every(
        (group) => !group.key || assignable.includes(group.key),
      ),
    );
  }
});

test("types keep their own edits and one undo stays within the current editor", () => {
  const values = new Map();
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
      return true;
    },
  };
  const layout = createKeypadLayout(storage);
  layout.set("r1c3", "5");
  layout.useShape("type4");
  layout.set("r2c2", "6");
  layout.useShape("type1");
  assert.equal(layout.keyAt("r1c3"), "5");
  assert.equal(layout.canUndo(), false);
  layout.useShape("type4");
  assert.equal(layout.keyAt("r2c2"), "6");
  layout.reset();
  assert.deepEqual(layout.grid(), defaultGrid("type4"));
  assert.equal(layout.undo(), true);
  assert.equal(layout.keyAt("r2c2"), "6");
  layout.beginEdit();
  assert.equal(layout.canUndo(), false);
  assert.deepEqual(createKeypadLayout(storage).grid(), layout.grid());
});
