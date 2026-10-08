// Opt-in keypad checkpoint checks against repository-authored SKT fixtures.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import net from "node:net";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [binary, engineName = "chromium"] = process.argv.slice(2);
if (!binary || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/checkpoint-skt.mjs SERVER [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const run = `checkpoint-skt-${engineName}-${Date.now()}`;
const output = resolve("var/acceptance", run);
const games = resolve("var/games", `.${run}`), ext = resolve("var/ext", run), saves = resolve("var/savedata", run);
for (const directory of [output, join(games, "library"), ext, saves]) mkdirSync(directory, { recursive: true });
copyFileSync("internal/platform/skt/testdata/checkpoint-skt.zip", join(games, "library", "java.zip"));

// The same authored SGS program used by the CLI and browser handler tests:
// load its counter and show white; a key saves the next value and shows black.
const script = [...Buffer.alloc(52)];
script[0] = 1;
script.splice(10, 15, ...Buffer.from("Host checkpoint"));
const set16 = (at, value) => { script[at] = value & 255; script[at + 1] = value >>> 8; };
for (const [index, code] of [
  [5, 16, 5, 1, 0x98, 0x55, 0x78, 0xff], [0xff], [0xff],
  [0x3a, 16, 1, 5, 16, 5, 1, 0x99, 0x56, 0x78, 0xff],
].entries()) {
  set16(28 + index * 2, script.length);
  script.push(...code);
}
const variables = script.length;
for (let index = 0; index < 17; index++) script.push(1, 1, 0, 0);
for (const [index, offset] of [variables, script.length, script.length, script.length].entries()) set16(44 + index * 2, offset);
const descriptor = Buffer.concat(["application/x-gnex-sgs", "SGS"].flatMap(value => {
  const length = Buffer.alloc(4);
  length.writeUInt32LE(value.length);
  return [length, Buffer.from(value)];
}));
// These tiny stored ZIP entries need no external archive package.
const zip = entries => {
  const local = [], central = [];
  let offset = 0;
  for (const [name, data] of entries) {
    let crc = 0xffffffff;
    for (const byte of data) {
      crc ^= byte;
      for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
    }
    crc = (crc ^ 0xffffffff) >>> 0;
    const filename = Buffer.from(name), header = Buffer.alloc(30), entry = Buffer.alloc(46);
    header.writeUInt32LE(0x04034b50);
    header.writeUInt16LE(20, 4);
    header.writeUInt32LE(crc, 14);
    header.writeUInt32LE(data.length, 18);
    header.writeUInt32LE(data.length, 22);
    header.writeUInt16LE(filename.length, 26);
    entry.writeUInt32LE(0x02014b50);
    entry.writeUInt16LE(20, 4);
    entry.writeUInt16LE(20, 6);
    header.copy(entry, 8, 6, 28);
    entry.writeUInt32LE(offset, 42);
    local.push(header, filename, data);
    central.push(entry, filename);
    offset += header.length + filename.length + data.length;
  }
  const directory = Buffer.concat(central), end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, directory, end]);
};
writeFileSync(join(games, "library", "sgs.zip"), zip([["checkpoint.mod", descriptor], ["checkpoint.sgs", Buffer.from(script)]]));
const digest = file => createHash("sha256").update(readFileSync(file)).digest("hex");
const files = directory => readdirSync(directory, { recursive: true, withFileTypes: true })
  .filter(entry => entry.isFile()).map(entry => join(entry.parentPath, entry.name));
