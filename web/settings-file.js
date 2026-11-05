// Carrying this page's own settings to another browser, or to this browser at
// another address of the same server.
//
// They live in the browser's storage, and a browser keeps that apart per
// address: `http://…:11541` and `https://…:11641` of one server are two sites
// to it, so a phone that moved from one to the other started over, and a
// keypad arranged by hand is the setting least worth arranging twice. Nothing
// else can reach them — not the other address's page, not the server — so the
// page writes them to a file and reads them back from one.
//
// A game's saves are not among them. They are on the server and travel one
// game at a time as a `.wfs` file (save-backup.js); this is the whole page's
// preferences as one `.json`. Both are under 백업, and each refuses the
// other's file by name.

import { askToConfirm } from "./confirm.js";
import { deliverFile } from "./save-backup.js";
import { local } from "./storage.js";
import { phrase } from "./text.js";

export const SETTINGS_FORMAT = "wfeature-settings";
export const SETTINGS_VERSION = 1;

// The file is read in full and parsed, so it is bounded first. A keypad
// arrangement is the largest value, at most 64 KiB (keypad-layout.js).
const MAX_FILE = 1 << 20;
const MAX_VALUE = 1 << 17;
const MAX_ENTRIES = 4096;
const MAX_KEY = 1024;

// What is a setting. The keypad's type and its arrangement (and the earlier
// records of both, which the page still reads when the current one is
// missing), the keyboard's and the game controller's bindings, the two
// volumes, picture against sound, the picture filter, fullscreen and
// vibration.
const SETTING_KEYS = new Set([
  "wfeature:keypadLayout",
  "wfeature:keypadGridV2",
  "wfeature:keypadGrid",
  "wfeature:keypadKeys",
  "wfeature:keyBindings",
  "wfeature:padBindings",
  "wfeature:padBindingsRaw",
  "wfeature:volume:midi",
  "wfeature:volume:pcm",
  "wfeature:avSync",
  "wfeature:frameScale",
  "wfeature:fullscreen",
  "wfeature:vibrate",
  "wfeature:speed",
]);
// Per game: the screen chosen for it and its speed, under the game's path.
const SETTING_PREFIXES = ["wfeature:screen-choice:", "wfeature:screen:", "wfeature:speed:"];

// Not settings, so they stay where they are: the token that lets this browser
// take a parked game back (two browsers holding one would fight over it), the
// game last picked (another server lists other games), whether a keyboard or a
// controller has been used here (which decides what this device's panel
// shows), and the copy of a damaged keypad record.
export const isSetting = key =>
  typeof key === "string" && key.length <= MAX_KEY &&
  (SETTING_KEYS.has(key) || SETTING_PREFIXES.some(prefix => key.length > prefix.length && key.startsWith(prefix)));

// SETTINGS_FILE_NAME is what the export is called: one short name, with the
// prefix saying whose settings they are in a downloads folder that has other
// programs' settings.json. When it was made is inside the file (`exported`).
export const SETTINGS_FILE_NAME = "wfeature-settings.json";

// exportSettings answers the file's text and how many settings it holds.
export const exportSettings = (storage = local, now = new Date()) => {
  const settings = {};
  for (const key of storage.keys().filter(isSetting).sort()) {
    const value = storage.getItem(key);
    if (typeof value === "string" && value.length <= MAX_VALUE) settings[key] = value;
  }
  const text = JSON.stringify({ format: SETTINGS_FORMAT, version: SETTINGS_VERSION, exported: now.toISOString(), settings }, null, 1);
  return { text, count: Object.keys(settings).length };
};

// readSettings checks a file and answers the settings in it, or throws with
// the sentence to show. It reads only what it would write: a key that is not
// a setting, or a value that is not a string of bounded length, is left out.
export const readSettings = text => {
  if (typeof text !== "string" || text.length > MAX_FILE) throw new Error(phrase("settingsfile.too-large"));
  let file;
  try {
    file = JSON.parse(text);
  } catch {
    throw new Error(phrase("settingsfile.not-settings"));
  }
  if (file === null || typeof file !== "object" || file.format !== SETTINGS_FORMAT || !Number.isInteger(file.version)) {
    throw new Error(phrase("settingsfile.not-settings"));
  }
  if (file.version > SETTINGS_VERSION) throw new Error(phrase("settingsfile.newer"));
  const entries = file.settings !== null && typeof file.settings === "object" && !Array.isArray(file.settings)
    ? Object.entries(file.settings) : [];
  const settings = entries
    .slice(0, MAX_ENTRIES)
    .filter(([key, value]) => isSetting(key) && typeof value === "string" && value.length <= MAX_VALUE);
  if (settings.length === 0) throw new Error(phrase("settingsfile.empty"));
  return new Map(settings);
};

// applySettings writes the settings over this browser's. A setting the file
// does not hold is left as it is: a game this browser has a screen size for
// and the other one never played keeps it.
export const applySettings = (settings, storage = local) => {
  let written = 0;
  for (const [key, value] of settings) {
    if (storage.setItem(key, value) !== false) written++;
  }
  return { written, total: settings.size };
};

// initSettingsFile wires the settings half of 백업. An import
// asks first, because it replaces what this browser has, and then opens the
// page again: every setting is read once, when the page starts.
export const initSettingsFile = ({
  document: doc,
  window: win = globalThis,
  storage = local,
  onStatus,
  confirm: ask = message => askToConfirm({ document: doc, message, confirmLabel: phrase("settingsfile.import-confirm") }),
  reload = () => win.location?.reload(),
  now = () => new Date(),
} = {}) => {
  const exportButton = doc.getElementById("settings-export");
  const importButton = doc.getElementById("settings-import");
  const input = doc.getElementById("settings-import-file");
  if (!exportButton || !importButton || !input) return;

  exportButton.addEventListener("click", async () => {
    try {
      const { text } = exportSettings(storage, now());
      const blob = new Blob([text], { type: "application/json" });
      const way = await deliverFile(blob, SETTINGS_FILE_NAME, doc, win);
      onStatus?.(phrase(way === "picker" ? "settingsfile.choose-place" : "settingsfile.downloaded"));
    } catch (error) {
      onStatus?.(error.message);
    }
  });

  importButton.addEventListener("click", () => input.click());

  input.addEventListener("change", async () => {
    const file = input.files?.[0];
    // The same file picked twice in a row is two imports.
    input.value = "";
    if (!file) return;
    try {
      if (/\.wfs$/i.test(file.name ?? "")) throw new Error(phrase("settingsfile.save-backup"));
      if (file.size > MAX_FILE) throw new Error(phrase("settingsfile.too-large"));
      const settings = readSettings(await file.text());
      if (!(await ask?.(phrase("settingsfile.import-question")))) return;
      const { written, total } = applySettings(settings, storage);
      if (written < total) {
        onStatus?.(phrase("settingsfile.storage"));
        return;
      }
      onStatus?.(phrase("settingsfile.imported", { count: written }));
      win.setTimeout?.(reload, 800) ?? reload();
    } catch (error) {
      onStatus?.(error.message);
    }
  });
};
