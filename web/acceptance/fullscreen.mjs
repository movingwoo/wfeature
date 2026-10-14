// Opt-in browser integration for 전체화면 against a server binary and an
// authored fixture: the layout at several window shapes, every way in and out,
// keys still reaching the game, and the choice surviving a reload.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdirSync, copyFileSync, rmSync, writeFileSync } from "node:fs";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import net from "node:net";

const [binary, engineName = "chromium"] = process.argv.slice(2);
if (!binary || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/fullscreen.mjs SERVER [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
const run = `fullscreen-${engineName}-${Date.now()}`;
const output = resolve("var/acceptance", run);
const games = resolve("var/games", `.${run}`);
const ext = resolve("var/ext", run);
const saves = resolve("var/savedata", run);
for (const path of [output, games, ext, saves]) mkdirSync(path, { recursive: true });
mkdirSync(join(games, "library"));
copyFileSync("internal/platform/skt/testdata/canvas-skt.zip", join(games, "library", "canvas.zip"));
const listener = net.createServer();
await new Promise(done => listener.listen(0, "127.0.0.1", done));
const port = listener.address().port;
await new Promise(done => listener.close(done));
const origin = `http://127.0.0.1:${port}`;
const server = spawn(resolve(binary), ["-addr", `127.0.0.1:${port}`, "-games", games, "-ext", ext,
  "-saves", join(saves, "skt"), "-logs", join(output, "logs"), "-web", join(output, "no-external-web"), "-open=false"]);
let logs = "", browser;
server.stdout.on("data", data => { logs += data; });
server.stderr.on("data", data => { logs += data; });
const pause = ms => new Promise(done => setTimeout(done, ms));
const result = { engine: engineName, checks: [], errors: [], layouts: [] };

try {
  for (let i = 0; ; i++) {
    try { if ((await fetch(`${origin}/api/status`)).ok) break; } catch {}
    if (i === 100 || server.exitCode !== null) throw new Error("server did not start");
    await pause(50);
  }
  browser = await engine.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  await context.addInitScript(() => {
    window.keyMessages = [];
    const send = WebSocket.prototype.send;
    WebSocket.prototype.send = function(data) {
      try {
        const message = JSON.parse(data);
        if (message.kind === "key") window.keyMessages.push(`${message.action === "press" ? "+" : "-"}${message.code}`);
      } catch {}
      return send.call(this, data);
    };
  });
  const page = await context.newPage();
  page.on("pageerror", error => result.errors.push(error.message));

  const on = () => page.evaluate(() => document.documentElement.classList.contains("fullscreen"));
  const display = selector => page.evaluate(s => getComputedStyle(document.querySelector(s)).display, selector);
  const settingsOpen = () => page.locator("#settings-panel").evaluate(e => e.classList.contains("visible"));
  const openSettings = async () => {
    if (!await settingsOpen()) await page.locator('button[data-key="SETTINGS"]').click();
  };

  await page.goto(origin);
  await page.locator("#game-start").click();
  await page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden"));
  assert.equal(await on(), false);
  assert.equal(await display("#fullscreen-exit"), "none");

  await openSettings();
  await page.locator("#fullscreen-toggle").click();
  assert.equal(await on(), true);
  assert.equal(await display(".button-container"), "none");
  assert.equal(await settingsOpen(), false, "a docked panel would be a modal over the game");
  assert.equal(await page.locator("#fullscreen-toggle").textContent(), "⛶ 전체화면 끝내기");
  assert.equal(await page.evaluate(() => localStorage.getItem("wfeature:fullscreen")), "on");
  result.checks.push("the settings button puts the keypad and the panels away");

  // The largest 3:4 picture that leaves the top-right corner clear, centred.
  const corner = 48;
  for (const [width, height] of [[1280, 800], [390, 844], [844, 390], [600, 800], [768, 1024]]) {
    await page.setViewportSize({ width, height });
    await pause(250);
    const picture = await page.locator(".canvas-wrapper").boundingBox();
    const exit = await page.locator("#fullscreen-exit").boundingBox();
    const expected = Math.min(width, height * 0.75, Math.max(width - 2 * corner, (height - 2 * corner) * 0.75));
    result.layouts.push({ width, height, picture, exit });
    assert.ok(Math.abs(picture.width - expected) <= 1, `${width}x${height}: ${picture.width} wide, expected ${expected}`);
    assert.ok(Math.abs(picture.x + picture.width / 2 - width / 2) <= 1, `${width}x${height}: not centred across`);
    assert.ok(Math.abs(picture.y + picture.height / 2 - height / 2) <= 1, `${width}x${height}: not centred down`);
    const apart = exit.x >= picture.x + picture.width || exit.y + exit.height <= picture.y;
    assert.ok(apart, `${width}x${height}: the exit button is on the picture`);
    // Against the picture's top-right corner: just above it, or just beside it.
    const right = picture.x + picture.width;
    const above = Math.abs(exit.y + exit.height + 8 - picture.y) <= 1 && Math.abs(exit.x + exit.width - right) <= 1;
    const beside = Math.abs(exit.x - 8 - right) <= 1 && Math.abs(exit.y - picture.y) <= 1;
    assert.ok(above || beside, `${width}x${height}: the exit button is not against the picture: ${JSON.stringify({ exit, picture })}`);
    assert.ok(exit.x + exit.width <= width && exit.y >= 0, `${width}x${height}: the exit button is off screen`);
    await page.screenshot({ path: join(output, `fullscreen-${width}x${height}.png`) });
  }
  await page.setViewportSize({ width: 1280, height: 800 });
  await pause(250);
  result.checks.push("largest centred picture at five window shapes, exit button against its corner");

  await page.evaluate(() => { window.keyMessages = []; document.activeElement?.blur(); });
  await page.keyboard.press("Space");
  await pause(150);
  assert.deepEqual(await page.evaluate(() => window.keyMessages), ["+148", "-148"]);
  result.checks.push("keyboard keys still reach the game");

  await page.keyboard.press("Escape");
  assert.equal(await on(), false);
  assert.equal(await display(".button-container"), "block");
  assert.equal(await settingsOpen(), true, "the wide layout opens its docked settings again");
  result.checks.push("Escape leaves it and the wide layout comes back as it opens");

  await page.locator("#fullscreen-toggle").click();
  await page.reload();
  await page.waitForLoadState("load");
  await pause(300);
  assert.equal(await on(), true, "the choice survives a reload");
  await page.locator("#fullscreen-exit").click();
  assert.equal(await on(), false);
  assert.equal(await page.evaluate(() => localStorage.getItem("wfeature:fullscreen")), "off");
  result.checks.push("remembered across a reload; the corner button leaves it");

  // Opening the keypad editor brings the keypad back: it is what is edited.
  // A reload keeps the session, which comes back by itself; a page that came
  // back to the picker instead starts the game again.
  const playing = () => !document.querySelector("#restart").classList.contains("hidden");
  await page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden") ||
    !document.querySelector("#game-start").disabled);
  if (!await page.evaluate(playing)) {
    await page.locator("#game-start").click();
    await page.waitForFunction(playing);
  }
  await openSettings();
  await page.locator("#fullscreen-toggle").click();
  await page.evaluate(() => document.querySelector("#keypad-arrange-open").click());
  assert.equal(await on(), false);
  assert.equal(await page.locator("#keypad-arrange").evaluate(e => getComputedStyle(e).display !== "none"), true);
  // A cell can carry it, and a press on the cell puts the keypad away.
  await page.locator('[data-cell="r2c1"]').click();
  await page.locator("#keypad-arrange-keys").getByRole("button", { name: "전체화면", exact: true }).click();
  await page.locator("#keypad-arrange-close").click();
  const cell = page.locator('button[data-cell="r2c1"][data-key="FULLSCREEN"]');
  assert.equal(await cell.count(), 1);
  await page.evaluate(() => { window.keyMessages = []; });
  await cell.click();
  assert.equal(await on(), true);
  assert.deepEqual(await page.evaluate(() => window.keyMessages), [], "the cell sends no handset key");
  await page.locator("#fullscreen-exit").click();
  result.checks.push("the keypad editor leaves it; a keypad cell can enter it");

  await page.setViewportSize({ width: 390, height: 844 });
  await pause(300);
  await openSettings();
  await page.locator("#fullscreen-toggle").click();
  assert.equal(await on(), true);
  assert.equal(await settingsOpen(), false);
  assert.equal(await display("#fullscreen-exit"), "flex");
  await page.screenshot({ path: join(output, "narrow-fullscreen.png") });
  await page.locator("#fullscreen-exit").click();
  assert.equal(await on(), false);
  assert.equal(await settingsOpen(), false, "a narrow window opens no modal on the way back");
  result.checks.push("narrow window: in from the modal settings, out by the corner button");

  assert.deepEqual(result.errors, []);
  console.log(JSON.stringify({ ...result, layouts: result.layouts.length }, null, 2));
} finally {
  await browser?.close();
  server.kill("SIGTERM");
  await new Promise(done => { if (server.exitCode !== null) done(); else server.once("exit", done); });
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  writeFileSync(join(output, "server.log"), logs);
  rmSync(ext, { recursive: true, force: true });
  rmSync(games, { recursive: true, force: true });
}
