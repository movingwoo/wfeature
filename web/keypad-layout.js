// Which phone key sits in which cell of the keypad.
//
// The pad this page draws had three shapes, and they were three blocks of
// markup with four stylesheet rules showing and hiding pieces of them. Written
// out, the three turn out to be one thing: the same cells with different keys
// in them.
//
//     where            type1       type2            type3
//     direction pad    2 4 6 8     ↑ ← 확인 → ↓     1 2 3 4 6 8
//     number pad       1 3 5 7 9   1‥9              5 7 9
//
// So this module is that table. The shapes are entries in it rather than a mode
// the stylesheet implements. What the person gets for it is the thing the
// shapes could not give: a cell whose key is theirs to choose.
//
// **And once the cells are a table, the pad is one grid.** The two 3x3 pads had
// a wide gap between them that a thumb crossed and nothing used, and the row of
// * 0 # was a band of its own below them. Filling the gap makes the middle a
// column like the others and the last row a row like the others: seven columns
// by four rows, twenty-eight cells, of which the shipped shape fills twelve.
// The keys did not move — every one of them is in the cell it was drawn in —
// and the grid is what makes the other sixteen reachable.
//
// The merge is free in the layout, which is why it is a merge rather than a
// redesign: the band gap that used to sit between the pads and the last row is
// gone and the grid's own row gap took its place, and the two are the same
// eight pixels, so `--keypad-floor` and `--keypad-slack` in style.css are the
// numbers they always were.
//
// Two rules here are the opposite of the ones `keybindings.js` holds, and the
// difference is not an oversight in either place:
//
//   - **A key may sit in more than one cell.** The keyboard's table may not do
//     that — one physical key sending two phone keys is one press sending two
//     keys to the game — but the pad has always printed 1 and 3 twice in the
//     type3 shape, and `initInput` lights *every* button carrying a key for
//     exactly that reason. Nothing has to be taken away to put a key somewhere.
//   - **A cell may be empty, and most are.** Sixteen of the twenty-eight are
//     empty in the shipped shape, which is what makes it that shape.
//
// And one rule is worth stating because a person editing cannot see it: **a
// finger sliding across keys only presses the pad's.** `app.js` decides that by
// which element a button sits in — inside the pad a drag presses what it
// crosses, in the band above it aims at one key at a time — so a key moved into
// the band stops answering a slide. The cell keeps its region; only its key
// changes. Merging the last row into the pad changed nothing there: it was
// already inside the region a slide runs through.

import { local } from "./storage.js";
import { keyLabel, keyOrder } from "./keybindings.js";

const KEYPAD_KEYS_KEY = "wfeature:keypadKeys";

// The pad is one grid, and these are its dimensions. Seven columns because the
// two three-column pads had a gap between them that a thumb crossed and nothing
// used: filling it makes the middle a column like the others and the row of
// * 0 # a row like the others. Four rows because that row is now one of them —
// the last band gap became the grid's own row gap, which is why the height
// budget in style.css did not have to change.
export const PAD_COLUMNS = 7;
export const PAD_ROWS = 4;

// The band above is the same seven columns, and Opts is the first of them. It
// was three buttons placed by hand — one at the left edge, two either side of
// the centre, one at the right — which is why there were three: absolute
// positions have to be written one at a time. As a row of the pad's own columns
// there are seven, six of them a person's to fill.
//
// Seven and not more, and the narrowest phone is what decides it: at a 320px
// viewport the band is 288 pixels of content box, so seven columns are 37.7
// each and eight are 32.5. Thirty-six is the smallest key the pad itself will
// draw — `--keypad-key-min` — and "Opts" needs about thirty of those pixels for
// its label, so eight is under both. Seven also lines the band up with the pad,
// which no other count does.
export const BAND_COLUMNS = 7;

// The column Opts holds. It is not a cell and cannot become one: it is the only
// way back into the panel that would undo an empty keypad.
export const BAND_FIXED_COLUMN = 1;

// The column the two former pads occupied, which is what the size setting's
// left/right share still weighs. The middle column belongs to neither.
export const LEFT_COLUMNS = [1, 2, 3];
export const RIGHT_COLUMNS = [5, 6, 7];

// The cells, in the order the editor walks them, which is the order they sit on
// the page: the band above and then the grid, row by row. `region` is the drag
// rule above; the grid places a cell by where its button is in the markup, so
// nothing here says which row or column a cell is in beyond naming it.
//
// The band has three cells and not four. The fourth button up there is Opts,
// and it is not a cell: it is the only way back into this editor, so a person
// cannot put a phone key on it and lose the panel that would undo that.
export const cells = [
  ...bandCells(),
  ...padCells(),
];

