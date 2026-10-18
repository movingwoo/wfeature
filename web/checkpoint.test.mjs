import assert from "node:assert/strict";
import { test } from "node:test";
import { initCheckpoints, QUICK_SAVE, QUICK_LOAD } from "./checkpoint.js";
import "./text-fixture.mjs";
import { phrase } from "./text.js";

const setup = () => {
  const handlers = {}, buttons = [], timers = new Map();
  const classes = new Set();
  const note = { hidden: true, textContent: "",
    classList: { toggle(name, on) { if (on) classes.add(name); else classes.delete(name); } } };
  const document = {
    hidden: false,
    getElementById: id => id === "checkpoint-status" ? note : null,
    querySelectorAll: selector => buttons.filter(button => selector.includes(`"${button.dataset.key}"`)),
    addEventListener: (kind, fn) => { handlers[kind] = fn; },
  };
  const addButton = name => {
    const button = { dataset: { key: name }, disabled: false,
      closest: () => button,
      click() { if (!this.disabled) return handlers.click?.({ target: button }); },
    };
    buttons.push(button);
    return button;
  };
  const save = addButton(QUICK_SAVE), load = addButton(QUICK_LOAD);
  let currentState = "ready", arranging = false, nextTimer = 0;
  const errors = [], loaded = [], calls = [];
  const socket = {
    quickSave: async () => { calls.push("save"); },
    quickLoad: async () => { calls.push("load"); return { started: { restored: true } }; },
  };
  const controls = initCheckpoints({ document, session: () => socket,
    state: () => currentState, editing: () => arranging,
    onLoaded: info => loaded.push(info), onError: error => errors.push(error),
    setTimer(fn, ms) { const id = ++nextTimer; timers.set(id, { fn, ms }); return id; },
    clearTimer: id => timers.delete(id),
  });
  return { controls, socket, errors, loaded, calls, save, load, addButton, document, note, classes, timers,
    state(value) { currentState = value; controls.refresh(); },
    edit(value) { arranging = value; controls.refresh(); },
    expire() { for (const { fn } of [...timers.values()]) fn(); },
  };
};

test("keypad actions run immediately and report inline without a modal", async () => {
  const ui = setup();
  assert.equal(ui.save.disabled, true);
  ui.state("playing");
  ui.controls.started({ can_checkpoint: false });
  assert.equal(ui.save.disabled, true);
  ui.controls.started({ can_checkpoint: true, has_checkpoint: false });
  assert.equal(ui.save.disabled, false);
  assert.equal(ui.load.disabled, true);
  const saving = ui.save.click();
  assert.deepEqual(ui.calls, ["save"], "the click must send before any further user action");
  await saving;
  assert.equal(ui.load.disabled, false);
  assert.equal(ui.note.hidden, false);
  assert.equal(ui.note.textContent, phrase("checkpoint.saved"));
  ui.expire();
  assert.equal(ui.note.hidden, true);
  const loading = ui.load.click();
  assert.deepEqual(ui.calls, ["save", "load"]);
  assert.equal(ui.note.textContent, phrase("checkpoint.loading"));
  await loading;
  assert.deepEqual(ui.loaded, [{ restored: true }]);
  // A load says the game is back at the quick save, not that something loaded.
  assert.equal(ui.note.textContent, phrase("checkpoint.loaded"));
  assert.deepEqual([...ui.timers.values()].map(timer => timer.ms), [3000]);
});

test("a refused load shows the server's reason and keeps the slot offered", async () => {
  const ui = setup();
  ui.state("playing");
  ui.controls.started({ can_checkpoint: true, has_checkpoint: true });
  const reason = phrase("checkpoint.refused.legacy");
  ui.socket.quickLoad = async () => { throw new Error(reason); };
  await ui.load.click();
  assert.equal(ui.note.textContent, phrase("checkpoint.load-failed", { reason }));
  assert.ok(ui.classes.has("error"));
  assert.equal(ui.loaded.length, 0);
  // The slot is still there and still refused, so the key stays as it was:
  // pressing it again says why again.
  assert.equal(ui.load.disabled, false);
  assert.equal(ui.save.disabled, false);
});

