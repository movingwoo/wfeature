// Opt-in check of what the iOS app does when it leaves the screen, in the
// simulator, on a Mac with Xcode and an iOS simulator runtime. Nothing here
// needs a phone or a signature.
//
// Leaving the app saves nothing and closes nothing (docs/mobile.md, "When the
// app leaves the screen"). The script builds the app for the simulator with
// mobile/ios/acceptance/ linked in beside the unchanged App.swift, makes a
// temporary simulator device and runs the app once. The driver adds two
// authored fixtures through the page's own add-game request and runs a cycle
// for each: it starts the game from inside the page and sets a marker in the
// page; the script opens Settings, which sends the app to the background,
// waits 25 seconds — longer than the 15-second grace a background drain would
// give the game — and opens the app again. Meanwhile it reads which
// process listens on the app's port, asks /api/status while the app's own
// process is the one listening, and lists the save tree in the app's
// container. A cycle passes when the page kept its marker, the app's server
// listened throughout and answered before and after, the game that was
// running is the one that came back, and nothing in the save tree appeared or
// changed. The canvas fixture shows its picture kept and a key still reaching
// it. The checkpoint fixture wrote an XFile at its start and keeps it open,
// which closing the game would store, so its cycle is the one that would see
// a save made because the app went away. RunningBoard's record of the app —
// when iOS suspended and resumed it — is kept beside the report. The device is
// deleted afterwards, so the simulators already on the Mac are left as they
// were.
//
//   node web/acceptance/ios-lifecycle.mjs
//
// IOS_RUNTIME picks the runtime (an identifier from `xcrun simctl list
// runtimes`), the newest by default. IOS_APP_SWIFT builds another App.swift in
// place of mobile/ios/src/App.swift, for a control run against a build that
// does something when it leaves the screen; that run should fail. The
// simulator shares the Mac's network, so while the app runs it holds
// 127.0.0.1:11541 on the Mac: a desktop server on that port keeps the
// connections it has, but new IPv4 loopback ones reach the app until the
// device is gone. It is not evidence about a phone, where iOS also ends a
// suspended app on its own.
import { execFileSync, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, lstatSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import net from "node:net";
import { join, relative, resolve } from "node:path";

const run = (command, args, options = {}) => execFileSync(command, args, { encoding: "utf8", ...options });
const xcrun = (...args) => run("xcrun", args);
const pause = ms => new Promise(done => setTimeout(done, ms));
const bundle = "com.movingwoo.wfeature";
// mobile/ios/lib/lib.go's serverPort, which the app's server always takes.
const port = 11541;
const away = 25000;
const appSource = resolve(process.env.IOS_APP_SWIFT ?? "mobile/ios/src/App.swift");
const work = resolve("build/ios-lifecycle");
const output = resolve("var/acceptance", `ios-lifecycle-${Date.now()}`);
mkdirSync(output, { recursive: true });
const result = { runtime: "", app: appSource, pid: 0, portBeforeLaunch: [], added: null, cycles: [], failures: [] };

// One cycle per fixture; keys are named by the page's default keyboard table,
// where phone key 5 is W and 1 is 1. The canvas fixture paints yellow while 5
// is down and cyan once it is let go, black while 1 is down, and magenta when
// a held key repeats. The checkpoint fixture paints 654321 from its first key
// on.
const cycles = [
  { name: "ios-lifecycle-canvas.zip", fixture: "internal/platform/skt/testdata/canvas-skt.zip",
    before: ["KeyW"], kept: "00ffff", probe: "Digit1", pressed: "000000" },
  { name: "ios-lifecycle-checkpoint.zip", fixture: "internal/platform/skt/testdata/checkpoint-skt.zip",
    before: ["KeyW"], kept: "654321", probe: null },
];

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
  "-c", "mobile/ios/acceptance/lifecycle-load.c", "-o", join(work, "load.o"));
