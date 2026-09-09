import assert from "node:assert/strict";
import { test } from "node:test";

import {
  describe,
  gameFileName,
  groupLabel,
  initAddGame,
  initRemoveGame,
  removeGame,
  syncRemoveButton,
  uploadGame,
  uploadGames,
} from "./add-game.js";

const fileOf = name => ({ name, size: 4 });

const recorder = (answer = { ok: true }) => {
  const calls = [];
  const fetcher = async (url, options) => {
    calls.push({ url, options });
    return typeof answer === "function" ? answer(url, options) : answer;
  };
  return { calls, fetcher };
};

// The names here are Korean, which is why the name travels in the query rather
// than a header: a header value is Latin-1 and would have to be escaped by
// hand on both sides.
test("the archive goes up with its name percent-encoded", async () => {
  const { calls, fetcher } = recorder();
  await uploadGame(fileOf("한글이름.zip"), fetcher);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "api/games?name=%ED%95%9C%EA%B8%80%EC%9D%B4%EB%A6%84.zip");
  assert.equal(calls[0].options.method, "POST");
  assert.equal(calls[0].options.body.name, "한글이름.zip");
});

// The server's refusals are written for this screen — "zip 또는 jar 파일을
// 골라주세요" is more use than "400" — so they are shown rather than replaced.
test("what the server says about a refusal is what the user reads", async () => {
  const { fetcher } = recorder({
    ok: false,
    status: 400,
    text: async () => ".txt 파일은 게임이 아닙니다. zip 또는 jar 파일을 골라주세요.",
  });
  await assert.rejects(
    () => uploadGame(fileOf("notes.txt"), fetcher),
    /zip 또는 jar 파일을 골라주세요/,
  );
});

test("a refusal with nothing to say still says something", async () => {
  const { fetcher } = recorder({ ok: false, status: 500, text: async () => "" });
  await assert.rejects(() => uploadGame(fileOf("game.zip"), fetcher), /500/);
});

// Picking five archives and losing four of them to one bad file is the failure
// worth designing against.
test("one bad file does not take the others down with it", async () => {
  const { fetcher } = recorder((url) => url.includes("bad")
    ? { ok: false, status: 400, text: async () => "게임이 아닙니다." }
    : { ok: true });
  const result = await uploadGames([fileOf("one.zip"), fileOf("bad.zip"), fileOf("two.zip")], fetcher);
  assert.deepEqual(result.added, ["one.zip", "two.zip"]);
  assert.equal(result.failed.length, 1);
  assert.equal(result.failed[0].name, "bad.zip");
});

test("what the user is told afterwards", () => {
  assert.match(describe({ added: ["영웅서기2.zip"], failed: [] }), /영웅서기2\.zip/);
  assert.match(describe({ added: ["a.zip", "b.zip", "c.zip"], failed: [] }), /3개/);
  // A single failure is its own reason rather than a count: there is room for
  // it, and the reason is the useful half.
  assert.equal(describe({ added: [], failed: [{ name: "x.txt", reason: "게임이 아닙니다." }] }), "게임이 아닙니다.");
  assert.match(describe({ added: ["a.zip"], failed: [{ name: "b", reason: "안 됨" }] }), /a\.zip.*b: 안 됨/);
});

// The picker has to refill itself: a game that was added and does not appear
// reads as a failure.
test("adding a game refills the list and says what happened", async () => {
  const listeners = {};
  const elements = {
    "game-file": {
      files: [fileOf("game.zip")],
      value: "picked",
      addEventListener: (name, handler) => { listeners[name] = handler; },
      click: () => {},
    },
    "game-add": {
      textContent: "＋ 게임 추가",
      disabled: false,
      addEventListener: (name, handler) => { listeners[`button:${name}`] = handler; },
    },
  };
  const document = { getElementById: id => elements[id] ?? null };

  let refills = 0;
  const said = [];
  const { fetcher } = recorder();
  initAddGame({
    document,
    fetcher,
    onAdded: () => { refills++; },
    onStatus: message => said.push(message),
  });

  await listeners.change();
  assert.equal(refills, 1);
  assert.match(said[0], /game\.zip/);
  // The input is cleared so that picking the same file twice in a row is two
  // changes rather than one — re-adding a corrected archive is a thing people
  // do.
  assert.equal(elements["game-file"].value, "");
  assert.equal(elements["game-add"].disabled, false);
  assert.equal(elements["game-add"].textContent, "＋ 게임 추가");
});

