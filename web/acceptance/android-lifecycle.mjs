// Opt-in check of the Android app across configuration changes and its end,
// on an emulator. It force-stops the app, which ends a game in progress; adds
// two test games to the library for the run and takes them off again, while
// what they save stays in the app's saves; and changes the display's rotation,
// its handling of the app's orientation request, its density and size, the
// night mode and the font scale, putting each back as it found it whether the
// checks pass or not. So it is not for a phone somebody plays on.
//
// What it checks is the activity's half of docs/mobile.md, "When the app
// leaves the screen":
//
// - A change the manifest's configChanges declares does not rebuild the
//   activity: the page is the same document, its session socket keeps
//   carrying the game, and the server keeps its pid. The changes are a
//   rotation, the same rotation on a display that ignores the app's portrait
//   request as a large screen does, a density change, a display size change
//   that moves the smallest width, and the night mode. On a phone the plain
//   rotation never reaches the portrait-only activity; the check records that.
//   Android rebuilds an activity for a size change only when it crosses a
//   width or height its resources are qualified by, and the app has no such
//   resources, so the two size changes pass whatever configChanges says; the
//   density change is the one an activity that does not declare it fails.
// - A change it does not declare (the font scale) rebuilds the activity in
//   the same app process: the server keeps its pid, no second one starts, and
//   the new page resumes the game the server kept for it.
// - Finishing the activity (BACK until it finishes) ends the server within a
//   few seconds, and the server's drain closes the game on the way out. The
//   game for that is the repository's destroy-save MIDlet, which writes its
//   record store only from destroyApp; it is finished three times, because
//   whether the drain gets that far can depend on a race. The same drain asked
//   for with POST /api/shutdown is the control. The app is started with
//   `am start`, and Android finishes such a task on BACK. Since Android 12 a
//   root activity started from the home screen is moved to the back on BACK
//   instead of being finished; that path is not exercised here.
//
// The pages are read through the web view's DevTools socket, one page at a
// time, and no DevTools client is attached while a change is made. Playwright
// is not used: its connectOverCDP attaches to every page and turns on focus
// emulation in each, which makes the hidden page of a destroyed activity's web
// view visible again, and that page then takes the game back from the new one.
//
// It expects the app installed (`make mobile-android`, then `adb install -r`)
// and Node 22 or newer, for its WebSocket. ANDROID_SERIAL picks the device as
// adb itself does, and ADB the binary:
//
//   node web/acceptance/android-lifecycle.mjs
//
// Each check is printed, and written with its evidence to
// var/acceptance/android-lifecycle-<time>/result.json; the exit status is
// non-zero when one fails. It is not evidence about a physical phone's own
// changes (a fold, a keyboard, a split screen), which reach the activity
// through the same configChanges entries.
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { crc32 } from "node:zlib";

const adbPath = process.env.ADB ?? "adb";
const adb = (...args) => execFileSync(adbPath, args, { encoding: "utf8" });
const tryAdb = (...args) => {
  try { return adb(...args).trim(); } catch { return ""; }
};
const pause = ms => new Promise(done => setTimeout(done, ms));
const app = "com.movingwoo.wfeature";
// MainActivity.SERVER_PORT: the server listens on the phone's loopback only.
const serverPort = 11541;
const finishes = 3;
const output = resolve("var/acceptance", `android-lifecycle-${Date.now()}`);
mkdirSync(output, { recursive: true });
const result = { device: {}, checks: [], notes: [] };
const check = (name, ok, evidence) => {
  result.checks.push({ name, ok: Boolean(ok), ...evidence });
  console.log(`${ok ? "ok  " : "FAIL"} ${name}`);
};

