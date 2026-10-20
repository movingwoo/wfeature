import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { AUTO_SCREEN, DEFAULT_SCREEN, MAX_SCREEN, MIN_SCREEN } from "./screen-choice.js";

// The screen menu is a list of handsets in the markup, and the size a start may
// carry is a range the server owns. Nothing at runtime compares the two, so an
// option added outside that range would look like a handset in the panel and
// fail only when somebody picked it — which is what these tests compare, by
// reading the server's own bounds rather than a copy of them.

const page = readFileSync(new URL("./index.html", import.meta.url), "utf8");
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");
const server = readFileSync(
  new URL("../internal/webhost/session.go", import.meta.url),
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
  const min = bound("minScreen");
  const max = bound("maxScreen");
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
  assert.equal(DEFAULT_SCREEN, AUTO_SCREEN);
  assert.equal(options.some(option => option.selected), false, "a size is preselected besides the automatic choice");
});

test("the page mirrors the server's screen range", () => {
  assert.equal(MIN_SCREEN, bound("minScreen"));
  assert.equal(MAX_SCREEN, bound("maxScreen"));
});

// A change reaches a game only through a new start, and a note that says so
// with nothing to press made the setting look broken. So the panel offers the
// restart beside the note: it stops the game, and the picker — back on that
// game — starts it the way 실행 would, on the screen just chosen.
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
  const picker = app.slice(app.indexOf("const initGameSelect = async () => {"), app.indexOf("const initRestart"));
  assert.match(picker, /if \(restarting && select\.value === restarting && !startButton\.disabled\) startButton\.click\(\);/);
});

test("no two options are the same handset", () => {
  const seen = new Set(options.map(option => `${option.width}x${option.height}`));
  assert.equal(seen.size, options.length, "the screen menu lists a handset twice");
});
