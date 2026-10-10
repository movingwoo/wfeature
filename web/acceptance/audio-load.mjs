// Native cost of the page's largest sound loads, with no listening server or
// external assets. Each scenario is rendered offline: the time a render takes
// against the audio it produces is the audio thread's share of one core, and the
// browser's resident memory is sampled from outside while it runs. These are
// this machine's numbers, not a phone's; PASS means every load was admitted and
// rendered, not that a slower device keeps up.
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";

const engineName = process.argv[2] || "chromium";
if (!process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-load.mjs [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");

// residentKiB sums the resident set of every descendant of a process, which is
// where a browser launched from this one keeps its renderer and audio service.
const run = promisify(execFile);
const residentKiB = async root => {
  const { stdout } = await run("ps", ["-A", "-o", "pid=,ppid=,rss="]);
  const children = new Map(), rss = new Map();
  for (const line of stdout.trim().split("\n")) {
    const [pid, ppid, size] = line.trim().split(/\s+/).map(Number);
    rss.set(pid, size);
    if (!children.has(ppid)) children.set(ppid, []);
    children.get(ppid).push(pid);
  }
  let total = 0;
  for (const pending = [...(children.get(root) || [])]; pending.length;) {
    const pid = pending.pop();
    total += rss.get(pid) || 0;
    pending.push(...(children.get(pid) || []));
  }
  return total;
};

const browser = await engine.launch({ headless: true });
const root = process.pid;
assert.ok(await residentKiB(root) > 0, "the launched browser has no process to measure");
let failed = false;
try {
  const page = await browser.newPage();
  await page.route("http://wfeature.test/**", async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/") return route.fulfill({ contentType: "text/html", body: "<!doctype html><title>Audio load</title>" });
    if (!/^\/[a-z0-9-]+\.js$/.test(path)) return route.abort();
    return route.fulfill({ contentType: "text/javascript", body: await readFile(new URL(`..${path}`, import.meta.url), "utf8") });
  });
  await page.goto("http://wfeature.test/");
  await page.evaluate(async () => {
    const { PageAudio } = await import("./audio.js");
    const { playAudioEvents } = await import("./session.js");
    const rate = 48000, duration = 4, waveRate = 8000;
    // A wave long enough to sound for the whole render, distinct per source so
    // nothing is shared through the buffer cache: the worst case for memory.
    const wave = (index, frames) => Float32Array.from({ length: frames },
      (_, frame) => .02 * Math.sin(2 * Math.PI * (220 + index) * frame / waveRate));
    const notes = count => Array.from({ length: count }, (_, index) =>
      ({ kind: "noteOn", at: 0, sound: 1, channel: index % 9, note: 48 + index, velocity: 90 }));
    const waves = (count, frames) => Array.from({ length: count }, (_, index) =>
      ({ kind: "playWave", at: 0, sound: 2 + index, channels: 1, rate: waveRate, samples: wave(index, frames) }));
    window.loadScenario = name => {
      switch (name) {
        case "idle": return [];
        case "24 notes": return notes(24);
        // The largest output the backend can admit: 256 PCM references whose
        // signed 16-bit charges fill its 32 MiB, under 24 sounding notes.
        case "backend maximum": return [...notes(24), ...waves(256, 65536)];
        // The largest batch the page admits: 512 retained sources and just
        // under 128 MiB of float payload.
        case "receiver maximum": return [...notes(24), ...waves(488, 65536)];
      }
      const real = /^(\d+) waves$/.exec(name);
      if (real) return [...notes(24), ...waves(Number(real[1]), waveRate * duration)];
      throw new Error(`unknown scenario ${name}`);
    };
    window.renderScenario = async name => {
      const context = new OfflineAudioContext(2, rate * duration, rate);
      const audio = new PageAudio();
      // A running clock held at zero, so every event is due at once, in front
      // of the offline context that builds and renders the nodes.
      let sources = 0;
      const clock = new Proxy({ state: "running", currentTime: 0, baseLatency: 0, sampleRate: rate }, {
        get(held, key) {
          if (key in held) return held[key];
          const value = context[key];
          if (typeof value !== "function") return value;
          return (...args) => {
            if (key === "createBufferSource" || key === "createOscillator") sources++;
            return value.apply(context, args);
          };
        },
        set(held, key, value) { held[key] = value; return true; },
      });
      audio.context = clock;
      audio.midiGain = context.createGain(); audio.midiGain.connect(context.destination);
      audio.waveGain = context.createGain(); audio.waveGain.connect(context.destination);
      const events = window.loadScenario(name);
      const built = performance.now();
      if (playAudioEvents(audio, [{ kind: "clock", at: 0 }]) !== true) throw new Error(`${name}: clock refused`);
      const admitted = playAudioEvents(audio, [...events, { kind: "clock", at: 0 }]) === true;
      const scheduled = performance.now();
      const rendered = await context.startRendering();
      const finished = performance.now();
      let peak = 0;
      for (let channel = 0; channel < 2; channel++) {
        for (const sample of rendered.getChannelData(channel)) peak = Math.max(peak, Math.abs(sample));
      }
      return {
        name, admitted, retained: audio.sources.size, nativeSources: sources,
        scheduleMs: scheduled - built, renderMs: finished - scheduled, seconds: duration, peak,
        heapMiB: performance.memory ? performance.memory.usedJSHeapSize / 1048576 : null,
      };
    };
  });

  const scenarios = ["idle", "24 notes", "4 waves", "16 waves", "32 waves", "64 waves", "128 waves",
    "backend maximum", "receiver maximum"];
  for (const name of scenarios) {
    const before = await residentKiB(root);
    let peakKiB = before, sampling = true;
    const sampler = (async () => {
      while (sampling) {
        peakKiB = Math.max(peakKiB, await residentKiB(root));
        await new Promise(next => setTimeout(next, 50));
      }
    })();
    let result;
    try {
      result = await page.evaluate(name => window.renderScenario(name), name);
    } finally {
      sampling = false;
      await sampler;
    }
    const realtime = result.seconds * 1000 / result.renderMs;
    const line = {
      engine: engineName, ...result, realtime: Number(realtime.toFixed(1)),
      residentBeforeMiB: Math.round(before / 1024), residentPeakMiB: Math.round(peakKiB / 1024),
    };
    console.log(JSON.stringify(line));
    if (!result.admitted || (name !== "idle" && !(result.peak > 0))) {
      failed = true;
      console.error(`${name}: not admitted or silent`);
    }
  }
} finally {
  await browser.close();
}
if (failed) process.exit(1);
console.log(`PASS ${engineName}`);