xcrun("--sdk", "iphonesimulator", "swiftc", "-target", "arm64-apple-ios17.0-simulator", "-sdk", sdk,
  "-swift-version", "5", "-import-objc-header", join(work, "bridge.h"), "-I", work, "-Onone", "-parse-as-library",
  "-suppress-warnings", "-framework", "UIKit", "-framework", "WebKit", "-framework", "Foundation",
  appSource, "mobile/ios/acceptance/LifecycleDriver.swift",
  join(work, "load.o"), join(work, "libwfeature.a"), "-o", join(app, "wfeature"));
writeFileSync(join(app, "Info.plist"), readFileSync("mobile/ios/Info.plist", "utf8")
  .replaceAll("__VERSION__", "ios-lifecycle-check")
  .replaceAll("__BUILD_NUMBER__", "1")
  .replaceAll("__SDK__", sdkVersion)
  .replace("<string>iphoneos</string>", "<string>iphonesimulator</string>")
  .replace(`<string>iphoneos${sdkVersion}</string>`, `<string>iphonesimulator${sdkVersion}</string>`)
  .replace("<string>iPhoneOS</string>", "<string>iPhoneSimulator</string>"));
run("codesign", ["--force", "--sign", "-", "--timestamp=none", app]);

// Who listens on the port, seen from the Mac: a suspended app still holds its
// socket, and a stopped server does not.
const listeners = () => {
  let text = "";
  try {
    text = run("lsof", ["-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-F", "pcn"]);
  } catch (error) {
    text = error.stdout ?? "";
  }
  const found = [];
  let pid = 0, command = "";
  for (const line of text.split("\n")) {
    if (line.startsWith("p")) pid = Number(line.slice(1));
    else if (line.startsWith("c")) command = line.slice(1);
    else if (line.startsWith("n")) found.push({ pid, command, address: line.slice(1) });
  }
  return found;
};

// One GET /api/status. The kernel accepts a connection for a process iOS has
// suspended, and nothing answers it: that is "silent", not a refusal.
const status = () => new Promise(done => {
  const socket = net.connect({ host: "127.0.0.1", port });
  let connected = false, text = "";
  const finish = answer => { clearTimeout(timer); socket.destroy(); done(answer); };
  const timer = setTimeout(() => finish({ state: connected ? "silent" : "no connection" }), 2000);
  socket.on("connect", () => {
    connected = true;
    socket.write(`GET /api/status HTTP/1.1\r\nHost: 127.0.0.1:${port}\r\nConnection: close\r\n\r\n`);
  });
  socket.on("data", data => { text += data; });
  socket.on("end", () => finish({
    state: "answered",
    code: Number(/^HTTP\/1\.\d (\d+)/.exec(text)?.[1] ?? 0),
    pid: Number(/"pid":(\d+)/.exec(text)?.[1] ?? 0),
    version: /"version":"([^"]*)"/.exec(text)?.[1] ?? "",
  }));
  socket.on("error", error => finish({ state: error.code === "ECONNREFUSED" ? "refused" : `error ${error.code}` }));
});

// Every directory and file under the save root, a file with its size, time
// and contents' digest.
const saveTree = root => {
  const entries = [];
  const walk = directory => {
    let names = [];
    try { names = readdirSync(directory).sort(); } catch { return; }
    for (const name of names) {
      const path = join(directory, name);
      const info = lstatSync(path);
      if (info.isDirectory()) {
        entries.push(`${relative(root, path)}/`);
        walk(path);
      } else {
        const digest = info.isFile() ? createHash("sha256").update(readFileSync(path)).digest("hex").slice(0, 16) : "not a file";
        entries.push(`${relative(root, path)} ${info.size}B ${new Date(info.mtimeMs).toISOString()} ${digest}`);
      }
    }
  };
  walk(root);
  return entries;
};

