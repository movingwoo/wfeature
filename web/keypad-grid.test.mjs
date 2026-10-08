import assert from "node:assert/strict";
import { test } from "node:test";
import {
  createKeypadLayout,
  COLUMNS,
  ROWS,
  GRID_KEY,
  SETTINGS,
  shapes,
  defaultGrid,
  migrateLegacy,
  validateGrid,
  legacyCells,
  legacyPresets,
} from "./keypad-layout.js";

const memory = (initial) => {
  const entries = new Map(Object.entries(initial ?? {}));
  return {
    entries,
    getItem: (key) => entries.get(key) ?? null,
    setItem: (key, value) => {
      entries.set(key, value);
      return true;
    },
  };
};
const empty = () => {
  const layout = createKeypadLayout(memory());
  layout.useShape("type4");
  return layout;
};
const key = (grid, cell) =>
  grid.groups.find((group) => group.cells.includes(cell))?.key;

test("all presets cover the same grid and keep one reachable settings action", () => {
  assert.equal(COLUMNS, 14);
  assert.equal(ROWS, 9);
  for (const shape of shapes) {
    const grid = defaultGrid(shape);
    assert.deepEqual(validateGrid(grid), grid);
    assert.equal(
      new Set(grid.groups.flatMap((group) => group.cells)).size,
      126,
    );
    assert.equal(
      grid.groups.filter((group) => group.key === SETTINGS).length,
      1,
    );
    if (shape !== "type4") {
      assert.deepEqual(
        grid.groups
          .filter((group) => group.id.startsWith("r1c"))
          .map((group) => [group.cells, group.key]),
        [
          [["r1c1", "r1c2"], SETTINGS],
          [["r1c3"], ""],
          [["r1c4"], ""],
          [["r1c5"], "QUICK_SAVE"],
          [["r1c6"], "QUICK_LOAD"],
          [["r1c7"], "RAPID_FIRE"],
          [["r1c8"], "MENU"],
          [["r1c9"], "CALL"],
          [["r1c10"], "SOFT2"],
          [["r1c11"], ""],
          [["r1c12"], ""],
          [["r1c13", "r1c14"], "CLR"],
        ],
        shape,
      );
    }
  }
  assert.deepEqual(
    defaultGrid("type4")
      .groups.filter((group) => group.key)
      .map((group) => group.key),
    [SETTINGS],
  );
  assert.equal(key(defaultGrid("type1"), "r2c3"), "UP");
  assert.equal(key(defaultGrid("type1"), "r3c3"), "UP");
  assert.equal(key(defaultGrid("type1"), "r4c3"), "OK");
});

test("migration preserves every old position, blank, duplicate and activation policy", () => {
  for (const shape of shapes) {
    const table = {
      ...legacyPresets[shape],
      "band-c2": "5",
      "pad-r1c1": "QUICK_SAVE",
      "pad-r2c7": "QUICK_LOAD",
      "pad-r4c1": SETTINGS,
      "band-c1": "",
    };
    const grid = migrateLegacy(table);
    for (const cell of legacyCells) {
      const row = cell.region === "band" ? 1 : cell.row * 2;
      const group = grid.groups.find((group) =>
        group.cells.includes(`r${row}c${cell.column * 2 - 1}`),
      );
      assert.equal(group.key, table[cell.id] ?? "", cell.id);
      if (
        group.key &&
        ![SETTINGS, "QUICK_SAVE", "QUICK_LOAD", "RAPID_FIRE"].includes(
          group.key,
        )
      ) {
        assert.equal(
          group.activation,
          cell.region === "band" ? "press" : "slide",
        );
      }
    }
    assert.equal(
      grid.groups.filter((group) => group.key === SETTINGS).length,
      1,
    );
  }
});

test("merging retains migrated activation and new assignments use their own policy", () => {
  const storage = memory({
    "wfeature:keypadLayout": "type4",
    "wfeature:keypadKeys": JSON.stringify({
      type4: {
        "band-c1": SETTINGS,
        "band-c2": "5",
        "pad-r1c3": "MENU",
      },
    }),
  });
  const layout = createKeypadLayout(storage);
  assert.equal(layout.merge(["r1c3", "r1c5"]).ok, true);
  assert.equal(layout.groupAt("r1c5").activation, "press");
  assert.equal(layout.merge(["r2c5", "r2c7"]).ok, true);
  assert.equal(layout.groupAt("r2c7").activation, "slide");
  assert.equal(layout.merge(["r1c3", "r2c5"], "UP").ok, true);
  assert.equal(layout.groupAt("r1c3").activation, "slide");
  layout.set("r1c3", "MENU");
  assert.equal(layout.groupAt("r1c3").activation, "press");
  for (const key of ["QUICK_SAVE", "QUICK_LOAD", "RAPID_FIRE"]) {
    layout.set("r1c3", key);
    assert.equal(layout.groupAt("r1c3").activation, "press");
  }
  assert.deepEqual(createKeypadLayout(storage).grid(), layout.grid());
});

