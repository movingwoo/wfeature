// Opt-in live check of one session's sound: the real server, the real page and
// a running AudioContext, rather than the offline renders beside this file.
// No authored fixture makes sound without a test calling it, so a local archive
// that plays on its own is required; it is copied into a scratch library and
// removed again, and nothing names it.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { copyFileSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import net from "node:net";
import { extname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [binary, engineName = "chromium"] = process.argv.slice(2);
const archive = process.env.WFEATURE_AUDIO_ARCHIVE;
if (!binary || !archive || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: WFEATURE_AUDIO_ARCHIVE=/path/to/archive PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-live.mjs DEBUG_SERVER [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const run = `audio-live-${engineName}-${Date.now()}`;
const output = resolve("var/acceptance", run);
const games = resolve("var/games", `.${run}`), ext = resolve("var/ext", run), saves = resolve("var/savedata", run);
for (const directory of [output, join(games, "library"), ext, saves]) mkdirSync(directory, { recursive: true });
const game = `probe${extname(archive).toLowerCase()}`;
copyFileSync(archive, join(games, "library", game));

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
  // Chromium otherwise keeps a context created before a gesture suspended;
  // the click that starts the game is the gesture WebKit needs.
  browser = await engine.launch({ headless: true,
    args: engineName === "chromium" ? ["--autoplay-policy=no-user-gesture-required"] : [] });
  const context = await browser.newContext({ viewport: { width: 1280, height: 1000 } });
  await context.addInitScript(() => {
    const probe = window.audioProbe = { sockets: [], sent: {}, soundPackets: 0, soundBytes: 0, errors: [],
      contexts: [], starts: 0, scheduledStarts: 0, lateStarts: 0, minimumLead: null, maximumLead: null };
    const NativeSocket = window.WebSocket;
    window.WebSocket = class extends NativeSocket {
      constructor(...args) {
        super(...args);
        probe.sockets.push(this);
        this.addEventListener("message", event => {
          if (typeof event.data === "string") {
            try {
              const message = JSON.parse(event.data);
              if (message.kind === "error") probe.errors.push(message);
            } catch {}
            return;
          }
          // "WFA2": a binary sound batch; see audio-stream.js.
          const view = new DataView(event.data);
          if (view.byteLength >= 4 && view.getUint32(0) === 0x57464132) {
            probe.soundPackets++;
            probe.soundBytes += view.byteLength;
          }
        });
      }
      send(data) {
        try {
          const kind = JSON.parse(data).kind;
          probe.sent[kind] = (probe.sent[kind] ?? 0) + 1;
        } catch {}
        return super.send(data);
      }
    };
    for (const name of ["AudioContext", "webkitAudioContext"]) {
      const Native = window[name];
      if (!Native) continue;
      window[name] = class extends Native {
        constructor(...args) {
          super(...args);
          probe.contexts.push(this);
        }
      };
    }
    // How far ahead of the render clock each source is started. Zero means
    // "now"; a negative lead beyond the page's 5 ms tolerance is a late start.
    for (const Node of [window.OscillatorNode, window.AudioBufferSourceNode]) {
      const start = Node.prototype.start;
      Node.prototype.start = function (when = 0, ...rest) {
        probe.starts++;
        if (when > 0) {
          const lead = when - this.context.currentTime;
          probe.scheduledStarts++;
          if (lead < -0.005) probe.lateStarts++;
          probe.minimumLead = probe.minimumLead === null ? lead : Math.min(probe.minimumLead, lead);
          probe.maximumLead = probe.maximumLead === null ? lead : Math.max(probe.maximumLead, lead);
        }
        return start.call(this, when, ...rest);
      };
    }
  });
  const page = await context.newPage();
  page.on("pageerror", error => result.errors.push(error.message));
  await page.goto(origin);
  // The page's own PageAudio is the module this import returns. Wrapping its
  // window check records, for the first batches and every refusal, when the
  // batch was examined on the page clock, the render clock and the batch's
  // own presentation times: a late page and a jumping server look different.
  await page.evaluate(async () => {
    const { PageAudio } = await import("/audio.js");
    const recordTiming = PageAudio.prototype.recordTiming;
    audioProbe.batches = [];
    PageAudio.prototype.recordTiming = function (events, anchor, current, fresh, refused) {
      if (audioProbe.batches.length < 160 || refused) {
        audioProbe.batches.push({ page: performance.now() / 1000, current, fresh, refused,
          anchorAudio: anchor.audio, anchorPresentation: anchor.presentation,
          first: events[0]?.at, clock: events.at(-1)?.at, events: events.length - 1 });
      }
      return recordTiming.call(this, events, anchor, current, fresh, refused);
    };
  });
  await page.locator("#game-select").selectOption(`games/library/${game}`);
  await page.locator("#game-start").click();
  await page.waitForFunction(() => !document.querySelector("#restart").classList.contains("hidden"));
  // The same three confirm presses as the real-scene probe, two seconds apart,
  // so a title waiting on a key reaches whatever plays next.
  for (const at of [2000, 3000, 3000]) {
    await pause(at);
    await page.keyboard.down("Space");
    await pause(120);
    await page.keyboard.up("Space");
  }
  await pause(4000);
  const observed = await page.evaluate(() => ({
    url: audioProbe.sockets.at(-1)?.url,
    sent: audioProbe.sent,
    soundPackets: audioProbe.soundPackets,
    soundBytes: audioProbe.soundBytes,
    errors: audioProbe.errors,
    contexts: audioProbe.contexts.map(context => ({ state: context.state, time: context.currentTime,
      sampleRate: context.sampleRate, baseLatency: context.baseLatency ?? null })),
    starts: audioProbe.starts,
    scheduledStarts: audioProbe.scheduledStarts,
    lateStarts: audioProbe.lateStarts,
    minimumLead: audioProbe.minimumLead,
    maximumLead: audioProbe.maximumLead,
    batches: audioProbe.batches,
    timing: [...document.querySelectorAll("#log-view .log-line")].map(row => row.textContent)
      .filter(line => line.includes("audio playout timing: "))
      .map(line => JSON.parse(line.slice(line.indexOf("audio playout timing: ") + 22))),
  }));
  result.observed = { ...observed, timing: undefined, batches: undefined, timingReports: observed.timing.length };
  result.reports = observed.timing;
  result.batches = observed.batches;

  const query = new URL(observed.url).searchParams;
  for (const [key, value] of [["protocol", "2"], ["sound", "resume"], ["timing", "1"], ["pcm", "1"], ["phase", "1"]]) {
    assert.equal(query.get(key), value, `${key} in ${observed.url}`);
  }
  check("the page negotiated protocol 2 with resumable, timed, routed and phased sound");

  assert.ok(observed.soundPackets > 0, "no binary sound batch arrived");
  assert.equal(observed.contexts.length > 0 && observed.contexts.at(-1).state, "running", JSON.stringify(observed.contexts));
  assert.ok(observed.contexts.at(-1).time > 1, "the render clock did not advance");
  check(`${observed.soundPackets} sound batches reached a running AudioContext`);

  assert.ok(observed.scheduledStarts > 0, `no source was scheduled (${observed.starts} immediate starts)`);
  assert.equal(observed.lateStarts, 0, `late source starts, minimum lead ${observed.minimumLead}`);
  check(`${observed.scheduledStarts} sources started ahead of the render clock (lead ${observed.minimumLead.toFixed(3)}–${observed.maximumLead.toFixed(3)} s)`);

  // Fold every report into totals. A report covers the batches since the one
  // before it, so the sum is the whole run.
  const totals = { batches: 0, admitted: 0, refused: { late: 0, future: 0, range: 0 }, events: 0, not_late: 0,
    late_le_5ms: 0, late_le_20ms: 0, late_le_100ms: 0, late_gt_100ms: 0, max_event_late_ms: 0 };
  for (const report of observed.timing) {
    for (const group of [report.anchored, report.continuing]) {
      for (const key of ["batches", "admitted", "events", "not_late", "late_le_5ms", "late_le_20ms", "late_le_100ms", "late_gt_100ms"]) {
        totals[key] += group[key];
      }
      for (const reason of ["late", "future", "range"]) totals.refused[reason] += group.refused[reason];
      totals.max_event_late_ms = Math.max(totals.max_event_late_ms, group.max_event_late_ms);
    }
  }
  result.timing = totals;
  assert.ok(observed.timing.length > 0, "the debug page reported no playout timing");
  assert.ok(totals.admitted > 0 && totals.events > 0, JSON.stringify(totals));
  check(`debug timing: ${totals.admitted}/${totals.batches} batches admitted, ${totals.events} events, ` +
    `${totals.late_gt_100ms} over 100 ms late, recovery requests ${observed.sent.audioResume ?? 0}`);

  assert.deepEqual(observed.errors, []);
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
  for (const directory of [games, ext, saves]) rmSync(directory, { recursive: true, force: true });
  console.log(`Live audio report: ${output}`);
}
