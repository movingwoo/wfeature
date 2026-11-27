// Run from the repository root with the packaged executable. Each invocation
// owns its database, libraries, save trees, HTTPS listener and browser contexts.
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { chmodSync, copyFileSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import net from "node:net";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";
import { phrase } from "./page-text.mjs";

const [serverPath, engineName = "chromium"] = process.argv.slice(2);
if (!serverPath || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/multi.mjs SERVER [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName));
const binary = resolve(serverPath);
const run = `multi-${engineName}-${randomUUID()}`;
const installation = resolve("var/acceptance", run);
const database = join(installation, "var", "accounts.sqlite");
const games = resolve("var/games", `.${run}`);
const added = resolve("var/ext", run);
const saves = resolve("var/savedata", run);
const game = "games/library/persistence.zip";
const fixture = resolve("internal/platform/skt/testdata/persistence-skt.zip");
for (const path of [join(installation, "var"), join(games, "library"), added, saves]) {
  mkdirSync(path, { recursive: true });
}
copyFileSync(fixture, join(games, "library", "persistence.zip"));
const canvasFixture = resolve("internal/platform/skt/testdata/canvas-skt.zip");
copyFileSync(canvasFixture, join(games, "personal-only.zip"));

const reservePort = async () => {
  const listener = net.createServer();
  await new Promise((done, reject) => listener.once("error", reject).listen(0, "127.0.0.1", done));
  return listener;
};
const plain = await reservePort(), secure = await reservePort();
const control = `http://127.0.0.1:${plain.address().port}`;
const origin = `https://127.0.0.1:${secure.address().port}`;
const args = [
  "-addr", new URL(control).host, "-https", new URL(origin).host, "-open=false",
  "-games", games, "-ext", added, "-saves", join(saves, "custom-ktf"),
  "-web", join(installation, "embedded-only"),
];
await Promise.all([plain, secure].map(listener => new Promise(done => listener.close(done))));

let server, exit, browser;
const startServer = async (mode, name) => {
  server = spawn(binary, [...args, ...(mode === "shared" ? ["-multi"] : [])], { cwd: installation });
  const child = server;
  let log = "";
  child.stdout.on("data", chunk => { log += chunk; });
  child.stderr.on("data", chunk => { log += chunk; });
  exit = new Promise((done, reject) => {
    child.once("error", reject);
    child.once("close", (code, signal) => {
      writeFileSync(join(installation, `${name}.log`), log);
      done({ code, signal });
    });
  });
  for (let attempts = 0; attempts < 200; attempts++) {
    if (child.exitCode !== null || child.signalCode !== null) throw new Error(`Server exited during startup: ${log}`);
    try {
      const status = await (await fetch(`${control}/api/status`)).json();
      if (status.pid === child.pid) return;
    } catch {}
    await delay(50);
  }
  throw new Error(`Server startup timed out: ${log}`);
};
const stopServer = async () => {
  if (!server) return;
  if (server.exitCode === null && server.signalCode === null) {
    const timer = setTimeout(() => server.kill("SIGKILL"), 15000);
    try {
      await fetch(`${control}/api/shutdown`, { method: "POST" });
      const { code, signal } = await exit;
      assert.equal(code, 0, `server shutdown: ${signal}`);
    } finally { clearTimeout(timer); }
  }
  server = null;
};
const setup = (...flags) => spawnSync(binary, [...args, ...flags], {
  cwd: installation, encoding: "utf8", timeout: 10000,
});
const report = { engine: engineName, checks: [] };
const check = message => {
  report.checks.push(message);
  console.log(`PASS ${engineName}: ${message}`);
};

const red = [255, 0, 0], green = [0, 255, 0], blue = [0, 0, 255];
const ready = page => page.waitForFunction(() =>
  !document.querySelector("#game-start").disabled && !document.querySelector("#prestart").inert);
const color = (page, expected) => page.waitForFunction(rgb => {
  const pixel = document.querySelector("#canvas").getContext("2d").getImageData(40, 40, 1, 1).data;
  return pixel[3] === 255 && rgb.every((value, index) => Math.abs(value - pixel[index]) < 8);
}, expected);
const options = page => page.locator("#game-select option").evaluateAll(items => items.map(item => item.value).filter(Boolean));
const key = (page, name) => page.locator(`button[data-key="${name}"]`).first();
const dismiss = async page => {
  if (await page.locator("#status-close").isVisible()) await page.locator("#status-close").click();
};
const choose = async page => {
  await ready(page);
  await dismiss(page);
  await page.locator("#game-select").selectOption(game);
};
const play = async (page, expected) => {
  await choose(page);
  await page.locator("#game-start").click();
  await page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden"));
  await color(page, expected);
};
const fire = async (page, expected) => {
  await key(page, "5").tap();
  await color(page, expected);
};
const quick = async (page, operation, expected) => {
  await key(page, operation === "save" ? "QUICK_SAVE" : "QUICK_LOAD").click();
  const status = phrase(operation === "save" ? "checkpoint.saved" : "checkpoint.loaded");
  await page.waitForFunction(text => document.querySelector("#checkpoint-status").textContent === text, status);
  if (expected) await color(page, expected);
};
const settings = async page => {
  if (!await page.locator("#settings-panel").evaluate(panel => panel.classList.contains("visible"))) {
    await key(page, "SETTINGS").click();
  }
};
const end = async page => {
  await settings(page);
  await page.locator("#restart").click();
  await page.locator("#confirm-accept").click();
  await ready(page);
};
const exportProgress = async (page, name) => {
  await choose(page);
  if (!await page.locator("#backup").evaluate(details => details.open)) await page.locator("#backup-open").click();
  const download = page.waitForEvent("download");
  await page.locator("#save-export").click();
  const file = join(installation, `${name}.wfs`);
  await (await download).saveAs(file);
  await dismiss(page);
  return file;
};
const restore = async (page, file) => {
  await choose(page);
  await page.locator("#save-import-file").setInputFiles(file);
  await page.locator("#confirm-accept").click();
  await page.waitForFunction(text => document.querySelector("#status-text").textContent.endsWith(text), phrase("savepack.restart"));
  await dismiss(page);
};

const submitAccount = async (page, operation, id, password) => {
  const form = page.locator("#account-form");
  await form.locator("#account-id").fill(id);
  await form.locator("#account-password").fill(password);
  await form.locator(`[value="${operation}"]`).click();
};
const signIn = async (page, id, operation) => {
  await submitAccount(page, operation, id, `fixture-password-${id}`);
  await ready(page);
  const login = await (await page.request.get(`${origin}/api/account`)).json();
  assert.equal(login.user.login, id);
  return login.user;
};
const token = page => page.evaluate(() => localStorage.getItem("wfeature.browser-token"));
const newPlayer = async () => {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 390, height: 844 }, hasTouch: true });
  context.setDefaultTimeout(60000);
  return context.newPage();
};
const archiveBackup = `${origin}/api/savepack?game=${encodeURIComponent(game)}`;
const noPrivateCache = async page => {
  const paths = await page.evaluate(async () => {
    const responses = await Promise.all((await caches.keys()).map(async name => (await caches.open(name)).keys()));
    return responses.flat().map(request => new URL(request.url).pathname);
  });
  assert.ok(paths.includes("/app.js"), "common shell was not cached");
  assert.ok(paths.every(path => path !== "/games.json" && !/^\/(api|games|ext)\//.test(path)), "private response in shell cache");
};

try {
  await startServer("personal", "personal-first");
  browser = await engine.launch({
    headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE || undefined,
    args: engineName === "chromium" ? ["--ignore-certificate-errors"] : [],
  });
  report.browser = browser.version();
  const a = await newPlayer();
  await a.goto(`${origin}/licenses`);
  await a.evaluate(async () => {
    const old = await caches.open("wfeature-pwa-v1");
    await old.put("/games.json", new Response('[{"path":"ext/old-private.zip"}]'));
  });
  await a.goto(origin);
  await ready(a);
  await a.waitForFunction(() => Boolean(navigator.serviceWorker.controller));
  await a.waitForFunction(async () => !(await caches.keys()).includes("wfeature-pwa-v1"));
  assert.ok((await options(a)).includes("ext/personal-only.zip"));
  await play(a, red);
  await fire(a, green);
  await quick(a, "save");
  await fire(a, blue);
  await end(a);
  const personalPack = await exportProgress(a, "personal");
  await play(a, blue);
  await quick(a, "load", green);
  await end(a);
  assert.ok(!existsSync(database));
  check("personal picker, game input, ordinary save and manual quick slot without an account database");
  await stopServer();

  // An added tree whose adoption marker is absent still belongs to the
  // personal host. A shared startup must leave a loose operator archive here.
  rmSync(join(added, ".adopted"));
  copyFileSync(canvasFixture, join(games, "operator-loose.zip"));
  await startServer("shared", "shared-first");
  assert.ok(existsSync(join(games, "operator-loose.zip")));
  assert.ok(!existsSync(join(added, ".adopted")));
  await a.reload();
  await a.locator("#account-form").waitFor();
  assert.deepEqual(await options(a), []);
  assert.ok(existsSync(database));
  for (const path of ["/games.json", `/${game}`, "/api/saves/skt/fixture", "/api/session", "/api/savepack?game=fixture"]) {
    assert.equal((await a.request.get(origin + path)).status(), 401, path);
  }
  const upgraded = await a.evaluate(() => new Promise(done => {
    const socket = new WebSocket(`wss://${location.host}/api/session`);
    socket.onopen = () => { socket.close(); done(true); };
    socket.onerror = () => done(false);
  }));
  assert.equal(upgraded, false);
  const alice = await signIn(a, "first", "register");
  const b = await newPlayer();
  await b.goto(origin);
  const rules = b.locator("#account-rules");
  assert.ok(await rules.isVisible());
  assert.equal(await rules.textContent(), phrase("account.signup-rules", { idMin: 3, idMax: 20, passwordMin: 8, passwordMax: 32 }));
  for (const [operation, id, password, key, values] of [
    ["login", "", "", "account.id-required"],
    ["login", "nobody", "wrong", "account.id-not-found"],
    ["login", "first", "wrong", "account.wrong-password"],
    ["register", "first", "another password", "account.id-taken"],
    ["login", "first", "x".repeat(128), "account.wrong-password"],
    ["register", "second", "x".repeat(33), "account.password-too-long", { passwordMax: 32 }],
  ]) {
    await submitAccount(b, operation, id, password);
    await b.waitForFunction(text => document.querySelector("#account-message").textContent === text, phrase(key, values));
  }
  check("account form distinguishes missing fields, unknown IDs, incorrect passwords, duplicate registration and the signup length limit");
  const bob = await signIn(b, "second", "register");
  assert.notEqual(alice.id, bob.id);
  for (const page of [a, b]) {
    const list = await page.request.get(`${origin}/games.json`);
    assert.match(list.headers()["cache-control"], /no-store/);
    assert.ok(!(await options(page)).some(path => path.startsWith("ext/")));
    assert.ok((await options(page)).includes("games/operator-loose.zip"));
    assert.equal((await page.request.get(`${origin}/ext/personal-only.zip`)).status(), 404);
    assert.equal((await page.request.post(`${origin}/api/games`, { data: "fixture" })).status(), 404);
    assert.equal((await page.request.delete(`${origin}/api/games?game=ext/personal-only.zip`)).status(), 404);
    assert.equal((await page.request.get(archiveBackup)).status(), 404);
    const login = (await page.context().cookies(origin)).find(cookie => cookie.name === "__Host-wfeature_login");
    assert.ok(login?.httpOnly && login.secure && login.sameSite === "Lax");
  }
  check("shared login gate, fresh accounts, secure cookies and read-only library");

  await play(a, red);
  assert.ok(await key(a, "QUICK_LOAD").isDisabled());
  const aToken = await token(a);
  assert.match(aToken, /^[a-f0-9]{32}$/);
  const foreign = await b.evaluate(async browserToken => {
    const { GameSession } = await import("/session.js");
    const socket = new GameSession();
    try { await socket.open(); return await socket.resume(browserToken, true); }
    finally { socket.close(); }
  }, aToken);
  assert.equal(foreign.kind, "resumed");
  assert.ok(!foreign.started && !foreign.occupied);
  await play(b, red);
  assert.ok(await key(b, "QUICK_LOAD").isDisabled());
  await quick(a, "save");
  await fire(a, green);
  await fire(b, green);
  await fire(b, blue);
  await quick(b, "save");
  await fire(b, red);
  await color(a, green);
  await end(a); await end(b);
  const aPack = await exportProgress(a, "first"), bPack = await exportProgress(b, "second");
  assert.notDeepEqual(readFileSync(aPack), readFileSync(bPack));
  await play(a, green); await quick(a, "load", red); await end(a);
  await play(b, red); await quick(b, "load", blue); await end(b);
  check("two browsers play the same archive; independent ordinary saves and quick loads survive relaunch");

  await restore(b, personalPack);
  await play(b, blue); await end(b);
  assert.deepEqual(readFileSync(await exportProgress(a, "first-after-import")), readFileSync(aPack));
  await restore(b, bPack);
  await play(b, red); await quick(b, "load", blue); await end(b);
  await play(a, green); await fire(a, blue); await end(a);
  await restore(a, aPack);
  await play(a, green); await quick(a, "load", red);
  assert.deepEqual(readFileSync(await exportProgress(b, "second-after-import")), readFileSync(bPack));
  check("both players restore WFS backups without changing the other tree or their manual slot");

  await settings(a);
  const loggedOut = a.waitForResponse(response => response.url() === `${origin}/api/logout`);
  await a.locator("#account-logout").click();
  assert.equal((await loggedOut).status(), 200);
  await a.waitForFunction(previous => localStorage.getItem("wfeature.browser-token") !== previous, aToken);
  await a.locator("#account-form").waitFor();
  assert.deepEqual(await options(a), []);
  assert.notEqual(await token(a), aToken);
  assert.equal(await a.evaluate(() => localStorage.getItem("wfeature:lastGame")), null);
  const frame = await a.locator("#canvas").evaluate(canvas => [...canvas.getContext("2d").getImageData(40, 40, 1, 1).data]);
  assert.notDeepEqual(frame, [...red, 255]);
  rmSync(join(games, "operator-loose.zip"));
  await signIn(a, "second", "login");
  assert.ok(!(await options(a)).includes("games/operator-loose.zip"));
  await noPrivateCache(a);
  await a.context().setOffline(true);
  await a.waitForFunction(() => document.documentElement.dataset.auth === "locked");
  assert.deepEqual(await options(a), []);
  await stopServer();
  // A stopped listener leaves the worker as the only source for a fresh
  // navigation. WebKit's emulated offline switch also blocks that navigation.
  await a.context().setOffline(false);
  await a.reload();
  await a.locator("#game-select").waitFor({ state: "attached" });
  await a.waitForLoadState("load");
  assert.deepEqual(await options(a), []);
  assert.equal(await a.locator("html").getAttribute("data-mode"), "shared");
  assert.ok(!await a.evaluate(() => fetch("/games.json").then(() => true, () => false)));
  await noPrivateCache(a);
  check("logout clears game and controller; offline reload after switching accounts exposes no cached list");

  await startServer("personal", "personal-again");
  await a.reload(); await ready(a);
  assert.ok((await options(a)).includes("ext/personal-only.zip"));
  await play(a, blue); await quick(a, "load", green); await end(a);
  assert.ok(existsSync(database));
  check("same address and binary reopen original personal progress with accounts.sqlite present");
  await stopServer();

  for (let attempt = 0; attempt < 2; attempt++) {
    const binding = setup("-multi", "-bind-personal-saves", "first");
    assert.equal(binding.status, 0, binding.stderr);
  }
  const other = setup("-multi", "-bind-personal-saves", "second");
  assert.ok(other.status > 0 && !other.error, "second legacy owner accepted");
  await startServer("shared", "shared-bound");
  const bound = await newPlayer(); await bound.goto(origin); await signIn(bound, "first", "login");
  await play(bound, blue); await quick(bound, "load", green); await end(bound);
  await play(bound, blue); await fire(bound, red); await end(bound);
  await b.reload(); await play(b, red); await quick(b, "load", blue); await end(b);
  assert.ok(existsSync(join(saves, "users", alice.id, "skt")));
  assert.ok(existsSync(join(saves, "users", bob.id, "skt")));
  await stopServer();
  await startServer("personal", "personal-bound");
  await a.reload(); await play(a, red); await quick(a, "load", green); await end(a);
  await stopServer();
  check("explicit binding is retryable and shares later writes only with the chosen account at the configured save root");
  chmodSync(database, 0);
  try {
    const denied = setup("-multi");
    assert.ok(denied.status > 0 && !denied.error, "unreadable account store admitted a server");
    assert.match(denied.stderr, /account store/);
  } finally { chmodSync(database, 0o600); }
  check("unreadable account database prevents shared startup");
  report.passed = true;
  console.log(`Evidence: ${installation}`);
} catch (error) {
  report.passed = false;
  report.failure = error.message;
  throw error;
} finally {
  await browser?.close();
  await stopServer();
  writeFileSync(join(installation, "result.json"), JSON.stringify(report, null, 2) + "\n");
  for (const path of [games, added, saves]) rmSync(path, { recursive: true, force: true });
}
