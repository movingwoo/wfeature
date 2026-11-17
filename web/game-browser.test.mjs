import assert from "node:assert/strict";
import { test } from "node:test";

import { folderOf, initGameBrowser, pickerFolders } from "./game-browser.js";

// Just enough of a node for the browser: children, text, attributes, clicks.
const element = tagName => {
  const listeners = {};
  const node = {
    tagName,
    children: [],
    attributes: {},
    textContent: "",
    scrollTop: 0,
    offsetTop: 400,
    offsetHeight: 40,
    clientHeight: 200,
    appendChild(child) { this.children.push(child); return child; },
    append(...parts) {
      for (const part of parts) {
        if (typeof part === "string") this.textContent += part;
        else { this.children.push(part); this.textContent += part.textContent; }
      }
    },
    replaceChildren(...children) { this.children = children; },
    setAttribute(name, value) { this.attributes[name] = String(value); },
    getAttribute(name) { return this.attributes[name] ?? null; },
    addEventListener(event, handler) { (listeners[event] ??= []).push(handler); },
    dispatchEvent(event) { for (const handler of listeners[event.type] ?? []) handler(event); },
    click() { this.dispatchEvent({ type: "click", target: this }); },
  };
  return node;
};

const option = (value, name) => Object.assign(element("OPTION"), { value, textContent: name });
const group = (label, ...options) => Object.assign(element("OPTGROUP"), { label, children: options });

// The picker the way listGames builds it: a group per folder.
const page = ({ disabled = false, value = "games/lgt/b.zip", groups } = {}) => {
  const select = element("SELECT");
  select.children = groups ?? [
    group("KTF", option("games/ktf/a.zip", "가")),
    group("LGT", option("games/lgt/b.zip", "나"), option("games/lgt/c.zip", "다")),
  ];
  Object.defineProperty(select, "options", {
    get: () => select.children.flatMap(child => (child.tagName === "OPTION" ? [child] : child.children)),
  });
  select.value = value;
  select.disabled = disabled;
  const changes = [];
  select.addEventListener("change", () => changes.push(select.value));

  const dialog = element("DIALOG");
  dialog.open = false;
  dialog.showModal = () => { dialog.open = true; };
  dialog.close = () => { dialog.open = false; };

  const elements = {
    "game-select": select,
    "game-browse": element("BUTTON"),
    "game-browser": dialog,
    "game-browser-folders": element("UL"),
    "game-browser-games": element("UL"),
    "game-browser-close": element("BUTTON"),
  };
  const document = {
    getElementById: id => elements[id] ?? null,
    createElement: name => element(name.toUpperCase()),
  };
  initGameBrowser({ document });
  const buttons = id => elements[id].children.map(item => item.children[0]);
  return {
    select,
    dialog,
    changes,
    open: () => elements["game-browse"].click(),
    lists: { folders: elements["game-browser-folders"], games: elements["game-browser-games"] },
    folders: () => buttons("game-browser-folders"),
    games: () => buttons("game-browser-games"),
    closeButton: elements["game-browser-close"],
  };
};

test("the folders are the picker's groups in its order, without messages or empty groups", () => {
  const select = element("SELECT");
  select.children = [
    group("KTF", option("games/ktf/a.zip", "가"), option("", "안내")),
    group("빈 폴더"),
    group("LGT", option("games/lgt/b.zip", "나")),
    option("", "등록된 게임이 없습니다"),
  ];
  assert.deepEqual(pickerFolders(select), [
    { label: "KTF", games: [{ value: "games/ktf/a.zip", name: "가" }] },
    { label: "LGT", games: [{ value: "games/lgt/b.zip", name: "나" }] },
  ]);
  assert.equal(folderOf(pickerFolders(select), "games/lgt/b.zip"), 1);
  assert.equal(folderOf(pickerFolders(select), "games/none.zip"), 0);
});

test("the window opens on the folder of the chosen game with that game marked", () => {
  const { open, dialog, folders, games, lists } = page();
  open();
  assert.equal(dialog.open, true);
  assert.deepEqual(folders().map(button => button.textContent), ["KTF1", "LGT2"]);
  assert.equal(folders()[1].getAttribute("aria-current"), "true");
  assert.equal(folders()[0].getAttribute("aria-current"), null);
  assert.deepEqual(games().map(button => button.textContent), ["나", "다"]);
  assert.equal(games()[0].getAttribute("aria-current"), "true");
  // Both lists put what is chosen in their middle: 400 - (200 - 40) / 2.
  assert.equal(lists.games.scrollTop, 320);
  assert.equal(lists.folders.scrollTop, 320);
});

test("a folder shows its own games", () => {
  const { open, folders, games } = page();
  open();
  folders()[0].click();
  assert.deepEqual(games().map(button => button.textContent), ["가"]);
  assert.equal(folders()[0].getAttribute("aria-current"), "true");
});

test("a game sets the picker, fires its change once and closes", () => {
  const { open, folders, games, select, dialog, changes } = page();
  open();
  folders()[0].click();
  games()[0].click();
  assert.equal(select.value, "games/ktf/a.zip");
  assert.deepEqual(changes, ["games/ktf/a.zip"]);
  assert.equal(dialog.open, false);
});

test("the game already chosen closes the window and changes nothing", () => {
  const { open, games, changes, dialog } = page();
  open();
  games()[0].click();
  assert.deepEqual(changes, []);
  assert.equal(dialog.open, false);
});

test("a game the rebuilt picker no longer offers is not set", () => {
  const { open, folders, games, select, changes } = page();
  open();
  folders()[0].click();
  const gone = games()[0];
  select.children = [group("LGT", option("games/lgt/b.zip", "나"))];
  gone.click();
  assert.equal(select.value, "games/lgt/b.zip");
  assert.deepEqual(changes, []);
});

test("a disabled or empty picker opens nothing", () => {
  const disabled = page({ disabled: true });
  disabled.open();
  assert.equal(disabled.dialog.open, false);

  const empty = page({ value: "", groups: [option("", "등록된 게임이 없습니다")] });
  empty.open();
  assert.equal(empty.dialog.open, false);
});

test("the close button and the dimmed page close it; a click inside does not", () => {
  const { open, dialog, closeButton, games } = page();
  open();
  closeButton.click();
  assert.equal(dialog.open, false);

  open();
  dialog.dispatchEvent({ type: "click", target: games()[1] });
  assert.equal(dialog.open, true);
  dialog.click();
  assert.equal(dialog.open, false);
});