test("rectangle, L, T and ring shapes merge, persist, split and undo", () => {
  const cases = [
    ["r2c2", "r2c3", "r3c2", "r3c3"],
    ["r2c2", "r3c2", "r3c3"],
    ["r2c2", "r2c3", "r2c4", "r3c3"],
    ["r2c2", "r2c3", "r2c4", "r3c2", "r3c4", "r4c2", "r4c3", "r4c4"],
  ];
  for (const cells of cases) {
    const storage = memory({ "wfeature:keypadLayout": "type4" });
    const layout = createKeypadLayout(storage);
    assert.equal(layout.merge(cells, "5").ok, true);
    const group = layout.groupAt(cells[0]);
    assert.deepEqual(group.cells, cells);
    for (const cell of cells) assert.equal(layout.keyAt(cell), "5");
    const reopened = createKeypadLayout(storage);
    assert.deepEqual(reopened.grid(), layout.grid());
    assert.equal(reopened.split(cells[0]).ok, true);
    assert.equal(reopened.keyAt(cells[0]), "5");
    for (const cell of cells.slice(1)) assert.equal(reopened.keyAt(cell), "");
    assert.equal(reopened.undo(), true);
    assert.deepEqual(reopened.grid(), layout.grid());
    assert.equal(reopened.undo(), false);
  }
});

test("disconnected selections and conflicting keys do not change the layout", () => {
  const layout = empty();
  const before = layout.grid();
  assert.equal(layout.merge(["r2c2", "r3c3"]).error, "disconnected");
  assert.deepEqual(layout.grid(), before);
  layout.set("r2c2", "5");
  layout.set("r2c3", "6");
  const assigned = layout.grid();
  assert.equal(layout.merge(["r2c2", "r2c3"]).error, "choose-key");
  assert.deepEqual(layout.grid(), assigned);
  assert.equal(layout.merge(["r2c2", "r2c3"], "7").ok, true);
  assert.equal(layout.keyAt("r2c2"), "7");
  assert.equal(layout.keyAt("r2c3"), "7");
  assert.equal(layout.merge(["invalid", "r2c2"]).ok, false);
});

test("settings survives merging, splitting, clearing and moving", () => {
  const layout = empty();
  layout.set("r1c1", "5");
  layout.set("r1c1", "");
  assert.equal(layout.keyAt("r1c1"), SETTINGS);
  assert.equal(layout.merge(["r1c1", "r1c3"], "5").error, "settings");
  assert.equal(layout.merge(["r1c1", "r1c3"]).ok, true);
  assert.equal(layout.keyAt("r1c3"), SETTINGS);
  assert.equal(layout.split("r1c3").ok, true);
  layout.set("r9c7", SETTINGS);
  assert.equal(layout.keyAt("r1c1"), "");
  assert.equal(
    layout.grid().groups.filter((group) => group.key === SETTINGS).length,
    1,
  );
});

test("clearing retains geometry and selecting part of a group merges its whole shape", () => {
  const layout = empty();
  layout.merge(["r2c2", "r3c2"], "5");
  layout.set("r3c2", "");
  assert.deepEqual(layout.groupAt("r2c2").cells, ["r2c2", "r3c2"]);
  layout.merge(["r3c2", "r3c3"], "6");
  assert.deepEqual(layout.groupAt("r3c3").cells, ["r2c2", "r3c2", "r3c3"]);
  assert.equal(layout.keyAt("r2c2"), "6");
});

test("migration is idempotent and reset never resurrects legacy edits", () => {
  const legacy = JSON.stringify({
    type1: { ...legacyPresets.type1, "band-c2": "QUICK_SAVE" },
    type2: { ...legacyPresets.type2, "band-c2": "QUICK_LOAD" },
  });
  const storage = memory({
    "wfeature:keypadKeys": legacy,
    "wfeature:keypadSize": '{"key":68}',
  });
  const layout = createKeypadLayout(storage);
  assert.equal(layout.keyAt("r1c3"), "QUICK_SAVE");
  const migrated = storage.getItem(GRID_KEY);
  assert.ok(migrated);
  assert.deepEqual(createKeypadLayout(storage).grid(), layout.grid());
  assert.equal(storage.getItem(GRID_KEY), migrated);
  layout.reset();
  const reset = createKeypadLayout(storage);
  assert.equal(reset.keyAt("r1c3"), "");
  assert.equal(reset.keyAt("r1c5"), "QUICK_SAVE");
  assert.equal(reset.keyAt("r1c6"), "QUICK_LOAD");
  assert.deepEqual(reset.groupAt("r1c5").cells, ["r1c5"]);
  layout.useShape("type2");
  assert.equal(layout.keyAt("r1c3"), "QUICK_LOAD");
  assert.equal(storage.getItem("wfeature:keypadKeys"), legacy);
  assert.equal(storage.getItem("wfeature:keypadSize"), '{"key":68}');
});

