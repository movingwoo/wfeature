import assert from "node:assert/strict";
import { test } from "node:test";
import { initKeypadEditor, fitKeypadLabel } from "./keypad-editor.js";
import { createKeypadLayout } from "./keypad-layout.js";

// Exercise the actual editor handlers with DOM stubs and explicit font metrics.
// These metrics do not establish rendered text or browser hit testing.
const fontSize = (label) => Number.parseFloat(label.style.fontSize) || 16;
const textWidth = (label) =>
  Array.from(label.textContent ?? "").reduce(
    (width, character) =>
      width + (character.codePointAt(0) > 127 ? 1 : 0.5) * fontSize(label),
    0,
  );
const element = (tag = "div") => {
  const attributes = new Map(),
    classes = new Set();
  return {
    tag,
    children: [],
    dataset: {},
    style: {},
    handlers: {},
    classList: {
      add: (name) => classes.add(name),
      remove: (name) => classes.delete(name),
      toggle: (name, on) => (on ? classes.add(name) : classes.delete(name)),
    },
    setAttribute: (name, value) => attributes.set(name, value),
    getAttribute: (name) => attributes.get(name) ?? null,
    removeAttribute: (name) => attributes.delete(name),
    addEventListener(name, handler) {
      this.handlers[name] = handler;
    },
    append(...nodes) {
      this.children.push(
        ...nodes.flatMap((node) =>
          node.tag === "fragment" ? node.children : [node],
        ),
      );
    },
    replaceChildren(...nodes) {
      this.children = [];
      this.append(...nodes);
    },
    querySelectorAll(selector) {
      assert.equal(selector, "button");
      return this.children.filter((node) => node.tag === "button");
    },
    closest(selector) {
      assert.equal(selector, "button[data-cell]");
      return this.tag === "button" && this.dataset.cell ? this : null;
    },
    get firstChild() {
      return this.children[0];
    },
    getBoundingClientRect() {
      return { width: Number.parseFloat(this.style.width) || 0, height: 0 };
    },
    setPointerCapture() {},
    focus() {},
    click() {
      if (!this.disabled) this.handlers.click();
    },
  };
};

const fixture = ({ width = 0, height = 0 } = {}) => {
  const nodes = new Map();
  const createElement = (tag) =>
    Object.assign(element(tag), { ownerDocument: document });
  const node = (id) => {
    if (!nodes.has(id)) nodes.set(id, createElement());
    return nodes.get(id);
  };
  const values = new Map([["wfeature:keypadLayout", "type4"]]);
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
  let resized;
  const window = {
    ...element(),
    matchMedia: () => ({ matches: false }),
    getComputedStyle: (label) => ({
      columnGap: "4px",
      fontSize: `${fontSize(label)}px`,
    }),
    ResizeObserver: class {
      constructor(callback) {
        resized = callback;
      }
      observe() {}
    },
  };
  const document = {
    defaultView: window,
    getElementById: node,
    querySelector: () => node("container"),
    querySelectorAll: () => [],
    createElement,
    createElementNS: (_, tag) => createElement(tag),
    createDocumentFragment: () => createElement("fragment"),
    createRange() {
      let label;
      return {
        selectNodeContents(node) {
          label = node;
        },
        getBoundingClientRect: () => ({ width: textWidth(label) }),
      };
    },
    addEventListener() {},
  };
  node("keypad-grid").getBoundingClientRect = () => ({ width, height });
  initKeypadEditor({
    document,
    releaseInput() {},
    onEditing() {},
    changed() {},
    storage,
  });
  const control = (name) => node(`keypad-arrange-${name}`);
  const board = node("keypad-grid");
  const cell = (id) =>
    board.children.find((button) => button.dataset.cell === id);
  const key = (name) =>
    control("keys").children.find((button) => button.dataset.assign === name);
  control("open").click();
  return {
    control,
    cell,
    key,
    label: (id) => cell(id).children.find((child) => child.tag === "span"),
    resize(nextWidth, nextHeight) {
      width = nextWidth;
      height = nextHeight;
      resized();
    },
    stored: () => createKeypadLayout(storage),
    pick(id) {
      assert.ok(cell(id), `missing cell ${id}`);
      board.handlers.pointerdown({
        target: cell(id),
        button: 0,
        pointerId: 1,
        clientX: 0,
        clientY: 0,
        preventDefault() {},
      });
      window.handlers.pointerup({ pointerId: 1 });
    },
  };
};

