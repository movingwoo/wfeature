import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { keyOrder } from "./keybindings.js";
import { isCheckpointKey } from "./checkpoint.js";
import { assignable, shapes, legacyPresets as shipped, cellIds } from "./keypad-layout.js";

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
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8") + readFileSync(new URL("./keypad-editor.js", import.meta.url), "utf8");
const style = readFileSync(new URL("./style.css", import.meta.url), "utf8");

// Every key any shape puts on the pad, plus every key the editor may put there:
// a cell may hold any of them, so the code table has to answer for all of them
// and not only for the ones a shape happens to use.
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
  // Local controls send no handset code.
  for (const name of buttonKeys.filter(name => name !== "RAPID_FIRE" && name !== "SETTINGS" && !isCheckpointKey(name))) {
    assert.ok(tableKeys.has(name), `the button ${name} has no code`);
  }
  assert.ok(!tableKeys.has("SETTINGS"), "the settings key sends the game a code");
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

test("the right soft key is on the keypad and the keyboard, and carries MH_KEY_SOFT2", () => {
  // -7 for the reason -6 is the menu key: MH_KEY_SOFT2 and the MIDP value for
  // the right soft key are one number, so the server hands it on untranslated,
  // and `internal/session`'s key translation test holds that end. Titles name
  // it by where it sat — 우측상단키 — and hang a minimap, a world map, a pause
  // or a shop on it with no other key for any of them.
  assert.equal(tableKeys.get("SOFT2"), -7);
  assert.ok(shippedHas("SOFT2"), "the keypad this page ships has no right soft key");
  assert.ok(keyboardKeys.includes("SOFT2"), "the right soft key has no keyboard shortcut");
});

test("the third soft key stays the command line's to send", () => {
  // `wfeature key soft3|ez`. The left soft key is on the page as MENU and the
  // right one as SOFT2; the third was not asked for, and a name for the left
  // one beside MENU would be one key under two names.
  for (const name of ["SOFT1", "SOFT3", "EZ"]) {
    assert.ok(!tableKeys.has(name), `the soft key ${name} is in the code table`);
    assert.ok(!buttonKeys.includes(name), `the soft key ${name} is on the keypad`);
    assert.ok(!assignable.includes(name), `the soft key ${name} is in the editor's list`);
    assert.ok(!keyboardKeys.includes(name), `the soft key ${name} is on the keyboard`);
  }
  // The positive numbers are what the page sent when it briefly carried all
  // three soft keys under their own names. The server still translates them
  // for a shell served from a phone's cache, and a page sending one now would
  // reach a MIDlet as nothing at all.
  for (const code of [6, 7, 9, -8]) {
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
  // All but one: the settings key cannot leave a pad, the empty one included.
  assert.deepEqual(Object.values(shipped.type4).filter(key => key !== ""), ["SETTINGS"]);
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

test("the grid editor replaces the sizing controls and retains its entry points", () => {
  for (const id of ["keypad-grid", "keypad-arrange", "keypad-arrange-open", "keypad-arrange-close",
    "keypad-arrange-multiple", "keypad-arrange-merge", "keypad-arrange-split", "keypad-arrange-clear", "keypad-arrange-undo"]) {
    assert.ok(page.includes(`id="${id}"`));
  }
  assert.ok(!page.includes("keypad-size-list"));
  assert.ok(!app.includes("keypad-size.js"));
  assert.ok(!style.includes("--keypad-split"));
  assert.ok(!style.includes("--keypad-band"));
  assert.equal(cellIds.length, 126);
});
