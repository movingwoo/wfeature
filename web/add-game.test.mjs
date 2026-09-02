import assert from "node:assert/strict";
import { test } from "node:test";

import { describe, initAddGame, uploadGame, uploadGames } from "./add-game.js";

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
  await uploadGame(fileOf("영웅서기2.zip"), fetcher);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "api/games?name=%EC%98%81%EC%9B%85%EC%84%9C%EA%B8%B02.zip");
  assert.equal(calls[0].options.method, "POST");
  assert.equal(calls[0].options.body.name, "영웅서기2.zip");
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
