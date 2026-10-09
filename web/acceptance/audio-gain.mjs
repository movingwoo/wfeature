// Compare live guest gain changes with uninterrupted Web Audio reference clips.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-gain.mjs URL [chromium|webkit]");
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
    const render = async (kind, owners, changeGain) => {
      const context = new OfflineAudioContext(2, Math.round(rate * 1.3), rate);
      const audio = new PageAudio();
      const clock = { state: "running", get currentTime() { return context.currentTime; }, baseLatency: 0 };
      for (const name of ["createGain", "createOscillator", "createStereoPanner", "createBuffer", "createBufferSource"]) {
        clock[name] = context[name].bind(context);
      }
      audio.context = clock;
      audio.master = context.createGain();
      audio.master.connect(context.destination);
      audio.midiGain = context.createGain();
      audio.midiGain.connect(audio.master);
      audio.waveGain = context.createGain();
      audio.waveGain.connect(audio.master);
      audio.setMasterVolume(0.8);
      audio.setMIDIVolume(0.6);
      audio.setWaveVolume(0.7);
      let wall = 0;
      const previous = Object.getOwnPropertyDescriptor(performance, "now");
      Object.defineProperty(performance, "now", { configurable: true, value: () => wall * 1000 });
      try {
        const events = [];
        for (const sound of owners) {
          events.push({ kind: "soundGain", sound, value: 10000 });
          if (kind === "MIDI") {
            events.push(
              { kind: "programChange", sound, channel: 0, program: 72 },
              { kind: "controlChange", sound, channel: 0, control: 10, value: sound === 1 ? 0 : 127 },
              { kind: "noteOn", sound, channel: 0, note: 69, velocity: 100 },
            );
          } else {
            const samples = new Float32Array(rate * 4);
            for (let frame = 0; frame < rate * 2; frame++) {
              const at = frame / rate;
              // A changing pitch makes replaying a sample prefix detectable.
              samples[frame * 2 + sound - 1] = 0.16 * Math.sin(2 * Math.PI * (220 * at + 55 * at * at)) + 0.05 * Math.sin(2 * Math.PI * 613 * at);
            }
            events.push({ kind: "playWave", sound, channels: 2, rate, samples });
          }
        }
        playAudioEvents(audio, events);
        const changes = changeGain ? [{ at: 0.3, value: 0 }, { at: 0.55, value: 2500 }, { at: 0.85, value: 10000 }] : [];
        const pauses = changes.map(change => context.suspend(change.at));
        const rendered = context.startRendering();
        for (const [index, change] of changes.entries()) {
          await pauses[index];
          wall = context.currentTime;
          playAudioEvents(audio, [{ kind: "soundGain", sound: 1, value: change.value }]);
          await context.resume();
        }
        const buffer = await rendered;
        return [buffer.getChannelData(0), buffer.getChannelData(1)];
      } finally {
        if (previous) Object.defineProperty(performance, "now", previous);
        else delete performance.now;
      }
    };
    const results = [];
    for (const kind of ["MIDI", "PCM"]) {
      const actual = await render(kind, [1, 2], true);
      const first = await render(kind, [1], false);
      const second = await render(kind, [2], false);
      const windows = [];
      for (const [name, start, end, level] of [
        ["initial", 0.18, 0.25, 1],
        ["muted", 0.4, 0.48, 0],
        ["quarter", 0.67, 0.75, 0.25],
        ["restored", 0.93, 1.1, 1],
      ]) {
        const begin = Math.round(start * rate), finish = Math.round(end * rate);
        let maximumError = 0, firstEnergy = 0, secondEnergy = 0, observedEnergy = 0;
        for (let frame = begin; frame < finish; frame++) {
          for (let channel = 0; channel < 2; channel++) {
            maximumError = Math.max(maximumError, Math.abs(actual[channel][frame] - first[channel][frame] * level - second[channel][frame]));
          }
          firstEnergy += first[0][frame] ** 2;
          secondEnergy += second[1][frame] ** 2;
          observedEnergy += (actual[0][frame] - second[0][frame]) ** 2;
        }
        windows.push({ name, level, maximumError, firstRMS: Math.sqrt(firstEnergy / (finish - begin)),
          secondRMS: Math.sqrt(secondEnergy / (finish - begin)), observedRatio: Math.sqrt(observedEnergy / firstEnergy) });
      }
      results.push({ kind, windows });
    }
    return results;
  });
  for (const { kind, windows } of results) {
    for (const window of windows) {
      assert.ok(window.firstRMS > 0.01 && window.secondRMS > 0.01, `${kind} ${window.name}: reference clips were silent`);
      assert.ok(window.maximumError < 0.000001, `${kind} ${window.name}: sample phase, envelope or peer output changed: ${window.maximumError}`);
      assert.ok(Math.abs(window.observedRatio - window.level) < 0.00001, `${kind} ${window.name}: level ${window.observedRatio} differs from ${window.level}`);
    }
    console.log(`PASS ${engineName} ${kind}: live mute, quarter gain and unmute preserve phase and peer output`);
  }
} finally {
  await browser.close();
}
