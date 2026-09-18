import assert from "node:assert/strict";
import { test } from "node:test";
import { createTextInputDialog } from "./text-input.js";

const fixture = connection => {
  const nodes = new Map();
  for (const name of ["dialog", "status", "value", "multiline", "apply", "cancel"]) {
    const handlers = {};
    nodes.set(`text-input-${name}`, {
      value: "", open: false, handlers, dataset: {},
      addEventListener(name, handler) { handlers[name] = handler; },
      showModal() { this.open = true; }, close() { this.open = false; }, focus() {},
    });
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
  assert.equal(f.node("status").dataset.state, "available");
  assert.equal(f.node("status").hidden, false);
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
  assert.equal(f.node("status").hidden, false);
  assert.equal(f.node("status").textContent, "입력 불가능");
});

test("append-only guest input keeps the insertion action and compact status", async () => {
  const f = fixture({
    openTextInput: async () => ({ textInput: { edit: 3, text: "", append: true, inputMode: "text" } }),
    commitTextInput: async () => {}, cancelTextInput: async () => {},
  });
  await f.controller.open();
  assert.equal(f.node("status").dataset.state, "available");
  assert.equal(f.node("status").hidden, false);
  assert.equal(f.node("apply").textContent, "삽입");
  assert.equal(f.node("status").textContent, "입력 가능");
});


test("opening distinguishes checking from an unavailable target", async () => {
  let reject;
  const f = fixture({ openTextInput: () => new Promise((_, fail) => { reject = fail; }) });
  const opening = f.controller.open();
  assert.equal(f.node("status").dataset.state, "checking");
  assert.equal(f.node("status").hidden, true);
  assert.equal(f.node("status").textContent, "");
  assert.equal(f.node("value").hidden, true);
  assert.equal(f.node("apply").disabled, true);
  reject(new Error("no supported text field is active"));
  await opening;
  assert.equal(f.node("status").dataset.state, "unavailable");
  assert.equal(f.node("status").textContent, "입력 불가능");
  assert.equal(f.node("apply").disabled, true);
  assert.equal(f.node("cancel").textContent, "닫기");
});

test("a stale target keeps the draft readable but prevents another submission", async () => {
  const f = fixture({
    openTextInput: async () => ({ textInput: { edit: 4, text: "" } }),
    commitTextInput: async () => { throw new Error("the active text field changed; open text input again"); },
    cancelTextInput: async () => {},
  });
  await f.controller.open();
  f.node("value").value = "draft";
  await f.node("apply").handlers.click();
  assert.equal(f.node("status").dataset.state, "unavailable");
  assert.equal(f.node("value").value, "draft");
  assert.equal(f.node("value").readOnly, true);
  assert.equal(f.node("apply").disabled, true);
});