// The band's cells, left to right, skipping the column Opts holds. Like the
// pad's they carry no row or column of their own: the markup emits Opts and
// then these, and the grid places them in that order.
function bandCells() {
  const cells = [];
  for (let column = 1; column <= BAND_COLUMNS; column++) {
    if (column === BAND_FIXED_COLUMN) continue;
    cells.push({ id: `band-c${column}`, region: "band", row: 0, column });
  }
  return cells;
}

// The pad's own cells, row by row, which is the order the markup emits them in
// and therefore the order the grid places them: no cell carries a row or a
// column of its own, and moving one in the markup moves it on the pad.
function padCells() {
  const cells = [];
  for (let row = 1; row <= PAD_ROWS; row++) {
    for (let column = 1; column <= PAD_COLUMNS; column++) {
      cells.push({ id: `pad-r${row}c${column}`, region: "pad", row, column });
    }
  }
  return cells;
}

export const cellIds = cells.map(cell => cell.id);
const knownCell = new Set(cellIds);

// What the editor calls each region. The band and the row below are named by
// where they are rather than by what is in them, because what is in them is the
// thing being changed.
export const regionLabel = {
  band: "윗줄",
  pad: "키패드",
};

// An empty cell is this rather than an absent entry, so a table always names
// every cell and "cleared" and "never set" cannot be told apart — they are the
// same thing on a pad.
export const EMPTY = "";


// The band and the pad are cells either way, so one table serves both: a shape
// names every cell it fills and leaves the rest at EMPTY.
const fill = assignment => {
  const table = Object.fromEntries(cellIds.map(id => [id, EMPTY]));
  return { ...table, ...assignment };
};

// The band is the same in all three, which is why it was never part of the
// shape: Opts, 메뉴, 통화, CLR, left to right.
//
// The three sat at hand-written offsets — the left edge for Opts, a little
// either side of the centre for 메뉴 and 통화, the right edge for CLR — and
// these are the columns they land in now. It is a column rather than the same
// pixel because a column's width depends on the screen and those offsets did
// not, so the row keeps how it reads rather than where it measured: 통화 near
// the middle, CLR anchored at the end a thumb looks for it at.
//
// 메뉴 is a column further left than the nearest one, which leaves the middle
// column empty between it and 통화. That is deliberate: it is the key a title
// puts its in-game menu on, so it is worth reaching without the send key next
// to it — brushing 통화 in a game is a quick save nobody asked for.
const band = { "band-c3": "MENU", "band-c5": "CALL", "band-c7": "CLR" };
// The last row's three, centred in it: the row is seven cells wide and these
// are the middle three, which is where they sat when the row was a band of its
// own that centred whatever was in it.
const lastRow = { "pad-r4c3": "*", "pad-r4c4": "0", "pad-r4c5": "#" };

// The three shapes this page has always drawn, in the cells they were drawn in.
// `keypad-layout.test.mjs` compares each one against what the markup used to
// hide and show, because a shape that does not reproduce what it drew is a
// keypad that moved under somebody who never asked it to.
//
// Read the tables as the grid: seven columns, and the two former pads are
// columns 1-3 and 5-7 with the new one between them.
export const shipped = {
  // The arrows are the number keys a handset put them on, so the left is
  // 2 4 6 8 and the right keeps the corners and the centre.
  //
  //     .  2  .  |  .  |  1  .  3
  //     4  .  6  |  .  |  .  5  .
  //     .  8  .  |  .  |  7  .  9
  //           *  0  #
  type1: fill({
    ...band,
    ...lastRow,
    "pad-r1c2": "2",
    "pad-r2c1": "4",
    "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r1c5": "1",
    "pad-r1c7": "3",
    "pad-r2c6": "5",
    "pad-r3c5": "7",
    "pad-r3c7": "9",
  }),
  // Named direction keys on the left and the whole number pad on the right,
  // which is the shape for a title that reads the arrows rather than the
  // digits. It is the only one with a 확인 key: the other two spend that cell
  // on 5, which is what a handset's centre key sent.
  //
  //     .  ↑  .  |  .  |  1  2  3
  //     ←  확 →  |  .  |  4  5  6
  //     .  ↓  .  |  .  |  7  8  9
  //           *  0  #
  type2: fill({
    ...band,
    ...lastRow,
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
  }),
  // type1 with the two upper diagonals brought over, so the row a thumb rests
  // on reads 1 2 3. The same keys moved rather than added — the number pad
  // gives them up while this shape holds.
  //
  //     1  2  3  |  .  |  .  .  .
  //     4  .  6  |  .  |  .  5  .
  //     .  8  .  |  .  |  7  .  9
  //           *  0  #
  type3: fill({
    ...band,
    ...lastRow,
    "pad-r1c1": "1",
    "pad-r1c2": "2",
    "pad-r1c3": "3",
    "pad-r2c1": "4",
    "pad-r2c3": "6",
    "pad-r3c2": "8",
    "pad-r2c6": "5",
    "pad-r3c5": "7",
    "pad-r3c7": "9",
  }),
};

