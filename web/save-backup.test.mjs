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