let saves = "";
const look = async () => {
  const listening = listeners();
  const own = listening.some(entry => entry.pid === result.pid && entry.address === `127.0.0.1:${port}`);
  return {
    at: new Date().toISOString(),
    listening: listening.map(entry => `${entry.command}[${entry.pid}] ${entry.address}`),
    own,
    // Only the app is asked: with its socket gone the request would reach
    // whatever else holds the port on the Mac.
    status: own ? await status() : { state: "not asked: the app does not listen" },
    saves: saveTree(saves),
  };
};

const near = (picture, expected) => typeof picture === "string" && /^[0-9a-f]{6}$/.test(picture) &&
  [16, 8, 0].every(shift => Math.abs(((parseInt(picture, 16) >> shift) & 255) - ((parseInt(expected, 16) >> shift) & 255)) <= 24);

const judge = (cycle, record) => {
  const checks = [];
  const check = (ok, what, detail) => {
    checks.push({ ok: Boolean(ok), what, ...(detail === undefined ? {} : { detail }) });
    if (!ok) result.failures.push(`${cycle.name}: ${what}`);
  };
  const before = record.ready.page ?? {}, after = record.after.page ?? {};
  check(before.playing && near(before.picture, cycle.kept), `before leaving, the game runs and shows ${cycle.kept}`,
    { picture: before.picture });

  const identity = page => ({ marker: page.marker, origin: page.origin, loads: page.loads });
  check(after.marker === before.marker && after.origin === before.origin && after.loads === before.loads,
    "the page was not loaded again: its marker, time origin and load count are the ones from before",
    { before: identity(before), after: identity(after) });

  const looks = [record.before, ...record.during, record.afterLook];
  check(looks.every(entry => entry.own), "the app's process held 127.0.0.1:11541 throughout",
    looks.map(entry => `${entry.at} ${entry.listening.join(", ") || "nobody"}`));
  check([record.before, record.afterLook].every(entry => entry.status.state === "answered" && entry.status.pid === result.pid),
    "/api/status answered from the app's process before and after");
  check(record.during.every(entry => entry.status.state === "silent" ||
    (entry.status.state === "answered" && entry.status.pid === result.pid)),
  "/api/status while away was the app's answer, or none while iOS kept the app suspended; never a closed port");
  const reopened = Number(/: (\d+)/.exec(record.reopened ?? "")?.[1] ?? 0);
  check([record.ready.pid, record.left, reopened, record.after.pid].every(pid => pid === result.pid),
    "one app process throughout, which opening the app again brought back", { launched: result.pid, reopened: record.reopened });

  const game = before.log?.findLast(([, what]) => / got started /.test(what))?.[1].replace(/^.* got started /, "");
  const since = after.marker === before.marker ? (after.log ?? []).slice(before.log?.length ?? 0) : after.log ?? [];
  const answers = since.filter(([, what]) => / got (started|resumed)\b/.test(what));
  const closed = since.some(([, what]) => / closed /.test(what));
  check(after.playing === true, "a game is running after the return");
  check(!since.some(([, what]) => / sent start$/.test(what)), "nothing started a game after the marker");
  check(answers.length ? answers.at(-1)[1].endsWith(` got started ${game}`) : !closed,
    "the game that came back is the one that was running: the page's resume was answered with it, or its socket never closed",
    since.map(([, what]) => what));
  check(near(after.picture, cycle.kept), `the picture after the return is the game's own from before (${cycle.kept})`,
    { picture: after.picture });
  if (cycle.probe) {
    check((record.alive.pressed ?? []).some(picture => near(picture, cycle.pressed)) && near(record.alive.released, cycle.kept),
      `a key after the return reaches the game (${cycle.pressed} while it is down, ${cycle.kept} again once let go)`, record.alive);
  }

  const reference = JSON.stringify(record.before.saves);
  const changed = [...record.during, record.afterLook].filter(entry => JSON.stringify(entry.saves) !== reference);
  check(changed.length === 0, "nothing in the save tree appeared or changed while the app was away",
    changed.length ? { before: record.before.saves, changed: changed[0].saves, at: changed[0].at } : record.before.saves);
  return checks;
};

