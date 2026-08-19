import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

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

test("the menu's preselected screen is the one the page falls back to", () => {
  const preselected = options.filter(option => option.selected);
  assert.equal(preselected.length, 1, "exactly one screen is the default");
  const fallback = app.match(/const DEFAULT_SCREEN = "(\d+)x(\d+)";/);
  assert.equal(preselected[0].width, Number(fallback[1]));
  assert.equal(preselected[0].height, Number(fallback[2]));
});

test("no two options are the same handset", () => {
  const seen = new Set(options.map(option => `${option.width}x${option.height}`));
  assert.equal(seen.size, options.length, "the screen menu lists a handset twice");
});