// The empty pad. It is a shape like the other three rather than a button that
// clears them, because that is what makes it survive a reload and what lets the
// size settings hang off it: everything here is stored per shape, and "no keys
// at all" has to be one of them to be stored at all.
//
// It empties the band as well. Nothing is unreachable that way — Opts is not a
// cell and never can be, so the panel that undoes this is always on screen —
// and a shape that kept three keys nobody asked for would not be the empty one.
shipped.type4 = fill({});

export const shapes = ["type1", "type2", "type3", "type4"];

// A shape has no name here. It had four, for the three "start from this one"
// buttons the editor used to carry, and one reset button replaced them — after
// which the only place a shape is named is the markup's own `<option>`, which
// `draw` reads to know what to put back when a pad stops being edited. A second
// list of the same four names would be a second place for them to be wrong.

// The keys a cell may hold. It is the keyboard panel's list, because the two
// answer the same question — which phone keys does this handset have — and a
// second list would be a second place to forget one.
export const assignable = keyOrder;
const knownKey = new Set(assignable);

// What a key is called *on a pad button*, where it differs from what it is
// called in a settings row. Three do. A 48-pixel key cannot carry 통화 or 확인
// beside a 5 without one of them being a different size of text, and these are
// the faces the pad has always printed; the Korean stays as the button's
// accessible name, which is `keyLabel`. Everything else — the digits, the
// arrows, CLR — reads the same either way and falls through.
const faces = { CALL: "Call", MENU: "Menu", OK: "OK" };

export const keyFace = name => faces[name] ?? keyLabel(name);

// clampCells folds whatever was stored into the table this build declares:
// every cell this build has, holding a key this build knows, and EMPTY for
// anything else. A cell or a key the page no longer has is dropped by never
// being asked for, which is what keeps an entry left behind by an older build
// from coming back when a later one reuses the name.
export const clampCells = stored => {
  const record = stored !== null && typeof stored === "object" ? stored : {};
  return Object.fromEntries(
    cellIds.map(id => {
      const asked = record[id];
      return [id, typeof asked === "string" && knownKey.has(asked) ? asked : EMPTY];
    }),
  );
};

export const isShape = name => Object.hasOwn(shipped, name);

// shapeMatching reports which shape a table *is*, if any, so a page that stored
// an edit which happens to equal a shape selects that shape plainly rather than
// calling it a modified one.
export const shapeMatching = table => {
  const asked = clampCells(table);
  return shapes.find(name => cellIds.every(id => shipped[name][id] === asked[id])) ?? "";
};

// Which shape a table is *while a shape is chosen*. Emptying every cell of
// type1 by hand makes a table that is also type4, and answering "type4" there
// would rename somebody's keypad under them: what they did was edit type1 down
// to nothing, and the shape they are on is the one they picked. So the chosen
// shape wins whenever the table matches it, and only otherwise is it looked up.
const shapeFor = (table, chosen) =>
  cellIds.every(id => shipped[chosen]?.[id] === table[id]) ? chosen : shapeMatching(table);

// assign puts a key in a cell, and clear empties one. Neither takes the key
// away from anywhere else — see the rule at the top of this file.
export const assign = (table, id, name) =>
  knownCell.has(id) && knownKey.has(name) ? { ...table, [id]: name } : table;

export const clear = (table, id) =>
  knownCell.has(id) ? { ...table, [id]: EMPTY } : table;

