// Opt-in real-browser acceptance. See docs/pwa-acceptance.md for setup and limits.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawn } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve, join, dirname } from "node:path";
import { pathToFileURL } from "node:url";
import net from "node:net";

const [baseline, candidate, engineName = "chromium"] = process.argv.slice(2);
if (!baseline || !candidate || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node web/acceptance/pwa.mjs BASELINE_SERVER CANDIDATE_SERVER [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const run = `pwa-${engineName}-${Date.now()}`;
const output = resolve("var/acceptance", run);
const games = resolve("var/games", `.${run}`), ext = resolve("var/ext", run), saves = resolve("var/savedata", run);
for (const directory of [output, games, ext, saves]) mkdirSync(directory, { recursive: true });
const fixture = resolve("internal/platform/skt/testdata/persistence-skt.zip");
mkdirSync(join(games, "library"));
copyFileSync(fixture, join(games, "library", "persistence.zip"));
copyFileSync("internal/platform/skt/testdata/text-input-skt.zip", join(games, "library", "text-input.zip"));
const hash = file => createHash("sha256").update(readFileSync(file)).digest("hex");
const tree = directory => Object.fromEntries(readdirSync(directory, { recursive: true, withFileTypes: true })
  .filter(entry => entry.isFile()).map(entry => {
    const file = join(entry.parentPath, entry.name);
    return [file.slice(directory.length + 1), hash(file)];
  }).sort(([a], [b]) => a.localeCompare(b)));
const reservation = net.createServer();
await new Promise(done => reservation.listen(0, "127.0.0.1", done));
const port = reservation.address().port;
await new Promise(done => reservation.close(done));
const origin = `http://127.0.0.1:${port}`;
const startedAt = Date.now();
const result = { run, startedAt: new Date(startedAt).toISOString(), engine: engineName, origin, baseline: hash(baseline), candidate: hash(candidate), fixture: hash(fixture), runner: hash(new URL(import.meta.url)), checks: [], errors: [] };
let server, context, page;
const pause = ms => new Promise(done => setTimeout(done, ms));
const check = name => { result.checks.push(name); console.log(`PASS ${engineName}: ${name}`); };
const startServer = async (binary, label) => {
  const child = spawn(resolve(binary), ["-addr", `127.0.0.1:${port}`, "-games", games, "-ext", ext,
    "-saves", join(saves, "ktf"), "-logs", join(output, "logs"), "-web", join(output, "no-external-web"), "-open=false"], { cwd: dirname(resolve(binary)) });
  server = child;
  let logs = "";
  child.stdout.on("data", data => { logs += data; });
  child.stderr.on("data", data => { logs += data; });
  child.on("exit", () => writeFileSync(join(output, `${label}-server.log`), logs));
  for (let attempt = 0; attempt < 200; attempt++) {
    if (child.exitCode !== null) throw new Error(`${label} server exited: ${logs}`);
    try { if ((await fetch(`${origin}/api/status`)).ok) return; } catch {}
    await pause(50);
  }
  throw new Error(`${label} server did not become ready`);
};
const stopServer = async () => {
  if (!server || server.exitCode !== null) return;
  const child = server;
  await new Promise((done, reject) => {
    const timer = setTimeout(() => { child.kill("SIGKILL"); reject(new Error("server shutdown timed out")); }, 10000);
    child.once("exit", () => { clearTimeout(timer); done(); });
    child.kill("SIGTERM");
  });
};
const openBrowser = async () => {
  context = await engine.launchPersistentContext(join(output, "browser-profile"), {
    headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE || undefined, viewport: { width: 390, height: 844 }, hasTouch: true,
  });
  result.browser = context.browser()?.version() ?? engineName;
  await context.addInitScript(() => {
    window.acceptanceAudioContexts = [];
    const NativeAudioContext = window.AudioContext || window.webkitAudioContext;
    if (NativeAudioContext) window.AudioContext = new Proxy(NativeAudioContext, {
      construct(target, argumentsList) {
        const audio = Reflect.construct(target, argumentsList);
        window.acceptanceAudioContexts.push(audio);
        return audio;
      },
    });
  });
  page = context.pages()[0] ?? await context.newPage();
  page.on("pageerror", error => result.errors.push(error.message));
  page.on("dialog", dialog => dialog.accept());
  await page.goto(origin);
};
const ready = () => page.waitForFunction(() => !document.querySelector("#game-start").disabled);
const playing = () => page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden"));
const settings = async () => {
  if (!await page.locator("#settings-panel").evaluate(element => element.classList.contains("visible"))) await page.locator("#settings-toggle").click();
};
const closeSettings = async () => {
  if (await page.locator("#settings-panel").evaluate(element => element.classList.contains("visible"))) await page.locator("#settings-close").click();
};
const dismissStatus = async () => {
  if (await page.locator("#status-close").isVisible()) await page.locator("#status-close").click();
};
const startGame = async name => {
  await ready(); await dismissStatus();
  const value = await page.locator("#game-select option").evaluateAll((options, suffix) => options.find(option => option.value.endsWith(suffix))?.value, name);
  assert.ok(value, `missing game ${name}`);
  await page.selectOption("#game-select", value);
  await page.locator("#game-start").click();
  await playing();
};
const stopGame = async () => {
  await settings(); await page.locator("#restart").click(); await page.locator("#confirm-accept").click(); await ready();
};
const color = expected => page.waitForFunction(rgb => {
  const pixel = document.querySelector("#canvas").getContext("2d").getImageData(40, 40, 1, 1).data;
  return rgb.every((value, index) => Math.abs(value - pixel[index]) < 8);
}, expected);
const fire = async () => { await page.locator('[data-key="OK"]').first().tap(); };
const storage = () => page.evaluate(() => Object.fromEntries(Object.entries(localStorage).filter(([key]) => /keypad|keyBindings|frameScale/.test(key))));
const download = async name => {
  const pending = page.waitForEvent("download"); await page.locator("#save-export").click();
  const file = join(output, name); await (await pending).saveAs(file); await dismissStatus(); return file;
};
const updateWorker = async () => {
  await page.evaluate(async () => { const registration = await navigator.serviceWorker.ready; await registration.update(); });
  await page.waitForFunction(async name => {
    const registration = await navigator.serviceWorker.getRegistration();
    const names = await caches.keys();
    return registration?.active?.state === "activated" && !registration.installing && !registration.waiting
      && navigator.serviceWorker.controller === registration.active
      && names.length === 1 && names[0] === name;
  }, result.expectedCache);
  // Settle the old page's resource responses before navigating under the new
  // controller; replaced workers may still be completing those fetches.
  await page.waitForLoadState("networkidle");
  await page.reload();
  await page.waitForLoadState("networkidle");
};
try {
  await startServer(baseline, "baseline"); await openBrowser(); await ready();
  await page.evaluate(async () => { await navigator.serviceWorker.ready; });
  await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  result.baselineCaches = await page.evaluate(() => caches.keys());
  await settings(); await page.selectOption("#keypad-shape-settings", "type2");
  await page.locator("#keypad-arrange-open").click();
  // Store a non-default size through the actual editor.
  await page.locator('#keypad-size-list input[type="range"]').first().evaluate(element => {
    element.value = "52"; element.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await page.locator("#keypad-arrange-close").click();
  if (await page.locator("#settings-panel").evaluate(element => element.classList.contains("visible"))) await closeSettings();
  await page.locator("#game-file").setInputFiles("internal/platform/skt/testdata/canvas-skt.zip");
  await page.waitForFunction(() => [...document.querySelectorAll("#game-select option")].some(option => option.value.startsWith("ext/")));
  await page.locator("#status-close").waitFor(); await dismissStatus();
  await startGame("persistence.zip"); await color([255, 0, 0]);
  await fire(); await color([0, 255, 0]);
  await stopGame();
  const backup = await download("baseline.wfs");
  const before = { games: tree(games), ext: tree(ext), saves: tree(saves), storage: await storage() };
  assert.ok(Object.keys(before.saves).length, "guest did not persist data");
  assert.ok(Object.keys(before.ext).length, "upload did not persist data");
  result.before = before;
  check("baseline guest progress, upload, settings and save export");
  await context.close(); context = null; await stopServer();

  await startServer(candidate, "candidate");
  const workerSource = await (await fetch(`${origin}/service-worker.js`)).text();
  result.expectedCache = workerSource.match(/const cacheName = "([^"]+)"/)[1];
  assert.ok(!result.baselineCaches.includes(result.expectedCache), "baseline must serve a different embedded shell");
  assert.deepEqual(tree(games), before.games); assert.deepEqual(tree(ext), before.ext); assert.deepEqual(tree(saves), before.saves);
  await openBrowser(); await ready(); await updateWorker(); await ready();
  assert.deepEqual(await storage(), before.storage);
  assert.equal(await page.locator("#keypad-shape-settings").inputValue(), "type2");
  result.cacheObservation = await page.evaluate(async () => {
    const entries = {};
    for (const name of await caches.keys()) entries[name] = (await (await caches.open(name)).keys()).map(request => request.url);
    return entries;
  });
  result.workers = await Promise.all(context.serviceWorkers().map(async worker => ({
    url: worker.url(), version: await worker.evaluate("cacheName").catch(() => "unavailable"),
  })));
  result.candidateCaches = await page.evaluate(() => caches.keys());
  assert.deepEqual(result.candidateCaches, [result.expectedCache]);
  assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--keypad-key-pref").trim()), "52px");
  const listed = await page.locator("#game-select option").evaluateAll(options => options.map(option => option.value));
  assert.ok(listed.some(value => value.endsWith("persistence.zip")));
  assert.ok(listed.some(value => value.startsWith("ext/") && value.endsWith("canvas-skt.zip")));
  check("server replacement preserves archive/save bytes, browser restart preserves settings, old shell retires");
  const manifest = await page.evaluate(async () => (await fetch("manifest.webmanifest")).json());
  assert.equal(manifest.display, "standalone"); assert.ok(manifest.icons.some(icon => icon.sizes === "192x192"));
  check("manifest and service worker prerequisites (not OS installation)");
  await startGame("persistence.zip"); await color([0, 255, 0]);
  check("updated runtime reads baseline guest progress");
  await page.waitForFunction(() => window.acceptanceAudioContexts.some(audio => audio.state === "running"));
  await page.evaluate(() => Promise.all(window.acceptanceAudioContexts.map(audio => audio.suspend())));
  await fire(); await color([0, 0, 255]);
  await page.waitForFunction(() => window.acceptanceAudioContexts.every(audio => audio.state === "running"));
  check("user gesture activates and resumes the real AudioContext (not audible-output proof)");
  // The top-left marker must return to black after a real browser touch tap.
  await page.waitForFunction(() => document.querySelector("#canvas").getContext("2d").getImageData(4, 4, 1, 1).data[0] < 8);
  check("touch press and release reach the guest");
  await settings(); await page.locator("#text-input-toggle").click();
  await page.waitForFunction(() => document.querySelector("#text-input-status").dataset.state === "unavailable");
  assert.equal(await page.locator("#text-input-apply").isDisabled(), true);
  assert.equal(await page.locator("#text-input-value").isVisible(), false);
  await page.screenshot({ path: join(output, "text-unavailable.png") });
  await page.locator("#text-input-cancel").click(); await closeSettings(); await stopGame();
  await page.locator("#save-import-file").setInputFiles(backup);
  await page.waitForFunction(() => document.querySelector("#status-text").textContent.includes("복원"));
  await startGame("persistence.zip"); await color([0, 255, 0]);
  await page.screenshot({ path: join(output, "restored-progress.png") });
  await stopGame();
  const restored = await download("restored.wfs");
  assert.equal(hash(restored), hash(backup));
  check("baseline WFS restores prior guest progress after a newer save");

  await startGame("text-input.zip"); await settings(); await page.locator("#text-input-toggle").click();
  await page.waitForFunction(() => document.querySelector("#text-input-status").dataset.state === "available");
  await page.locator("#text-input-dialog input:visible, #text-input-dialog textarea:visible").fill("한글 test");
  await page.locator("#text-input-dialog input:visible, #text-input-dialog textarea:visible").dispatchEvent("compositionstart");
  await page.locator("#text-input-apply").click();
  assert.equal(await page.locator("#text-input-dialog").evaluate(element => element.open), true);
  await page.locator("#text-input-dialog input:visible, #text-input-dialog textarea:visible").dispatchEvent("compositionend");
  await page.screenshot({ path: join(output, "text-available.png") });
  await page.locator("#text-input-apply").click(); await page.locator("#text-input-dialog").waitFor({ state: "hidden" });
  await settings(); await page.locator("#text-input-toggle").click();
  await page.waitForFunction(() => document.querySelector("#text-input-dialog input:not([hidden]), #text-input-dialog textarea:not([hidden])")?.value === "한글 test");
  await page.locator("#text-input-dialog input:visible, #text-input-dialog textarea:visible").fill("x".repeat(17)); await page.locator("#text-input-apply").click();
  await page.waitForFunction(() => document.querySelector("#text-input-status").dataset.state === "error");
  assert.equal(await page.locator("#text-input-apply").isEnabled(), true);
  await page.locator("#text-input-dialog input:visible, #text-input-dialog textarea:visible").fill("retry"); await page.locator("#text-input-apply").click();
  await page.locator("#text-input-dialog").waitFor({ state: "hidden" });
  check("text availability, Korean composition isolation, guest readback, constraint retry");
  await closeSettings();
  await page.reload();
  await playing(); await settings(); await page.locator("#text-input-toggle").click();
  await page.waitForFunction(() => document.querySelector("#text-input-dialog input:not([hidden]), #text-input-dialog textarea:not([hidden])")?.value === "retry");
  check("page reload resumes the retained session and preserves guest text");
  await page.locator("#text-input-cancel").click(); await closeSettings(); await stopGame();
  await stopServer();
  await page.reload();
  await page.locator("#settings-toggle").waitFor();
  assert.equal(await page.locator("#text-input-toggle").isVisible(), false);
  await startServer(candidate, "candidate-restarted");
  await page.reload(); await ready();
  await startGame("persistence.zip"); await color([0, 255, 0]); await stopGame();
  check("cached shell loads with server unavailable; restart restores saved guest progress");
  assert.deepEqual(result.errors, []);
  result.passed = true;
} catch (error) {
  result.passed = false; result.failure = error.stack;
  if (page && !page.isClosed()) await page.screenshot({ path: join(output, "failure.png") }).catch(() => {});
  throw error;
} finally {
  if (context) await context.close();
  await stopServer();
  result.seconds = (Date.now() - startedAt) / 1000;
  result.durationNote = "Browser automation; no physical-device, audible-output or OS-install claim.";
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  console.log(`Evidence: ${output}`);
}
