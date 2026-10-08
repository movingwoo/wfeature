import assert from "node:assert/strict";
import { test } from "node:test";
import {
  COLUMNS,
  ROWS,
  GRID_KEY,
  GRID_V1_KEY,
  createKeypadLayout,
  validateGrid,
} from "./keypad-layout.js";

const memory = (initial) => {
  const values = new Map(Object.entries(initial));
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
      return true;
    },
  };
};
// The version-1 shape is recorded independently of current presets.
const oldGrid = () => ({
  columns: 7,
  rows: 9,
  groups: Array.from({ length: 63 }, (_, index) => {
    const id = `r${Math.floor(index / 7) + 1}c${(index % 7) + 1}`;
    return {
      id,
      cells: [id],
      key: index === 0 ? "SETTINGS" : "",
      activation: index === 0 ? "press" : "slide",
    };
  }),
});
const combine = (grid, members, key, activation) => {
  grid.groups = grid.groups.filter((group) => !members.includes(group.id));
  grid.groups.push({ id: members[0], cells: members, key, activation });
};
const source = (grid) =>
  JSON.stringify({ version: 1, layouts: { type4: grid } });

test("version 1 refines to paired columns while preserving shapes, keys and policies", () => {
  assert.equal(COLUMNS, 14);
  assert.equal(ROWS, 9);
  const old = oldGrid();
  combine(old, ["r2c2", "r3c2", "r3c3"], "5", "press");
  combine(
    old,
    ["r5c4", "r5c5", "r5c6", "r6c4", "r6c6", "r7c4", "r7c5", "r7c6"],
    "MENU",
    "slide",
  );
  old.groups.find((group) => group.id === "r6c5").key = "9";
  const raw = source(old);
  const storage = memory({
    [GRID_V1_KEY]: raw,
    "wfeature:keypadLayout": "type4",
  });
  const layout = createKeypadLayout(storage);
  assert.deepEqual(validateGrid(layout.grid()), layout.grid());
  assert.deepEqual(layout.groupAt("r2c3").cells, [
    "r2c3",
    "r2c4",
    "r3c3",
    "r3c4",
    "r3c5",
    "r3c6",
  ]);
  assert.equal(layout.groupAt("r3c6").activation, "press");
  assert.equal(layout.keyAt("r6c9"), "9");
  assert.equal(layout.keyAt("r6c10"), "9");
  assert.equal(layout.keyAt("r6c8"), "MENU");
  assert.equal(layout.groupAt("r6c8").activation, "slide");
  assert.deepEqual(layout.groupAt("r1c1").cells, ["r1c1", "r1c2"]);
  assert.equal(storage.getItem(GRID_V1_KEY), raw);
  assert.equal(JSON.parse(storage.getItem(GRID_KEY)).version, 2);
  const saved = storage.getItem(GRID_KEY);
  assert.deepEqual(createKeypadLayout(storage).grid(), layout.grid());
  assert.equal(storage.getItem(GRID_KEY), saved);
});

test("empty single cells become separate halves and custom empty shapes retain their form", () => {
  const old = oldGrid();
  combine(old, ["r3c3", "r4c3"], "", "slide");
  const layout = createKeypadLayout(
    memory({ [GRID_V1_KEY]: source(old), "wfeature:keypadLayout": "type4" }),
  );
  assert.deepEqual(layout.groupAt("r1c3").cells, ["r1c3"]);
  assert.deepEqual(layout.groupAt("r1c4").cells, ["r1c4"]);
  assert.deepEqual(layout.groupAt("r3c5").cells, [
    "r3c5",
    "r3c6",
    "r4c5",
    "r4c6",
  ]);
  assert.equal(layout.split("r3c5").ok, true);
  for (const cell of ["r3c5", "r3c6", "r4c5", "r4c6"])
    assert.deepEqual(layout.groupAt(cell).cells, [cell]);
});

test("a denied refined write keeps the old source and leaves edits usable in memory", () => {
  const raw = source(oldGrid());
  const storage = memory({
    [GRID_V1_KEY]: raw,
    "wfeature:keypadLayout": "type4",
  });
  const write = storage.setItem;
  storage.setItem = (key, value) =>
    key === GRID_KEY ? false : write(key, value);
  const layout = createKeypadLayout(storage);
  assert.equal(layout.grid().columns, 14);
  layout.set("r9c14", "5");
  assert.equal(layout.keyAt("r9c14"), "5");
  assert.equal(storage.getItem(GRID_V1_KEY), raw);
  assert.equal(storage.getItem(GRID_KEY), null);
  assert.equal(layout.persistence().saved, false);
  storage.setItem = write;
  layout.set("r9c13", "6");
  assert.equal(storage.getItem(GRID_V1_KEY), raw);
  assert.equal(createKeypadLayout(storage).keyAt("r9c14"), "5");
});

test("a malformed old type recovers independently without losing valid siblings", () => {
  const old = oldGrid();
  old.groups.find((group) => group.id === "r9c7").key = "QUICK_LOAD";
  old.groups.find((group) => group.id === "r9c7").activation = "press";
  const raw = JSON.stringify({
    version: 1,
    layouts: { type1: { ...oldGrid(), columns: 8 }, type4: old },
  });
  const storage = memory({ [GRID_V1_KEY]: raw });
  const layout = createKeypadLayout(storage);
  assert.ok(validateGrid(layout.grid()));
  layout.useShape("type4");
  assert.equal(layout.keyAt("r9c14"), "QUICK_LOAD");
  assert.equal(storage.getItem(GRID_V1_KEY), raw);
});

test("an already-open coarse editor cannot overwrite refined edits", () => {
  const old = oldGrid();
  const storage = memory({
    [GRID_V1_KEY]: source(old),
    "wfeature:keypadLayout": "type4",
  });
  const layout = createKeypadLayout(storage);
  layout.set("r9c14", "5");
  old.groups.find((group) => group.id === "r9c7").key = "6";
  storage.setItem(GRID_V1_KEY, source(old));
  assert.equal(createKeypadLayout(storage).keyAt("r9c14"), "5");
  layout.reset();
  assert.equal(createKeypadLayout(storage).keyAt("r9c14"), "");
  assert.equal(storage.getItem(GRID_V1_KEY), source(old));
});
