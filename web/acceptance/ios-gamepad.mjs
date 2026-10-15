// Opt-in check of the iOS app with a game controller, in the simulator, on a
// Mac with Xcode and an iOS simulator runtime. Nothing here needs a phone or a
// signature.
//
// It builds the app for the simulator with mobile/ios/acceptance/ linked in
// beside the unchanged App.swift, makes a temporary simulator device, adds the
// authored fixture to the app's games, and runs the app twice. The driver
// connects a GCVirtualController, which is a real GameController device as far
// as WebKit is concerned, presses it programmatically, and prints the keys the
// page sent. The device is deleted afterwards, so the simulators already on the
// Mac are left as they were.
//
//   node web/acceptance/ios-gamepad.mjs
//
// IOS_RUNTIME picks the runtime (an identifier from `xcrun simctl list
// runtimes`), the newest by default. An app built with the iOS 27 SDK starts
// on iOS 27 only with the scene life cycle; the script says so when the app
// stops at launch. It is not evidence about a physical controller's pairing.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

const run = (command, args, options = {}) => execFileSync(command, args, { encoding: "utf8", ...options });
const xcrun = (...args) => run("xcrun", args);
const pause = ms => new Promise(done => setTimeout(done, ms));
const bundle = "com.movingwoo.wfeature";
const work = resolve("build/ios-gamepad");
const output = resolve("var/acceptance", `ios-gamepad-${Date.now()}`);
mkdirSync(output, { recursive: true });
const result = { runtime: "", checks: [] };

// Handset codes the page sends, as in app.js's table.
const OK = 148, CLR = 8, UP = 141, LEFT = 142, RIGHT = 145, DOWN = 146, MENU = -6, SOFT2 = -7;
const STAR = 42, HASH = 35, KEY = n => 48 + n;

const versionOf = runtime => runtime.version.split(".").map(Number);
const newer = (a, b) => {
  const [x, y] = [versionOf(a), versionOf(b)];
  for (let i = 0; i < Math.max(x.length, y.length); i++) if ((x[i] ?? 0) !== (y[i] ?? 0)) return (x[i] ?? 0) - (y[i] ?? 0);
  return 0;
};
const runtimes = JSON.parse(xcrun("simctl", "list", "runtimes", "-j")).runtimes
  .filter(runtime => runtime.platform === "iOS" && runtime.isAvailable)
  .sort(newer);
const runtime = process.env.IOS_RUNTIME
  ? runtimes.find(candidate => candidate.identifier === process.env.IOS_RUNTIME)
  : runtimes.at(-1);
if (!runtime) throw new Error(`no iOS simulator runtime${process.env.IOS_RUNTIME ? ` ${process.env.IOS_RUNTIME}` : ""}`);
const deviceType = runtime.supportedDeviceTypes.find(type => type.productFamily === "iPhone");
result.runtime = `${runtime.identifier} on ${deviceType.name}`;

// The app, built the way mobile/ios/build.sh builds it, for the simulator.
const sdk = xcrun("--sdk", "iphonesimulator", "--show-sdk-path").trim();
const sdkVersion = xcrun("--sdk", "iphonesimulator", "--show-sdk-version").trim();
const clang = xcrun("--sdk", "iphonesimulator", "-f", "clang").trim();
const app = join(work, "wfeature.app");
rmSync(work, { recursive: true, force: true });
mkdirSync(app, { recursive: true });
run("go", ["build", "-trimpath", "-buildmode=c-archive", "-o", join(work, "libwfeature.a"), "./mobile/ios/lib"], {
  env: { ...process.env, CGO_ENABLED: "1", GOOS: "ios", GOARCH: "arm64",
    CC: `${clang} -arch arm64 -isysroot ${sdk} -mios-simulator-version-min=17.0` },
});
writeFileSync(join(work, "bridge.h"), '#include "libwfeature.h"\n');
xcrun("--sdk", "iphonesimulator", "clang", "-arch", "arm64", "-isysroot", sdk, "-mios-simulator-version-min=17.0",
  "-c", "mobile/ios/acceptance/load.c", "-o", join(work, "load.o"));
xcrun("--sdk", "iphonesimulator", "swiftc", "-target", "arm64-apple-ios17.0-simulator", "-sdk", sdk,
  "-swift-version", "5", "-import-objc-header", join(work, "bridge.h"), "-I", work, "-Onone", "-parse-as-library",
  "-suppress-warnings", "-framework", "UIKit", "-framework", "WebKit", "-framework", "Foundation",
  "-framework", "GameController", "mobile/ios/src/App.swift", "mobile/ios/acceptance/PadDriver.swift",
  join(work, "load.o"), join(work, "libwfeature.a"), "-o", join(app, "wfeature"));
