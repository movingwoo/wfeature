// Opt-in browser checks for unchanged frames and explicit redraw boundaries.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { copyFileSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import net from "node:net";

const [binary, engineName = "chromium"] = process.argv.slice(2);
if (!binary || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/frame-delivery.mjs SERVER [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const run = `frame-delivery-${engineName}-${Date.now()}`;
const output = resolve("var/acceptance", run);
const games = resolve("var/games", `.${run}`), ext = resolve("var/ext", run), saves = resolve("var/savedata", run);
for (const directory of [output, join(games, "library"), ext, saves]) mkdirSync(directory, { recursive: true });
copyFileSync("internal/platform/skt/testdata/canvas-skt.zip", join(games, "library", "canvas.zip"));
copyFileSync("internal/platform/lgt/testdata/text-input.zip", join(games, "library", "wipi.zip"));
if (process.env.WFEATURE_FRAME_ARCHIVE) {
  copyFileSync(process.env.WFEATURE_FRAME_ARCHIVE, join(games, "library", "probe.zip"));
}
const listener = net.createServer();
await new Promise(done => listener.listen(0, "127.0.0.1", done));
const port = listener.address().port;
await new Promise(done => listener.close(done));
const origin = `http://127.0.0.1:${port}`;
const server = spawn(resolve(binary), ["-addr", `127.0.0.1:${port}`, "-games", games, "-ext", ext,
  "-saves", join(saves, "ktf"), "-logs", join(output, "logs"), "-web", join(output, "no-external-web"), "-open=false"]);
let logs = "", browser;
server.stdout.on("data", data => { logs += data; });
server.stderr.on("data", data => { logs += data; });
const pause = ms => new Promise(done => setTimeout(done, ms));
const result = { engine: engineName, checks: [], errors: [] };
const check = name => { result.checks.push(name); console.log(`PASS ${engineName}: ${name}`); };
try {
  for (let i = 0; ; i++) {
    try { if ((await fetch(`${origin}/api/status`)).ok) break; } catch {}
    if (i === 100 || server.exitCode !== null) throw new Error("server did not start");
    await pause(50);
  }
  browser = await engine.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 1000 } });
  await context.addInitScript(() => {
    window.frameProbe = { pictures: 0, draws: 0, messages: [], sockets: [] };
    const NativeSocket = window.WebSocket;
    window.WebSocket = class extends NativeSocket {
      constructor(...args) {
        super(...args);
        window.frameProbe.sockets.push(this);
        this.addEventListener("message", event => {
          if (typeof event.data !== "string") window.frameProbe.pictures++;
          else {
            try { window.frameProbe.messages.push(JSON.parse(event.data)); } catch {}
          }
        });
      }
    };
    const draw = CanvasRenderingContext2D.prototype.drawImage;
    CanvasRenderingContext2D.prototype.drawImage = function (...args) {
      const answer = draw.apply(this, args);
      if (this.canvas.id === "canvas") window.frameProbe.draws++;
      return answer;
    };
  });
  const page = await context.newPage();
  page.on("pageerror", error => result.errors.push(error.message));
  const snapshot = () => page.evaluate(() => {
    const canvas = document.querySelector("#canvas");
    const pixels = canvas.getContext("2d").getImageData(0, 0, canvas.width, canvas.height).data;
    let hash = 2166136261;
    for (const byte of pixels) hash = Math.imul(hash ^ byte, 16777619);
    return { pictures: frameProbe.pictures, draws: frameProbe.draws, width: canvas.width, height: canvas.height, hash: hash >>> 0 };
  });
  const nextDraw = before => page.waitForFunction(value => frameProbe.draws > value, before.draws);
  const settings = async () => {
    if (!await page.locator("#settings-panel").evaluate(element => element.classList.contains("visible"))) {
      await page.locator("#settings-toggle").click();
    }
  };
  const start = async value => {
    await page.locator("#game-select").selectOption(value);
    const before = await snapshot();
    await page.locator("#game-start").click();
    await nextDraw(before);
    await page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden"));
  };
  const stop = async () => {
    await settings();
    await page.locator("#restart").click();
    await page.locator("#confirm-accept").click();
    await page.waitForFunction(() => !document.querySelector("#game-start").disabled);
  };
  await page.goto(origin);
  await start("games/library/canvas.zip");
  await pause(250);
  const first = await snapshot();
  await pause(1200);
  assert.deepEqual(await snapshot(), first);
  check("original static canvas remains visible without new PNG messages");

  await stop();
  await start("games/library/canvas.zip");
  assert.equal((await snapshot()).hash, first.hash);
  check("same-image restart presents a new first frame");

  let before = await snapshot();
  await settings();
  await page.locator("#frame-scale").selectOption("2");
  await nextDraw(before);
  assert.equal((await snapshot()).width, 240);
  check("MIDP scale setting preserves dimensions and explicitly redraws");

  before = await snapshot();
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, value: true });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await pause(200);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, value: false });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await nextDraw(before);
  assert.equal((await snapshot()).hash, first.hash);
  check("park and resume redraw an unchanged picture");

  await page.reload();
  await page.waitForFunction(() => frameProbe.draws > 0);
  assert.equal((await snapshot()).hash, first.hash);
  check("new connection restores a static canvas through the page client");

  before = await snapshot();
  await page.keyboard.down("Space");
  await nextDraw(before);
  const pressed = await snapshot();
  assert.notEqual(pressed.hash, first.hash);
  await page.keyboard.up("Space");
  await nextDraw(pressed);
  assert.notEqual((await snapshot()).hash, pressed.hash);
  check("changed input frames still decode and draw");

  await stop();
  await start("games/library/wipi.zip");
  for (const scale of [4, 1, 2]) {
    before = await snapshot();
    await settings();
    await page.locator("#frame-scale").selectOption(String(scale));
    await nextDraw(before);
    const picture = await snapshot();
    assert.equal(picture.width, 240 * scale);
    assert.equal(picture.height, 320 * scale);
  }
  check("WIPI static frames change dimensions across hq4x, original and hq2x");
  if (process.env.WFEATURE_FRAME_ARCHIVE) {
    await stop();
    await settings();
    await page.locator("#frame-scale").selectOption("1");
    await start("games/library/probe.zip");
    const began = await snapshot();
    await pause(6000);
    const ended = await snapshot();
    const stats = await page.evaluate(() => frameProbe.messages.filter(message => message.kind === "stats").at(-1)?.stats);
    assert.ok(stats?.tick_rate > 0, "the local runtime stopped ticking");
    assert.equal(ended.width, 240);
    assert.equal(ended.height, 320);
    result.localArchive = { additionalPictures: ended.pictures - began.pictures, stats };
    check("local archive keeps ticking with original-scale browser presentation");
  }
  const protocolErrors = await page.evaluate(() => frameProbe.messages.filter(message => message.kind === "error"));
  assert.deepEqual(protocolErrors, []);
  assert.deepEqual(result.errors, []);
  result.passed = true;
} catch (error) {
  result.failure = error.stack;
  throw error;
} finally {
  await browser?.close();
  if (server.exitCode === null) {
    await new Promise(done => { server.once("exit", done); server.kill("SIGTERM"); });
  }
  writeFileSync(join(output, "server.log"), logs);
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  rmSync(ext, { recursive: true, force: true });
  console.log(`Frame delivery report: ${output}`);
}