test("malformed groups cannot overlap, escape bounds, disconnect or remove settings", () => {
  const good = defaultGrid("type4");
  const change = (update) => {
    const grid = structuredClone(good);
    update(grid);
    return grid;
  };
  for (const bad of [
    null,
    [],
    {},
    change((g) => (g.rows = 1000000)),
    change((g) => g.groups.push(g.groups[0])),
    change((g) => (g.groups[1].cells = Array(127).fill("r1c3"))),
    change((g) => (g.groups[1].cells = ["r1c3", "r1c3"])),
    change((g) => (g.groups[1].cells = ["r0c1"])),
    change((g) => (g.groups[1].id = "r1c4")),
    change((g) => (g.groups[1].key = "UNKNOWN")),
    change((g) => (g.groups[1].activation = "UNKNOWN")),
    change((g) => (g.groups[0].activation = "slide")),
    change((g) => (g.groups[0].key = "")),
    change((g) => (g.groups[1].key = SETTINGS)),
    change((g) => {
      g.groups[1].cells = ["r1c3", "r9c7"];
      g.groups.pop();
    }),
  ])
    assert.equal(validateGrid(bad), null);
});

test("corrupt types recover independently and preserve the source before edits", () => {
  const good = defaultGrid("type4");
  good.groups[1].key = "5";
  const raw = JSON.stringify({
    version: 2,
    layouts: { type1: "bad", type4: good },
  });
  const storage = memory({ [GRID_KEY]: raw });
  const layout = createKeypadLayout(storage);
  assert.deepEqual(layout.grid(), defaultGrid("type1"));
  layout.useShape("type4");
  assert.equal(layout.keyAt("r1c3"), "5");
  layout.set("r1c4", "6");
  assert.equal(storage.getItem(`${GRID_KEY}Backup`), raw);
  assert.equal(createKeypadLayout(storage).keyAt("r1c4"), "6");
});

test("a failed recovery backup never overwrites the damaged source", () => {
  const raw = '{"version":2,"layouts":{"type1":"damaged"}}';
  const storage = memory({ [GRID_KEY]: raw });
  const save = storage.setItem;
  storage.setItem = (key, value) =>
    key === `${GRID_KEY}Backup` ? false : save(key, value);
  const layout = createKeypadLayout(storage);
  layout.set("r1c3", "5");
  assert.equal(layout.keyAt("r1c3"), "5");
  assert.equal(storage.getItem(GRID_KEY), raw);
  assert.equal(layout.persistence().saved, false);
  storage.setItem = save;
  layout.set("r1c4", "6");
  assert.equal(storage.getItem(`${GRID_KEY}Backup`), raw);
  assert.equal(createKeypadLayout(storage).keyAt("r1c3"), "5");
});

test("oversized records remain intact through edits, reset and undo", () => {
  const raw = " ".repeat(65537);
  const storage = memory({ [GRID_KEY]: raw });
  const layout = createKeypadLayout(storage);
  layout.set("r1c3", "5");
  layout.reset();
  layout.undo();
  assert.equal(layout.keyAt("r1c3"), "5");
  assert.equal(storage.getItem(GRID_KEY), raw);
  assert.equal(layout.persistence().reason, "oversized");
});

test("future records and failed writes remain usable without destroying stored data", () => {
  const raw = JSON.stringify({ version: 999, layouts: {} });
  const storage = memory({ [GRID_KEY]: raw });
  const future = createKeypadLayout(storage);
  future.set("r1c3", "5");
  assert.equal(future.keyAt("r1c3"), "5");
  assert.equal(storage.getItem(GRID_KEY), raw);
  assert.equal(future.persistence().reason, "future");
  const hostile = {
    getItem: () => {
      throw Error("blocked");
    },
    setItem: () => {
      throw Error("full");
    },
  };
  const temporary = createKeypadLayout(hostile);
  temporary.set("r1c3", "5");
  assert.equal(temporary.keyAt("r1c3"), "5");
  assert.equal(temporary.persistence().saved, false);
});
