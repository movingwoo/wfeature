import assert from "node:assert/strict";
import { test } from "node:test";

import { keyLabel, keyOrder } from "./keybindings.js";
import {
  BAND_COLUMNS,
  BAND_FIXED_COLUMN,
  EMPTY,
  LEFT_COLUMNS,
  PAD_COLUMNS,
  PAD_ROWS,
  RIGHT_COLUMNS,
  assignable,
  assign,
  clampCells,
  clear,
  createKeypadLayout,
  isShape,
  keyFace,
  shapeLabel,
  shapes,
  shipped,
  regionLabel,
  shapeMatching,
  cellIds,
  cells,
} from "./keypad-layout.js";

// A storage the tests can look inside, in the shape storage.js presents rather
// than the browser's own — see keypad-size.test.mjs, which says why.
const fakeStorage = () => {
  const entries = new Map();
  return {
    entries,
    getItem: key => entries.get(key) ?? null,
    setItem: (key, value) => {
      entries.set(key, value);
      return true;
    },
    removeItem: key => entries.delete(key),
  };
};

const LAYOUT_KEY = "wfeature:keypadLayout";
const KEYS_KEY = "wfeature:keypadKeys";

// What the three shapes drew when they were three blocks of markup with four
// stylesheet rules showing and hiding pieces of them. This is a recording: it
// was read out of `index.html` before the markup became cells, by resolving
// `type2-only`, `type3-only` and `type3-hidden` per shape and reading the grid
// cell off each button's class. The markup it came from is gone, so the numbers
// live here — **this is the gate that says nobody's keypad moved.**
//
// The two 3x3 pads became columns 1-3 and 5-7 of one seven-column grid, and the
// row of * 0 # became its fourth row, centred in it. The mapping is written out
// rather than computed, because a recording that is derived from the thing it
// is checking checks nothing.
//
// **The band is the one place a key really did move**, and by request: it was
// three buttons at hand-written offsets and it is the pad's seven columns now,
// six of them cells. The three keys are in the columns nearest the offsets they
// had — the pair around the middle and CLR at the right-hand end — which is the
// row as it read, not the pixels it read at. Those pixels could not be kept:
// a column's width depends on the screen and an offset does not.
const asDrawn = {
  // Type4 is not one of the three: it is the empty pad, added with the editor
  // because "no keys at all" has to be a shape to be stored as one. It is here
  // so the loop below covers it, and it is empty.
  type4: {},
  type1: {
    "band-c3": "MENU", "band-c5": "CALL", "band-c7": "CLR",
    "pad-r1c2": "2",
    "pad-r2c1": "4", "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r1c5": "1", "pad-r1c7": "3",
    "pad-r2c6": "5",
    "pad-r3c5": "7", "pad-r3c7": "9",
    "pad-r4c3": "*", "pad-r4c4": "0", "pad-r4c5": "#",
  },
  type2: {
    "band-c3": "MENU", "band-c5": "CALL", "band-c7": "CLR",
    "pad-r1c2": "UP",
    "pad-r2c1": "LEFT", "pad-r2c2": "OK", "pad-r2c3": "RIGHT",
    "pad-r3c2": "DOWN",
    "pad-r1c5": "1", "pad-r1c6": "2", "pad-r1c7": "3",
    "pad-r2c5": "4", "pad-r2c6": "5", "pad-r2c7": "6",
    "pad-r3c5": "7", "pad-r3c6": "8", "pad-r3c7": "9",
    "pad-r4c3": "*", "pad-r4c4": "0", "pad-r4c5": "#",
  },
  type3: {
    "band-c3": "MENU", "band-c5": "CALL", "band-c7": "CLR",
    "pad-r1c1": "1", "pad-r1c2": "2", "pad-r1c3": "3",
    "pad-r2c1": "4", "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r2c6": "5",
    "pad-r3c5": "7", "pad-r3c7": "9",
    "pad-r4c3": "*", "pad-r4c4": "0", "pad-r4c5": "#",
  },
};

test("each shape is the keypad it was when it was markup", () => {
  for (const name of shapes) {
    for (const id of cellIds) {
      assert.equal(
        shipped[name][id],
        asDrawn[name][id] ?? EMPTY,
        `${name} draws ${shipped[name][id] || "nothing"} at ${id}, and drew ${asDrawn[name][id] || "nothing"}`,
      );
    }
  }
});

test("the three drawn shapes differ only in the pad's first three rows", () => {
  // The band and the last row were never part of a shape, which is why they
  // were plain markup outside the pads. If a shape ever changes one, the
  // comment in keypad-layout.js that says so is what has to change with it.
  // Type4 is outside this: it is empty everywhere, which is the whole of it.
  const outside = cellIds.filter(id => !/^pad-r[123]c/.test(id));
  for (const name of ["type1", "type2", "type3"]) {
    for (const id of outside) {
      assert.equal(shipped[name][id], shipped[shapes[0]][id], `${name} moved ${id}`);
    }
  }
});