const ordinarySaves = () => Object.fromEntries(files(saves).filter(file => !file.includes("/.wfeature-quicksave/"))
  .map(file => [file.slice(saves.length + 1), digest(file)]).sort(([a], [b]) => a.localeCompare(b)));
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
const result = { run, engine: engineName, binary: digest(binary), checks: [], errors: [], fixtures: {} };
const check = name => { result.checks.push(name); console.log(`PASS ${engineName}: ${name}`); };
try {
  for (let attempt = 0; ; attempt++) {
    try { if ((await fetch(`${origin}/api/status`)).ok) break; } catch {}
    if (attempt === 100 || server.exitCode !== null) throw new Error("server did not start");
    await pause(50);
  }
  browser = await engine.launch({ headless: true });
  result.browser = browser.version();
  const context = await browser.newContext({ viewport: { width: 1280, height: 1000 }, hasTouch: true });
  await context.addInitScript(() => {
    window.checkpointProbe = { events: [], sent: [] };
    const NativeSocket = window.WebSocket;
    window.WebSocket = class extends NativeSocket {
      constructor(...args) {
        super(...args);
        this.addEventListener("message", event => {
          if (typeof event.data === "string") {
            try { checkpointProbe.events.push(JSON.parse(event.data)); } catch {}
          } else if (event.data instanceof ArrayBuffer && event.data.byteLength >= 16) {
            const view = new DataView(event.data);
            if (view.getUint32(0) === 0x57465032) checkpointProbe.events.push({ kind: "picture", operation: view.getUint8(4) });
          }
        });
      }
      send(data) {
        try { checkpointProbe.sent.push(JSON.parse(data)); } catch {}
        return super.send(data);
      }
    };
  });
  const page = await context.newPage();
  page.on("pageerror", error => result.errors.push(error.message));
  // Keep the wire order across page reloads; each new page also keeps its own
  // probe for waiting on the requests sent through the actual client.
  const received = [];
  page.on("websocket", socket => socket.on("framereceived", ({ payload }) => {
    if (typeof payload === "string") {
      try { received.push(JSON.parse(payload)); } catch {}
    } else if (payload.length >= 16 && payload.readUInt32BE(0) === 0x57465032) {
      received.push({ kind: "picture", operation: payload[4] });
    }
  }));
  const events = async () => received.slice();
  const snapshot = () => page.evaluate(() => {
    const canvas = document.querySelector("#canvas");
    const pixels = canvas.getContext("2d").getImageData(0, 0, canvas.width, canvas.height).data;
    let hash = 2166136261;
    for (const byte of pixels) hash = Math.imul(hash ^ byte, 16777619);
    return { width: canvas.width, height: canvas.height, hash: hash >>> 0 };
  });
  const waitForFrame = expected => page.waitForFunction(expected => {
    const canvas = document.querySelector("#canvas");
    if (canvas.width !== expected.width || canvas.height !== expected.height) return false;
    const pixels = canvas.getContext("2d").getImageData(0, 0, canvas.width, canvas.height).data;
    let hash = 2166136261;
    for (const byte of pixels) hash = Math.imul(hash ^ byte, 16777619);
    return (hash >>> 0) === expected.hash;
  }, expected, { timeout: 15000 });
  const color = rgb => page.waitForFunction(expected => {
    const canvas = document.querySelector("#canvas");
    const pixel = canvas.getContext("2d").getImageData(40, 40, 1, 1).data;
    return pixel[3] === 255 && expected.every((value, index) => value === pixel[index]);
  }, rgb);
  const settings = async () => {
    if (!await page.locator("#settings-panel").evaluate(element => element.classList.contains("visible"))) {
      await page.locator('button[data-key="SETTINGS"]').click();
    }
  };
  await page.goto(origin);
  await page.waitForFunction(() => !document.querySelector("#game-start").disabled);
  await settings();
  await page.locator("#keypad-arrange-open").click();
  for (const [cell, label] of [["r2c1", "퀵세이브"], ["r6c1", "퀵로드"]]) {
    await page.locator(`[data-cell="${cell}"]`).click();
    await page.locator("#keypad-arrange-keys").getByRole("button", { name: label, exact: true }).click();
  }
  await page.locator("#keypad-arrange-close").click();
  const save = page.locator('button[data-key="QUICK_SAVE"]');
  const load = page.locator('button[data-key="QUICK_LOAD"]');
  assert.equal(await save.isDisabled(), true);
  assert.equal(await load.isDisabled(), true);
  check("checkpoint keys assigned through the keypad editor and disabled without a session");

  for (const [kind, initial, changed] of [["java", [18, 52, 86], [101, 67, 33]], ["sgs", [255, 255, 255], [0, 0, 0]]]) {
    await page.locator("#game-select").selectOption(`games/library/${kind}.zip`);
    await page.locator("#game-start").click();
    await color(initial);
    await page.waitForFunction(() => !document.querySelector('button[data-key="QUICK_SAVE"]').disabled);
    const started = (await events()).findLast(message => message.kind === "started");
    assert.equal(started.started.can_checkpoint, true);
    assert.equal(started.started.has_checkpoint ?? false, false);
    assert.equal(await load.isDisabled(), true);
    const savedFrame = await snapshot();
    await save.click();
    await page.waitForFunction(() => document.querySelector("#checkpoint-status").textContent === "퀵세이브를 저장했습니다.");
    assert.equal(await load.isDisabled(), false);
    const identity = digest(join(games, "library", `${kind}.zip`));
    const slots = files(saves).filter(file => file.endsWith(`/${identity}.v2.wfq`));
    assert.equal(slots.length, 1);
    const slot = readFileSync(slots[0]);
    result.fixtures[kind] = { identity, slotBytes: slot.length, savedFrame };
    check(`${kind}: advertised capability and keypad quicksave writes the slot`);

    await page.keyboard.down("1");
    await color(changed);
    const changedFrame = await snapshot();
    assert.notDeepEqual(changedFrame, savedFrame);
    const damaged = Buffer.from(slot);
    damaged[damaged.length - 1] ^= 1;
    writeFileSync(slots[0], damaged);
    const errorCount = (await events()).filter(message => message.kind === "error").length;
    await load.click();
    await page.waitForFunction(() => document.querySelector("#checkpoint-status").classList.contains("error"));
    assert.deepEqual(await snapshot(), changedFrame);
    assert.equal(await save.isDisabled(), false);
    assert.equal(await load.isDisabled(), false);
    assert.equal((await events()).filter(message => message.kind === "error").length, errorCount + 1);
    assert.equal((await events()).filter(message => message.kind === "restored").length, kind === "java" ? 0 : 1);
    check(`${kind}: damaged slot reports an inline error and keeps the live picture and controls`);

    writeFileSync(slots[0], slot);
    const savesBeforeLoad = ordinarySaves(), beforeLoad = (await events()).length;
    assert.ok(Object.keys(savesBeforeLoad).length, "fixture did not persist ordinary save data");
    await load.click();
    await page.waitForFunction(() => document.querySelector("#checkpoint-status").textContent === "퀵세이브 시점으로 돌아갔습니다. 세이브는 되돌리지 않았습니다.");
    await waitForFrame(savedFrame);
    assert.deepEqual(await snapshot(), savedFrame);
    assert.deepEqual(ordinarySaves(), savesBeforeLoad);
    const restoredEvents = (await events()).slice(beforeLoad);
    const restoredIndex = restoredEvents.findIndex(message => message.kind === "restored");
    assert.ok(restoredIndex >= 0);
    const restored = restoredEvents[restoredIndex];
    assert.equal(restored.started.restored, true);
    assert.equal(restored.started.has_checkpoint, true);
    // These fixtures are static before the load. Check the first picture of
    // the whole operation, including a picture that wrongly beats the reset.
    const firstPicture = restoredEvents.findIndex(message => message.kind === "picture");
    assert.ok(firstPicture > restoredIndex, "the first checkpoint picture must follow its reset");
    assert.equal(restoredEvents[firstPicture].operation, 0);
    assert.equal(await page.locator('button[data-key="1"]').first().evaluate(element => element.classList.contains("pressed")), false);
    result.fixtures[kind].epoch = restored.epoch;
    await page.screenshot({ path: join(output, `${kind}-restored.png`) });
    check(`${kind}: keypad quickload redraws the saved pixels, clears held input and preserves ordinary saves`);

    await page.keyboard.up("1");
    const sentBefore = await page.evaluate(() => checkpointProbe.sent.length);
    await page.keyboard.press("1");
    await waitForFrame(changedFrame);
    const nextKeys = await page.evaluate(at => checkpointProbe.sent.slice(at).filter(message => message.kind === "key"), sentBefore);
    assert.deepEqual(nextKeys.map(message => [message.action, message.epoch]), [["press", restored.epoch], ["release", restored.epoch]]);
    check(`${kind}: new keyboard input reaches the restored session with its new epoch`);

    const path = `games/library/${kind}.zip`;
    const frameBeforeReconnect = await snapshot(), savesBeforeReconnect = ordinarySaves();
    await settings();
    await page.locator("#game-speed").selectOption("2");
    assert.equal(await page.locator("#game-speed").inputValue(), "2");
    assert.equal(await page.evaluate(path => localStorage.getItem(`wfeature:speed:${path}`), path), "2");
    // The page's park request follows its speed request. Its answer proves
    // the server processed both before reload closes this connection.
    await page.evaluate(() => {
      Object.defineProperty(document, "hidden", { configurable: true, value: true });
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await page.waitForFunction(() => {
      const park = checkpointProbe.sent.findLast(message => message.kind === "park");
      return park && checkpointProbe.events.some(message => message.kind === "result" && message.id === park.id);
    });
    const beforeReconnect = (await events()).length;
    await page.reload();
    await page.waitForFunction(path => checkpointProbe.events.some(message =>
      message.kind === "started" && message.started?.game === path) &&
      !document.querySelector('button[data-key="QUICK_SAVE"]').disabled, path);
    await waitForFrame(frameBeforeReconnect);
    const resumed = (await events()).slice(beforeReconnect).find(message => message.kind === "started");
    assert.equal(resumed?.started.token, restored.started.token);
    assert.equal(resumed.started.restored, true);
    assert.equal(resumed.started.speed, 2);
    assert.deepEqual(await snapshot(), frameBeforeReconnect);
    assert.deepEqual(ordinarySaves(), savesBeforeReconnect);
    await settings();
    assert.equal(await page.locator("#game-speed").inputValue(), "2");
    assert.equal(await page.evaluate(path => localStorage.getItem(`wfeature:speed:${path}`), path), "2");
    result.fixtures[kind].resumedSpeed = resumed.started.speed;
    await page.screenshot({ path: join(output, `${kind}-reconnected.png`) });
    check(`${kind}: page reconnect keeps later speed, static pixels and ordinary saves`);

    await settings();
    await page.locator("#restart").click();
    await page.locator("#confirm-accept").click();
    await page.waitForFunction(() => !document.querySelector("#game-start").disabled);
  }
  assert.deepEqual(result.errors, []);
  assert.equal((await events()).filter(message => message.kind === "error").length, 2);
  console.log(`Results: ${output}`);
} catch (error) {
  result.errors.push(error.message);
  throw error;
} finally {
  await browser?.close();
  server.kill("SIGTERM");
  await new Promise(done => { if (server.exitCode !== null) done(); else server.once("exit", done); });
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  writeFileSync(join(output, "server.log"), logs);
  rmSync(ext, { recursive: true, force: true });
}