// The repository's destroy-save MIDlet is a bare JAR, which no platform
// claims; beside a descriptor in a zip it is the package the SKT loader takes,
// as the CLI runs it. Stored entries, so nothing but a CRC is computed here.
const storedZip = entries => {
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const [name, data] of entries) {
    const path = Buffer.from(name);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(0x21, 12);
    local.writeUInt32LE(crc32(data), 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(path.length, 26);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(0x21, 14);
    central.writeUInt32LE(crc32(data), 16);
    central.writeUInt32LE(data.length, 20);
    central.writeUInt32LE(data.length, 24);
    central.writeUInt16LE(path.length, 28);
    central.writeUInt32LE(offset, 42);
    locals.push(local, path, data);
    centrals.push(central, path);
    offset += local.length + path.length + data.length;
  }
  const size = centrals.reduce((sum, part) => sum + part.length, 0);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(size, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, ...centrals, end]);
};
const fixtures = [
  ["android-lifecycle-check.zip", readFileSync("internal/platform/skt/testdata/canvas-skt.zip")],
  ["android-lifecycle-drain.zip", storedZip([
    ["destroy-save.msd", readFileSync("cmd/cli/testdata/DESTROY_SAVE.MF")],
    ["destroy-save.jar", readFileSync("cmd/cli/testdata/destroy-save.jar")],
  ])],
];
// How many times destroyApp has written the fixture's record, wherever the
// server keeps it under the app's save tree.
const drainSaves = () => Number(tryAdb("shell",
  `grep -r -a -o -h written-by-destroyApp /sdcard/Android/data/${app}/files/savedata 2>/dev/null | wc -l`)) || 0;

// Device settings, read before anything changes so each goes back exactly.
const setting = (namespace, key) => tryAdb("shell", "settings", "get", namespace, key);
const putSetting = (namespace, key, value) => value === "null" || value === ""
  ? tryAdb("shell", "settings", "delete", namespace, key)
  : adb("shell", "settings", "put", namespace, key, value);
const display = what => {
  const text = tryAdb("shell", "wm", what);
  return { physical: /Physical \w+: (\S+)/.exec(text)?.[1], override: /Override \w+: (\S+)/.exec(text)?.[1] ?? null };
};
const original = {
  accelerometer: setting("system", "accelerometer_rotation"),
  rotation: setting("system", "user_rotation"),
  fontScale: setting("system", "font_scale"),
  ignoreOrientation: /ignoreOrientationRequest (true|false)/.exec(tryAdb("shell", "wm", "get-ignore-orientation-request"))?.[1] ?? null,
  size: display("size"),
  density: display("density"),
  night: /Night mode: (\w+)/.exec(tryAdb("shell", "cmd", "uimode", "night"))?.[1] ?? null,
};
result.device.settings = original;
const restore = {
  rotation: () => {
    putSetting("system", "user_rotation", original.rotation);
    putSetting("system", "accelerometer_rotation", original.accelerometer);
  },
  ignoreOrientation: () => adb("shell", "wm", "set-ignore-orientation-request", original.ignoreOrientation ?? "reset"),
  size: () => adb("shell", "wm", "size", original.size.override ?? "reset"),
  density: () => adb("shell", "wm", "density", original.density.override ?? "reset"),
  night: () => adb("shell", "cmd", "uimode", "night", original.night),
  fontScale: () => putSetting("system", "font_scale", original.fontScale),
};
const touched = new Set();
const turn = () => {
  touched.add("rotation");
  adb("shell", "settings", "put", "system", "accelerometer_rotation", "0");
  adb("shell", "settings", "put", "system", "user_rotation", original.rotation === "1" ? "3" : "1");
};
const scaled = (size, factor) => size.split("x").map(side => 2 * Math.round(Number(side) * factor / 2)).join("x");
const declared = [
  { name: "rotation", apply: turn, putBack: () => restore.rotation() },
  {
    name: "rotation on a display that ignores the orientation request",
    apply: () => {
      touched.add("ignoreOrientation");
      adb("shell", "wm", "set-ignore-orientation-request", "true");
      turn();
    },
    putBack: () => { restore.rotation(); restore.ignoreOrientation(); },
  },
  {
    name: "density",
    apply: () => {
      touched.add("density");
      adb("shell", "wm", "density", String(Math.round(Number(original.density.override ?? original.density.physical) * 5 / 6)));
    },
    putBack: () => restore.density(),
  },
  {
    name: "display size (smallest width)",
    apply: () => {
      touched.add("size");
      adb("shell", "wm", "size", scaled(original.size.override ?? original.size.physical, 0.85));
    },
    putBack: () => restore.size(),
  },
  // Only a mode that can be put back as it was: a custom schedule cannot.
  ...["yes", "no", "auto"].includes(original.night) ? [{
    name: "night mode",
    apply: () => {
      touched.add("night");
      adb("shell", "cmd", "uimode", "night", original.night === "yes" ? "no" : "yes");
    },
    putBack: () => restore.night(),
  }] : [],
];
const undeclared = {
  name: "font scale",
  apply: () => {
    touched.add("fontScale");
    adb("shell", "settings", "put", "system", "font_scale", original.fontScale === "1.3" ? "1.15" : "1.3");
  },
  putBack: () => restore.fontScale(),
};

