// Opt-in browser integration for game controllers, against a server binary and
// an authored fixture. A browser offers no way to plug in a controller, so the
// page is handed a stand-in for `navigator.getGamepads` and the events that
// announce one; everything from there on — the page's reading of it, the holds,
// the settings rows and the key messages on the socket — is the real page.
// This is not evidence about any physical controller or operating system.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdirSync, copyFileSync, rmSync, writeFileSync } from "node:fs";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import net from "node:net";

const [binary, engineName = "chromium"] = process.argv.slice(2);
if (!binary || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/gamepad.mjs SERVER [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
const run = `gamepad-${engineName}-${Date.now()}`;
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
const result = { engine: engineName, checks: [], errors: [] };

// Handset codes the page sends, as in app.js's table.
const OK = 148, CLR = 8, UP = 141, STAR = 42, KEY = n => 48 + n;

try {
  for (let i = 0; ; i++) {
    try { if ((await fetch(`${origin}/api/status`)).ok) break; } catch {}
    if (i === 100 || server.exitCode !== null) throw new Error("server did not start");
    await pause(50);
  }
  browser = await engine.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 1000 } });
  await context.addInitScript(() => {
    window.keyMessages = [];
    const send = WebSocket.prototype.send;
    WebSocket.prototype.send = function(data) {
      try {
        const message = JSON.parse(data);
        if (message.kind === "key") window.keyMessages.push(message);
      } catch {}
      return send.call(this, data);
    };
    // The stand-in controllers. Each reading hands out the same objects the
    // tests change, as a browser that keeps live objects does.
    window.fakePads = [];
    window.rumbleCalls = [];
    Object.defineProperty(Navigator.prototype, "getGamepads", {
      configurable: true,
      value() { return window.fakePads.slice(); },
    });
    window.connectPad = (index, id, mapping, buttons = 17, axes = 4) => {
      const pad = {
        index, id, mapping, connected: true, timestamp: performance.now(),
        buttons: Array.from({ length: buttons }, () => ({ pressed: false, touched: false, value: 0 })),
        axes: new Array(axes).fill(0),
        vibrationActuator: {
          effects: ["dual-rumble"],
          playEffect(kind, options) { window.rumbleCalls.push([kind, options]); return Promise.resolve("complete"); },
          reset() { window.rumbleCalls.push(["reset"]); return Promise.resolve("complete"); },
        },
      };
      window.fakePads[index] = pad;
      const event = new Event("gamepadconnected");
      event.gamepad = pad;
      window.dispatchEvent(event);
    };
    window.setButton = (index, n, value) => {
      window.fakePads[index].buttons[n] = { pressed: value > 0, touched: value > 0, value };
    };
    window.setAxes = (index, axes) => { window.fakePads[index].axes = axes; };
    window.disconnectPad = index => {
      const pad = window.fakePads[index];
      window.fakePads[index] = null;
      const event = new Event("gamepaddisconnected");
      event.gamepad = pad;
      window.dispatchEvent(event);
    };
  });
  const page = await context.newPage();
  page.on("pageerror", error => result.errors.push(error.message));
  page.on("websocket", socket => socket.on("framereceived", event => {
    if (typeof event.payload !== "string") return;
    const message = JSON.parse(event.payload);
    if (message.kind === "error") result.errors.push(message.message);
  }));

  const clear = () => page.evaluate(() => { window.keyMessages = []; });
  const sent = () => page.evaluate(() => window.keyMessages.map(m => `${m.action === "press" ? "+" : "-"}${m.code}`));
  // Long enough for a few animation frames and the socket.
  const settle = () => pause(150);
  const button = async (n, value, index = 0) => {
    await page.evaluate(([i, b, v]) => window.setButton(i, b, v), [index, n, value]);
    await settle();
  };
  const axes = async (values, index = 0) => {
    await page.evaluate(([i, v]) => window.setAxes(i, v), [index, values]);
    await settle();
  };
  const expectSent = async (expected, why) => {
    assert.deepEqual(await sent(), expected, why);
    await clear();
  };

  await page.goto(origin);
  assert.equal(await page.locator("#pad-bindings").evaluate(e => e.classList.contains("hidden")), true);
  result.checks.push("controller settings hidden before any controller is used");

  await page.locator("#game-start").click();
  await page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden"));
  if (!await page.locator("#settings-panel").evaluate(e => e.classList.contains("visible"))) {
    await page.locator('button[data-key="SETTINGS"]').click();
  }

  await page.evaluate(() => window.connectPad(0, "Xbox Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 0b13)", "standard"));
  await page.waitForFunction(() => !document.querySelector("#pad-bindings").classList.contains("hidden"));
  await settle();
  assert.match(await page.locator("#pad-bindings-status").textContent(), /연결됨: Xbox/);
  assert.equal(await page.locator("#pad-bindings-list select").count(), 28);
  assert.equal(await page.locator('#pad-bindings-list div[data-input="b0"] .key-binding-key').textContent(), "A (아래)");
  await clear();
  result.checks.push("first controller reveals the settings with the standard layout");

  await button(0, 1);
  await expectSent([`+${OK}`], "A sends the centre key");
  assert.equal(await page.locator('button[data-key="OK"]').first().evaluate(e => e.classList.contains("pressed")), true);
  assert.equal(await page.locator('#pad-bindings-list div[data-input="b0"]').evaluate(e => e.classList.contains("active")), true);
  await button(0, 0);
  await expectSent([`-${OK}`]);
  assert.equal(await page.locator('#pad-bindings-list div[data-input="b0"]').evaluate(e => e.classList.contains("active")), false);
  await button(1, 1);
  await button(1, 0);
  await expectSent([`+${CLR}`, `-${CLR}`], "B sends 취소");
  result.checks.push("buttons hold keys, light the keypad and the row");

  await button(12, 1);
  await axes([0, -1, 0, 0]);
  await expectSent([`+${UP}`], "direction pad and left stick are one press");
  await button(12, 0);
  await expectSent([]);
  await axes([0, 0, 0, 0]);
  await expectSent([`-${UP}`]);
  result.checks.push("direction pad and left stick share the direction key");

  await axes([0, 0, 0, -1]);
  await axes([0, 0, Math.cos(-Math.PI / 4), Math.sin(-Math.PI / 4)]);
  await axes([0, 0, 0, 0]);
  await expectSent([`+${KEY(2)}`, `-${KEY(2)}`, `+${KEY(3)}`, `-${KEY(3)}`], "right stick slides 2 to 3");
  result.checks.push("right stick is the number block and slides between keys");

  await button(6, 0.3);
  await expectSent([], "a resting finger on the trigger is not a press");
  await button(6, 0.8);
  await button(6, 0.4);
  await expectSent([`+${STAR}`]);
  await button(6, 0.1);
  await expectSent([`-${STAR}`]);
  result.checks.push("trigger thresholds");

  await page.locator("#pad-bindings summary").click();
  await page.locator('#pad-bindings-list select[data-input="b0"]').selectOption("5");
  await button(0, 1);
  await button(0, 0);
  await expectSent([`+${KEY(5)}`, `-${KEY(5)}`], "a rebound button sends its new key");
  assert.equal(JSON.parse(await page.evaluate(() => localStorage.getItem("wfeature:padBindings"))).b0, "5");
  await button(0, 1);
  await page.locator('#pad-bindings-list select[data-input="b0"]').selectOption("CLR");
  await button(0, 0);
  await expectSent([`+${KEY(5)}`, `-${KEY(5)}`], "a rebind while held releases the key the press sent");
  result.checks.push("rebinding from the panel, stored, safe while held");

  await button(9, 1);
  assert.equal(await page.locator("#settings-panel").evaluate(e => e.classList.contains("visible")), false);
  await button(9, 0);
  await button(9, 1);
  await button(9, 0);
  assert.equal(await page.locator("#settings-panel").evaluate(e => e.classList.contains("visible")), true);
  await expectSent([], "a page control sends no handset key");
  result.checks.push("Menu button toggles the settings panel");

  await page.locator('#pad-bindings-list select[data-input="b0"]').selectOption("OK");
  await button(10, 1);
  await button(10, 0);
  await button(0, 1);
  await pause(600);
  await button(0, 0);
  const repeated = await sent();
  await clear();
  assert.ok(repeated.filter(entry => entry === `+${OK}`).length >= 3, JSON.stringify(repeated));
  assert.ok(repeated.every(entry => entry.endsWith(String(OK))));
  assert.equal(repeated.at(-1), `-${OK}`);
  await button(10, 1); await button(10, 0);
  await button(10, 1); await button(10, 0);
  await button(0, 1);
  await button(0, 0);
  await expectSent([`+${OK}`, `-${OK}`], "rapid fire is off again");
  result.checks.push("L3 cycles rapid fire, which repeats a controller's hold");

  await button(0, 1);
  await expectSent([`+${OK}`]);
  await page.evaluate(() => window.dispatchEvent(new Event("blur")));
  await settle();
  await expectSent([`-${OK}`], "focus loss lets go");
  await pause(200);
  await expectSent([], "a button held across the reset stays dead");
  await button(0, 0);
  await button(0, 1);
  await expectSent([`+${OK}`]);
  await button(0, 0);
  await expectSent([`-${OK}`]);
  result.checks.push("focus loss releases and a held button stays dead until let go");

  await page.setViewportSize({ width: 390, height: 844 });
  await pause(300);
  assert.equal(await page.locator("#settings-panel").evaluate(e => e.classList.contains("visible")), false);
  await button(9, 1); await button(9, 0);
  assert.equal(await page.locator("#settings-panel").evaluate(e => e.classList.contains("visible")), true);
  await button(0, 1); await button(0, 0);
  await expectSent([], "a panel over the game takes the game's keys");
  await button(9, 1); await button(9, 0);
  assert.equal(await page.locator("#settings-panel").evaluate(e => e.classList.contains("visible")), false);
  await button(0, 1); await button(0, 0);
  await expectSent([`+${OK}`, `-${OK}`]);
  await page.screenshot({ path: join(output, "narrow.png") });
  await page.setViewportSize({ width: 1280, height: 1000 });
  await pause(300);
  result.checks.push("narrow modal settings block game keys and Menu closes them");

  await page.evaluate(() => window.connectPad(1, "USB Gamepad (Vendor: 0079 Product: 0011)", "", 10, 2));
  await settle();
  await button(3, 1, 1);
  await button(3, 0, 1);
  assert.match(await page.locator("#pad-bindings-status").textContent(), /표준 배치로 읽히지 않는/);
  assert.equal(await page.locator("#pad-bindings-list select").count(), 14);
  assert.equal(await page.locator('#pad-bindings-list div[data-input="b3"] .key-binding-key').textContent(), "버튼 4");
  await expectSent([], "an unrecognised controller starts with nothing bound");
  if (!await page.locator("#pad-bindings").evaluate(e => e.open)) await page.locator("#pad-bindings summary").click();
  await page.locator('#pad-bindings-list select[data-input="b3"]').selectOption("OK");
  await page.locator('#pad-bindings-list select[data-input="a1-"]').selectOption("UP");
  await button(3, 1, 1);
  await button(3, 0, 1);
  await axes([0, -1], 1);
  await axes([0, 0], 1);
  await expectSent([`+${OK}`, `-${OK}`, `+${UP}`, `-${UP}`]);
  const raw = JSON.parse(await page.evaluate(() => localStorage.getItem("wfeature:padBindingsRaw")));
  assert.deepEqual(raw["USB Gamepad (Vendor: 0079 Product: 0011)"], { b3: "OK", "a1-": "UP" });
  await button(0, 1);
  await button(0, 0);
  await expectSent([`+${OK}`, `-${OK}`], "the standard controller keeps its own table");
  result.checks.push("unrecognised controller gets numbered rows and a table of its own");

  await page.evaluate(() => { window.disconnectPad(1); window.disconnectPad(0); });
  await settle();
  assert.match(await page.locator("#pad-bindings-status").textContent(), /아무거나 누르면/);
  result.checks.push("disconnecting returns the panel to its waiting state");

  await page.screenshot({ path: join(output, "settings.png") });
  await page.reload();
  await page.waitForFunction(() => document.querySelector("#pad-bindings") && !document.querySelector("#pad-bindings").classList.contains("hidden"));
  assert.equal(await page.locator('#pad-bindings-list select[data-input="b0"]').inputValue(), "OK");
  await page.locator("#settings-panel").evaluate(e => e.classList.add("visible"));
  if (!await page.locator("#pad-bindings").evaluate(e => e.open)) await page.locator("#pad-bindings summary").click();
  await page.locator('#pad-bindings-list select[data-input="b2"]').selectOption("");
  await page.reload();
  await page.waitForFunction(() => document.querySelector("#pad-bindings") && !document.querySelector("#pad-bindings").classList.contains("hidden"));
  assert.equal(await page.locator('#pad-bindings-list select[data-input="b2"]').inputValue(), "", "a cleared control stays clear");
  await page.locator("#settings-panel").evaluate(e => e.classList.add("visible"));
  if (!await page.locator("#pad-bindings").evaluate(e => e.open)) await page.locator("#pad-bindings summary").click();
  await page.locator("#pad-bindings-reset").click();
  assert.equal(await page.locator('#pad-bindings-list select[data-input="b2"]').inputValue(), "5");
  assert.equal(JSON.parse(await page.evaluate(() => localStorage.getItem("wfeature:padBindings"))).b2, "5");
  result.checks.push("a seen controller's settings show at once after reload, survive it, and reset");

  assert.deepEqual(result.errors, []);
  console.log(JSON.stringify(result, null, 2));
} finally {
  await browser?.close();
  server.kill("SIGTERM");
  await new Promise(done => { if (server.exitCode !== null) done(); else server.once("exit", done); });
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  writeFileSync(join(output, "server.log"), logs);
  rmSync(ext, { recursive: true, force: true });
  rmSync(games, { recursive: true, force: true });
}
