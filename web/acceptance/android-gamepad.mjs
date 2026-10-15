// Opt-in check of the Android app with a game controller, on an emulator or a
// phone with no controller attached. The controller is a virtual HID device the
// platform makes from a real controller's report descriptor (`adb shell hid`),
// so the kernel's driver and Android's key layout turn its reports into the
// same key and motion events a physical one sends; the page is read through
// the web view's DevTools socket. Everything between the reports and the
// page's key messages is the real app: the activity, the web view's Gamepad
// API and the page.
//
// It expects the app installed (`make mobile-android`, then `adb install -r`)
// and a `hid` register command for an Xbox One S over Bluetooth (vendor 045e,
// product 02fd), whose report layout the presses below are written in. Android's
// own input tests carry one. ANDROID_SERIAL picks the device as adb itself does.
//
//   PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
//     node web/acceptance/android-gamepad.mjs REGISTER.json
//
// It is not evidence about a physical controller's radio, or about any
// controller whose layout Android or the web view maps differently.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [registerPath] = process.argv.slice(2);
if (!registerPath || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/android-gamepad.mjs REGISTER.json");
}
const { chromium } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
const adbPath = process.env.ADB ?? "adb";
const adb = (...args) => execFileSync(adbPath, args, { encoding: "utf8" });
const pause = ms => new Promise(done => setTimeout(done, ms));
const app = "com.movingwoo.wfeature";
const output = resolve("var/acceptance", `android-gamepad-${Date.now()}`);
mkdirSync(output, { recursive: true });
const result = { checks: [], notes: [] };

// The descriptor file writes its bytes in hexadecimal, which JSON does not.
const register = JSON.parse(readFileSync(registerPath, "utf8").replace(/0x([0-9a-fA-F]+)/g, (_, h) => String(parseInt(h, 16))));
if (register.vid !== 0x045e || register.pid !== 0x02fd) {
  throw new Error("expects the Xbox One S Bluetooth register command (045e:02fd); the presses below are in its report layout");
}

// Handset codes the page sends, as in app.js's table.
const OK = 148, CLR = 8, UP = 141, LEFT = 142, RIGHT = 145, DOWN = 146, CALL = 10, MENU = -6, SOFT2 = -7;
const STAR = 42, HASH = 35, KEY = n => 48 + n;

