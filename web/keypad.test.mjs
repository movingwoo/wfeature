import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { keyOrder } from "./keybindings.js";

// The keypad is markup on one side and a code table on the other, and nothing
// at runtime complains when they disagree: a button whose data-key has no entry
// in the table simply does nothing when pressed, which is indistinguishable
// from a game that ignores the key. So the two are compared here instead. The
// keyboard is a third list — the keys the settings panel offers — and it has to
// name keys from the same table.

const page = readFileSync(new URL("./index.html", import.meta.url), "utf8");
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");

const buttonKeys = [...page.matchAll(/data-key="([^"]+)"/g)].map(match => match[1]);
// app.js reaches the DOM as it loads and cannot be imported here, so its code
// table is read as text. keybindings.js is a plain module and is imported.
const codesStart = app.indexOf("const keyCodes");
const tableSource = app.slice(codesStart, app.indexOf("]);", codesStart));
const tableKeys = new Map(
  [...tableSource.matchAll(/\["([^"]+)",\s*(-?\d+)\]/g)].map(match => [match[1], Number(match[2])]),
);
const keyboardKeys = keyOrder;

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
  assert.ok(buttonKeys.includes("CALL"), "the send key has no button");
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
  assert.ok(buttonKeys.includes("MENU"), "the menu key has no button");
  assert.ok(keyboardKeys.includes("MENU"), "the menu key has no keyboard shortcut");
});

test("the other two soft keys stay the command line's to send", () => {
  // `wfeature key soft2|ez`. Only the menu key earned a place on the keypad:
  // a title asks for it by name on its own screen, and these two are asked for
  // rarely enough that the page would be spending a button on nothing.
  for (const name of ["SOFT1", "SOFT2", "EZ"]) {
    assert.ok(!tableKeys.has(name), `the soft key ${name} is back in the code table`);
    assert.ok(!buttonKeys.includes(name), `the soft key ${name} is back on the keypad`);
    assert.ok(!keyboardKeys.includes(name), `the soft key ${name} is back on the keyboard`);
  }
  for (const code of [7, 9, -7, -8]) {
    assert.ok(
      ![...tableKeys.values()].includes(code),
      `the page sends ${code}, which is a soft key it has no button for`,
    );
  }
});

test("the keypad layout is a setting rather than a key", () => {
  // It was a button in the keypad's top row, cycling the three layouts, and
  // that spot is the menu key's now. The control has to exist in the panel and
  // be read from script, because a missing id is a layout nothing can change
  // and a keypad stuck on Type1 looks exactly like a page with no control.
  assert.ok(page.includes('id="keypad-layout"'), "the settings panel has no keypad layout control");
  assert.ok(app.includes('"keypad-layout"'), "app.js never looks up keypad-layout");
  assert.ok(
    !page.includes("keypad-layout-toggle"),
    "the layout toggle is back in the keypad's top row",
  );
  for (const layout of ["type1", "type2", "type3"]) {
    assert.ok(page.includes(`value="${layout}"`), `the keypad layout list has no ${layout}`);
  }
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

test("the keys a finger can slide across are the pad's, and the top row is not", () => {
  // app.js decides what a slide may press by where a button sits: inside
  // `.keypad-main` or `.keypad-footer` it is the pad and a finger crossing it
  // presses it, and outside them it is the top row, which is aimed at one key
  // at a time. Moving a button between the two in the markup would change what
  // a drag does with nothing else to say so.
  const container = page.slice(page.indexOf('class="button-container"'), page.indexOf("</main>"));
  const padStart = container.indexOf('class="keypad-main"');
  const padEnd = container.indexOf("</p>", container.indexOf('class="keypad-footer"'));
  assert.ok(padStart > 0 && padEnd > padStart, "the keypad's own markup has moved");

  const keysIn = source => [...source.matchAll(/data-key="([^"]+)"/g)].map(match => match[1]);
  assert.deepEqual(keysIn(container.slice(0, padStart)), ["CLR", "CALL", "MENU"]);
  assert.deepEqual(keysIn(container.slice(padEnd)), [], "a key sits below the pad");
  // Everything else, and the pad is where a slide runs.
  assert.deepEqual(
    keysIn(container.slice(padStart, padEnd)),
    buttonKeys.filter(name => !["CLR", "CALL", "MENU"].includes(name)),
  );
});
