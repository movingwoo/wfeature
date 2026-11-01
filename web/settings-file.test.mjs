import assert from "node:assert/strict";
import { test } from "node:test";

import {
  SETTINGS_FORMAT,
  applySettings,
  exportSettings,
  initSettingsFile,
  isSetting,
  readSettings,
  settingsFileName,
} from "./settings-file.js";
import { createSafeStorage } from "./storage.js";
import "./text-fixture.mjs";
import { phrase } from "./text.js";

const memory = entries => {
  const map = new Map(entries);
  return createSafeStorage(() => ({
    get length() { return map.size; },
    key: index => [...map.keys()][index] ?? null,
    getItem: key => map.get(key) ?? null,
    setItem: (key, value) => map.set(key, String(value)),
    removeItem: key => map.delete(key),
  }));
};

const SETTINGS = [
  ["wfeature:keypadLayout", "type3"],
  ["wfeature:keypadGridV2", JSON.stringify({ version: 2, layouts: { type3: { groups: [] } } })],
  ["wfeature:keyBindings", "{\"KeyZ\":\"OK\"}"],
  ["wfeature:padBindings", "{\"0\":\"OK\"}"],
  ["wfeature:volume:midi", "70"],
  ["wfeature:volume:pcm", "30"],
  ["wfeature:frameScale", "2"],
  ["wfeature:vibrate", "off"],
  ["wfeature:fullscreen", "on"],
  ["wfeature:speed:games/ktf/a.zip", "1.5"],
  ["wfeature:screen-choice:games/ktf/a.zip", "176x220"],
];
const NOT_SETTINGS = [
  ["wfeature.browser-token", "secret"],
  ["wfeature:lastGame", "games/ktf/a.zip"],
  ["wfeature:keyboardSeen", "1"],
  ["wfeature:padSeen", "1"],
  ["wfeature:keypadGridV2Backup", "{}"],
  ["wfeature:speed:", "2"],
  ["something:else", "x"],
];

// **What travels is the page's settings and nothing else.** The keypad, the
// keys, the controller, the sound, the picture, the screen and speed per game,
// vibration and fullscreen go; the token that takes a parked game back, the
// last game, what this device has seen and a damaged record's copy stay.
test("an export holds every setting and nothing that is not one", () => {
  const { text, count } = exportSettings(memory([...SETTINGS, ...NOT_SETTINGS]), new Date("2026-11-01T12:00:00Z"));
  const file = JSON.parse(text);
  assert.equal(file.format, SETTINGS_FORMAT);
  assert.equal(file.version, 1);
  assert.equal(file.exported, "2026-11-01T12:00:00.000Z");
  assert.deepEqual(Object.keys(file.settings), SETTINGS.map(([key]) => key).sort());
  assert.equal(count, SETTINGS.length);
  for (const [key] of NOT_SETTINGS) assert.equal(isSetting(key), false, key);
});

test("the export is named for the day it was made", () => {
  assert.equal(settingsFileName(new Date(2026, 10, 1)), "wfeature-settings-2026-11-01.json");
});

// **Another browser gets the same settings back.** A setting the file does not
// hold is left alone: a game only this browser played keeps its screen size.
test("an import writes the file's settings over this browser's and keeps the rest", () => {
  const { text } = exportSettings(memory(SETTINGS));
  const target = memory([["wfeature:keypadLayout", "type1"], ["wfeature:speed:games/ktf/b.zip", "2"], ["wfeature.browser-token", "mine"]]);
  const { written, total } = applySettings(readSettings(text), target);
  assert.equal(written, SETTINGS.length);
  assert.equal(total, SETTINGS.length);
  for (const [key, value] of SETTINGS) assert.equal(target.getItem(key), value, key);
  assert.equal(target.getItem("wfeature:speed:games/ktf/b.zip"), "2");
  assert.equal(target.getItem("wfeature.browser-token"), "mine");
});

// The file came from somewhere else: it is bounded, its kind and version are
// checked, and only settings of bounded size are read out of it.
test("an import reads only settings out of a file it recognises", () => {
  const file = settings => JSON.stringify({ format: SETTINGS_FORMAT, version: 1, settings });
  const read = readSettings(file({
    "wfeature:keypadLayout": "type2",
    "wfeature.browser-token": "stolen",
    "wfeature:volume:midi": 5,
    "wfeature:volume:pcm": "x".repeat((1 << 17) + 1),
  }));
  assert.deepEqual([...read], [["wfeature:keypadLayout", "type2"]]);
  assert.throws(() => readSettings("not json"), { message: phrase("settingsfile.not-settings") });
  assert.throws(() => readSettings(JSON.stringify({ format: "other", version: 1, settings: {} })), { message: phrase("settingsfile.not-settings") });
  assert.throws(() => readSettings(JSON.stringify({ format: SETTINGS_FORMAT, version: 2, settings: {} })), { message: phrase("settingsfile.newer") });
  assert.throws(() => readSettings(file({ "wfeature:lastGame": "a" })), { message: phrase("settingsfile.empty") });
  assert.throws(() => readSettings(file([])), { message: phrase("settingsfile.empty") });
  assert.throws(() => readSettings(" ".repeat((1 << 20) + 1)), { message: phrase("settingsfile.too-large") });
});

