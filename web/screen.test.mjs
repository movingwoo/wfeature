import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { AUTO_SCREEN, SCREEN_SIZES } from "./screen-choice.js";

// The screen menu is a list of handsets in the markup, and the size a start may
// carry is a range the server owns. Nothing at runtime compares the two, so an
// option added outside that range would look like a handset in the panel and
// fail only when somebody picked it — which is what these tests compare, by
// reading the server's own bounds rather than a copy of them.

const page = readFileSync(new URL("./index.html", import.meta.url), "utf8");
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");
const server = readFileSync(
  new URL("../internal/backend/screen.go", import.meta.url),
  "utf8",
);

const menu = page.slice(
  page.indexOf('<select id="screen-size"'),
  page.indexOf("</select>", page.indexOf('<select id="screen-size"')),
);
const options = [...menu.matchAll(/<option value="(\d+)x(\d+)"([^>]*)>/g)].map(match => ({
  width: Number(match[1]),
  height: Number(match[2]),
  selected: match[3].includes("selected"),
}));

const bound = name => Number(server.match(new RegExp(`${name}\\s*=\\s*(\\d+)`))[1]);

test("the screen menu offers handsets, and every one is inside the range the server takes", () => {
  assert.ok(options.length > 0, "the screen menu has no options");
  const min = bound("MinScreenSide");
  const max = bound("MaxScreenSide");
  for (const { width, height } of options) {
    assert.ok(width >= min && width <= max, `${width}x${height} is outside the width range`);
    assert.ok(height >= min && height <= max, `${width}x${height} is outside the height range`);
  }
});

// The preselected option is the automatic one, which is what a game nobody
// chose a screen for starts on; the sizes are all choices.
test("the menu's preselected screen is the one the page falls back to", () => {
  const values = [...menu.matchAll(/<option value="([^"]+)"([^>]*)>/g)];
  const preselected = values.filter(match => match[2].includes("selected"));
  assert.equal(preselected.length, 1, "exactly one screen is the default");
  assert.equal(preselected[0][1], AUTO_SCREEN);
  assert.equal(options.some(option => option.selected), false, "a size is preselected besides the automatic choice");
});

// screen-choice.js reads a stored value the menu does not offer as automatic,
// so the list it keeps has to be the menu's, in the menu's order.
test("the page's list of sizes is the menu's", () => {
  const values = [...menu.matchAll(/<option value="([^"]+)"/g)].map(match => match[1]);
  assert.deepEqual(values, [AUTO_SCREEN, ...SCREEN_SIZES]);
});

// A change reaches a game only through a new start, and a note that says so
// with nothing to press made the setting look broken. So the panel offers the
// restart beside the note: it stops the game, and once the picker is back it
// starts that game the way 실행 would, on the screen just chosen — whether or
// not the list came back, because a restart that only stopped the game is the
// symptom the button was added to remove.
test("the screen menu offers a restart that starts the same game again", () => {
  const note = page.indexOf('id="screen-note"');
  const restart = page.indexOf('id="screen-restart"');
  assert.ok(note > 0 && restart > note, "the restart is not beside the note");
  const handler = app.slice(app.indexOf('screenRestart?.addEventListener("click"'));
  assert.ok(handler.length > 0, "nothing listens to the restart");
  const asks = handler.indexOf("askToConfirm(");
  const names = handler.indexOf("restartAfterStop = currentGamePath");
  const stops = handler.indexOf("sessionLink.stop()");
  assert.ok(asks >= 0 && asks < names && names < stops, "the restart does not ask, name the game, then stop it");
  const picker = app.slice(app.indexOf("const initGameSelect = async () => {"), app.indexOf("// listGames fills"));
  const lists = picker.indexOf("await listGames(");
  const starts = picker.indexOf("if (restarting) void startChosenGame(restarting);");
  assert.ok(lists > 0 && starts > lists, "the restart is not carried out after the list, whatever it answered");
  assert.match(app, /startButton\.addEventListener\("click", \(\) => startChosenGame\(select\.value\)\);/,
    "실행 and the restart do not share one start");
});

test("no two options are the same handset", () => {
  const seen = new Set(options.map(option => `${option.width}x${option.height}`));
  assert.equal(seen.size, options.length, "the screen menu lists a handset twice");
});