test("merging returns to assignment with the new button selected", () => {
  const f = fixture();
  f.control("multiple").click();
  // Selection order differs from the merged group's canonical first cell.
  for (const id of ["r3c3", "r3c2", "r2c2"]) f.pick(id);
  f.control("merge").click();
  assert.equal(f.control("multiple").getAttribute("aria-pressed"), "false");
  assert.equal(f.cell("r2c2").getAttribute("aria-pressed"), "true");
  assert.equal(f.control("keys").hidden, false);
  f.key("5").click();
  assert.equal(f.cell("r2c2").dataset.key, "5");
  assert.deepEqual(f.stored().groupAt("r3c3").cells, ["r2c2", "r3c2", "r3c3"]);
  assert.equal(f.stored().keyAt("r3c3"), "5");
});

test("a conflicting merge keeps its selection until a key is chosen", () => {
  const f = fixture();
  f.pick("r2c2");
  f.key("1").click();
  f.pick("r2c3");
  f.key("2").click();
  f.control("multiple").click();
  f.pick("r2c2");
  f.pick("r2c3");
  f.control("merge").click();
  assert.equal(f.control("multiple").getAttribute("aria-pressed"), "true");
  for (const id of ["r2c2", "r2c3"])
    assert.equal(f.cell(id).getAttribute("aria-pressed"), "true");
  assert.match(f.control("hint").textContent, /서로 다른 키/);
  f.key("3").click();
  assert.equal(f.stored().keyAt("r2c2"), "1");
  assert.equal(f.stored().keyAt("r2c3"), "2");
  f.control("merge").click();
  assert.equal(f.control("multiple").getAttribute("aria-pressed"), "false");
  assert.equal(f.cell("r2c2").dataset.key, "3");
  f.key("9").click();
  assert.equal(f.stored().keyAt("r2c3"), "9");
});

test("the selected merged settings button remains protected", () => {
  const f = fixture();
  f.control("multiple").click();
  f.pick("r1c3");
  f.pick("r1c1");
  f.control("merge").click();
  assert.equal(f.control("multiple").getAttribute("aria-pressed"), "false");
  assert.equal(f.cell("r1c1").getAttribute("aria-pressed"), "true");
  assert.equal(f.control("clear").disabled, true);
  assert.equal(f.key("5").disabled, true);
  f.key("5").click();
  assert.equal(f.stored().keyAt("r1c3"), "SETTINGS");
});

test("single-cell two-character and longer labels fit their available width", () => {
  const f = fixture({ width: 390, height: 320 });
  f.pick("r2c2");
  for (const key of ["MENU", "CLR", "CALL", "QUICK_SAVE"]) {
    f.key(key).click();
    const label = f.label("r2c2");
    assert.ok(
      textWidth(label) <= label.getBoundingClientRect().width + 0.01,
      key,
    );
    assert.ok(fontSize(label) < 16, key);
  }
});

test("label fitting recalculates on resize and restores the normal size after merging", () => {
  const f = fixture({ width: 320, height: 320 });
  f.pick("r2c2");
  f.key("MENU").click();
  const small = fontSize(f.label("r2c2"));
  f.resize(480, 400);
  assert.ok(fontSize(f.label("r2c2")) > small);
  f.control("multiple").click();
  f.pick("r2c2");
  f.pick("r2c3");
  f.control("merge").click();
  assert.equal(fontSize(f.label("r2c2")), 16);
  assert.ok(
    textWidth(f.label("r2c2")) <= f.label("r2c2").getBoundingClientRect().width,
  );
});

test("a dynamic label refits when its text changes without retaining an old shrink", () => {
  const f = fixture({ width: 390, height: 320 });
  f.pick("r2c2");
  f.key("RAPID_FIRE").click();
  const label = f.label("r2c2");
  for (const text of ["연사 off", "연사 manual", "연사 auto", "연사 off"]) {
    label.textContent = text;
    fitKeypadLabel(label);
    const fitted = fontSize(label);
    assert.ok(textWidth(label) <= label.getBoundingClientRect().width + 0.01);
    fitKeypadLabel(label);
    assert.equal(fontSize(label), fitted);
  }
  label.textContent = "5";
  fitKeypadLabel(label);
  assert.equal(fontSize(label), 16);
});