let device, child, failure, consoleText = "";
// The device goes whichever way the run ends, an interrupted one included.
const takeDown = () => {
  child?.kill();
  if (device) {
    try { xcrun("simctl", "shutdown", device); } catch {}
    try { xcrun("simctl", "delete", device); } catch {}
    device = undefined;
  }
};
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.once(signal, () => {
    takeDown();
    process.exit(1);
  });
}
try {
  device = xcrun("simctl", "create", `wfeature-ios-lifecycle-${Date.now()}`, deviceType.identifier, runtime.identifier).trim();
  xcrun("simctl", "boot", device);
  xcrun("simctl", "bootstatus", device, "-b");
  xcrun("simctl", "install", device, app);
  const container = xcrun("simctl", "get_app_container", device, bundle, "data").trim();
  const handoff = join(container, "tmp", "lifecheck");
  mkdirSync(handoff, { recursive: true });
  for (const cycle of cycles) copyFileSync(cycle.fixture, join(handoff, cycle.name));
  saves = join(container, "Documents", "savedata");
  result.portBeforeLaunch = listeners().map(entry => `${entry.command}[${entry.pid}] ${entry.address}`);

  const steps = [];
  let consumed = 0, pending = "";
  child = spawn("xcrun", ["simctl", "launch", "--console-pty", "--terminate-running-process", device, bundle], {
    env: { ...process.env, SIMCTL_CHILD_LIFECHECK_PLAN: JSON.stringify(cycles.map(({ name, before, probe }) => ({ name, before, probe }))) },
  });
  const take = data => {
    consoleText += data;
    pending += data;
    const lines = pending.split(/\r?\n/);
    pending = lines.pop();
    for (const line of lines) {
      const at = line.indexOf("LIFECHECK ");
      if (at >= 0) {
        try { steps.push(JSON.parse(line.slice(at + "LIFECHECK ".length))); } catch {}
      }
      const launched = /com\.movingwoo\.wfeature: (\d+)/.exec(line);
      if (launched && !result.pid) result.pid = Number(launched[1]);
    }
  };
  child.stdout.on("data", take);
  child.stderr.on("data", take);
  const next = async (step, seconds) => {
    const deadline = Date.now() + seconds * 1000;
    while (Date.now() < deadline) {
      const found = steps.findIndex((entry, index) => index >= consumed && entry.step === step);
      if (found >= 0) {
        consumed = found + 1;
        return steps[found].value;
      }
      const failed = steps.find(entry => entry.step === "failed");
      if (failed) throw new Error(`the driver gave up: ${failed.value}`);
      if (child.exitCode !== null) throw new Error(`the app's console ended before "${step}"`);
      await pause(200);
    }
    throw new Error(`no "${step}" from the driver within ${seconds} s; see the console in ${output}`);
  };

  const pid = await next("pid", 300);
  if (!result.pid) result.pid = pid;
  if (pid !== result.pid) throw new Error(`the driver runs in pid ${pid}, simctl launched ${result.pid}`);
  result.added = await next("added", 120);
  if (Object.values(result.added).some(code => code !== 200)) throw new Error(`adding the fixtures: ${JSON.stringify(result.added)}`);

  for (const [index, cycle] of cycles.entries()) {
    const record = { name: cycle.name, ready: await next("ready", 300) };
    record.before = await look();
    xcrun("simctl", "launch", device, "com.apple.Preferences");
    record.left = await next("background", 60);
    const start = Date.now();
    record.during = [];
    while (Date.now() - start < away) {
      record.during.push({ second: Math.round((Date.now() - start) / 1000), ...await look() });
      await pause(2500);
    }
    record.reopened = xcrun("simctl", "launch", device, bundle).trim();
    await next("foreground", 60);
    record.after = await next("after", 120);
    record.afterLook = await look();
    writeFileSync(join(handoff, `continue-${index + 1}`), "");
    record.alive = await next("alive", 60);
    record.checks = judge(cycle, record);
    result.cycles.push(record);
  }
  await next("done", 60);
} catch (error) {
  failure = error;
} finally {
  // What iOS did with the app, as RunningBoard recorded it: when it suspended
  // and resumed the process, and any background time the app asked for.
  if (device) {
    try {
      writeFileSync(join(output, "runningboard.log"), run("xcrun", ["simctl", "spawn", device, "log", "show", "--last", "20m",
        "--style", "compact", "--predicate", `process == "runningboardd" AND eventMessage CONTAINS "${bundle}"`],
      { maxBuffer: 64 << 20, stdio: ["ignore", "pipe", "ignore"] }));
    } catch {}
  }
  takeDown();
  writeFileSync(join(output, "console.log"), consoleText);
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
}

