import assert from "node:assert/strict";
import { test } from "node:test";

import { drawQr, fetchConnectURL, initConnect, noAddress } from "./connect.js";
import { encodeQr } from "./qr.js";

// A canvas that records what was painted, so the drawing can be checked
// without a browser. Only what drawQr uses is answered.
const canvasOf = () => {
  const filled = [];
  return {
    width: 0,
    height: 0,
    classList: { add() {}, remove() {} },
    filled,
    getContext: () => ({
      fillStyle: "",
      fillRect(x, y, width, height) {
        filled.push({ x, y, width, height, style: this.fillStyle });
      },
    }),
  };
};

// The smallest document the panel needs, with the elements it looks for.
const documentOf = () => {
  const elements = new Map();
  const make = id => {
    const classes = new Set();
    const element = {
      id,
      textContent: "",
      listeners: {},
      classList: {
        add: name => classes.add(name),
        remove: name => classes.delete(name),
        contains: name => classes.has(name),
        toggle: (name, on) => (on ? classes.add(name) : classes.delete(name)),
      },
      addEventListener: (name, handler) => { element.listeners[name] = handler; },
      click: () => element.listeners.click?.(),
    };
    return element;
  };
  for (const id of ["connect-panel", "connect-toggle", "connect-close", "connect-copy", "connect-url"]) {
    elements.set(id, make(id));
  }
  elements.set("connect-qr", Object.assign(canvasOf(), { id: "connect-qr" }));
  return { getElementById: id => elements.get(id) ?? null, elements };
};

const answering = url => async () => ({ ok: true, json: async () => ({ url }) });

test("the code drawn is the code the encoder made, with its quiet zone", () => {
  const canvas = canvasOf();
  const url = "http://192.168.0.5:11541/?k=K6F7EZL2FPF6McP6DvsnCw";
  const drawn = drawQr(canvas, url);
  const expected = encodeQr(url, { level: "M" });

  assert.equal(drawn.size, expected.size);
  // Four modules of quiet zone on each side, and a whole number of pixels per
  // module: a fractional scale is a blurred edge, and a blurred edge is a code
  // that takes three tries to scan.
  assert.equal(drawn.pixels, (expected.size + 8) * drawn.scale);
  assert.ok(Number.isInteger(drawn.scale) && drawn.scale >= 2);
  assert.equal(canvas.width, drawn.pixels);

  // The white ground is painted first and covers everything, so the quiet zone
  // exists whatever colour the panel behind it is.
  const ground = canvas.filled[0];
  assert.deepEqual(
    [ground.x, ground.y, ground.width, ground.height, ground.style],
    [0, 0, drawn.pixels, drawn.pixels, "#ffffff"],
  );
  // The rows are typed arrays, so they are counted rather than flattened.
  let dark = 0;
  for (const row of expected.modules) for (const module of row) if (module) dark++;
  assert.equal(canvas.filled.length - 1, dark);
  // Nothing is painted in the quiet zone.
  for (const rectangle of canvas.filled.slice(1)) {
    assert.ok(rectangle.x >= 4 * drawn.scale && rectangle.y >= 4 * drawn.scale);
  }
});

test("the address comes from the server, because the page cannot know it", async () => {
  const url = "http://192.168.0.5:11541/?k=secret";
  assert.equal(await fetchConnectURL(answering(url)), url);
  // A server with no address to give says so with an empty string rather than
  // an error: a machine with nothing but loopback is an ordinary state.
  assert.equal(await fetchConnectURL(answering("")), "");
  await assert.rejects(() => fetchConnectURL(async () => ({ ok: false, status: 403 })));
});

test("opening the panel draws the address once", async () => {
  const document = documentOf();
  let asked = 0;
  const fetcher = async () => { asked++; return { ok: true, json: async () => ({ url: "http://192.168.0.5:11541" }) }; };
  initConnect({ document, fetcher });

  const toggle = document.elements.get("connect-toggle");
  const panel = document.elements.get("connect-panel");
  toggle.click();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.ok(panel.classList.contains("visible"));
  assert.equal(document.elements.get("connect-url").textContent, "http://192.168.0.5:11541");
  assert.ok(document.elements.get("connect-qr").width > 0);

  // Closing and opening again does not ask twice: the address does not change
  // while the server runs.
  document.elements.get("connect-close").click();
  assert.equal(panel.classList.contains("visible"), false);
  toggle.click();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(asked, 1);
});

test("a server with no address to give explains itself instead of showing a dead link", async () => {
  const document = documentOf();
  initConnect({ document, fetcher: answering("") });
  document.elements.get("connect-toggle").click();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(document.elements.get("connect-url").textContent, noAddress);
});