test("the pad is a grid, and every cell in it has a place", () => {
  assert.equal(PAD_COLUMNS, 7);
  assert.equal(PAD_ROWS, 4);
  const pad = cells.filter(cell => cell.region === "pad");
  assert.equal(pad.length, PAD_COLUMNS * PAD_ROWS);
  // Reading order, which is the order the markup emits them in and therefore
  // the order the grid places them. A cell out of order here is a key drawn
  // somewhere else with nothing to say so.
  const inReadingOrder = [];
  for (let row = 1; row <= PAD_ROWS; row++) {
    for (let column = 1; column <= PAD_COLUMNS; column++) inReadingOrder.push(`pad-r${row}c${column}`);
  }
  assert.deepEqual(pad.map(cell => cell.id), inReadingOrder);
  for (const cell of pad) {
    assert.equal(cell.id, `pad-r${cell.row}c${cell.column}`, "a cell's name and its place disagree");
  }
  // The two former pads, which are what the size setting's share still divides.
  assert.deepEqual(LEFT_COLUMNS, [1, 2, 3]);
  assert.deepEqual(RIGHT_COLUMNS, [5, 6, 7]);
});

test("the cells are a fixed set, each in a region the editor can name", () => {
  assert.equal(cells.length, 34);
  assert.equal(new Set(cellIds).size, cellIds.length, "a cell is declared twice");
  for (const cell of cells) {
    assert.ok(regionLabel[cell.region], `the region ${cell.region} has no label`);
  }
  // Two regions and not four, because the pad is one grid. The band is seven
  // columns less the one Opts holds, and the pad is seven by four. The regions
  // are the drag rule, so a third one would be a third answer to "does a slide
  // press this".
  const count = region => cells.filter(cell => cell.region === region).length;
  assert.deepEqual([count("band"), count("pad")], [BAND_COLUMNS - 1, PAD_COLUMNS * PAD_ROWS]);
  assert.deepEqual([count("band"), count("pad")], [6, 28]);
  assert.deepEqual(Object.keys(regionLabel).sort(), ["band", "pad"]);
  // Opts's column is not among them, at either end of the band.
  assert.ok(!cellIds.includes(`band-c${BAND_FIXED_COLUMN}`), "a phone key can take Opts's column");
  for (const cell of cells.filter(entry => entry.region === "band")) {
    assert.equal(cell.id, `band-c${cell.column}`, "a band cell's name and its column disagree");
    assert.notEqual(cell.column, BAND_FIXED_COLUMN);
  }
});

test("a shape names only keys the editor can also choose", () => {
  for (const name of shapes) {
    for (const [id, key] of Object.entries(shipped[name])) {
      if (key === EMPTY) continue;
      assert.ok(assignable.includes(key), `${name} puts ${key} at ${id} and nothing offers it`);
    }
    assert.deepEqual(Object.keys(shipped[name]).sort(), [...cellIds].sort());
  }
  assert.deepEqual(assignable, keyOrder, "the editor's list has drifted from the keyboard panel's");
  for (const name of shapes) assert.ok(shapeLabel[name], `${name} has no label`);
});

test("a pad face is the key's name where one fits, and the name is what is spoken", () => {
  // Three keys are printed shorter than they are called, because a 48-pixel key
  // cannot carry 통화 beside a 5 without one of them being a different size of
  // text. Everything else has to read the same either way, or the editor's list
  // and the pad would name the same key two ways.
  const shortened = assignable.filter(name => keyFace(name) !== keyLabel(name));
  assert.deepEqual(shortened, ["CALL", "MENU", "OK"]);
  for (const name of assignable) assert.ok(keyFace(name), `${name} has no face`);
});

test("a stored table keeps only cells and keys this build has", () => {
  const folded = clampCells({
    "pad-r2c6": "7",
    "pad-r1c2": "SOFT2",
    "cell-that-went-away": "5",
    "band-c4": 5,
  });
  assert.deepEqual(Object.keys(folded).sort(), [...cellIds].sort());
  assert.equal(folded["pad-r2c6"], "7");
  // A key this build does not have, a cell it does not have, and a number where
  // a name should be: all three are an empty cell rather than a thrown error or
  // a key nothing can send.
  assert.equal(folded["pad-r1c2"], EMPTY);
  assert.equal(folded["band-c4"], EMPTY);
  assert.ok(!Object.hasOwn(folded, "cell-that-went-away"));
  // And nonsense of the wrong kind entirely is the empty pad, not a crash.
  for (const nonsense of [null, undefined, 7, "type1", []]) {
    assert.deepEqual(Object.values(clampCells(nonsense)), cellIds.map(() => EMPTY));
  }
});

