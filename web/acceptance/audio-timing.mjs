// Render short notes against the coarse output clock observed in Android WebView.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-timing.mjs URL [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const browser = await engine.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.goto(origin);
  const result = await page.evaluate(async () => {
    const { PageAudio } = await import("./audio.js");
    const rate = 48000;
    const context = new OfflineAudioContext(1, rate * 3, rate);
    const audio = new PageAudio();
    const clock = { state: "running", currentTime: 0, baseLatency: 0.09 };
    for (const name of ["createGain", "createOscillator", "createStereoPanner"]) {
      clock[name] = context[name].bind(context);
    }
    audio.context = clock;
    audio.midiGain = context.createGain();
    audio.midiGain.connect(context.destination);
    audio.programChange(0, 72);
    let wall = 0;
    const previous = Object.getOwnPropertyDescriptor(performance, "now");
    Object.defineProperty(performance, "now", { configurable: true, value: () => wall * 1000 });
    try {
      for (let index = 0; index < 12; index++) {
        wall = index * 0.18;
        clock.currentTime = Math.floor((wall + 1e-9) / 0.09) * 0.09;
        audio.noteOn(0, 69, 100);
        wall += 0.035;
        audio.noteOff(0, 69);
      }
    } finally {
      if (previous) Object.defineProperty(performance, "now", previous);
      else delete performance.now;
    }
    const samples = (await context.startRendering()).getChannelData(0);
    const rms = (start, end) => {
      const first = Math.round(start * rate), last = Math.round(end * rate);
      let sum = 0;
      for (let index = first; index < last; index++) sum += samples[index] ** 2;
      return Math.sqrt(sum / (last - first));
    };
    return Array.from({ length: 12 }, (_, index) => {
      const start = 0.09 + index * 0.18;
      return { body: rms(start + 0.015, start + 0.030), rest: rms(start + 0.11, start + 0.17) };
    });
  });
  for (const [index, note] of result.entries()) {
    assert.ok(note.body > 0.02, `short note ${index} lost its envelope: ${note.body}`);
    assert.ok(note.rest < 0.00001, `short note ${index} rang through its rest: ${note.rest}`);
  }
  console.log(`PASS ${engineName}: all 12 short notes render with their attacks and rests on a 90 ms clock`);
} finally {
  await browser.close();
}
