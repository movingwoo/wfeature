// Render independent clips with the actual Web Audio graph in both engines.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-ownership.mjs URL [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const browser = await engine.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.goto(origin);
  const results = await page.evaluate(async () => {
    const { PageAudio } = await import("./audio.js");
    const { playAudioEvents } = await import("./session.js");
    const rate = 48000;
    const render = async (kind, concurrent) => {
      const context = new OfflineAudioContext(2, rate, rate);
      const audio = new PageAudio();
      const clock = { state: "running", get currentTime() { return context.currentTime; }, baseLatency: 0 };
      for (const name of ["createGain", "createOscillator", "createStereoPanner", "createBuffer", "createBufferSource"]) {
        clock[name] = context[name].bind(context);
      }
      audio.context = clock;
      audio.midiGain = context.createGain();
      audio.midiGain.connect(context.destination);
      audio.waveGain = context.createGain();
      audio.waveGain.connect(context.destination);
      let wall = 0;
      const previous = Object.getOwnPropertyDescriptor(performance, "now");
      Object.defineProperty(performance, "now", { configurable: true, value: () => wall * 1000 });
      const events = [];
      for (const sound of concurrent ? [1, 2] : [2]) {
        if (kind === "MIDI") {
          events.push(
            { kind: "programChange", sound, channel: 0, program: 72 },
            { kind: "controlChange", sound, channel: 0, control: 10, value: sound === 1 ? 0 : 127 },
            { kind: "noteOn", sound, channel: 0, note: 69, velocity: 100 },
          );
        } else {
          const samples = new Float32Array(rate * 2);
          for (let frame = 0; frame < rate; frame++) {
            samples[frame * 2 + sound - 1] = 0.2 * Math.sin(2 * Math.PI * 440 * frame / rate);
          }
          events.push({ kind: "playWave", sound, channels: 2, rate, samples });
        }
      }
      try {
        playAudioEvents(audio, events);
        const paused = context.suspend(0.35);
        const rendered = context.startRendering();
        await paused;
        wall = context.currentTime;
        playAudioEvents(audio, [{ kind: "stopSound", sound: 1 }]);
        await context.resume();
        const buffer = await rendered;
        const energy = (channel, start, end) => {
          const samples = buffer.getChannelData(channel);
          let sum = 0;
          const first = Math.round(start * rate), last = Math.round(end * rate);
          for (let i = first; i < last; i++) sum += samples[i] ** 2;
          return Math.sqrt(sum / (last - first));
        };
        return { before: [energy(0, 0.2, 0.3), energy(1, 0.2, 0.3)], after: [energy(0, 0.5, 0.7), energy(1, 0.5, 0.7)] };
      } finally {
        if (previous) Object.defineProperty(performance, "now", previous);
        else delete performance.now;
      }
    };
    const results = [];
    for (const kind of ["MIDI", "PCM"]) {
      results.push({ kind, concurrent: await render(kind, true), reference: await render(kind, false) });
    }
    return results;
  });
  for (const { kind, concurrent, reference } of results) {
    assert.ok(concurrent.before[0] > 0.02, `${kind}: first clip never sounded`);
    assert.ok(concurrent.after[1] > 0.02, `${kind}: stopping first clip silenced second clip`);
    for (let channel = 0; channel < 2; channel++) {
      assert.ok(Math.abs(concurrent.after[channel] - reference.after[channel]) < 0.000001,
        `${kind}: output after cancellation differs from surviving clip on channel ${channel}`);
    }
    console.log(`PASS ${engineName} ${kind}: first clip cancelled; surviving clip RMS ${concurrent.after[1].toFixed(6)} matches reference`);
  }
} finally {
  await browser.close();
}