writeFileSync(join(app, "Info.plist"), readFileSync("mobile/ios/Info.plist", "utf8")
  .replaceAll("__VERSION__", "ios-gamepad-check")
  .replaceAll("__BUILD_NUMBER__", "1")
  .replaceAll("__SDK__", sdkVersion)
  .replace("<string>iphoneos</string>", "<string>iphonesimulator</string>")
  .replace(`<string>iphoneos${sdkVersion}</string>`, `<string>iphonesimulator${sdkVersion}</string>`)
  .replace("<string>iPhoneOS</string>", "<string>iPhoneSimulator</string>"));
run("codesign", ["--force", "--sign", "-", "--timestamp=none", app]);

let device;
try {
  device = xcrun("simctl", "create", `wfeature-ios-gamepad-${Date.now()}`, deviceType.identifier, runtime.identifier).trim();
  xcrun("simctl", "boot", device);
  xcrun("simctl", "bootstatus", device, "-b");
  xcrun("simctl", "install", device, app);
  const games = join(xcrun("simctl", "get_app_container", device, bundle, "data").trim(), "Documents", "games");
  mkdirSync(games, { recursive: true });
  copyFileSync("internal/platform/skt/testdata/canvas-skt.zip", join(games, "ios-gamepad-check.zip"));

  // One run of the app: every PADCHECK line, by step.
  const launch = async mode => {
    const child = spawn("xcrun", ["simctl", "launch", "--console-pty", "--terminate-running-process", device, bundle], {
      env: { ...process.env, SIMCTL_CHILD_PADCHECK_MODE: mode },
    });
    let text = "";
    child.stdout.on("data", data => { text += data; });
    child.stderr.on("data", data => { text += data; });
    for (let i = 0; i < 360 && child.exitCode === null && !/"step":"(done|failed)"/.test(text); i++) await pause(500);
    child.kill();
    writeFileSync(join(output, `${mode}.log`), text);
    const steps = Object.fromEntries(text.split(/\r?\n/)
      .filter(line => line.startsWith("PADCHECK "))
      .map(line => JSON.parse(line.slice("PADCHECK ".length)))
      .map(({ step, value }) => [step, value]));
    if (Object.keys(steps).length === 0) {
      const said = xcrun("simctl", "spawn", device, "log", "show", "--last", "3m", "--style", "compact",
        "--predicate", 'process == "wfeature" AND eventMessage CONTAINS "failed to launch"');
      throw new Error(said.includes("UIScene")
        ? `the app stopped at launch: ${runtime.identifier} starts an app built with this SDK only with the scene life cycle`
        : "the app printed nothing; see the log in the output directory");
    }
    if (steps.failed) throw new Error(`the driver gave up: ${steps.failed}`);
    return steps;
  };

  const pressed = code => [`+${code}`, `-${code}`];
  const plain = await launch("plain");
  assert.ok(String(plain.start).includes("ios-gamepad-check"), `start: ${plain.start}`);
  assert.equal(plain.running, true);
  assert.equal(plain["first responder at load"], "WKContentView", "the app hands the web view first responder");
  assert.ok(plain["page events"].includes("standard"), "the page sees a controller in the standard layout");
  result.checks.push("the web view is first responder and the page sees the controller as standard");
  const expected = {
    "A (first)": OK, A: OK, B: CLR, X: KEY(5), Y: KEY(0), LB: MENU, RB: SOFT2, LT: STAR, RT: HASH,
    "pad up": UP, "pad right": RIGHT, "pad down": DOWN, "pad left": LEFT,
    "left stick left": LEFT, "right stick up": KEY(2), "right stick up-left": KEY(1),
  };
  for (const [step, code] of Object.entries(expected)) assert.deepEqual(plain[step], pressed(code), step);
  result.checks.push("every face button, shoulder and trigger, the direction pad and both sticks, from the first press");

  const resign = await launch("resign");
  assert.equal(resign["first responder before the controller"], "none");
  for (const step of Object.keys(expected)) assert.deepEqual(resign[step], [], `${step} without first responder`);
  assert.equal(resign["first responder after taking it back"], "WKContentView");
  assert.deepEqual(resign["A after taking it back"], pressed(OK));
  result.checks.push("without first responder nothing reaches the page, and taking it back is the cure");
  console.log(JSON.stringify(result, null, 2));
} finally {
  if (device) {
    try { xcrun("simctl", "shutdown", device); } catch {}
    try { xcrun("simctl", "delete", device); } catch {}
  }
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
}
