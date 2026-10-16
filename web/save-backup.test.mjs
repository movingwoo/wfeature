import assert from "node:assert/strict";
import { test } from "node:test";

import {
  SAVE_BACKUP_EXTENSION,
  backupFileName,
  describeImport,
  exportSave,
  importSave,
  initSaveBackup,
} from "./save-backup.js";

const GAME = "games/skt/%EA%B2%8C%EC%9E%84.jar";

const ok = body => ({
  ok: true,
  status: 200,
  blob: async () => body,
  json: async () => body,
  text: async () => JSON.stringify(body),
});

const refused = (status, text) => ({
  ok: false,
  status,
  text: async () => text,
});

test("the downloaded file is named after the game, decoded", () => {
  assert.equal(backupFileName(GAME), `게임${SAVE_BACKUP_EXTENSION}`);
  assert.equal(backupFileName("games/ktf/a.zip"), `a${SAVE_BACKUP_EXTENSION}`);
  // A path that is not valid percent-encoding still has to produce a name:
  // it came from this server's own listing either way.
  assert.equal(backupFileName("games/ktf/100%.zip"), `100%${SAVE_BACKUP_EXTENSION}`);
  assert.equal(backupFileName(""), `save${SAVE_BACKUP_EXTENSION}`);
});

test("the game travels in the query, percent-encoded once", async () => {
  let seen = "";
  await exportSave(GAME, async url => {
    seen = url;
    return ok("bytes");
  });
  assert.equal(seen, `api/savepack?game=${encodeURIComponent(GAME)}`);
});

// The server's two refusals are written for this screen and are what a person
// acts on: one means find the right file, the other means the file is no
// longer usable. Rewording them here would flatten them back into one.
test("the server's own words are what the person reads", async () => {
  for (const sentence of [
    "이 백업은 다른 게임의 것입니다. 백업 1a2b3c4d, 지금 게임 5e6f7a8b.",
    "백업 파일이 손상되었습니다. 옮기는 중에 파일이 깨진 것이니 다시 내보내거나 다시 옮겨 오세요.",
    "이 게임이 실행 중입니다(다른 창). 그 창에서 게임을 멈춘 뒤 다시 가져오세요.",
  ]) {
    await assert.rejects(
      () => importSave(GAME, new Uint8Array(), async () => refused(409, sentence)),
      { message: sentence },
    );
  }
});

test("a refusal with nothing to say still says something", async () => {
  await assert.rejects(
    () => exportSave(GAME, async () => refused(500, "   ")),
    { message: "세이브를 내보내지 못했습니다 (500)" },
  );
});

test("what a restore did is said, not hidden behind a tick", () => {
  assert.match(describeImport({ written: 3, removed: 0 }), /세이브 3개를 복원/);
  assert.doesNotMatch(describeImport({ written: 3, removed: 0 }), /지웠습니다/);
  assert.match(describeImport({ written: 3, removed: 4 }), /4개는 지웠습니다/);
  assert.match(describeImport(), /세이브 0개/);
});

