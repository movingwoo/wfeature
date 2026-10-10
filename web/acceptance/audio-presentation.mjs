// Render one authored score under different packet sizes without a server port.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const engineName = process.argv[2] || "chromium";
if (!process.env.PLAYWRIGHT_MODULE) throw new Error("Set PLAYWRIGHT_MODULE to playwright/index.mjs");
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const browser = await engine.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.route("http://wfeature.test/**", async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/") return route.fulfill({ contentType: "text/html", body: "<!doctype html><title>Audio presentation acceptance</title>" });
    if (!/^\/[a-z0-9-]+\.js$/.test(path)) return route.abort();
    return route.fulfill({ contentType: "text/javascript", body: await readFile(new URL(`..${path}`, import.meta.url), "utf8") });
  });
  await page.goto("http://wfeature.test/");
  const result = await page.evaluate(async () => {
    const { PageAudio } = await import("./audio.js");
    const { playAudioEvents } = await import("./session.js");
    const rate = 48000;
    const render = async (packetMilliseconds, cacheable) => {
      const context = new OfflineAudioContext(2, rate * 2, rate);
      const audio = new PageAudio();
      const clock = { state: "running", currentTime: 0, baseLatency: .09, sampleRate: rate };
      for (const name of ["createGain", "createOscillator", "createStereoPanner", "createBufferSource", "createBuffer", "createBiquadFilter"]) clock[name] = context[name].bind(context);
      let waveBufferRequests = 0;
      clock.createBuffer = (...args) => { waveBufferRequests++; return context.createBuffer(...args); };
      audio.context = clock;
      audio.midiGain = context.createGain(); audio.midiGain.connect(context.destination);
      audio.waveGain = context.createGain(); audio.waveGain.connect(context.destination);
      audio._noise = context.createBuffer(1, rate / 4, rate);
      const noise = audio._noise.getChannelData(0);
      for (let i = 0; i < noise.length; i++) noise[i] = Math.sin(i * 1.234);
      const samples = new Float32Array(rate / 4);
      for (let i = 0; i < samples.length; i++) samples[i] = Math.sin(i * 2 * Math.PI * 330 / rate) * .15;
      const events = [
        { kind: "programChange", at: 0, sound: 1, program: 72 },
        { kind: "playWave", at: 0, sound: 2, channels: 1, rate, samples, cacheable },
        { kind: "soundGain", at: .04, sound: 2, value: 5000 },
        { kind: "stopSound", at: .08, sound: 2 },
        { kind: "soundGain", at: .085, sound: 2, value: 2500 },
        { kind: "playWave", at: .085, sound: 2, channels: 1, rate, samples, cacheable },
        { kind: "controlChange", at: .3, sound: 1, control: 64, value: 127 },
        { kind: "pitchBend", at: .33, sound: 1, value: 10000 },
        { kind: "controlChange", at: .4, sound: 1, control: 64, value: 0 },
        { kind: "controlChange", at: .5, sound: 1, control: 11, value: 75 },
        { kind: "controlChange", at: .55, sound: 1, control: 10, value: 100 },
      ];
      // Later intervals model a continuous backend rate change. Pitch and PCM
      // sampling rate stay unchanged; only the presentation spacing changes.
      let at = 0;
      for (let i = 0; i < 10; i++) {
        events.push({ kind: "noteOn", at, sound: 1, note: 60 + i, velocity: 100 });
        events.push({ kind: "noteOff", at: at + (i % 2 ? .035 : .005), sound: 1, note: 60 + i });
        if (i % 3 === 0) events.push({ kind: "noteOn", at, sound: 3, channel: 9, note: 40, velocity: 80 });
        at += i < 4 ? .14 : .07;
      }
      events.sort((a, b) => a.at - b.at);
      if (!playAudioEvents(audio, [{ kind: "clock", at: 0 }])) throw new Error("initial clock refused");
      let cursor = 0;
      for (let milliseconds = packetMilliseconds; milliseconds <= 1400; milliseconds += packetMilliseconds) {
        const frontier = milliseconds / 1000;
        clock.currentTime = Math.floor((frontier + 1e-9) / .09) * .09;
        const batch = [];
        while (cursor < events.length && events[cursor].at <= frontier) batch.push(events[cursor++]);
        batch.push({ kind: "clock", at: frontier });
        if (!playAudioEvents(audio, batch)) throw new Error(`packet ${packetMilliseconds}/${frontier} refused`);
      }
      if (cursor !== events.length) throw new Error("score was not fully delivered");
      const rendered = await context.startRendering();
      return { channels: Array.from({ length: 2 }, (_, channel) => rendered.getChannelData(channel)), waveBufferRequests };
    };
    const fine = await render(5, false), coarse = await render(70, true);
    if (fine.waveBufferRequests !== 2 || coarse.waveBufferRequests !== 1) throw new Error("PCM cache did not reuse exactly one immutable buffer");
    let maximumError = 0, energy = 0;
    for (let channel = 0; channel < 2; channel++) {
      for (let i = 0; i < fine.channels[channel].length; i++) {
        maximumError = Math.max(maximumError, Math.abs(fine.channels[channel][i] - coarse.channels[channel][i]));
        energy += fine.channels[channel][i] ** 2;
      }
    }
    return { maximumError, energy };
  });
  assert.ok(result.energy > 1, "the authored score rendered silence");
  assert.ok(result.maximumError < 1e-6, `packet size changed rendered output: ${result.maximumError}`);
  console.log(`PASS ${engineName}: 5/70 ms packets and PCM buffer reuse preserve rendered notes, controls and stops; max error ${result.maximumError}`);
} finally {
  await browser.close();
}
