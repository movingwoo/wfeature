import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// The shell list in the service worker is hand written, and nothing at runtime
// complains when it falls behind: a module missing from it is fetched from the
// network like any other file, so the page works everywhere except offline,
// where it comes up and then fails on an import nobody cached. Two modules had
// drifted out of it that way. So the list is compared here against what the
// page actually loads — the assets index.html names, plus every module reached
// by following relative imports from app.js.

const here = new URL("./", import.meta.url);
const read = name => readFileSync(new URL(name, here), "utf8");

const worker = read("service-worker.js");
const shellSource = worker.slice(worker.indexOf("const shell = ["), worker.indexOf("];"));
const shell = new Set(
  [...shellSource.matchAll(/"\.\/([^"]*)"/g)].map(match => match[1]).filter(name => name !== ""),
);

// index.html's own href/src attributes: the stylesheet, the manifest, the
// icons and the entry module. Absolute URLs are somebody else's to serve.
const page = read("index.html");
const pageAssets = [...page.matchAll(/(?:href|src)="([^"]+)"/g)]
  .map(match => match[1])
  .filter(name => !/^[a-z]+:|^\/\//.test(name));

// The module graph, followed transitively so a module imported only by another
// module counts too.
const modules = new Set();
const walk = name => {
  if (modules.has(name)) return;
  modules.add(name);
  const source = read(name);
  for (const match of source.matchAll(/from\s+"\.\/([^"]+)"/g)) walk(match[1]);
};
walk("app.js");

test("the service worker precaches every asset the page names", () => {
  for (const name of pageAssets) {
    assert.ok(shell.has(name), `${name} is loaded by index.html but is not in the shell list`);
  }
});

test("the service worker precaches every module app.js reaches", () => {
  for (const name of modules) {
    assert.ok(shell.has(name), `${name} is imported by the page but is not in the shell list`);
  }
});

test("the shell list names only files that exist", () => {
  for (const name of shell) {
    assert.doesNotThrow(() => read(name), `${name} is in the shell list but is not a file`);
  }
});