// A minimal stand-in for the three elements the page has, so the wiring below
// is tested against what it actually calls.
const pageOf = () => {
  const listeners = new Map();
  const element = id => ({
    id,
    textContent: id,
    disabled: false,
    value: "",
    files: [],
    clicked: 0,
    href: "",
    download: "",
    addEventListener: (kind, handler) => listeners.set(`${id}:${kind}`, handler),
    click() {
      this.clicked += 1;
    },
  });
  const elements = new Map(
    ["save-export", "save-import", "save-import-file"].map(id => [id, element(id)]),
  );
  const created = [];
  return {
    elements,
    created,
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

test("a page missing the controls is wired without throwing", () => {
  initSaveBackup({ document: { getElementById: () => null } });
});

test("nothing is sent until a game is chosen", async () => {
  const page = pageOf();
  const messages = [];
  let calls = 0;
  initSaveBackup({
    document: page.document,
    fetcher: async () => {
      calls += 1;
      return ok("x");
    },
    chosenGame: () => "",
    onStatus: message => messages.push(message),
  });
  await page.fire("save-export", "click");
  await page.fire("save-import", "click");
  assert.equal(calls, 0);
  assert.equal(page.elements.get("save-import-file").clicked, 0);
  assert.deepEqual(messages, ["먼저 게임을 고르세요.", "먼저 게임을 고르세요."]);
});

test("an import asks before it replaces what is there", async () => {
  const page = pageOf();
  let posts = 0;
  let asked = 0;
  initSaveBackup({
    document: page.document,
    fetcher: async () => {
      posts += 1;
      return ok({ written: 1, removed: 0 });
    },
    chosenGame: () => GAME,
    onStatus: () => {},
    confirm: () => {
      asked += 1;
      return false;
    },
  });
  page.elements.get("save-import-file").files = [new Uint8Array([1])];
  await page.fire("save-import-file", "change");
  assert.equal(asked, 1);
  assert.equal(posts, 0, "a refused confirmation must not send anything");
});

test("the file input is cleared so the same file can be picked twice", async () => {
  const page = pageOf();
  const input = page.elements.get("save-import-file");
  initSaveBackup({
    document: page.document,
    fetcher: async () => ok({ written: 1, removed: 0 }),
    chosenGame: () => GAME,
    onStatus: () => {},
    confirm: () => true,
  });
  input.files = [new Uint8Array([1])];
  input.value = "C:\\fakepath\\a.wfs";
  await page.fire("save-import-file", "change");
  assert.equal(input.value, "");
});

// A web view drops `<a download>` and a blob: URL without native help, and
// says nothing — so the button worked in every desktop browser and did
// nothing on the two platforms this feature is most for. These three say
// which road each host takes.
const settle = () => new Promise(resolve => setTimeout(resolve, 0));
const BYTES = new Uint8Array([0, 1, 2, 250, 255]);
const BLOB = { arrayBuffer: async () => BYTES.buffer };

test("an Android web view is handed the bytes rather than a download", async () => {
  const page = pageOf();
  const handed = [];
  const messages = [];
  initSaveBackup({
    document: page.document,
    window: { wfeatureExport: { save: (name, data) => handed.push([name, data]) } },
    fetcher: async () => ok(BLOB),
    chosenGame: () => GAME,
    onStatus: message => messages.push(message),
  });
  await page.fire("save-export", "click");
  await settle();
  assert.deepEqual(handed, [[`게임${SAVE_BACKUP_EXTENSION}`, "AAEC+v8="]]);
  assert.equal(page.created.length, 0, "no anchor: the web view would drop it");
  assert.deepEqual(messages, ["저장할 위치를 고르세요."]);
});

test("an iOS web view is handed the same two things", async () => {
  const page = pageOf();
  const posted = [];
  initSaveBackup({
    document: page.document,
    window: { webkit: { messageHandlers: { wfeatureExport: { postMessage: body => posted.push(body) } } } },
    fetcher: async () => ok(BLOB),
    chosenGame: () => GAME,
    onStatus: () => {},
  });
  await page.fire("save-export", "click");
  await settle();
  assert.deepEqual(posted, [{ name: `게임${SAVE_BACKUP_EXTENSION}`, data: "AAEC+v8=" }]);
  assert.equal(page.created.length, 0);
});

test("a browser still gets the download it knows what to do with", async () => {
  const page = pageOf();
  const url = globalThis.URL;
  globalThis.URL = { createObjectURL: () => "blob:x", revokeObjectURL: () => {} };
  try {
    initSaveBackup({
      document: page.document,
      window: {},
      fetcher: async () => ok(BLOB),
      chosenGame: () => GAME,
      onStatus: () => {},
    });
    await page.fire("save-export", "click");
    await settle();
  } finally {
    globalThis.URL = url;
  }
  assert.equal(page.created.length, 1, "the anchor is how a browser is asked");
  assert.equal(page.created[0].download, `게임${SAVE_BACKUP_EXTENSION}`);
  assert.equal(page.created[0].clicked, 1);
});

// The iOS app's web view answers window.confirm with "no" and shows nothing,
// so an import that asked through it never ran there and said nothing. The
// question is the page's own dialog, and it still has to be answered before
// anything is sent.
const dialogPage = () => {
  const page = pageOf();
  const listeners = {};
  const classes = new Set();
  const button = name => ({
    textContent: "",
    focus() {},
    addEventListener: (kind, handler) => { listeners[`${name}:${kind}`] = handler; },
    removeEventListener: kind => { delete listeners[`${name}:${kind}`]; },
  });
  const dialog = new Map([
    ["confirm-dialog", { classList: { add: name => classes.add(name), remove: name => classes.delete(name) } }],
    ["confirm-text", { textContent: "" }],
    ["confirm-accept", button("accept")],
    ["confirm-cancel", button("cancel")],
  ]);
  const own = page.document.getElementById;
  Object.assign(page.document, {
    getElementById: id => dialog.get(id) ?? own(id),
    querySelector: () => null,
    addEventListener() {},
    removeEventListener() {},
  });
  return { ...page, listeners, classes, text: dialog.get("confirm-text"), accept: dialog.get("confirm-accept") };
};

test("an import asks in the page's own dialog rather than through window.confirm", async () => {
  const original = globalThis.confirm;
  let browserAsked = 0;
  globalThis.confirm = () => {
    browserAsked += 1;
    return false;
  };
  try {
    for (const accepted of [false, true]) {
      const page = dialogPage();
      let posts = 0;
      const messages = [];
      initSaveBackup({
        document: page.document,
        fetcher: async () => {
          posts += 1;
          return ok({ written: 2, removed: 0 });
        },
        chosenGame: () => GAME,
        onStatus: message => messages.push(message),
      });
      page.elements.get("save-import-file").files = [new Uint8Array([1])];
      page.fire("save-import-file", "change");
      await settle();
      assert.ok(page.classes.has("visible"), "the page's own question is on screen");
      assert.match(page.text.textContent, /백업 파일로 되돌립니다/);
      assert.equal(page.accept.textContent, "가져오기");
      assert.equal(posts, 0, "nothing is sent before the answer");
      page.listeners[accepted ? "accept:click" : "cancel:click"]();
      await settle();
      assert.equal(posts, accepted ? 1 : 0, accepted ? "a yes restores" : "a no sends nothing");
      if (accepted) assert.match(messages.at(-1), /세이브 2개를 복원/);
    }
  } finally {
    globalThis.confirm = original;
  }
  assert.equal(browserAsked, 0, "window.confirm is never asked");
});

test("an import with no way to ask sends nothing", async () => {
  const page = pageOf();
  let posts = 0;
  initSaveBackup({
    document: page.document,
    fetcher: async () => {
      posts += 1;
      return ok({ written: 1, removed: 0 });
    },
    chosenGame: () => GAME,
    onStatus: () => {},
  });
  page.elements.get("save-import-file").files = [new Uint8Array([1])];
  page.fire("save-import-file", "change");
  await settle();
  assert.equal(posts, 0, "a page that cannot ask has not been answered");
});

// A request that never reached the server rejects with the browser's own
// sentence — "Failed to fetch", "Load failed" — which is English and says
// nothing a player can act on.
test("a server that cannot be reached is said in Korean, not in the browser's words", async () => {
  const unreachable = async () => {
    throw new TypeError("Failed to fetch");
  };
  await assert.rejects(() => exportSave(GAME, unreachable), { message: /^서버에 연결하지 못했습니다/ });
  await assert.rejects(() => importSave(GAME, new Uint8Array(), unreachable), { message: /^서버에 연결하지 못했습니다/ });
  const page = pageOf();
  const messages = [];
  initSaveBackup({
    document: page.document,
    window: {},
    fetcher: unreachable,
    chosenGame: () => GAME,
    onStatus: message => messages.push(message),
  });
  page.fire("save-export", "click");
  await settle();
  assert.equal(messages.length, 1);
  assert.match(messages[0], /^서버에 연결하지 못했습니다/);
  assert.doesNotMatch(messages[0], /fetch/i);
});