const pageOf = () => {
  const listeners = new Map();
  const element = id => ({
    id, value: "", files: [], clicked: 0, href: "", download: "",
    addEventListener: (kind, handler) => listeners.set(`${id}:${kind}`, handler),
    click() { this.clicked += 1; },
  });
  const elements = new Map(["settings-export", "settings-import", "settings-import-file"].map(id => [id, element(id)]));
  const created = [];
  return {
    elements, created,
    fire: (id, kind) => listeners.get(`${id}:${kind}`)?.(),
    document: {
      getElementById: id => elements.get(id) ?? null,
      createElement: () => {
        const anchor = element("a");
        created.push(anchor);
        return anchor;
      },
    },
  };
};

const fileOf = (name, text) => ({ name, size: text.length, text: async () => text });

test("the buttons export, and import after asking, then open the page again", async t => {
  t.mock.method(URL, "createObjectURL", () => "blob:settings");
  t.mock.method(URL, "revokeObjectURL", () => {});
  const page = pageOf();
  const storage = memory(SETTINGS);
  const statuses = [];
  let reloads = 0, asked = 0, answer = true;
  initSettingsFile({
    document: page.document, window: {}, storage, onStatus: message => statuses.push(message),
    confirm: async () => { asked++; return answer; }, reload: () => { reloads++; },
    now: () => new Date(2026, 10, 1),
  });

  await page.fire("settings-export", "click");
  assert.equal(page.created.at(-1).download, "wfeature-settings-2026-11-01.json");
  assert.equal(page.created.at(-1).clicked, 1);
  assert.equal(statuses.at(-1), phrase("settingsfile.downloaded"));

  page.fire("settings-import", "click");
  assert.equal(page.elements.get("settings-import-file").clicked, 1);

  const input = page.elements.get("settings-import-file");
  const moved = exportSettings(memory([["wfeature:keypadLayout", "type4"]])).text;
  answer = false;
  input.files = [fileOf("wfeature-settings.json", moved)];
  await page.fire("settings-import-file", "change");
  assert.equal(asked, 1);
  assert.equal(storage.getItem("wfeature:keypadLayout"), "type3", "a no changes nothing");
  assert.equal(reloads, 0);

  answer = true;
  input.files = [fileOf("wfeature-settings.json", moved)];
  await page.fire("settings-import-file", "change");
  assert.equal(storage.getItem("wfeature:keypadLayout"), "type4");
  assert.equal(statuses.at(-1), phrase("settingsfile.imported", { count: 1 }));
  assert.equal(reloads, 1);
});

// **A save backup is not a settings file.** Each import refuses the other's
// file and says where it belongs.
test("a save backup picked here is sent to where it belongs", async () => {
  const page = pageOf();
  const statuses = [];
  initSettingsFile({ document: page.document, window: {}, storage: memory([]), onStatus: message => statuses.push(message), confirm: async () => true, reload: () => {} });
  page.elements.get("settings-import-file").files = [fileOf("게임.wfs", "WFSAVE")];
  await page.fire("settings-import-file", "change");
  assert.equal(statuses.at(-1), phrase("settingsfile.save-backup"));
});

test("an import this browser could not store says so and does not reopen the page", async () => {
  const page = pageOf();
  const statuses = [];
  let reloads = 0;
  const refusing = createSafeStorage(() => ({ length: 0, key: () => null, getItem: () => null, setItem: () => { throw new Error("quota"); }, removeItem: () => {} }));
  initSettingsFile({ document: page.document, window: {}, storage: refusing, onStatus: message => statuses.push(message), confirm: async () => true, reload: () => { reloads++; } });
  page.elements.get("settings-import-file").files = [fileOf("s.json", exportSettings(memory(SETTINGS)).text)];
  await page.fire("settings-import-file", "change");
  assert.equal(statuses.at(-1), phrase("settingsfile.storage"));
  assert.equal(reloads, 0);
});
