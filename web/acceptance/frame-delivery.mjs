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
    window.frameProbe = { pictures: 0, patches: 0, shifts: 0, sounds: 0, bytes: 0, draws: 0, messages: [], sockets: [] };
    const NativeSocket = window.WebSocket;
    window.WebSocket = class extends NativeSocket {
      constructor(...args) {
        super(...args);
        window.frameProbe.sockets.push(this);
        this.addEventListener("message", event => {
          if (typeof event.data !== "string") {
            // The page reads binary messages as bytes; see session.js.
            const view = new DataView(event.data);
            window.frameProbe.bytes += event.data.byteLength;
            if (view.getUint32(0) === 0x57464132) {
              window.frameProbe.sounds++;
              return;
            }
            window.frameProbe.pictures++;
            if (view.getUint32(0) === 0x57465032 && view.getUint8(4) !== 0) {
              window.frameProbe.patches++;
              if (view.getInt16(6) || view.getInt16(8)) window.frameProbe.shifts++;
            }
          }
          else {
            try { window.frameProbe.messages.push(JSON.parse(event.data)); } catch {}
          }
        });
      }
    };
    // The page draws a picture at its own size and puts a magnified one.
    for (const method of ["drawImage", "putImageData"]) {
      const original = CanvasRenderingContext2D.prototype[method];
      CanvasRenderingContext2D.prototype[method] = function (...args) {
        const answer = original.apply(this, args);
        if (this.canvas.id === "canvas") window.frameProbe.draws++;
        return answer;
      };
    }
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

  before = await snapshot();
  await page.keyboard.down("1");
  await nextDraw(before);
  const patchesBefore = await page.evaluate(() => frameProbe.patches);
  before = await snapshot();
  await page.keyboard.down("2");
  await nextDraw(before);
  const patched = await snapshot();
  assert.ok(await page.evaluate(() => frameProbe.patches) > patchesBefore, "small change did not arrive as an update");
  await page.evaluate(() => frameProbe.sockets.at(-1).send(JSON.stringify({ kind: "scale", value: 1 })));
  await nextDraw(patched);
  assert.equal((await snapshot()).hash, patched.hash, "update composition differs from a complete server picture");
  await page.keyboard.up("1");
  await page.keyboard.up("2");
  check("update composition matches a forced complete picture pixel for pixel");

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

  // The fixtures that are magnified draw nothing new on input, so the page's
  // own modules are driven here, in the engine, with authored pictures: the
  // canvas round trips they rely on are what a mocked canvas cannot show.
  const magnification = await page.evaluate(async () => {
    const { Magnifier } = await import("/magnify.js");
    const { HqxScaler } = await import("/hqx.js");
    const width = 96, height = 72;
    const source = document.createElement("canvas");
    source.width = width;
    source.height = height;
    const context = source.getContext("2d", { willReadFrequently: true });
    const image = context.createImageData(width, height);
    const palette = [[18, 38, 200], [200, 60, 30], [60, 200, 90], [24, 44, 203], [250, 240, 20], [10, 10, 11]];
    let state = 12345;
    for (let index = 0; index < width * height; index++) {
      state = (state * 1103515245 + 12345) >>> 0;
      image.data.set([...palette[(state >>> 16) % palette.length], 255], index * 4);
    }
    context.putImageData(image, 0, 0);
    const display = document.createElement("canvas").getContext("2d");
    const magnifier = new Magnifier();
    magnifier.present(display, source, 2, null);
    context.fillStyle = "rgb(120, 120, 121)";
    context.fillRect(40, 30, 3, 2);
    context.fillRect(95, 0, 1, 1);
    magnifier.present(display, source, 2, { left: 40, top: 30, right: 43, bottom: 32 });
    magnifier.present(display, source, 2, { left: 95, top: 0, right: 96, bottom: 1 });
    const pixels = context.getImageData(0, 0, width, height).data;
    const whole = new Int32Array(width * 2 * height * 2);
    new HqxScaler().scale(new Int32Array(pixels.slice().buffer), width, height, 2, 0, 0, width, height, whole);
    const expected = new Uint8ClampedArray(whole.buffer);
    const shown = display.getImageData(0, 0, width * 2, height * 2).data;
    let differences = 0;
    for (let index = 0; index < expected.length; index++) if (expected[index] !== shown[index]) differences++;
    return { differences, size: [display.canvas.width, display.canvas.height] };
  });
  assert.deepEqual(magnification, { differences: 0, size: [192, 144] });
  check("hq2x redone around partial changes matches hq2x of the whole picture in the engine");

  const composition = await page.evaluate(async () => {
    const { FrameReceiver } = await import("/frame-stream.js");
    const width = 32, height = 24;
    const encode = async (pixels, w, h) => {
      const canvas = document.createElement("canvas");
      canvas.width = w;
      canvas.height = h;
      const context = canvas.getContext("2d");
      const image = context.createImageData(w, h);
      image.data.set(pixels);
      context.putImageData(image, 0, 0);
      const blob = await new Promise(done => canvas.toBlob(done, "image/png"));
      return new Uint8Array(await blob.arrayBuffer());
    };
    const message = async (operation, x, y, dx, pixels, w, h) => {
      const png = pixels ? await encode(pixels, w, h) : new Uint8Array(0);
      const bytes = new Uint8Array(16 + png.length);
      const view = new DataView(bytes.buffer);
      bytes.set([0x57, 0x46, 0x50, 0x32, operation, 1]);
      view.setInt16(6, dx);
      view.setUint16(10, x);
      view.setUint16(12, y);
      bytes.set(png, 16);
      return bytes.buffer;
    };
    let state = 99;
    const colour = () => {
      state = (state * 1103515245 + 12345) >>> 0;
      return [(state >>> 8) & 0xff, (state >>> 16) & 0xff, (state >>> 24) & 0xff, 255];
    };
    // expected is what the page must end up holding, built by hand.
    const expected = new Uint8ClampedArray(width * height * 4);
    for (let index = 0; index < width * height; index++) expected.set(colour(), index * 4);
    const messages = [await message(0, 0, 0, 0, expected.slice(), width, height)];
    // A masked rectangle: every third pixel new, the rest transparent.
    const masked = new Uint8ClampedArray(6 * 3 * 4);
    for (let index = 0; index < 18; index++) {
      if (index % 3 !== 0) continue;
      const pixel = colour();
      masked.set(pixel, index * 4);
      expected.set(pixel, ((4 + Math.floor(index / 6)) * width + 5 + (index % 6)) * 4);
    }
    messages.push(await message(2, 5, 4, 0, masked, 6, 3));
    // A shift two pixels left, then the uncovered strip on the right.
    const shifted = expected.slice();
    for (let y = 0; y < height; y++) {
      for (let x = 0; x < width - 2; x++) shifted.set(expected.subarray((y * width + x + 2) * 4, (y * width + x + 3) * 4), (y * width + x) * 4);
    }
    const strip = new Uint8ClampedArray(2 * height * 4);
    for (let y = 0; y < height; y++) {
      for (let x = 0; x < 2; x++) {
        const pixel = colour();
        strip.set(pixel, (y * 2 + x) * 4);
        shifted.set(pixel, (y * width + width - 2 + x) * 4);
      }
    }
    expected.set(shifted);
    messages.push(await message(2, width - 2, 0, -2, strip, 2, height));
    // A replacing rectangle erases a pixel to transparency.
    const replacing = new Uint8ClampedArray(2 * 2 * 4);
    for (let index = 1; index < 4; index++) {
      const pixel = colour();
      replacing.set(pixel, index * 4);
      expected.set(pixel, ((index >> 1) * width + (index & 1)) * 4);
    }
    expected.set([0, 0, 0, 0], 0);
    messages.push(await message(1, 0, 0, 0, replacing, 2, 2));
    let held = null, failure = null;
    const receiver = new FrameReceiver(canvas => { held = canvas; }, error => { failure = String(error); });
    for (const buffer of messages) receiver.receive(buffer);
    for (let wait = 0; (receiver.reading || receiver.queue.length) && wait < 200; wait++) {
      await new Promise(done => setTimeout(done, 10));
    }
    if (failure || !held) return { failure: failure ?? "nothing composed" };
    const shown = held.getContext("2d").getImageData(0, 0, width, height).data;
    let differences = 0;
    for (let index = 0; index < expected.length; index++) if (expected[index] !== shown[index]) differences++;
    return { differences };
  });
  assert.deepEqual(composition, { differences: 0 });
  check("masked, shifted and replacing updates compose exactly in the engine");
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
  result.transport = await page.evaluate(() => ({ pictures: frameProbe.pictures, patches: frameProbe.patches,
    shifts: frameProbe.shifts, sounds: frameProbe.sounds, bytes: frameProbe.bytes }));
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