test("a key may sit in two cells, and a cell may be emptied", () => {
  // The opposite of the keyboard's rule, and deliberately: the shipped type3
  // prints 1 and 3 twice, and app.js lights every button carrying a key for
  // exactly that reason.
  let table = shipped.type1;
  table = assign(table, "pad-r1c6", "5");
  assert.equal(table["pad-r1c6"], "5");
  assert.equal(table["pad-r2c6"], "5", "putting a key somewhere took it from where it was");
  table = clear(table, "pad-r2c6");
  assert.equal(table["pad-r2c6"], EMPTY);
  assert.equal(table["pad-r1c6"], "5");
  // Neither touches a cell or a key the build does not have.
  assert.equal(assign(table, "no-such-cell", "5"), table);
  assert.equal(assign(table, "pad-r1c6", "SOFT2"), table);
  assert.equal(clear(table, "no-such-cell"), table);
});

test("a page that never opened the editor draws the keypad it always drew", () => {
  // The whole of the migration. `wfeature:keypadLayout` is the entry this page
  // has always written; the second one appears only once a cell moves. A page
  // with only the first has to be pixel-for-pixel the keypad it was.
  for (const name of shapes) {
    const storage = fakeStorage();
    storage.setItem(LAYOUT_KEY, name);
    const layout = createKeypadLayout(storage, LAYOUT_KEY);
    assert.equal(layout.shape(), name);
    assert.equal(layout.edited(), false);
    assert.deepEqual(layout.keys(), shipped[name]);
    assert.ok(!storage.entries.has(KEYS_KEY), "reading the layout wrote a table");
  }
  // And a page that never chose one gets the shape this build ships first,
  // which is a shape with keys on it rather than the empty one.
  const fresh = createKeypadLayout(fakeStorage(), LAYOUT_KEY);
  assert.equal(fresh.shape(), shapes[0]);
  assert.deepEqual(fresh.keys(), shipped[shapes[0]]);
});

test("an edit is stored, and is what the pad draws next time", () => {
  const storage = fakeStorage();
  storage.setItem(LAYOUT_KEY, "type1");
  const layout = createKeypadLayout(storage, LAYOUT_KEY);
  layout.set("pad-r2c2", "OK");
  assert.equal(layout.edited(), true);
  assert.equal(layout.keyAt("pad-r2c2"), "OK");
  assert.ok(storage.entries.has(KEYS_KEY), "the edit was not stored");
  // The shape it started from stays stored, so 되돌리기 has somewhere to go.
  assert.equal(storage.getItem(LAYOUT_KEY), "type1");

  const reopened = createKeypadLayout(storage, LAYOUT_KEY);
  assert.equal(reopened.edited(), true);
  assert.equal(reopened.keyAt("pad-r2c2"), "OK");
  assert.equal(reopened.shape(), "type1");
});

test("an edit that lands back on a shape is not an edit", () => {
  // Otherwise the list would go on calling a shape modified when the pad is
  // exactly that shape again.
  const storage = fakeStorage();
  storage.setItem(LAYOUT_KEY, "type1");
  const layout = createKeypadLayout(storage, LAYOUT_KEY);
  layout.set("pad-r2c2", "OK");
  assert.equal(layout.edited(), true);
  layout.set("pad-r2c2", EMPTY);
  assert.equal(layout.edited(), false, "the pad is type1 again and does not say so");
  assert.ok(!storage.entries.has(KEYS_KEY), "the table outlived the edit");

  // The same on the way in: a stored table that happens to be the shape is the
  // shape.
  const stored = fakeStorage();
  stored.setItem(LAYOUT_KEY, "type2");
  stored.setItem(KEYS_KEY, JSON.stringify(shipped.type2));
  assert.equal(createKeypadLayout(stored, LAYOUT_KEY).edited(), false);
  assert.equal(shapeMatching(shipped.type3), "type3");
  assert.equal(shapeMatching({ ...shipped.type3, "pad-r4c3": "7" }), "");
});