test("pending operations block every copy, including cells assigned after wiring", async () => {
  const ui = setup();
  ui.state("playing");
  ui.controls.started({ can_checkpoint: true, has_checkpoint: true });
  const secondSave = ui.addButton(QUICK_SAVE);
  const secondLoad = ui.addButton(QUICK_LOAD);
  ui.controls.refresh();
  let reject, requests = 0;
  ui.socket.quickLoad = () => { requests++; return new Promise((_, no) => { reject = no; }); };
  const loading = secondLoad.click();
  for (const button of [ui.save, ui.load, secondSave, secondLoad]) {
    assert.equal(button.disabled, true);
    await button.click();
  }
  assert.equal(requests, 1);
  reject(new Error("checksum differs"));
  await loading;
  assert.equal(ui.loaded.length, 0);
  assert.equal(ui.errors[0].message, "checksum differs");
  assert.match(ui.note.textContent, /checksum differs/);
  assert.ok(ui.classes.has("error"));
  for (const button of [ui.save, ui.load, secondSave, secondLoad]) assert.equal(button.disabled, false);
});

test("a refused save does not enable an absent slot and can be retried immediately", async () => {
  const ui = setup();
  ui.state("playing");
  ui.controls.started({ can_checkpoint: true, has_checkpoint: false });
  ui.socket.quickSave = async () => { throw new Error("unsupported continuation"); };
  await ui.save.click();
  assert.equal(ui.load.disabled, true);
  assert.equal(ui.errors.length, 1);
  assert.match(ui.note.textContent, /unsupported continuation/);
  let finish;
  ui.socket.quickSave = () => new Promise(resolve => { finish = resolve; });
  const saving = ui.save.click();
  assert.equal(ui.timers.size, 0, "old feedback must not dismiss a pending operation");
  assert.equal(ui.classes.has("error"), false);
  finish();
  await saving;
  assert.equal(ui.load.disabled, false);
});

test("unavailable cells remain editable without invoking checkpoint actions", async () => {
  const ui = setup();
  ui.edit(true);
  for (const button of [ui.save, ui.load]) {
    assert.equal(button.disabled, false);
    await button.click();
  }
  assert.deepEqual(ui.calls, []);
  ui.state("playing");
  ui.controls.started({ can_checkpoint: true, has_checkpoint: true });
  await ui.save.click();
  await ui.load.click();
  assert.deepEqual(ui.calls, []);
  ui.edit(false);
  await ui.save.click();
  assert.deepEqual(ui.calls, ["save"]);
  ui.document.hidden = true;
  await ui.load.click();
  assert.deepEqual(ui.calls, ["save"]);
  ui.document.hidden = false;
  ui.state("offline");
  assert.equal(ui.save.disabled, true);
  assert.equal(ui.load.disabled, true);
  assert.equal(ui.note.hidden, true);
});

test("a control that is not a cell runs the same action under the same rules", async () => {
  const f = setup();
  f.state("playing");
  f.controls.started({ can_checkpoint: true, has_checkpoint: false });
  await f.controls.run(QUICK_LOAD);
  assert.deepEqual(f.calls, [], "no slot, no load — as for the cell");
  await f.controls.run(QUICK_SAVE);
  await f.controls.run(QUICK_LOAD);
  assert.deepEqual(f.calls, ["save", "load"]);
  f.edit(true);
  await f.controls.run(QUICK_SAVE);
  assert.deepEqual(f.calls, ["save", "load"], "editing the pad blocks it too");
});

// A dropped socket rejects the pending quick save and leaves play in the same
// turn, before the failure is written. The note has to outlive that, or the
// player sees the saving note vanish and may take it for a save that happened.
test("a quick save the dropped connection broke still says it failed", async () => {
  const ui = setup();
  ui.state("playing");
  ui.controls.started({ can_checkpoint: true, has_checkpoint: false });
  ui.socket.quickSave = async () => {
    ui.state("offline");
    throw new Error(phrase("session.disconnected"));
  };
  await ui.save.click();
  assert.equal(ui.note.hidden, false);
  assert.equal(ui.note.textContent, phrase("checkpoint.save-failed", { reason: phrase("session.disconnected") }));
  assert.ok(ui.classes.has("error"));
  assert.deepEqual([...ui.timers.values()].map(timer => timer.ms), [8000]);
  // Reconnecting to the same game does not take it away before it is read.
  ui.state("connecting");
  ui.state("playing");
  assert.equal(ui.note.hidden, false);
  // The picker covers the screen, and the note goes with the game.
  ui.state("ready");
  assert.equal(ui.note.hidden, true);
});
