import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// app.js reaches the DOM as it loads and cannot be imported here, so the
// handlers below are read as text, the way keypad.test.mjs and touch.test.mjs
// read it.
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");

const between = (from, to) => {
  const start = app.indexOf(from);
  assert.ok(start >= 0, `app.js no longer has ${JSON.stringify(from)}`);
  const end = app.indexOf(to, start + from.length);
  assert.ok(end > start, `app.js no longer has ${JSON.stringify(to)} after ${JSON.stringify(from)}`);
  return app.slice(start, end);
};

// The restart question focuses 취소, and Space is OK by default: a keyboard
// handler that did not step aside sent OK to the game behind the question and
// cancelled the Space that would have pressed 취소.
test("a key pressed while a question is open goes to the question, not the game", () => {
  const handler = between('document.addEventListener("keydown", event => {\n    if (isTextEntry(', "\n  });");
  const guard = handler.search(/if \([^\n]*dialogOpen\(\)[^\n]*\) return;/);
  assert.ok(guard > 0, "the keyboard handler does not check for an open dialog");
  assert.ok(guard < handler.indexOf("event.preventDefault()"), "the key is taken before the dialog is asked about");
  assert.ok(guard < handler.indexOf("holds.latch("), "the key reaches the game before the dialog is asked about");
});

// The picker is rebuilt on every return to 'ready' and lands on the last game
// played, which fires no change event of its own. The speed and screen menus
// follow that event, so without it they showed the game that was selected
// before while the start used the one selected now.
test("rebuilding the picker shows the selected game's speed and screen", () => {
  const body = between("const initGameSelect = async () => {", "const initRestart");
  const landed = body.indexOf("select.value = previous");
  assert.ok(landed > 0, "initGameSelect no longer lands on the last game");
  const told = body.indexOf('select.dispatchEvent(new Event("change"))');
  assert.ok(told > landed, "the rebuilt picker does not tell its followers what it shows");
  assert.match(app, /getElementById\("game-select"\)\?\.addEventListener\("change", showStoredSpeed\)/);
  assert.match(app, /getElementById\("game-select"\)\?\.addEventListener\("change", showStored\)/);
});

// A games.json request that never arrives rejects with the browser's English
// sentence, which reportError would put on screen as it is.
test("the game list is asked for through the Korean network failure", () => {
  const body = between("const initGameSelect = async () => {", "const initRestart");
  assert.match(body, /await reach\(fetch, "games\.json"/);
  assert.doesNotMatch(body, /await fetch\("games\.json"/);
});
