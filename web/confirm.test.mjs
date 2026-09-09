import assert from "node:assert/strict";
import { test } from "node:test";

import { askToConfirm } from "./confirm.js";

// A stub of exactly the four nodes the dialog needs, plus the backdrop and the
// document's own key handling.
const page = () => {
  const listeners = { accept: {}, cancel: {}, document: {}, backdrop: {} };
  const node = name => ({
    textContent: "",
    focused: false,
    focus() { this.focused = true; },
    addEventListener: (event, handler) => { listeners[name][event] = handler; },
    removeEventListener: event => { delete listeners[name][event]; },
  });
  const accept = node("accept");
  const cancel = node("cancel");
  const backdrop = node("backdrop");
  const classes = new Set();
  const dialog = {
    classList: { add: name => classes.add(name), remove: name => classes.delete(name) },
  };
  const text = { textContent: "" };
  const elements = {
    "confirm-dialog": dialog,
    "confirm-text": text,
    "confirm-accept": accept,
    "confirm-cancel": cancel,
  };
  return {
    listeners,
    classes,
    accept,
    cancel,
    text,
    document: {
      getElementById: id => elements[id] ?? null,
      querySelector: () => backdrop,
      addEventListener: (event, handler) => { listeners.document[event] = handler; },
      removeEventListener: event => { delete listeners.document[event]; },
    },
  };
};

test("the question is shown and the verb goes on the button", async () => {
  const { document, classes, text, accept, cancel, listeners } = page();
  const answer = askToConfirm({ document, message: "게임.zip 을(를) 지울까요?", confirmLabel: "삭제" });
  assert.equal(text.textContent, "게임.zip 을(를) 지울까요?");
  assert.equal(accept.textContent, "삭제");
  assert.ok(classes.has("visible"));
  // The safe button holds the focus, so a stray Enter is a "no".
  assert.ok(cancel.focused);

  listeners.accept.click();
  assert.equal(await answer, true);
  assert.equal(classes.has("visible"), false);
});

test("cancel is no, and closes", async () => {
  const { document, classes, listeners } = page();
  const answer = askToConfirm({ document, message: "지울까요?" });
  listeners.cancel.click();
  assert.equal(await answer, false);
  assert.equal(classes.has("visible"), false);
});

test("escape and the backdrop are no", async () => {
  for (const dismiss of ["escape", "backdrop"]) {
    const { document, listeners } = page();
    const answer = askToConfirm({ document, message: "지울까요?" });
    if (dismiss === "escape") {
      listeners.document.keydown({ key: "Escape", stopPropagation: () => {} });
    } else {
      listeners.backdrop.click();
    }
    assert.equal(await answer, false, dismiss);
  }
});

// The keypad's own handlers are bound to the document, so a key that is not
// the dialog's is left alone.
test("another key is not an answer", async () => {
  const { document, listeners } = page();
  const answer = askToConfirm({ document, message: "지울까요?" });
  listeners.document.keydown({ key: "Enter", stopPropagation: () => {} });
  let settled = false;
  void answer.then(() => { settled = true; });
  await Promise.resolve();
  assert.equal(settled, false);
  listeners.cancel.click();
  assert.equal(await answer, false);
});

// A caller that cannot ask has not been answered. The destructive default is
// the one thing this must not have — which is also why window.confirm is not
// what asks in the app's WebView.
test("a page with no dialog in it answers no", async () => {
  assert.equal(await askToConfirm({ document: { getElementById: () => null } }), false);
  assert.equal(await askToConfirm(), false);
});

// Nothing is left bound to the document once the question is answered.
test("the listeners go when the dialog does", async () => {
  const { document, listeners } = page();
  const answer = askToConfirm({ document, message: "지울까요?" });
  assert.ok(listeners.document.keydown);
  listeners.accept.click();
  await answer;
  assert.equal(listeners.document.keydown, undefined);
  assert.equal(listeners.accept.click, undefined);
  assert.equal(listeners.cancel.click, undefined);
  assert.equal(listeners.backdrop.click, undefined);
});