test("each shape keeps its own cells, and choosing another does not disturb them", () => {
  // The point of a shape is a place to keep a keypad. Somebody arranges type1
  // the way they like it, switches to type4 to build a second pad from nothing,
  // and finds the first still there on the way back — which is also what makes
  // the size settings hang off a shape rather than off the page.
  const storage = fakeStorage();
  const layout = createKeypadLayout(storage, LAYOUT_KEY);
  layout.useShape("type1");
  layout.set("pad-r2c2", "OK");
  assert.equal(layout.edited(), true);

  layout.useShape("type4");
  assert.deepEqual(layout.keys(), shipped.type4, "the empty shape came with somebody's edit in it");
  assert.equal(layout.edited(), false);
  layout.set("pad-r1c1", "5");
  assert.equal(layout.edited(), true);

  layout.useShape("type1");
  assert.equal(layout.keyAt("pad-r2c2"), "OK", "the shape's own cells did not come back");
  assert.equal(layout.edited(), true);
  layout.useShape("type4");
  assert.equal(layout.keyAt("pad-r1c1"), "5");

  // And they come back from storage the same way.
  const reopened = createKeypadLayout(storage, LAYOUT_KEY);
  assert.equal(reopened.shape(), "type4");
  assert.equal(reopened.keyAt("pad-r1c1"), "5");
  reopened.useShape("type1");
  assert.equal(reopened.keyAt("pad-r2c2"), "OK");
});

test("reset is one shape's, and forgets the entry rather than storing its cells", () => {
  const storage = fakeStorage();
  const layout = createKeypadLayout(storage, LAYOUT_KEY);
  layout.useShape("type2");
  layout.set("pad-r4c1", "7");
  layout.useShape("type1");
  layout.set("pad-r2c2", "OK");

  assert.deepEqual(layout.reset(), shipped.type1);
  assert.equal(layout.edited(), false);
  assert.equal(layout.shape(), "type1", "reset changed the shape as well as its cells");
  // The other shape's edit is untouched, and the entry still holds only it.
  layout.useShape("type2");
  assert.equal(layout.keyAt("pad-r4c1"), "7");
  assert.deepEqual(Object.keys(JSON.parse(storage.getItem(KEYS_KEY))), ["type2"]);

  // With nothing edited anywhere the entry goes rather than holding empties.
  layout.reset();
  assert.ok(!storage.entries.has(KEYS_KEY), "an edit-free keypad still stores a table");

  // A name that is not a shape changes nothing.
  layout.useShape("type9");
  assert.equal(layout.shape(), "type2");
  assert.ok(isShape("type4") && !isShape("edited") && !isShape(null));
});

test("emptying a named shape by hand is not renamed to the empty one", () => {
  // Clearing every cell of type1 makes a table type4 also matches. Answering
  // "type4" would rename somebody's keypad under them: what they did was edit
  // type1 down to nothing, and the shape they are on is the one they picked.
  const layout = createKeypadLayout(fakeStorage(), LAYOUT_KEY);
  layout.useShape("type1");
  for (const id of cellIds) layout.set(id, EMPTY);
  assert.equal(layout.shape(), "type1");
  assert.equal(layout.edited(), true, "the emptied shape was renamed");
  assert.deepEqual(layout.keys(), shipped.type4, "the cells are not empty");
  // And on the shape that *is* empty, the same table is not an edit at all.
  const empty = createKeypadLayout(fakeStorage(), LAYOUT_KEY);
  empty.useShape("type4");
  empty.set("pad-r1c1", "5");
  empty.set("pad-r1c1", EMPTY);
  assert.equal(empty.edited(), false);
});

test("nonsense in storage is a shape rather than a throw", () => {
  const storage = fakeStorage();
  // A shape this build does not have. It used to be "type4", which this build
  // does — a test whose nonsense becomes real stops testing anything.
  storage.setItem(LAYOUT_KEY, "type9");
  storage.setItem(KEYS_KEY, "{not json");
  const layout = createKeypadLayout(storage, LAYOUT_KEY);
  assert.equal(layout.shape(), shapes[0]);
  assert.equal(layout.edited(), false);
  assert.deepEqual(layout.keys(), shipped[shapes[0]]);

  // A storage that throws on every call is a page that still draws a keypad;
  // it simply cannot remember one.
  const hostile = {
    getItem: () => {
      throw new Error("blocked");
    },
    setItem: () => {
      throw new Error("blocked");
    },
    removeItem: () => {
      throw new Error("blocked");
    },
  };
  const blocked = createKeypadLayout(hostile, LAYOUT_KEY);
  assert.deepEqual(blocked.keys(), shipped[shapes[0]]);
  assert.doesNotThrow(() => blocked.set("pad-r2c2", "OK"));
  assert.equal(blocked.keyAt("pad-r2c2"), "OK");
  assert.doesNotThrow(() => blocked.useShape("type2"));
});

test("a cell nothing knows about is not a cell", () => {
  const layout = createKeypadLayout(fakeStorage(), LAYOUT_KEY);
  const before = layout.keys();
  assert.deepEqual(layout.set("no-such-cell", "5"), before);
  assert.equal(layout.edited(), false);
  assert.equal(layout.keyAt("no-such-cell"), EMPTY);
});
