import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { keyOrder } from "./keybindings.js";
import { assignable, shapes, shipped, cellIds } from "./keypad-layout.js";

// The keypad is a layout table on one side and a code table on the other, and
// nothing at runtime complains when they disagree: a cell holding a key with no
// entry in the table simply does nothing when pressed, which is
// indistinguishable from a game that ignores the key. So the two are compared
// here instead. The keyboard is a third list — the keys the settings panel
// offers — and it has to name keys from the same table.
//
// The keys used to be in the markup and are now in `keypad-layout.js`, which is
// why these read a module rather than `index.html`. What still has to be read
// out of the page is the *cells*: their region is what decides whether a
// sliding finger presses them, and that is markup.

const page = readFileSync(new URL("./index.html", import.meta.url), "utf8");
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");
const style = readFileSync(new URL("./style.css", import.meta.url), "utf8");

// Every key any shape puts on the pad, plus every key the editor may put there:
// a cell may hold any of the twenty, so the code table has to answer for all of
// them and not only for the ones a shape happens to use.
const buttonKeys = [
  ...new Set([...shapes.flatMap(name => Object.values(shipped[name])), ...assignable]),
].filter(name => name !== "");
// app.js reaches the DOM as it loads and cannot be imported here, so its code
// table is read as text. keybindings.js is a plain module and is imported.
const codesStart = app.indexOf("const keyCodes");
const tableSource = app.slice(codesStart, app.indexOf("]);", codesStart));
const tableKeys = new Map(
  [...tableSource.matchAll(/\["([^"]+)",\s*(-?\d+)\]/g)].map(match => [match[1], Number(match[2])]),
);
const keyboardKeys = keyOrder;

// A key "is on the keypad" when the shape this page ships has it somewhere.
// It used to mean every shape, which stopped being the right question when the
// empty shape arrived: type4 has nothing on it on purpose, and a rule that
// every shape carries the send key would forbid it.
const shippedHas = name => Object.values(shipped[shapes[0]]).includes(name);

test("every keypad button sends a key the page knows a code for", () => {
  for (const name of buttonKeys) {
    assert.ok(tableKeys.has(name), `the button ${name} has no code`);
  }
});

test("every keyboard shortcut names a key the page knows a code for", () => {
  for (const name of keyboardKeys) {
    assert.ok(tableKeys.has(name), `the keyboard shortcut for ${name} has no code`);
  }
});

test("the send key is on the keypad, on the keyboard, and carries its handset code", () => {
  // The one key a game reaches for when it wants something the keypad cannot
  // otherwise say — a quick save, most often. 10 is what a handset sends and
  // what the server translates into each vendor's own value.
  assert.equal(tableKeys.get("CALL"), 10);
  assert.ok(shippedHas("CALL"), "the keypad this page ships has no send key");
  assert.ok(keyboardKeys.includes("CALL"), "the send key has no keyboard shortcut");
});

test("the menu key is the handset's left soft key, on the keypad and the keyboard", () => {
  // -6 is MH_KEY_SOFT1 and the MIDP soft key a MIDlet of this era compares
  // against, which are the same number: the server hands it on untranslated to
  // both, and `internal/session`'s key translation test holds that end. The
  // page sent 6 for this key once, when it carried the soft keys under their
  // own names, and that is not the same number — a positive 6 reaches a MIDlet
  // as nothing at all.
  assert.equal(tableKeys.get("MENU"), -6);
  assert.ok(shippedHas("MENU"), "the keypad this page ships has no menu key");
  assert.ok(keyboardKeys.includes("MENU"), "the menu key has no keyboard shortcut");
});

test("the other two soft keys stay the command line's to send", () => {
  // `wfeature key soft2|ez`. Only the menu key earned a place on the keypad:
  // a title asks for it by name on its own screen, and these two are asked for
  // rarely enough that the page would be spending a button on nothing.
  for (const name of ["SOFT1", "SOFT2", "EZ"]) {
    assert.ok(!tableKeys.has(name), `the soft key ${name} is back in the code table`);
    assert.ok(!buttonKeys.includes(name), `the soft key ${name} is back on the keypad`);
    assert.ok(!assignable.includes(name), `the soft key ${name} is back in the editor's list`);
    assert.ok(!keyboardKeys.includes(name), `the soft key ${name} is back on the keyboard`);
  }
  for (const code of [7, 9, -7, -8]) {
    assert.ok(
      ![...tableKeys.values()].includes(code),
      `the page sends ${code}, which is a soft key it has no button for`,
    );
  }
});

test("the empty shape is offered, and it is empty", () => {
  // It is a shape rather than a button that clears the pad, because that is what
  // survives a reload and what the size settings hang off: everything is stored
  // per shape, and "no keys at all" has to be one of them to be stored at all.
  assert.deepEqual(Object.values(shipped.type4).filter(key => key !== ""), []);
  assert.ok(shapes.includes("type4"));
  assert.ok(page.includes('value="type4"'), "the shape list does not offer the empty one");
  // And it is last, so `shapeMatching` answers a named shape before the empty one
  // for a table both match — which only the empty table is.
  assert.equal(shapes[shapes.length - 1], "type4");
});

test("the keypad layout is a setting rather than a key, and every list of it is driven", () => {
  // It was a button in the keypad's top row, cycling the three layouts, and
  // that spot is the menu key's now.
  assert.ok(
    !page.includes("keypad-layout-toggle"),
    "the layout toggle is back in the keypad's top row",
  );
  for (const layout of ["type1", "type2", "type3", "type4"]) {
    assert.ok(page.includes(`value="${layout}"`), `the keypad layout list has no ${layout}`);
  }

  // **There are two lists and neither is the one that counts.** One is on the
  // keypad screen beside the sliders and the cells, and one is in the settings
  // panel where it can be reached without opening anything; on a wide window
  // the rail is docked and both are on screen at once. So the script drives
  // them by class rather than reaching for an id, and a stale one is the whole
  // failure this pins: a change at either has to redraw both.
  const lists = [...page.matchAll(/<select[^>]*class="[^"]*\bkeypad-shape-list\b[^"]*"/g)];
  assert.equal(lists.length, 2, "the page does not draw two shape lists");
  assert.match(app, /querySelectorAll\("\.keypad-shape-list"\)/, "app.js reaches for one list");
  assert.match(app, /for \(const list of shapeLists\) list\.value =/, "the lists are not all redrawn");
  assert.match(app, /for \(const list of shapeLists\) \{/, "the lists are not all listened to");
  // Every list offers the same shapes, or one of them would be a control that
  // cannot reach a keypad the other can.
  const options = [...page.matchAll(/<select[^>]*\bkeypad-shape-list\b[^>]*>([\s\S]*?)<\/select>/g)]
    .map(match => [...match[1].matchAll(/value="([^"]+)"/g)].map(option => option[1]));
  assert.equal(options.length, 2);
  assert.deepEqual(options[0], options[1], "the two shape lists offer different shapes");

  // And no list carries an entry that is not a shape. There was one — hidden
  // until a cell moved, then revealed to say the pad was no longer any of them
  // — and it was wrong twice: `hidden` on an `<option>` is honoured by some
  // browsers and ignored by others, so it showed where it should not have; and
  // an entry nobody can usefully choose is a question rather than an answer.
  // The chosen option's own name says it now, which app.js writes.
  // Scoped to the shape lists: the page has other selects — the picture filter,
  // the handset screen, the speed — and their options are not shapes.
  assert.deepEqual(
    options.flat().filter(value => !/^type\d$/.test(value)),
    [],
    "a shape list offers something that is not a shape",
  );
  assert.match(app, /option\.textContent = mine && edited \? `\$\{name\} \(수정됨\)` : name;/,
    "an edited pad is not said on the shape it started from");
  assert.match(app, /shapeNames\.set\(option, option\.textContent\)/,
    "the name an option goes back to is not remembered");
});

test("the key settings the page draws are the keys the panel offers", () => {
  // The list is built in script against markup that has to be there for it: a
  // missing id is a section that silently never appears, which looks exactly
  // like the page deciding there is no keyboard.
  for (const id of ["key-bindings", "key-bindings-list", "key-bindings-reset"]) {
    assert.ok(page.includes(`id="${id}"`), `the settings panel has no ${id}`);
    assert.ok(app.includes(`"${id}"`), `app.js never looks up ${id}`);
  }
  // Hidden in the markup and revealed from script. Shipping it visible would
  // put the list on every phone.
  assert.match(page, /id="key-bindings"[^>]*class="[^"]*\bhidden\b/);
});

test("no two keys share a code", () => {
  // A collision would send one key where the other was pressed, and nothing at
  // runtime would say so.
  const seen = new Map();
  for (const [name, code] of tableKeys) {
    assert.ok(!seen.has(code), `${name} and ${seen.get(code)} both send ${code}`);
    seen.set(code, name);
  }
});

test("the cells a finger can slide across are the pad's, and the band is not", () => {
  // app.js decides what a slide may press by where a button sits: inside
  // `.keypad-pad` a finger crossing a cell presses it, and outside it the cell
  // is in the band, which is aimed at one key at a time. Moving a button
  // between the two in the markup would change what a drag does with nothing
  // else to say so — and now that the keys are a setting it is the *cell* that
  // carries the rule, so this is what has to be pinned.
  const container = page.slice(page.indexOf('class="button-container"'), page.indexOf("</main>"));
  const padStart = container.indexOf('class="keypad-pad"');
  assert.ok(padStart > 0, "the keypad's own markup has moved");

  const cellsIn = source => [...source.matchAll(/data-cell="([^"]+)"/g)].map(match => match[1]);
  // The band, which is a grid of its own for exactly this reason: its cells are
  // aimed at one at a time. Opts is among them in the markup and is not a cell.
  assert.deepEqual(
    cellsIn(container.slice(0, padStart)),
    cellIds.filter(id => id.startsWith("band-")),
  );
  assert.match(
    container.slice(0, padStart),
    /class="keypad-band">\s*<button id="settings-toggle"/,
    "Opts is not the band's first column",
  );
  // Everything else, and the pad is where a slide runs. The order is the grid's
  // too: the cells are placed by where they sit in the markup, so this is also
  // what says row 1 is drawn before row 2.
  assert.deepEqual(
    cellsIn(container.slice(padStart)),
    cellIds.filter(id => !id.startsWith("band-")),
  );
});

test("every cell the layout declares is a button on the page, and no others", () => {
  // Two lists that have to be the same one: a cell the module names and the
  // markup lacks is a key that can be chosen and never appears, and a button
  // the markup has and the module does not name can never be given a key —
  // both of which look like the editor losing a press.
  const container = page.slice(page.indexOf('class="button-container"'), page.indexOf("</main>"));
  const drawn = [...container.matchAll(/data-cell="([^"]+)"/g)].map(match => match[1]);
  assert.deepEqual([...drawn].sort(), [...cellIds].sort());
  assert.equal(new Set(drawn).size, drawn.length, "a cell is drawn twice");
});

test("the cells carry no key of their own, and the shapes are gone from the page", () => {
  // The whole of the change: what used to be three blocks of markup with keys
  // written into them is one block of cells with none. A `data-key` back in the
  // page would be a key the editor cannot move and the code table never sees.
  const container = page.slice(page.indexOf('class="button-container"'), page.indexOf("</main>"));
  assert.ok(!container.includes("data-key="), "a keypad button carries a key of its own again");
  for (const gone of ["type1-direction-pad", "type2-direction-pad", "type2-only", "type3-only",
                      "type3-hidden", "keypad-main", "number-pad", "key-top-left"]) {
    assert.ok(!page.includes(gone), `the page still draws shapes with ${gone}`);
    assert.ok(!style.includes(gone), `the stylesheet still implements shapes with ${gone}`);
  }
  // And the editor has to be reachable, or the cells are a table nobody can
  // change: a missing id is a button that silently never appears.
  for (const id of ["keypad-arrange", "keypad-arrange-open", "keypad-arrange-close",
                    "keypad-arrange-keys", "keypad-arrange-reset", "keypad-arrange-hint"]) {
    assert.ok(page.includes(`id="${id}"`), `the page has no ${id}`);
    assert.ok(app.includes(`"${id}"`), `app.js never looks up ${id}`);
  }
});

test("the editor makes the pad inert, and says so where both halves can see it", () => {
  // A pad that both edited and played would send the key it was being asked to
  // replace. The pointer handler and the keydown handler each read the same
  // flag, and the stylesheet reveals the empty cells off the same class.
  assert.match(app, /if \(keypadArranging\) \{/, "the pointer handler does not check the flag");
  assert.match(app, /if \(keypadArranging\) return;/, "the keydown handler does not check the flag");
  assert.match(style, /\.button-container\.arranging \.keypad-cell\.empty \{/);
  // An empty cell has to keep its place. The grid places cells by auto-flow in
  // markup order, so one taken out of the layout is not a hole — every cell
  // after it moves up one and the pad is scrambled from there on. `display:
  // none` is exactly that mistake, and it is the obvious way to write this.
  assert.match(style, /\.keypad-cell\.empty \{\s*visibility: hidden;/,
    "an empty cell is hidden in a way that moves the cells after it");
  assert.match(style, /\.button-container\.arranging \.keypad-cell\.empty \{\s*visibility: visible;/);
  const padRule = style.slice(style.indexOf(".keypad-pad {"), style.indexOf("}", style.indexOf(".keypad-pad {")));
  assert.ok(!/grid-auto-flow/.test(padRule), "the pad's flow is no longer the markup's order");
  // The key list is toggled with `el.hidden`, and an author rule setting
  // `display` beats the browser's own `[hidden] { display: none }` whatever its
  // specificity. The page has been caught by that once already — the settings
  // panel's own rows carry the same rule — so the pair is pinned here.
  assert.match(style, /\.keypad-arrange-keys\[hidden\] \{\s*display: none;/);
  assert.match(page, /id="keypad-arrange-keys"[^>]*hidden/, "the key list starts open");
});
