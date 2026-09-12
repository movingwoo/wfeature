import assert from "node:assert/strict";
import { test } from "node:test";
import { createTextInputDialog } from "./text-input.js";

const fixture = connection => {
  const nodes = new Map();
  for (const name of ["dialog", "status", "value", "multiline", "apply", "cancel"]) {
    const handlers = {};
    nodes.set(`text-input-${name}`, {
      value: "", open: false, handlers,
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
