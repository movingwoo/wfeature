import assert from "node:assert/strict";
import { test } from "node:test";

import { createExternalLaunchNotice } from "./external-launch.js";

const fixture = connection => {
  const nodes = new Map();
  for (const name of ["notice", "url", "link", "dismiss"]) {
    const classes = new Set();
    const handlers = {};
    const node = {
      handlers,
      textContent: "",
      href: "",
      classList: {
        add: value => classes.add(value),
        remove: value => classes.delete(value),
        contains: value => classes.has(value),
      },
      addEventListener: (kind, handler) => { handlers[kind] = handler; },
      removeAttribute: attribute => { if (attribute === "href") node.href = ""; },
    };
    Object.defineProperty(node, "innerHTML", { set() { throw new Error("guest URL parsed as markup"); } });
    nodes.set(`external-launch-${name}`, node);
  }
  const controller = createExternalLaunchNotice({
    document: { getElementById: id => nodes.get(id) },
    getSession: () => connection,
  });
  return { controller, node: name => nodes.get(`external-launch-${name}`) };
};

test("a validated request becomes a literal user-activated link", async () => {
  const acknowledgements = [];
  const f = fixture({ acknowledgeExternalLaunch: async request => acknowledgements.push(request) });
  assert.equal(f.controller.show({ request: 7, url: "HTTP://example.invalid/a b?q=1" }), true);
  const target = "http://example.invalid/a%20b?q=1";
  assert.equal(f.node("url").textContent, target);
  assert.equal(f.node("link").href, target);
  assert.equal(f.node("notice").classList.contains("visible"), true);
  assert.deepEqual(acknowledgements, []);

  // The click listener does not prevent the anchor action or remove its href
  // before the browser follows it; it only retires the Host notice.
  f.node("link").handlers.click();
  await Promise.resolve();
  assert.deepEqual(acknowledgements, [7]);
  assert.equal(f.node("notice").classList.contains("visible"), false);
  assert.equal(f.node("link").href, target);
});

test("executable, relative, credentialed, and malformed targets never become links", () => {
  for (const url of [
    "javascript:alert(1)",
    "data:text/plain,hello",
    "file:///tmp/item",
    "/relative/item",
    "https://user:pass@example.invalid/item",
    "not a url",
  ]) {
    const f = fixture({ acknowledgeExternalLaunch: async () => {} });
    assert.equal(f.controller.show({ request: 1, url }), false, url);
    assert.equal(f.node("notice").classList.contains("visible"), false, url);
    assert.equal(f.node("link").href, "", url);
  }
});

test("dismiss acknowledges, while park detaches without consuming the request", async () => {
  const acknowledgements = [];
  const connection = { acknowledgeExternalLaunch: async request => acknowledgements.push(request) };
  const dismissed = fixture(connection);
  dismissed.controller.show({ request: 11, url: "https://example.invalid/item" });
  dismissed.node("dismiss").handlers.click();
  await Promise.resolve();
  assert.deepEqual(acknowledgements, [11]);
  assert.equal(dismissed.node("link").href, "");

  const parked = fixture(connection);
  parked.controller.show({ request: 12, url: "https://example.invalid/parked" });
  parked.controller.detach();
  await Promise.resolve();
  assert.deepEqual(acknowledgements, [11]);
  assert.equal(parked.node("notice").classList.contains("visible"), false);
  assert.equal(parked.node("link").href, "");
});

test("focused notice controls isolate activation keydown but leave held-key release alone", () => {
  const f = fixture({ acknowledgeExternalLaunch: async () => {} });
  let stopped = false, prevented = false;
  f.node("notice").handlers.keydown({
    stopPropagation() { stopped = true; },
    preventDefault() { prevented = true; },
  });
  assert.equal(stopped, true);
  assert.equal(prevented, false);
  assert.equal(f.node("notice").handlers.keyup, undefined);
});