// What the system and the app say about the activity: its instance (the app's
// own Activity object, which a rebuild replaces) and the configuration it
// holds, without the sequence number every update moves.
const activity = () => {
  const text = tryAdb("shell", "dumpsys", "activity", "top");
  const at = text.indexOf(`ACTIVITY ${app}/.MainActivity`);
  if (at < 0) return null;
  const rest = text.slice(at);
  const next = rest.indexOf("\n  ACTIVITY ");
  const own = next > 0 ? rest.slice(0, next) : rest;
  const config = (/mCurrentConfig=(\{.*)$/m.exec(own)?.[1] ?? "").replace(/ s\.\d+/, "");
  const summary = [/^\{(\S+)/, /\bsw\d+dp/, /\bw\d+dp/, /\bh\d+dp/, /\b\d+dpi/, /\b(port|land)\b/, /\bnight\b/, /mDisplayRotation=\w+/]
    .map(pattern => pattern.exec(config)?.[pattern.source.startsWith("^") ? 1 : 0]).filter(Boolean).join(" ");
  return { instance: /Local Activity (\w+) State:/.exec(own)?.[1] ?? null, config, summary };
};
const deviceNow = () => tryAdb("shell", "date", "+%s.%3N");
// The server's own lines since a moment on the device's clock. The app logs
// the server's output under its tag, and its own "starting" line when it
// launches one.
const serverLog = since => tryAdb("logcat", "-d", "-v", "epoch", "-t", since, "-s", "wfeature").split("\n")
  .map(line => / I wfeature: starting /.test(line) ? "(the app starts a server)" : /msg="([^"]*)"/.exec(line)?.[1])
  .filter(line => line && /^session |^parked |^\(the app starts|^serving|^shutting/.test(line));
const lifecycleEvents = since => tryAdb("logcat", "-d", "-b", "events", "-v", "epoch", "-t", since).split("\n")
  .filter(line => line.includes(`${app}/.MainActivity`) || line.includes(`${app}.MainActivity`))
  .map(line => /^\s*([\d.]+)\s+\d+\s+\d+\s+\w\s+(\w+):/.exec(line))
  .filter(Boolean)
  .map(([, at, event]) => ({ at: Number(at), event }));

let serverForward = null;
const status = async () => {
  try {
    const response = await fetch(`http://127.0.0.1:${serverForward}/api/status`, { signal: AbortSignal.timeout(3000) });
    return response.ok ? await response.json() : null;
  } catch {
    return null;
  }
};
const servers = () => tryAdb("shell", "pidof", "libwfeature.so").split(/\s+/).filter(Boolean);
const alive = pid => {
  const stat = tryAdb("shell", "cat", `/proc/${pid}/stat`);
  return stat.includes("(libwfeature.so)") && !/\) Z /.test(stat);
};

// The DevTools socket belongs to the app's process, so it is found again
// whenever the process is a different one.
let devtoolsPort = null;
let devtoolsPid = null;
const devtools = async () => {
  const pid = tryAdb("shell", "pidof", app);
  if (!pid) throw new Error("the app is not running");
  if (pid !== devtoolsPid) {
    if (devtoolsPort) tryAdb("forward", "--remove", `tcp:${devtoolsPort}`);
    devtoolsPort = null;
    const socket = `webview_devtools_remote_${pid}`;
    for (let i = 0; i < 80 && !tryAdb("shell", "cat", "/proc/net/unix").includes(socket); i++) await pause(250);
    // A port adb picks, so no forward somebody else made is moved or removed.
    devtoolsPort = adb("forward", "tcp:0", `localabstract:${socket}`).trim();
    devtoolsPid = pid;
  }
  return devtoolsPort;
};
// The app's pages as the web views describe themselves, without attaching to
// any of them.
const pages = async () => {
  try {
    const targets = await (await fetch(`http://127.0.0.1:${await devtools()}/json/list`, { signal: AbortSignal.timeout(3000) })).json();
    return targets.filter(target => target.type === "page" && target.url.startsWith(`http://127.0.0.1:${serverPort}`))
      .map(target => {
        let view = {};
        try { view = JSON.parse(target.description || "{}"); } catch {}
        return { id: target.id, socket: target.webSocketDebuggerUrl, attached: Boolean(view.attached), visible: Boolean(view.visible) };
      });
  } catch {
    return [];
  }
};
// One DevTools session on one page, and nothing else switched on in it.
const sessions = new Set();
const attach = async target => {
  const socket = new WebSocket(target.socket);
  sessions.add(socket);
  await new Promise((done, fail) => {
    const late = setTimeout(() => fail(new Error(`could not attach to ${target.id} in time`)), 10000);
    socket.onopen = () => { clearTimeout(late); done(); };
    socket.onerror = () => { clearTimeout(late); fail(new Error(`could not attach to ${target.id}`)); };
  }).catch(error => {
    sessions.delete(socket);
    socket.close();
    throw error;
  });
  let next = 0;
  const waiting = new Map();
  socket.onmessage = event => {
    const message = JSON.parse(event.data);
    const reply = waiting.get(message.id);
    if (!reply) return;
    waiting.delete(message.id);
    if (message.error) reply.fail(new Error(message.error.message));
    else reply.done(message.result);
  };
  socket.onclose = () => {
    for (const reply of waiting.values()) reply.fail(new Error("the page's DevTools session closed"));
    waiting.clear();
  };
  // Nothing the page is asked waits longer than this for its answer.
  const send = (method, params = {}) => new Promise((done, fail) => {
    const id = ++next;
    const late = setTimeout(() => {
      waiting.delete(id);
      fail(new Error(`${method} had no answer`));
    }, 20000);
    waiting.set(id, {
      done: value => { clearTimeout(late); done(value); },
      fail: error => { clearTimeout(late); fail(error); },
    });
    socket.send(JSON.stringify({ id, method, params }));
  });
  const evaluate = async expression => {
    const { result: value, exceptionDetails } = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text);
    return value.value;
  };
  const until = async (expression, timeout = 30000) => {
    const start = Date.now();
    while (Date.now() - start < timeout) {
      try {
        if (await evaluate(expression)) return true;
      } catch {}
      await pause(250);
    }
    return false;
  };
  const close = () => {
    sessions.delete(socket);
    socket.close();
  };
  return { id: target.id, send, evaluate, until, close };
};
// The page in the web view that is on the activity's window. A destroyed
// activity's web view can linger as another target, not attached to a window.
const shown = async () => {
  for (let i = 0; i < 120; i++) {
    const on = (await pages()).filter(page => page.attached && page.visible);
    if (on.length === 1) return attach(on[0]);
    await pause(250);
  }
  throw new Error("no page of the app is on screen");
};