test("picking nothing does nothing", async () => {
  const listeners = {};
  const elements = {
    "game-file": { files: [], value: "", addEventListener: (n, h) => { listeners[n] = h; }, click: () => {} },
    "game-add": { textContent: "＋ 게임 추가", disabled: false, addEventListener: () => {} },
  };
  let refills = 0;
  initAddGame({ document: { getElementById: id => elements[id] ?? null }, onAdded: () => { refills++; } });
  await listeners.change();
  assert.equal(refills, 0);
});

// Deleting is the same route in the other direction, and the path is the
// picker's own: one spelling of "which game" across the API.
test("the removal names the game the way the listing did", async () => {
  const { calls, fetcher } = recorder();
  await removeGame("ext/한글이름.zip", fetcher);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "api/games?game=ext%2F%ED%95%9C%EA%B8%80%EC%9D%B4%EB%A6%84.zip");
  assert.equal(calls[0].options.method, "DELETE");
});

test("what the server says about a refused removal is what the user reads", async () => {
  const { fetcher } = recorder({
    ok: false,
    status: 403,
    text: async () => "이 게임은 서버 폴더에 직접 넣은 것이라 여기서 지울 수 없습니다.",
  });
  await assert.rejects(() => removeGame("games/ktf/game.zip", fetcher), /직접 넣은 것이라/);
});

// The name in the question is the one the person picked, not the path this
// page uses, and `games.json` hands that path out percent-encoded.
test("the file name comes back out of the path", () => {
  assert.equal(gameFileName("ext/%ED%95%9C%EA%B8%80%EC%9D%B4%EB%A6%84.zip"), "한글이름.zip");
  assert.equal(gameFileName("games/ktf/game.zip"), "game.zip");
  assert.equal(gameFileName("ext/100%.zip"), "100%.zip");
  assert.equal(gameFileName(""), "");
});

// The delete button's own test rig: one button and the picker beside it, since
// what the button may do is the picker's answer rather than the button's.
const removalPage = ({ added = true } = {}) => {
  const listeners = {};
  const button = {
    textContent: "게임 삭제",
    disabled: false,
    addEventListener: (name, handler) => { listeners[name] = handler; },
  };
  const select = { selectedOptions: [{ dataset: added ? { added: "yes" } : {} }] };
  // What the picker does after a removal: rebuild, and land on whatever is
  // first now.
  const selectInstead = game => { select.selectedOptions = [{ dataset: game }]; };
  return {
    button,
    select,
    selectInstead,
    listeners,
    document: {
      getElementById: id => (id === "game-remove" ? button : id === "game-select" ? select : null),
    },
  };
};

// A deletion is the one thing on this screen that cannot be undone from it.
test("nothing is deleted until the question is answered", async () => {
  const { document, listeners, button } = removalPage();
  const { calls, fetcher } = recorder();
  const asked = [];
  let refills = 0;
  initRemoveGame({
    document,
    fetcher,
    chosenGame: () => "ext/%ED%95%9C%EA%B8%80%EC%9D%B4%EB%A6%84.zip",
    confirmRemoval: message => { asked.push(message); return false; },
    onRemoved: () => { refills++; },
  });

  await listeners.click();
  assert.equal(calls.length, 0);
  assert.equal(refills, 0);
  // The question names the file, because the picker is a list and the wrong
  // row is the whole failure — and it says the saves stay, because they do.
  assert.match(asked[0], /한글이름\.zip/);
  assert.match(asked[0], /세이브는 남습니다/);
  assert.equal(button.disabled, false);
});