let browser, hid;
try {
  // A phone opened by a touch is in touch mode, where no view has focus until
  // one is touched. The tap is on the status bar, outside the app's window, so
  // the app starts the way it does for a person and its web view is not given
  // focus by the tap.
  adb("shell", "am", "force-stop", app);
  adb("shell", "input", "tap", "5", "5");
  adb("shell", "am", "start", "-W", "-n", `${app}/.MainActivity`);
  const touchMode = /mInTouchMode=true/.test(adb("shell", "dumpsys", "window"));
  result.notes.push(`touch mode at launch: ${touchMode}`);

  let socket;
  for (let i = 0; i < 60 && !socket; i++) {
    socket = adb("shell", "cat", "/proc/net/unix").match(/webview_devtools_remote_\d+/)?.[0];
    if (!socket) await pause(250);
  }
  if (!socket) throw new Error("the app's web view did not open a DevTools socket");
  adb("forward", "tcp:9333", `localabstract:${socket}`);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9333");
  const context = browser.contexts()[0];
  let page = context.pages()[0];
  for (let i = 0; i < 40 && !page; i++) {
    await pause(250);
    page = context.pages()[0];
  }
  await page.waitForSelector("#game-start", { timeout: 30000 });
  const hook = () => {
    window.keyMessages = [];
    window.padEvents = [];
    const send = WebSocket.prototype.send;
    WebSocket.prototype.send = function(data) {
      try {
        const message = JSON.parse(data);
        if (message.kind === "key") window.keyMessages.push(`${message.action === "press" ? "+" : "-"}${message.code}`);
      } catch {}
      return send.call(this, data);
    };
    addEventListener("gamepadconnected", event => window.padEvents.push(event.gamepad.mapping));
  };
  await context.addInitScript(hook);

  // The authored fixture, added the way the page's own button adds a game.
  const zip = readFileSync("internal/platform/skt/testdata/canvas-skt.zip").toString("base64");
  const status = await page.evaluate(async bytes => {
    const body = Uint8Array.from(atob(bytes), c => c.charCodeAt(0));
    return (await fetch("api/games?name=android-gamepad-check.zip", { method: "POST", body })).status;
  }, zip);
  assert.equal(status, 200, "the fixture is added");
  await page.reload();
  await page.waitForSelector("#game-start");
  const game = await page.locator("#game-select option").evaluateAll(list =>
    list.map(option => option.value).find(value => value.includes("android-gamepad-check")));
  await page.locator("#game-select").selectOption(game);
  // Started from inside the page: a DevTools click is input to the web view,
  // which focuses it the way a touch would and hides what is checked below.
  await page.evaluate(() => document.getElementById("game-start").click());
  await page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden"), null, { timeout: 30000 });
  await pause(1000);

  const where = () => page.evaluate(() => `${location.href} ${history.length}`);
  const resumed = () =>
    /ResumedActivity: ActivityRecord\{[^}]*com\.movingwoo\.wfeature\/\.MainActivity/.test(adb("shell", "dumpsys", "activity", "activities"));
  const before = await where();

  hid = spawn(adbPath, ["shell", "hid", "-"], { stdio: ["pipe", "pipe", "pipe"] });
  let hidOutput = "";
  hid.stdout.on("data", data => { hidOutput += data; });
  hid.stdin.write(JSON.stringify(register) + "\n");
  await pause(2000);

  // Report 1: four 16-bit stick axes centred at 0x7fff, two 10-bit triggers,
  // the hat (1 up, clockwise to 8, 0 none), then three bytes of buttons.
  const rest = () => [1, 0xff, 0x7f, 0xff, 0x7f, 0xff, 0x7f, 0xff, 0x7f, 0, 0, 0, 0, 0, 0, 0, 0];
  const withByte = (n, value) => { const r = rest(); r[n] = value; return r; };
  const stick = (first, x, y) => {
    const r = rest();
    r.splice(first, 4, x & 0xff, x >> 8, y & 0xff, y >> 8);
    return r;
  };
  const trigger = (first, level) => { const r = rest(); r[first] = level & 0xff; r[first + 1] = level >> 8; return r; };
  const report = async bytes => {
    hid.stdin.write(JSON.stringify({ id: register.id, command: "report", report: bytes }) + "\n");
    await pause(250);
  };
  const take = () => page.evaluate(() => window.keyMessages.splice(0));
  const press = async (bytes, expected, why) => {
    await report(bytes);
    await report(rest());
    assert.deepEqual(await take(), expected, why);
  };

  await report(rest());
  // The web view lets the first press after a page load or a connection go by
  // without showing the controller; the second is the first the page sees.
  // Home is pressed for it because it sends nothing either way.
  await report(withByte(15, 16));
  await report(rest());
  await pause(300);
  result.notes.push(`first press showed the controller: ${(await page.evaluate(() => window.padEvents.length)) > 0}`);
  await press(withByte(14, 1), [`+${OK}`, `-${OK}`], "A, with no touch on the screen since launch");
  assert.deepEqual(await page.evaluate(() => window.padEvents), ["standard"], "read in the standard layout");
  result.checks.push("the controller reaches the page without a touch, in the standard layout");

  await press(withByte(14, 2), [`+${CLR}`, `-${CLR}`], "B is 취소");
  await pause(400);
  assert.equal(await where(), before, "B does not walk the page's history");
  assert.ok(resumed(), "B does not leave the app");
  result.checks.push("B sends 취소 and is never Android's BACK");

  await press(withByte(14, 8), [`+${KEY(5)}`, `-${KEY(5)}`], "X");
  await press(withByte(14, 16), [`+${KEY(0)}`, `-${KEY(0)}`], "Y");
  await press(withByte(14, 64), [`+${MENU}`, `-${MENU}`], "LB");
  await press(withByte(14, 128), [`+${SOFT2}`, `-${SOFT2}`], "RB");
  await press(withByte(16, 1), [`+${CALL}`, `-${CALL}`], "View");
  result.checks.push("face buttons, shoulders and View");

  await press(withByte(13, 1), [`+${UP}`, `-${UP}`], "hat up");
  await press(withByte(13, 3), [`+${RIGHT}`, `-${RIGHT}`], "hat right");
  await press(withByte(13, 5), [`+${DOWN}`, `-${DOWN}`], "hat down");
  await press(withByte(13, 7), [`+${LEFT}`, `-${LEFT}`], "hat left");
  await press(withByte(13, 8), [`+${UP}`, `+${LEFT}`, `-${UP}`, `-${LEFT}`], "hat up-left");
  await press(stick(1, 0, 0x7fff), [`+${LEFT}`, `-${LEFT}`], "left stick");
  await press(stick(5, 0x7fff, 0), [`+${KEY(2)}`, `-${KEY(2)}`], "right stick up");
  await press(stick(5, 0x1000, 0x1000), [`+${KEY(1)}`, `-${KEY(1)}`], "right stick up-left");
  await press(trigger(9, 0x3ff), [`+${STAR}`, `-${STAR}`], "LT");
  await press(trigger(11, 0x3ff), [`+${HASH}`, `-${HASH}`], "RT");
  result.checks.push("direction pad, both sticks and both triggers");

  const settings = () => page.evaluate(() => document.getElementById("settings-panel").classList.contains("visible"));
  const shown = await settings();
  await press(withByte(15, 8), [], "Menu sends no handset key");
  assert.equal(await settings(), !shown, "Menu toggles the settings");
  await press(withByte(15, 8), []);
  assert.equal(await settings(), shown);
  await press(withByte(15, 16), [], "Home sends nothing");
  assert.ok(resumed(), "nor leaves the app");
  result.checks.push("Menu toggles the settings, Home does nothing");

  // The controller's motor, through the Gamepad API: the platform's driver
  // turns it into an output report the virtual device prints.
  hidOutput = "";
  const played = await page.evaluate(() => {
    const pad = Array.from(navigator.getGamepads()).find(Boolean);
    return pad?.vibrationActuator?.playEffect("dual-rumble", { duration: 300, strongMagnitude: 1, weakMagnitude: 0.5 }) ?? "none";
  });
  await pause(500);
  assert.equal(played, "complete");
  assert.match(hidOutput, /"reportType":1,"reportData":\[3,/, "the rumble reached the controller as an output report");
  result.checks.push("the controller's motor rumbles");

  // The phone's own motor, which the page drives with navigator.vibrate.
  const vibrations = () => (adb("shell", "dumpsys", "vibrator_manager").match(/\| *usage: \w+ *\| *com\.movingwoo\.wfeature/g) ?? []).length;
  const vibratedBefore = vibrations();
  assert.equal(await page.evaluate(() => navigator.vibrate(300)), true);
  await pause(800);
  assert.ok(vibrations() > vibratedBefore, "navigator.vibrate reached the phone's motor");
  result.checks.push("the phone's motor vibrates");

  await page.evaluate(async path => {
    await fetch(`api/games?game=${encodeURIComponent(path)}`, { method: "DELETE" });
  }, game);
  console.log(JSON.stringify(result, null, 2));
} finally {
  hid?.stdin.end();
  await pause(500);
  await browser?.close().catch(() => {});
  try { adb("forward", "--remove", "tcp:9333"); } catch {}
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
}