// Counts what each session socket receives, so a check can tell the socket
// that carried the game before a change from one opened after it.
const hook = () => {
  window.lifecycleSockets = [];
  const Native = WebSocket;
  window.WebSocket = class extends Native {
    constructor(...args) {
      super(...args);
      const counts = { stats: 0, binary: 0, started: 0, occupied: 0, closed: false };
      window.lifecycleSockets.push(counts);
      this.addEventListener("message", event => {
        if (typeof event.data !== "string") {
          counts.binary++;
          return;
        }
        try {
          const message = JSON.parse(event.data);
          if (message.kind === "stats") counts.stats++;
          if (message.kind === "started") counts.started++;
          if (message.occupied) counts.occupied++;
        } catch {}
      });
      this.addEventListener("close", () => { counts.closed = true; });
    }
  };
};
const state = `({
  marker: window.lifecycleMarker ?? null,
  document: performance.timeOrigin,
  sockets: window.lifecycleSockets?.map(counts => ({ ...counts })) ?? null,
  playing: !document.getElementById("restart").classList.contains("hidden"),
  occupied: !document.getElementById("session-return").classList.contains("hidden"),
  viewport: innerWidth + "x" + innerHeight + "@" + devicePixelRatio,
})`;
const playing = `!document.getElementById("restart").classList.contains("hidden")`;
const ready = `!!document.getElementById("game-start") && !document.getElementById("game-start").disabled`;
// Started from inside the page: input from DevTools would be input to the web
// view, which a player's start is not.
const start = game => `(() => {
  const select = document.getElementById("game-select");
  select.value = ${JSON.stringify(game)};
  select.dispatchEvent(new Event("change", { bubbles: true }));
  document.getElementById("game-start").click();
  return select.value;
})()`;