test("a confirmed removal refills the list and says what happened", async () => {
  const { document, listeners, button } = removalPage();
  const { calls, fetcher } = recorder();
  const said = [];
  let refills = 0;
  initRemoveGame({
    document,
    fetcher,
    chosenGame: () => "ext/game.zip",
    confirmRemoval: () => true,
    onRemoved: () => { refills++; },
    onStatus: message => said.push(message),
  });

  await listeners.click();
  assert.equal(calls.length, 1);
  assert.equal(refills, 1);
  assert.match(said[0], /game\.zip/);
  assert.equal(button.textContent, "게임 삭제");
});

// A refusal leaves the button pressable: the list it was working from is what
// was wrong, and the reload behind the message is what fixes it.
test("a removal that fails says so and gives the button back", async () => {
  const { document, listeners, button } = removalPage();
  // The picker did not move, so the game the button was over is still there.
  const { fetcher } = recorder({ ok: false, status: 404, text: async () => "그 게임이 이미 없습니다." });
  const said = [];
  initRemoveGame({
    document,
    fetcher,
    chosenGame: () => "ext/game.zip",
    confirmRemoval: () => true,
    onStatus: message => said.push(message),
  });

  await listeners.click();
  assert.match(said[0], /이미 없습니다/);
  assert.equal(button.disabled, false);
});

// With nothing chosen there is nothing to ask about.
test("no selection asks nothing and sends nothing", async () => {
  const { document, listeners } = removalPage();
  const { calls, fetcher } = recorder();
  let asked = 0;
  initRemoveGame({
    document,
    fetcher,
    chosenGame: () => "",
    confirmRemoval: () => { asked++; return true; },
  });

  await listeners.click();
  assert.equal(asked, 0);
  assert.equal(calls.length, 0);
});

// The two roots are one list to a player, so the heading is the only thing
// that says which games this page can delete.
test("the picker heads each game by where it came from", () => {
  assert.equal(groupLabel({ group: "ktf", name: "x" }), "KTF");
  assert.equal(groupLabel({ group: "", name: "x" }), "기타");
  assert.equal(groupLabel({ group: "", name: "x", added: true }), "추가한 게임");
});

// The button follows the selection, and a game put beside the server by hand
// is not the page's to delete — the server refuses it either way, and this is
// what keeps a person from finding that out by pressing.
test("the delete button belongs to whatever the picker is showing", () => {
  const button = { disabled: false };
  const select = { selectedOptions: [{ dataset: { added: "yes" } }] };
  const document = {
    getElementById: id => (id === "game-remove" ? button : id === "game-select" ? select : null),
  };

  syncRemoveButton(document);
  assert.equal(button.disabled, false);

  select.selectedOptions = [{ dataset: {} }];
  syncRemoveButton(document);
  assert.equal(button.disabled, true);

  // An empty list, and a list that could not be loaded at all, leave nothing
  // selected — and nothing selected is nothing to delete.
  select.selectedOptions = [];
  syncRemoveButton(document);
  assert.equal(button.disabled, true);
});

// Removing the last added game leaves a game the page may not delete selected,
// and the button was handed back enabled over it — lit, and asking a question
// about a file it could never remove. The state belongs to the rebuilt list.
test("the button goes back to whatever the picker landed on", async () => {
  const { document, listeners, button, selectInstead } = removalPage();
  const { fetcher } = recorder();
  initRemoveGame({
    document,
    fetcher,
    chosenGame: () => "ext/game.zip",
    confirmRemoval: () => true,
    // What initGameSelect does: reload, and land on a library game because the
    // added one that was showing is gone.
    onRemoved: () => selectInstead({}),
  });

  await listeners.click();
  assert.equal(button.disabled, true);
  assert.equal(button.textContent, "게임 삭제");
});

