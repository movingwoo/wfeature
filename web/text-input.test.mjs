import assert from "node:assert/strict";
import { test } from "node:test";
import { createTextInputDialog } from "./text-input.js";

const fixture = connection => {
  const nodes = new Map();
  for (const name of ["dialog", "status", "label", "value", "multiline", "apply", "cancel"]) {
    const handlers = {};
    const item = {
      value: "", textContent: "", open: false, handlers,
      addEventListener(name, handler) { handlers[name] = handler; },
      showModal() { this.open = true; }, close() { this.open = false; }, focus() {},
      getBoundingClientRect() { return { left: 100, top: 100, right: 300, bottom: 300 }; },
    };
    if (name === "label") Object.defineProperty(item, "innerHTML", { set() { throw new Error("prompt parsed as markup"); } });
    nodes.set(`text-input-${name}`, item);
  }
  let releases = 0;
  const controller = createTextInputDialog({
    document: { getElementById: id => nodes.get(id) },
    getSession: () => connection, releaseInput: () => releases++,
  });
  return { controller, node: name => nodes.get(`text-input-${name}`), releases: () => releases };
};

test("IME composition stays local until explicit complete-text submission", async () => {
  const commits = [], cancels = [];
  const f = fixture({
    openTextInput: async () => ({ textInput: { edit: 7, text: "이름", maxLength: 20 } }),
    commitTextInput: async (...args) => commits.push(args),
    cancelTextInput: async id => cancels.push(id),
  });
  await f.controller.open();
  assert.equal(f.releases(), 1);
  assert.match(f.node("status").textContent, /한글.*1칸.*이모지.*2칸 이상/);
  f.node("dialog").handlers.compositionstart();
  f.node("value").value = "한";
  f.node("dialog").handlers.cancel({ preventDefault() {} });
  assert.equal(f.node("dialog").open, true);
  await f.node("apply").handlers.click();
  assert.deepEqual(commits, []);
  f.node("dialog").handlers.compositionend();
  f.node("value").value = "한글 이름";
  await f.node("apply").handlers.click();
  assert.deepEqual(commits, [[7, "한글 이름"]]);
  assert.deepEqual(cancels, []);
  assert.equal(f.node("dialog").open, false);
  assert.equal(f.node("value").value, "");
});

test("a late open response is cancelled after the game disconnects", async () => {
  let resolve;
  const cancels = [];
  const f = fixture({
    openTextInput: () => new Promise(done => { resolve = done; }),
    cancelTextInput: async id => cancels.push(id),
  });
  const opening = f.controller.open();
  f.controller.close();
  resolve({ textInput: { edit: 9, text: "secret" } });
  await opening;
  assert.deepEqual(cancels, [9]);
  assert.equal(f.node("value").value, "");
  assert.equal(f.node("dialog").open, false);
});

test("failed validation preserves draft and password never uses textarea", async () => {
  const f = fixture({
    openTextInput: async () => ({ textInput: { edit: 1, text: "", password: true, multiline: true } }),
    commitTextInput: async () => { throw new Error("text does not satisfy the active field constraints"); },
    cancelTextInput: async () => {},
  });
  await f.controller.open();
  assert.equal(f.node("value").type, "password");
  assert.equal(f.node("multiline").hidden, true);
  f.node("value").value = "123한";
  await f.node("apply").handlers.click();
  assert.equal(f.node("value").value, "123한");
  assert.equal(f.node("value").disabled, false);
  assert.equal(f.node("dialog").open, true);
  assert.match(f.node("status").textContent, /길이/);
});


test("guest prompt and byte limit are rendered without parsing markup", async () => {
  let opens = 0;
  const f = fixture({
    openTextInput: async () => {
      opens++;
      return { textInput: { edit: 12, text: "값", prompt: "<b>이름</b>", maxBytes: 32 } };
    },
    commitTextInput: async () => {},
    cancelTextInput: async () => {},
  });
  await f.controller.open();
  await f.controller.open();
  assert.equal(opens, 1);
  assert.equal(f.node("label").textContent, "<b>이름</b>");
  assert.match(f.node("status").textContent, /32바이트/);
});

test("button, backdrop, and Escape each cancel the active native dialog", async () => {
  for (const route of ["button", "backdrop", "escape"]) {
    const cancels = [];
    const f = fixture({
      openTextInput: async () => ({ textInput: { edit: 21, text: "draft" } }),
      commitTextInput: async () => {},
      cancelTextInput: async id => cancels.push(id),
    });
    await f.controller.open();
    if (route === "button") f.node("cancel").handlers.click();
    if (route === "backdrop") f.node("dialog").handlers.click({ target: f.node("dialog"), clientX: 50, clientY: 50 });
    if (route === "escape") {
      let prevented = false;
      f.node("dialog").handlers.cancel({ preventDefault() { prevented = true; } });
      assert.equal(prevented, true);
    }
    await Promise.resolve();
    assert.deepEqual(cancels, [21], route);
    assert.equal(f.node("dialog").open, false, route);
  }
});


test("a late first open cannot cancel the replacement edit", async () => {
  const resolves = [], cancels = [], commits = [];
  const f = fixture({
    openTextInput: () => new Promise(resolve => resolves.push(resolve)),
    commitTextInput: async (...args) => commits.push(args),
    cancelTextInput: async id => cancels.push(id),
  });
  const first = f.controller.open();
  f.controller.close();
  const second = f.controller.open();
  resolves[1]({ textInput: { edit: 22, text: "replacement" } });
  await second;
  resolves[0]({ textInput: { edit: 11, text: "stale" } });
  await first;
  await Promise.resolve();
  assert.deepEqual(cancels, [11]);
  assert.equal(f.node("value").value, "replacement");
  f.node("value").value = "kept draft";
  await f.node("apply").handlers.click();
  assert.deepEqual(commits, [[22, "kept draft"]]);
});

test("detaching for park does not complete the guest dialog", async () => {
  const cancels = [];
  const active = fixture({
    openTextInput: async () => ({ textInput: { edit: 31, text: "parked" } }),
    commitTextInput: async () => {},
    cancelTextInput: async id => cancels.push(id),
  });
  await active.controller.open();
  active.controller.detach();
  await Promise.resolve();
  assert.deepEqual(cancels, []);
  assert.equal(active.node("dialog").open, false);

  let resolve;
  const pending = fixture({
    openTextInput: () => new Promise(done => { resolve = done; }),
    cancelTextInput: async id => cancels.push(id),
  });
  const opening = pending.controller.open();
  pending.controller.detach();
  resolve({ textInput: { edit: 32, text: "late parked" } });
  await opening;
  await Promise.resolve();
  assert.deepEqual(cancels, []);
});


test("clicking dialog padding does not cancel guest input", async () => {
  const cancels = [];
  const f = fixture({
    openTextInput: async () => ({ textInput: { edit: 41, text: "draft" } }),
    commitTextInput: async () => {},
    cancelTextInput: async id => cancels.push(id),
  });
  await f.controller.open();
  f.node("dialog").handlers.click({ target: f.node("dialog"), clientX: 110, clientY: 110 });
  await Promise.resolve();
  assert.deepEqual(cancels, []);
  assert.equal(f.node("dialog").open, true);
});