const launch = async () => {
  adb("shell", "am", "force-stop", app);
  adb("shell", "am", "start", "-W", "-n", `${app}/.MainActivity`);
  for (let i = 0; i < 80 && !(await status()); i++) await pause(250);
};
// A page playing the game, with the socket counter in it, made the way a
// player makes one: the page loaded, the game picked and started inside it.
const prepare = async game => {
  for (let attempt = 0; ; attempt++) {
    let page = null;
    try {
      page = await shown();
      const now = await page.evaluate(state);
      if (now.sockets && now.playing) return page;
      await page.send("Page.enable");
      await page.send("Page.addScriptToEvaluateOnNewDocument", { source: `(${hook})()` });
      await page.send("Page.reload");
      await pause(500);
      // The page resumes a game the server kept for it, or comes up ready.
      if (!(await page.until(`(${playing}) || (${ready})`))) throw new Error("the page came up neither playing nor ready");
      if (!(await page.evaluate(playing))) {
        await page.evaluate(start(game));
        if (!(await page.until(playing))) throw new Error("the game did not start");
      }
      return page;
    } catch (error) {
      page?.close();
      // A build that lost its server to a change can show no page; the run
      // goes on from a fresh start rather than ending there.
      if (attempt > 0) throw error;
      result.notes.push(`started the app again before a check: ${String(error.message ?? error).split("\n")[0]}`);
      await launch();
    }
  }
};
// Waits for the activity to take a configuration or to be replaced.
const settle = async (before, timeout) => {
  const begun = Date.now();
  while (Date.now() - begun < timeout) {
    const now = activity();
    if (now && (now.instance !== before?.instance || now.config !== before?.config)) return now;
    await pause(500);
  }
  return activity();
};
const current = sockets => sockets?.findLastIndex(counts => !counts.closed) ?? -1;
// What a step reads before a change, with the page's session still open.
const before = async page => {
  const marker = `lifecycle-${Date.now()}`;
  await page.evaluate(`window.lifecycleMarker = ${JSON.stringify(marker)}`);
  await pause(2500);
  return { marker, target: page.id, activity: activity(), page: await page.evaluate(state), status: await status(), servers: servers() };
};
// What a step reads after one, from whichever page is then on screen.
const after = async since => {
  const page = await shown().catch(() => null);
  const read = {
    target: page?.id ?? null, targets: (await pages()).map(({ attached, visible }) => ({ attached, visible })),
    activity: activity(), page: page ? await page.evaluate(state).catch(() => null) : null,
    status: await status(), servers: servers(), log: serverLog(since), events: lifecycleEvents(since).map(({ event }) => event),
  };
  page?.close();
  return read;
};
const sameServer = (was, is) => was.status?.pid > 0 && is.status?.pid === was.status.pid &&
  is.servers.length === 1 && is.servers[0] === String(was.status.pid) &&
  !is.log.some(line => /^\(the app starts|^serving/.test(line));

let games = [];
try {
  result.device.android = `${tryAdb("shell", "getprop", "ro.build.version.release")} (SDK ${tryAdb("shell", "getprop", "ro.build.version.sdk")})`;
  result.device.webView = /\(([^)]*)\)\s*$/.exec(tryAdb("shell", "dumpsys", "webviewupdate").split("\n")
    .find(line => /Current WebView package/.test(line)) ?? "")?.[1] ?? null;
  const packageInfo = tryAdb("shell", "dumpsys", "package", app);
  result.device.app = `${/versionName=(\S+)/.exec(packageInfo)?.[1]} (code ${/versionCode=(\d+)/.exec(packageInfo)?.[1]})`;
  console.log(JSON.stringify(result.device));

  serverForward = adb("forward", "tcp:0", `tcp:${serverPort}`).trim();
  await launch();
  {
    const page = await shown();
    // An empty library keeps the start button off, so a loaded page is enough.
    if (!(await page.until(`document.readyState === "complete" && !!document.getElementById("game-start")`))) {
      throw new Error("the page did not come up");
    }
    // The authored fixtures, added the way the page's own button adds a game.
    for (const [name, bytes] of fixtures) {
      const added = await page.evaluate(`(async () => {
        const body = Uint8Array.from(atob(${JSON.stringify(bytes.toString("base64"))}), c => c.charCodeAt(0));
        return (await fetch("api/games?name=${name}", { method: "POST", body })).status;
      })()`);
      if (added !== 200) throw new Error(`the fixture ${name} was not added (${added})`);
      // Where the server keeps a game the page added, until the list says.
      games.push(`ext/${name}`);
    }
    await page.send("Page.reload");
    await pause(500);
    await page.until(ready);
    const listed = await page.evaluate(`[...document.querySelectorAll("#game-select option")]
      .map(option => option.value).filter(value => /android-lifecycle-(check|drain)/.test(value))`);
    if (listed.length) games = listed;
    page.close();
  }
  const canvas = games.find(game => game.includes("android-lifecycle-check"));
  const drain = games.find(game => game.includes("android-lifecycle-drain"));
  if (!canvas || !drain) throw new Error(`the test games are not in the library: ${games}`);

  // A: each declared change and its putting back, one at a time.
  for (const change of declared) {
    for (const [step, action] of [[change.name, change.apply], [`${change.name}, put back`, change.putBack]]) {
      const page = await prepare(canvas);
      const was = await before(page);
      page.close();
      const since = deviceNow();
      action();
      const reached = await settle(was.activity, 8000);
      await pause(4500);
      const is = await after(since);
      const socket = current(was.page.sockets);
      const counts = [was.page.sockets[socket] ?? null, is.page?.sockets?.[socket] ?? null];
      const sameActivity = Boolean(is.activity) && is.activity.instance === was.activity?.instance;
      const sameDocument = is.target === was.target && is.page?.marker === was.marker && is.page?.document === was.page.document;
      const sameSocket = socket >= 0 && is.page?.sockets?.length === was.page.sockets.length && counts[1] && !counts[1].closed &&
        counts[1].stats > counts[0].stats && counts[1].binary > counts[0].binary;
      const server = sameServer(was, is);
      check(`A ${step}: the activity takes it, the page and its game go on`,
        sameActivity && sameDocument && sameSocket && server && is.page?.playing, {
          reachedActivity: reached?.config !== was.activity?.config || reached?.instance !== was.activity?.instance,
          activity: [`${was.activity?.instance} ${was.activity?.summary}`, `${is.activity?.instance} ${is.activity?.summary}`],
          viewport: [was.page.viewport, is.page?.viewport ?? null],
          sameActivity, sameDocument, sameSocket, socket: counts,
          server: [was.status?.pid ?? null, is.status?.pid ?? null], servers: is.servers, sameServer: server,
          playing: is.page?.playing ?? null, pages: is.targets, serverLog: is.log, events: is.events,
        });
    }
  }

  // B: the undeclared change and its putting back.
  for (const [step, action] of [[undeclared.name, undeclared.apply], [`${undeclared.name}, put back`, undeclared.putBack]]) {
    const page = await prepare(canvas);
    const was = await before(page);
    page.close();
    const since = deviceNow();
    action();
    let rebuilt = null;
    for (let i = 0; i < 40 && !rebuilt; i++) {
      const now = activity();
      if (now?.instance && now.instance !== was.activity?.instance) rebuilt = now;
      else await pause(500);
    }
    // The new page asks the server for the game it was playing on its own; it
    // is read once it has an answer.
    const asked = await shown().catch(() => null);
    await asked?.until(`(${playing}) || !document.getElementById("session-return").classList.contains("hidden") || (${ready})`);
    asked?.close();
    await pause(1500);
    const is = await after(since);
    const server = sameServer(was, is);
    const resumed = Boolean(is.page?.playing) && !is.page?.occupied && is.log.includes("session resumed");
    check(`B ${step}: the activity is rebuilt, the server stays and the new page resumes the game`,
      Boolean(rebuilt) && server && resumed, {
        activity: [`${was.activity?.instance} ${was.activity?.summary}`, `${is.activity?.instance} ${is.activity?.summary}`],
        rebuilt: Boolean(rebuilt), newDocument: is.page ? is.page.marker !== was.marker : null,
        server: [was.status?.pid ?? null, is.status?.pid ?? null], servers: is.servers, sameServer: server,
        page: is.page && { playing: is.page.playing, occupied: is.page.occupied },
        resumed, pages: is.targets, serverLog: is.log, events: is.events,
      });
  }

  // C: the activity finished for good, and the control.
  const finish = async how => {
    await launch();
    const page = await shown();
    if (!(await page.until(ready))) throw new Error("the page did not come up ready");
    await page.evaluate(start(drain));
    if (!(await page.until(playing))) throw new Error("the drain fixture did not start");
    await pause(1500);
    const saved = drainSaves();
    const server = (await status())?.pid;
    const since = deviceNow();
    let presses = 0;
    if (how === "request") {
      await page.evaluate(`fetch("api/shutdown", { method: "POST" }).then(response => response.status)`);
      page.close();
    } else {
      page.close();
      const destroyed = () => lifecycleEvents(since).some(({ event }) => event === "wm_on_destroy_called");
      while (presses < 3 && !destroyed()) {
        adb("shell", "input", "keyevent", "KEYCODE_BACK");
        presses++;
        for (let i = 0; i < 30 && !destroyed(); i++) await pause(500);
      }
    }
    // The device's clock for the moment the process is gone, polled on the
    // device so the time is not adb's.
    const gone = Number(tryAdb("shell",
      `i=0; while [ -e /proc/${server} ] && [ $i -lt 300 ]; do sleep 0.1; i=$((i+1)); done; date +%s.%3N`));
    const ended = !alive(server);
    await pause(1500);
    const events = lifecycleEvents(since);
    const destroyedAt = events.find(({ event }) => event === "wm_on_destroy_called")?.at ?? null;
    return {
      server, presses, ended, saved: [saved, drainSaves()],
      finished: events.some(({ event }) => event === "wm_finish_activity"),
      destroyedAfter: destroyedAt && Math.round((destroyedAt - Number(since)) * 1000),
      endedAfterDestroy: destroyedAt && ended ? Math.round((gone - destroyedAt) * 1000) : null,
      endedAfterAsking: ended ? Math.round((gone - Number(since)) * 1000) : null,
      serverLog: serverLog(since), events: events.map(({ event }) => event),
    };
  };
  const finished = [];
  for (let i = 0; i < finishes; i++) finished.push(await finish("back"));
  check("C finishing the activity ends the server within 5 s of onDestroy",
    finished.every(run => run.finished && run.ended && run.endedAfterDestroy !== null && run.endedAfterDestroy <= 5000), { runs: finished });
  const control = await finish("request");
  const drains = run => run.ended && run.saved[1] === run.saved[0] + 1;
  check("C control: the drain POST /api/shutdown asks for closes the game (its destroyApp save is written)", drains(control), control);
  check("C the server ended by finishing the activity drains too (its destroyApp save is written)",
    finished.every(drains), {
      drained: `${finished.filter(drains).length} of ${finished.length}`,
      saved: finished.map(run => run.saved), serverLog: finished.map(run => run.serverLog),
      established: drains(control),
    });
} catch (error) {
  result.notes.push(`stopped: ${String(error.stack ?? error)}`);
  console.error(error);
} finally {
  for (const socket of sessions) socket.close();
  // Every setting goes back as it was, whatever happened above.
  for (const key of ["rotation", "ignoreOrientation", "size", "density", "night", "fontScale"]) {
    if (!touched.has(key)) continue;
    try {
      restore[key]();
    } catch (error) {
      console.error(`could not put ${key} back: ${error.message}; it was ${JSON.stringify(original)}`);
    }
  }
  // The test games come off whether the checks passed or not. The last check
  // leaves no server, so the app is started again for it; the request carries
  // no Origin, as the stop command's does not.
  if (games.length && serverForward) {
    if (!(await status())) await launch();
    for (const game of games) {
      const removed = await fetch(`http://127.0.0.1:${serverForward}/api/games?game=${encodeURIComponent(game)}`,
        { method: "DELETE", signal: AbortSignal.timeout(5000) }).then(response => response.status, error => String(error));
      if (removed !== 200) console.error(`the test game ${game} was not removed (${removed}); take it off from the page`);
    }
  }
  for (const port of [devtoolsPort, serverForward]) {
    if (port) tryAdb("forward", "--remove", `tcp:${port}`);
  }
  const failed = result.checks.filter(entry => !entry.ok).map(entry => entry.name);
  result.failed = failed;
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  console.log(`${result.checks.length - failed.length} of ${result.checks.length} checks passed; ${join(output, "result.json")}`);
  process.exit(failed.length || result.notes.some(note => note.startsWith("stopped")) ? 1 : 0);
}
