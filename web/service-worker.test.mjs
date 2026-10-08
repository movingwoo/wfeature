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

// Model a late response from the replaced worker, without a browser-specific
// timer. The integration route separately upgrades a real installed worker.
const cacheFixture = () => {
  const stores = new Map();
  return {
    async keys() { return [...stores.keys()]; },
    async delete(name) { return stores.delete(name); },
    async open(name) {
      if (!stores.has(name)) stores.set(name, new Map());
      const entries = stores.get(name);
      return {
        async add(url) { entries.set(new URL(url, "https://example.test/").href, new Response("installed shell")); },
        async put(request, response) { entries.set(request.url, response); },
        async match(request) { return entries.get(request.url)?.clone(); },
      };
    },
    async match(request) {
      for (const entries of stores.values()) if (entries.has(request.url)) return entries.get(request.url);
    },
  };
};
const workerFixture = async ({ source = worker, caches = cacheFixture(), fetchResponse } = {}) => {
  const { runInNewContext } = await import("node:vm");
  const handlers = new Map();
  let offline = false;
  runInNewContext(source, {
    URL, caches,
    self: { location: { origin: "https://example.test" }, clients: { claim: async () => {} },
      skipWaiting: () => {},
      addEventListener: (name, callback) => handlers.set(name, callback) },
    fetch: async () => {
      if (offline) throw new Error("offline");
      return fetchResponse ? fetchResponse() : new Response("current shell");
    },
  });
  const dispatch = async (name, request) => {
    const pending = [];
    let response;
    handlers.get(name)({ request, waitUntil: promise => pending.push(promise), respondWith: promise => { response = promise; } });
    const answer = await response;
    await Promise.all(pending);
    return answer;
  };
  return { caches, dispatch, goOffline: () => { offline = true; } };
};

test("a controlled navigation retires an old shell recreated after activation", async () => {
  const f = await workerFixture();
  await f.caches.open("wfeature-shell-old");
  await f.caches.open("another-app");
  await f.dispatch("activate");
  assert.deepEqual(await f.caches.keys(), ["another-app"]);
  await f.caches.open("wfeature-shell-old");
  const request = { method: "GET", url: "https://example.test/", mode: "navigate" };
  assert.equal(await (await f.dispatch("fetch", request)).text(), "current shell");
  const names = await f.caches.keys();
  assert.ok(names.includes("another-app"));
  assert.ok(!names.includes("wfeature-shell-old"));
  f.goOffline();
  assert.equal(await (await f.dispatch("fetch", request)).text(), "current shell");
});

test("offline reads never fall back to a leftover cache from another shell", async () => {
  const f = await workerFixture();
  const request = { method: "GET", url: "https://example.test/app.js", mode: "cors" };
  await (await f.caches.open("wfeature-shell-old")).put(request, new Response("stale module"));
  f.goOffline();
  assert.equal(await f.dispatch("fetch", request), undefined);
});

test("a late legacy navigation cannot delete the newly installed offline shell", async () => {
  const f = await workerFixture();
  await f.caches.open("wfeature-shell-v34");
  await f.dispatch("install");
  await f.dispatch("activate");
  // Reproduce the shipped version-34 fetch completion, after replacement. Its
  // prefix-wide sweep is immutable in browsers that already installed it.
  const request = { method: "GET", url: "https://example.test/", mode: "navigate" };
  await (await f.caches.open("wfeature-shell-v34")).put(request, new Response("old navigation"));
  for (const name of await f.caches.keys()) {
    if (name.startsWith("wfeature-shell-") && name !== "wfeature-shell-v34") await f.caches.delete(name);
  }
  f.goOffline();
  for (const url of ["/", "/app.js", "/keypad-editor.js", "/keypad-layout.js", "/keypad-geometry.js"]) {
    const response = await f.dispatch("fetch", { ...request, url: `https://example.test${url}` });
    assert.ok(response, `${url} must survive the old worker's sweep`);
    assert.equal(await response.text(), "installed shell");
  }
});

test("a replaced worker's late response preserves future and unrelated caches", async () => {
  const currentName = worker.match(/const cacheName = "([^"]+)"/)[1];
  const nextName = currentName.replace(/\d+$/, digits => String(Number(digits) + 1));
  const shared = cacheFixture();
  let finish;
  const old = await workerFixture({ caches: shared, fetchResponse: () => new Promise(resolve => { finish = resolve; }) });
  const next = await workerFixture({ caches: shared, source: worker.replace(currentName, nextName) });
  await shared.open("another-app");
  await old.dispatch("install");
  const request = { method: "GET", url: "https://example.test/", mode: "navigate" };
  const pending = old.dispatch("fetch", request);
  await next.dispatch("install");
  await next.dispatch("activate");
  finish(new Response("late old response"));
  await pending;
  next.goOffline();
  const offline = await next.dispatch("fetch", request);
  assert.ok(offline, "the next version must stay available offline");
  assert.equal(await offline.text(), "installed shell");
  assert.ok((await shared.keys()).includes("another-app"));
});