// createKeypadLayout answers the object app.js drives, over whatever storage it
// is given. The storage is a parameter so a test can hand it a map; the default
// is the page's own fail-safe store, for the reason keypad-size.js states at
// its own default — a browser told to block site data throws on the property,
// and a default argument is evaluated at the call.
//
// Two entries are read, and which one is present is the whole of the
// migration. `wfeature:keypadLayout` is the setting this page has always
// stored and still means "which shape" — **the entry keeps its 0.2 name on
// purpose**, because renaming it would lose the choice of everybody who has
// one, and a storage key is a promise to a browser rather than a word in this
// file. `wfeature:keypadKeys` appears only once somebody edits a cell. **A page with no second entry draws exactly the
// keypad it drew before**, which is what makes this safe for everybody who
// never opens the editor.
export const createKeypadLayout = (storage = local, layoutKey = "wfeature:keypadLayout") => {
  const readJSON = () => {
    try {
      const stored = JSON.parse(storage?.getItem(KEYPAD_KEYS_KEY) ?? "null");
      return stored !== null && typeof stored === "object" ? stored : {};
    } catch {
      // Both halves can fail: a storage handed in by a caller may throw where
      // the page's own does not, and a stored string that is not JSON is what
      // a hand edit leaves behind. Either way every shape is its own.
      return {};
    }
  };

  const readShape = () => {
    try {
      const stored = storage?.getItem(layoutKey);
      return isShape(stored) ? stored : shapes[0];
    } catch {
      return shapes[0];
    }
  };

  let shape = readShape();

  // **The edits are kept per shape**, which is what lets somebody arrange type1
  // the way they like, switch to type4 to build another pad from nothing, and
  // find the first one still there on the way back. A shape with no entry is
  // the shape as shipped.
  //
  // An edit is not merged into its shape: a cell somebody emptied has to stay
  // empty where the shipped shape fills it, or clearing a key would not be
  // something this editor can do.
  const stored = readJSON();
  const edits = new Map();
  for (const name of shapes) {
    // An entry that is not a table at all is no entry. Folding one through
    // `clampCells` would answer every cell EMPTY, and an empty table is a
    // *valid* edit — so a hand-edited or truncated entry would come back as a
    // keypad with no keys on it rather than as the shape it names.
    const entry = stored[name];
    if (entry === null || typeof entry !== "object" || Array.isArray(entry)) continue;
    const table = clampCells(entry);
    if (shapeFor(table, name) === name) continue;
    edits.set(name, table);
  }

  const writeKeys = () => {
    try {
      if (edits.size === 0) {
        storage?.removeItem?.(KEYPAD_KEYS_KEY);
        return true;
      }
      return storage?.setItem(KEYPAD_KEYS_KEY, JSON.stringify(Object.fromEntries(edits))) !== false;
    } catch {
      return false;
    }
  };

  const writeShape = () => {
    try {
      return storage?.setItem(layoutKey, shape) !== false;
    } catch {
      return false;
    }
  };

  const table = () => ({ ...(edits.get(shape) ?? shipped[shape]) });

  return {
    // keys answers the table the pad is drawn from, whichever half it came out
    // of.
    keys: table,

    // shape is the shape the panel shows as chosen, and edited says whether
    // the pad is still that shape. The panel needs both: it selects a shape
    // *and* says the pad is no longer it.
    shape: () => shape,
    edited: () => edits.has(shape),

    keyAt: id => table()[id] ?? EMPTY,

    // set puts a key in a cell of the shape now chosen, and answers the table
    // that resulted.
    set: (id, name) => {
      if (!knownCell.has(id)) return table();
      const next = name === EMPTY ? clear(table(), id) : assign(table(), id, name);
      // An edit that lands back on the shape is not an edit. Storing it as one
      // would leave the list calling a shape modified when it is not — and
      // emptying every cell of type1 makes a table type4 also matches, which is
      // why the shape now chosen is asked about first.
      if (shapeFor(next, shape) === shape) edits.delete(shape);
      else edits.set(shape, next);
      writeKeys();
      return table();
    },

    // useShape chooses a shape. The edits of the one being left are kept and
    // so are the edits of the one being arrived at, because a shape is a place
    // to keep a keypad rather than a button that builds one.
    useShape: name => {
      if (!isShape(name)) return table();
      shape = name;
      writeShape();
      return table();
    },

    // reset takes the chosen shape back to the keypad this page ships it as,
    // and forgets the edit rather than storing the shape's own cells: a stored
    // copy would go on meaning "this shape" after the shipped one changed.
    reset: () => {
      edits.delete(shape);
      writeKeys();
      return table();
    },
  };
};
