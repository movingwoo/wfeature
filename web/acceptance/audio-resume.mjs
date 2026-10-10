// Render held envelopes and PCM suffixes after pausing one owned sound.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-resume.mjs URL [chromium|webkit]");
}
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const browser = await engine.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.goto(origin);
  const results = await page.evaluate(async () => {
    const { PageAudio } = await import("./audio.js");
    const rate = 48000, duration = 1.2;
    const peak = 100 / 127 * 0.25;
    const context = () => new OfflineAudioContext(2, rate * duration, rate);
    const pcm = side => {
      const samples = new Float32Array(rate * 4);
      for (let frame = 0; frame < rate * 2; frame++) {
        const at = frame / rate;
        samples[frame * 2 + side] = 0.16 * Math.sin(2 * Math.PI * (227 * at + 73 * at * at)) + 0.05 * Math.sin(2 * Math.PI * 619 * at);
      }
      return samples;
    };
    const firstPCM = pcm(0), peerPCM = pcm(1);
    const buffer = (ctx, samples, channels) => {
      const result = ctx.createBuffer(channels, samples.length / channels, rate);
      for (let channel = 0; channel < channels; channel++) {
        const target = result.getChannelData(channel);
        for (let frame = 0; frame < target.length; frame++) target[frame] = samples[frame * channels + channel];
      }
      return result;
    };
    const noise = ctx => {
      const samples = new Float32Array(rate / 4);
      let seed = 7;
      for (let i = 0; i < samples.length; i++) {
        seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
        samples[i] = seed / 2147483648 - 1;
      }
      return buffer(ctx, samples, 1);
    };
    const pageAudio = ctx => {
      const audio = new PageAudio();
      const clock = { state: "running", get currentTime() { return ctx.currentTime; }, baseLatency: 0, sampleRate: rate };
      for (const name of ["createGain", "createOscillator", "createStereoPanner", "createBuffer", "createBufferSource", "createBiquadFilter"]) {
        clock[name] = ctx[name].bind(ctx);
      }
      audio.context = clock;
      audio.midiGain = ctx.createGain();
      audio.midiGain.connect(ctx.destination);
      audio.waveGain = ctx.createGain();
      audio.waveGain.connect(ctx.destination);
      audio._noise = noise(ctx);
      return audio;
    };
    const renderPaused = async kind => {
      const ctx = context(), audio = pageAudio(ctx);
      let wall = 0;
      const previous = Object.getOwnPropertyDescriptor(performance, "now");
      Object.defineProperty(performance, "now", { configurable: true, value: () => wall * 1000 });
      try {
        const firstStart = audio.eventTime();
        audio.playWave(2, rate, peerPCM, 2);
        const channel = kind === "percussion" ? 9 : 0;
        const note = kind === "percussion" ? 40 : 69;
        const restoreChannel = () => {
          audio.setSoundGain(1, 10000);
          audio.programChange(channel, 72, 1);
          audio.controlChange(channel, 10, 0, 1);
        };
        restoreChannel();
        if (kind === "PCM") audio.playWave(2, rate, firstPCM, 1);
        else audio.noteOn(channel, note, 100, 1);
        const paused = ctx.suspend(kind === "percussion" ? 0.1 : 0.3);
        const resumed = ctx.suspend(kind === "percussion" ? 0.4 : 0.65);
        const rendering = ctx.startRendering();
        await paused;
        wall = ctx.currentTime;
        const stoppedAt = ctx.currentTime;
        const savedFrames = Math.floor((stoppedAt - firstStart) * rate + 1e-6);
        const savedAgeMS = Math.floor((stoppedAt - firstStart) * 1000);
        audio.stopSound(1);
        await ctx.resume();
        await resumed;
        wall = ctx.currentTime;
        const resumeStart = audio.eventTime();
        restoreChannel();
        if (kind === "PCM") audio.playWave(2, rate, firstPCM.subarray(savedFrames * 2), 1);
        else audio.noteResume(channel, note, 100, savedAgeMS, 1);
        await ctx.resume();
        const rendered = await rendering;
        return { samples: [rendered.getChannelData(0), rendered.getChannelData(1)],
          firstStart, stoppedAt, resumeStart, savedFrames, savedAgeMS };
      } finally {
        if (previous) Object.defineProperty(performance, "now", previous);
        else delete performance.now;
      }
    };
    const renderReference = async (kind, saved) => {
      const ctx = context();
      const playPCM = (samples, at, offset = 0) => {
        const source = ctx.createBufferSource(), gain = ctx.createGain();
        source.buffer = buffer(ctx, samples, 2);
        gain.gain.value = 0.8;
        source.connect(gain);
        gain.connect(ctx.destination);
        source.start(at, offset);
      };
      playPCM(peerPCM, saved.firstStart);
      if (kind === "PCM") {
        // Offset the complete original buffer independently of the suffix path.
        playPCM(firstPCM, saved.resumeStart, saved.savedFrames / rate);
      } else {
        const gain = ctx.createGain(), channelGain = ctx.createGain(), pan = ctx.createStereoPanner();
        channelGain.gain.value = 100 / 127;
        gain.connect(channelGain);
        channelGain.connect(pan);
        pan.pan.value = -1;
        pan.connect(ctx.destination);
        if (kind === "MIDI") {
          // Oscillator phase restarts. The reference starts a fresh oscillator
          // directly at the saved sustain level, without another attack.
          const source = ctx.createOscillator();
          source.type = "sine";
          source.frequency.value = 440;
          gain.gain.value = peak * 0.7;
          source.connect(gain);
          source.start(saved.resumeStart);
        } else {
          const source = ctx.createBufferSource(), filter = ctx.createBiquadFilter();
          source.buffer = noise(ctx);
          filter.type = "bandpass";
          filter.frequency.value = 3080;
          source.connect(filter);
          filter.connect(gain);
          const age = saved.savedAgeMS / 1000;
          gain.gain.setValueAtTime(peak * (0.0001 / peak) ** (age / 0.18), saved.resumeStart);
          gain.gain.exponentialRampToValueAtTime(0.0001, saved.resumeStart + 0.18 - age);
          source.start(saved.resumeStart, age);
          source.stop(saved.resumeStart + 0.2 - age);
        }
      }
      const rendered = await ctx.startRendering();
      return [rendered.getChannelData(0), rendered.getChannelData(1)];
    };
    const rms = (samples, start, end) => {
      const first = Math.ceil(start * rate), last = Math.floor(end * rate);
      let energy = 0;
      for (let frame = first; frame < last; frame++) energy += samples[frame] ** 2;
      return Math.sqrt(energy / (last - first));
    };
    const maximumError = (actual, expected, start, end) => {
      let error = 0;
      for (let frame = Math.ceil(start * rate); frame < Math.floor(end * rate); frame++) {
        error = Math.max(error, Math.abs(actual[frame] - expected[frame]));
      }
      return error;
    };
    const results = [];
    for (const kind of ["MIDI", "PCM", "percussion"]) {
      const saved = await renderPaused(kind);
      const reference = await renderReference(kind, saved);
      const onset = saved.resumeStart + 0.0005;
      const tailEnd = saved.resumeStart + 0.2 - saved.savedAgeMS / 1000;
      results.push({ kind, savedAgeMS: saved.savedAgeMS, savedFrames: saved.savedFrames,
        beforeRMS: rms(saved.samples[0], 0.03, 0.06),
        pausedRMS: rms(saved.samples[0], saved.stoppedAt + 0.02, saved.resumeStart - 0.02),
        onsetRMS: rms(saved.samples[0], onset, onset + 0.006),
        referenceOnsetRMS: rms(reference[0], onset, onset + 0.006),
        resumeError: maximumError(saved.samples[0], reference[0], onset, saved.resumeStart + 0.08),
        peerError: maximumError(saved.samples[1], reference[1], 0.02, 1.1),
        peerPausedRMS: rms(saved.samples[1], saved.stoppedAt + 0.02, saved.resumeStart - 0.02),
        tailRMS: kind === "percussion" ? rms(saved.samples[0], tailEnd + 0.02, tailEnd + 0.05) : null });
    }
    return results;
  });
  for (const result of results) {
    const { kind } = result;
    assert.ok(result.beforeRMS > 0.005, `${kind}: clip did not sound before pause`);
    assert.ok(result.pausedRMS < 0.000001, `${kind}: paused owner remained audible`);
    assert.ok(result.peerPausedRMS > 0.02 && result.peerError < 0.000001, `${kind}: pause/resume changed the peer's output`);
    assert.ok(result.referenceOnsetRMS > 0.0001, `${kind}: resume reference was silent`);
    assert.ok(Math.abs(result.onsetRMS - result.referenceOnsetRMS) < 0.000001, `${kind}: resume replayed an attack or a sample prefix`);
    assert.ok(result.resumeError < 0.000001, `${kind}: resumed output differs from the envelope or PCM suffix reference: ${result.resumeError}`);
    if (kind === "percussion") assert.ok(result.tailRMS < 0.0000001, "percussion: resume extended the saved tail");
  }
  const metrics = results.map(result => Object.fromEntries(Object.entries(result).map(([key, value]) =>
    [key, typeof value === "number" ? Number(value.toPrecision(6)) : value])));
  console.log(JSON.stringify({ engine: engineName, status: "pass", metrics }));
} finally {
  await browser.close();
}
