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

test("the page sends no soft keys", () => {
  // The three soft keys are the CLI's to send — `wfeature key soft1|soft2|ez`.
  // The page carried them for a while as buttons and then as keyboard-only
  // shortcuts, and both are gone: 6, 7 and 9 are not the page's codes to send.
  for (const name of ["SOFT1", "SOFT2", "EZ"]) {
    assert.ok(!tableKeys.has(name), `the soft key ${name} is back in the code table`);
    assert.ok(!buttonKeys.includes(name), `the soft key ${name} is back on the keypad`);
    assert.ok(!keyboardKeys.includes(name), `the soft key ${name} is back on the keyboard`);
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
  assert.deepEqual(keysIn(container.slice(0, padStart)), ["CLR", "CALL"]);
  assert.deepEqual(keysIn(container.slice(padEnd)), [], "a key sits below the pad");
  // Everything else, and the pad is where a slide runs.
  assert.deepEqual(
    keysIn(container.slice(padStart, padEnd)),
    buttonKeys.filter(name => name !== "CLR" && name !== "CALL"),
  );
});