// What a reader needs from result.json, in order. Times are the Mac's local
// clock, which is the one the simulator's log is written in.
const clock = iso => {
  const time = new Date(iso);
  return `${[time.getHours(), time.getMinutes(), time.getSeconds()].map(n => String(n).padStart(2, "0")).join(":")}.${String(time.getMilliseconds()).padStart(3, "0")}`;
};
// When the save tree first differed from its listing before leaving, and what
// had appeared (+) or gone (-) by the return, or at that first look when the
// return found it as it was.
const savesChanged = record => {
  const differs = entry => JSON.stringify(entry.saves) !== JSON.stringify(record.before.saves);
  const first = [...record.during, record.afterLook].find(differs);
  if (!first) return "nothing";
  const shown = differs(record.afterLook) ? record.afterLook : first;
  const added = shown.saves.filter(entry => !record.before.saves.includes(entry)).map(entry => `+ ${entry}`);
  const gone = record.before.saves.filter(entry => !shown.saves.includes(entry)).map(entry => `- ${entry}`);
  return `first seen ${clock(first.at)}; ${[...added, ...gone].join(" | ")}`;
};
let suspensions = [];
try {
  const task = new RegExp(`^\\S+ (\\S+) .*\\[app<${bundle.replaceAll(".", "\\.")}[^\\]]*>:${result.pid}\\] (Suspending task|Resuming task)`);
  suspensions = readFileSync(join(output, "runningboard.log"), "utf8").split("\n")
    .map(line => task.exec(line)).filter(Boolean).map(([, time, what]) => `${time.slice(0, 12)} ${what.toLowerCase()}`);
} catch {}
const lines = [`${result.runtime}, ${result.app}, app pid ${result.pid}`,
  `port ${port} before the launch: ${result.portBeforeLaunch.join(", ") || "free"}`,
  `iOS (RunningBoard) for pid ${result.pid}: ${suspensions.join(", ") || "no record"}`];
for (const record of result.cycles) {
  const before = record.ready.page ?? {}, after = record.after.page ?? {};
  lines.push(`${record.name}:`,
    `  page before: marker ${before.marker}, loads ${before.loads}, origin ${before.origin}, picture ${before.picture}`,
    `  page after:  marker ${after.marker}, loads ${after.loads}, origin ${after.origin}, picture ${after.picture}, playing ${after.playing}`,
    `  status before: ${JSON.stringify(record.before.status)}`,
    ...record.during.map(entry => `  away ${String(entry.second).padStart(2)} s (${clock(entry.at)}): ${entry.own ? "the app listens" : "the app does not listen"}; ${JSON.stringify(entry.status)}; ${entry.saves.length} save entries`),
    `  status after: ${JSON.stringify(record.afterLook.status)}`,
    `  saves before: ${record.before.saves.join(" | ") || "none"}`,
    `  saves changed while away: ${savesChanged(record)}`,
    `  key after the return: ${JSON.stringify(record.alive)}`,
    ...record.checks.map(entry => `  ${entry.ok ? "ok    " : "FAILED"} ${entry.what}`));
}
lines.push(`evidence: ${output}`);
process.stdout.write(lines.join("\n") + "\n");
if (failure) throw failure;
if (result.failures.length) throw new Error(`leaving the app did something:\n- ${result.failures.join("\n- ")}`);
