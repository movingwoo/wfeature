// Compare live MIDI controllers with an independent native Web Audio graph.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-controls.mjs URL [chromium|webkit]");
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
    const rate = 48000, fade = 0.005;
    const cc = (control, value, sound = 11, channel = 0) => ({ kind: "controlChange", sound, channel, control, value });
    const on = (note = 69, velocity = 100, sound = 11, channel = 0) => ({ kind: "noteOn", sound, channel, note, velocity });
    const off = (note = 69, sound = 11, channel = 0) => ({ kind: "noteOff", sound, channel, note, velocity: 0 });
    const setupChannel = (sound = 11, channel = 0, pan = 64, volume = 127) => [
      { kind: "programChange", sound, channel, program: 72 }, cc(7, volume, sound, channel),
      cc(11, 127, sound, channel), cc(10, pan, sound, channel),
    ];
    const step = (time, ...events) => ({ time, events: events.flat() });
    const pcm = side => {
      const samples = new Float32Array(rate * 3);
      for (let frame = 0; frame < samples.length / 2; frame++) {
        const at = frame / rate;
        samples[frame * 2 + side] = 0.026 * Math.sin(2 * Math.PI * (227 * at + 73 * at * at)) + 0.008 * Math.sin(2 * Math.PI * 619 * at);
      }
      return samples;
    };
    const wave = sound => ({ kind: "playWave", sound, channels: 2, rate, samples: pcm(sound === 11 ? 0 : 1) });
    const cases = [
      { name: "held", duration: 1.55, steps: [
        step(0, setupChannel(), on()),
        step(0.2, cc(7, 0)), step(0.35, cc(7, 32)), step(0.5, cc(7, 127)),
        step(0.65, cc(11, 0)), step(0.8, cc(11, 32)), step(0.95, cc(11, 127)),
        step(1.1, cc(10, 0)), step(1.3, cc(10, 127)),
      ], windows: [
        ["before", 0.13, 0.18], ["volumeZero", 0.25, 0.30], ["volumeQuarter", 0.40, 0.45], ["volumeRestored", 0.55, 0.60],
        ["expressionZero", 0.70, 0.75], ["expressionQuarter", 0.85, 0.90], ["expressionRestored", 1, 1.05],
        ["panLeft", 1.18, 1.23], ["panRight", 1.38, 1.43],
      ] },
      { name: "overlap-release-percussion-peers", duration: 1.2, steps: [
        step(0, setupChannel(11, 0, 0), setupChannel(11, 1), setupChannel(22, 0, 127),
          setupChannel(11, 9, 0), setupChannel(22, 9, 127),
          on(69, 110), on(69, 85, 22), on(76, 60, 11, 1), wave(11), wave(22)),
        step(0.06, on(72, 55)),
        step(0.08, on(40, 90, 11, 9), on(40, 100, 22, 9)),
        step(0.12, cc(7, 32, 11, 9), cc(10, 127, 11, 9)),
        step(0.14, off(40, 11, 9)),
        step(0.16, cc(11, 0, 11, 9)), step(0.2, cc(11, 127, 11, 9)),
        step(0.25, cc(7, 0), cc(10, 127)), step(0.253, off()),
        step(0.3, cc(7, 64), cc(11, 64)), step(0.33, cc(11, 0)), step(0.45, cc(11, 127)),
        step(0.5, off(72)), step(0.53, cc(7, 0)),
        step(0.6, on(76, 127)), step(0.7, cc(7, 127)), step(0.82, cc(10, 0)), step(0.9, cc(11, 32)),
      ], windows: [["mixedBefore", 0.03, 0.05], ["mixedAfter", 1, 1.05]] },
      { name: "initial-zero", duration: 0.85, steps: [
        step(0, setupChannel(11, 0, 64, 0), on()), step(0.15, cc(7, 127)),
        step(0.3, cc(11, 0)), step(0.45, cc(11, 64)), step(0.6, cc(10, 0)), step(0.7, cc(7, 32)),
      ], windows: [["beforeZero", 0.05, 0.1], ["restored", 0.2, 0.25], ["expressionZero", 0.35, 0.4]] },
    ];
    for (const [name, age, channel] of [["attack", 5, 0], ["decay", 65, 0], ["sustain", 1500, 0], ["percussion", 90, 9]]) {
      const note = channel === 9 ? 40 : 69;
      cases.push({ name: `resume-${name}`, duration: 0.3, steps: [
        step(0, setupChannel(11, channel, 0, 32), cc(11, 100, 11, channel),
          { kind: "noteResume", sound: 11, channel, note, velocity: 100, age }),
        step(0.025, cc(7, 127, 11, channel), cc(11, 64, 11, channel), cc(10, 127, 11, channel)),
        step(0.07, off(note, 11, channel)), step(0.085, cc(7, 0, 11, channel)), step(0.1, cc(7, 127, 11, channel)),
      ], windows: [["audible", 0.035, 0.065]] });
    }

    const makeBuffer = (context, samples, channels, samplingRate = rate) => {
      const buffer = context.createBuffer(channels, samples.length / channels, samplingRate);
      for (let channel = 0; channel < channels; channel++) {
        const target = buffer.getChannelData(channel);
        for (let frame = 0; frame < target.length; frame++) target[frame] = samples[frame * channels + channel];
      }
      return buffer;
    };
    const noise = context => {
      const samples = new Float32Array(rate / 4);
      let seed = 7;
      for (let i = 0; i < samples.length; i++) {
        seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
        samples[i] = seed / 2147483648 - 1;
      }
      return makeBuffer(context, samples, 1);
    };
    const samplesOf = buffer => [buffer.getChannelData(0), buffer.getChannelData(1)];
    const renderPage = async scenario => {
      const context = new OfflineAudioContext(2, Math.ceil(rate * scenario.duration), rate);
      const audio = new PageAudio();
      const clock = { state: "running", get currentTime() { return context.currentTime; }, baseLatency: 0, sampleRate: rate };
      let sources = 0;
      for (const method of ["createGain", "createOscillator", "createStereoPanner", "createBuffer", "createBufferSource", "createBiquadFilter"]) {
        clock[method] = (...args) => {
          if (method === "createOscillator" || method === "createBufferSource") sources++;
          return context[method](...args);
        };
      }
      audio.context = clock;
      audio.midiGain = context.createGain();
      audio.waveGain = context.createGain();
      audio.midiGain.connect(context.destination);
      audio.waveGain.connect(context.destination);
      audio._noise = noise(context);
      let wall = 0;
      const previous = Object.getOwnPropertyDescriptor(performance, "now");
      Object.defineProperty(performance, "now", { configurable: true, value: () => wall * 1000 });
      const timeline = [];
      const apply = entry => {
        timeline.push({ at: audio.eventTime(), events: entry.events });
        playAudioEvents(audio, entry.events);
      };
      try {
        apply(scenario.steps[0]);
        const pauses = scenario.steps.slice(1).map(entry => context.suspend(entry.time));
        const rendering = context.startRendering();
        for (let i = 1; i < scenario.steps.length; i++) {
          await pauses[i - 1];
          wall = context.currentTime;
          apply(scenario.steps[i]);
          await context.resume();
        }
        return { samples: samplesOf(await rendering), timeline, sources };
      } finally {
        if (previous) Object.defineProperty(performance, "now", previous);
        else delete performance.now;
      }
    };

    const renderReference = async (scenario, timeline) => {
      const context = new OfflineAudioContext(2, Math.ceil(rate * scenario.duration), rate);
      const owners = new Map(), channels = new Map(), voices = new Map();
      const drumNoise = noise(context);
      const ownerOutput = (sound, kind) => {
        const key = `${sound}:${kind}`;
        if (!owners.has(key)) {
          const gain = context.createGain();
          gain.connect(context.destination);
          owners.set(key, gain);
        }
        return owners.get(key);
      };
      const channelState = (sound, channel) => {
        const key = `${sound}:${channel}`;
        if (!channels.has(key)) channels.set(key, { volume: 100, expression: 127, pan: 64 });
        return channels.get(key);
      };
      const channelOutput = (sound, channel) => {
        const state = channelState(sound, channel);
        if (!state.gain) {
          state.gain = context.createGain();
          state.panner = context.createStereoPanner();
          const level = state.volume * state.expression / (127 * 127), pan = (state.pan - 64) / 64;
          state.gain.gain.value = level;
          state.panner.pan.value = pan;
          state.levelRamp = { from: level, to: level, start: 0, end: 0 };
          state.panRamp = { from: pan, to: pan, start: 0, end: 0 };
          state.gain.connect(state.panner);
          state.panner.connect(ownerOutput(sound, "midi"));
        }
        return state.gain;
      };
      const ramp = (parameter, previous, value, at) => {
        const from = at >= previous.end ? previous.to : at <= previous.start ? previous.from
          : previous.from + (previous.to - previous.from) * (at - previous.start) / (previous.end - previous.start);
        parameter.cancelScheduledValues(at);
        parameter.setValueAtTime(from, at);
        parameter.linearRampToValueAtTime(value, at + fade);
        return { from, to: value, start: at, end: at + fade };
      };
      const envelopeLevel = (peak, age) => {
        if (age < 0.01) return 0.0001 * (peak / 0.0001) ** (age / 0.01);
        if (age < 0.12) return peak * 0.7 ** ((age - 0.01) / 0.11);
        return peak * 0.7;
      };
      const release = (key, at) => {
        const voice = voices.get(key);
        if (!voice) return;
        voices.delete(key);
        if (voice.drum) return;
        const elapsed = at - voice.at + voice.age;
        voice.envelope.gain.cancelScheduledValues(at);
        voice.envelope.gain.exponentialRampToValueAtTime(envelopeLevel(voice.peak, elapsed), at);
        voice.envelope.gain.exponentialRampToValueAtTime(0.0001, at + 0.06);
        voice.source.stop(at + 0.07);
      };
      for (const { at, events } of timeline) {
        for (const event of events) {
          const { sound, channel, note } = event;
          const key = `${sound}:${channel}:${note}`;
          if (event.kind === "controlChange") {
            const state = channelState(sound, channel);
            if (event.control === 7) state.volume = event.value;
            if (event.control === 11) state.expression = event.value;
            if (event.control === 10) state.pan = event.value;
            if (state.gain && (event.control === 7 || event.control === 11)) {
              state.levelRamp = ramp(state.gain.gain, state.levelRamp, state.volume * state.expression / (127 * 127), at);
            }
            if (state.panner && event.control === 10) state.panRamp = ramp(state.panner.pan, state.panRamp, (state.pan - 64) / 64, at);
          } else if (event.kind === "noteOff") {
            release(key, at);
          } else if (event.kind === "noteOn" || event.kind === "noteResume") {
            const age = event.kind === "noteResume" ? event.age / 1000 : 0;
            const peak = event.velocity / 127 * 0.25, drum = channel === 9;
            const envelope = context.createGain();
            envelope.connect(channelOutput(sound, channel));
            let source;
            if (drum) {
              source = context.createBufferSource();
              source.buffer = drumNoise;
              const filter = context.createBiquadFilter();
              filter.type = "bandpass";
              filter.frequency.value = 200 + (note % 24) * 180;
              source.connect(filter);
              filter.connect(envelope);
              envelope.gain.setValueAtTime(age < 0.18 ? peak * (0.0001 / peak) ** (age / 0.18) : 0.0001, at);
              if (age < 0.18) envelope.gain.exponentialRampToValueAtTime(0.0001, at + 0.18 - age);
              source.start(at, age);
              source.stop(at + 0.2 - age);
            } else {
              source = context.createOscillator();
              source.type = "sine";
              source.frequency.value = 440 * 2 ** ((note - 69) / 12);
              source.connect(envelope);
              envelope.gain.setValueAtTime(envelopeLevel(peak, age), at);
              if (age < 0.01) envelope.gain.exponentialRampToValueAtTime(peak, at + 0.01 - age);
              if (age < 0.12) envelope.gain.exponentialRampToValueAtTime(peak * 0.7, at + 0.12 - age);
              source.start(at);
            }
            voices.set(key, { source, envelope, peak, age, at, drum });
          } else if (event.kind === "playWave") {
            const source = context.createBufferSource(), gain = context.createGain();
            source.buffer = makeBuffer(context, event.samples, event.channels, event.rate);
            gain.gain.value = 0.8;
            source.connect(gain);
            gain.connect(ownerOutput(sound, "wave"));
            source.start(at);
          }
        }
      }
      return samplesOf(await context.startRendering());
    };

    const results = [];
    for (const scenario of cases) {
      const actual = await renderPage(scenario);
      const expected = await renderReference(scenario, actual.timeline);
      let maximumError = 0;
      for (let channel = 0; channel < 2; channel++) {
        for (let frame = 0; frame < expected[channel].length; frame++) {
          maximumError = Math.max(maximumError, Math.abs(actual.samples[channel][frame] - expected[channel][frame]));
        }
      }
      const windows = {};
      for (const [name, start, end] of scenario.windows) {
        const begin = Math.round(start * rate), finish = Math.round(end * rate);
        let leftEnergy = 0, rightEnergy = 0;
        for (let frame = begin; frame < finish; frame++) {
          leftEnergy += actual.samples[0][frame] ** 2;
          rightEnergy += actual.samples[1][frame] ** 2;
        }
        leftEnergy /= finish - begin;
        rightEnergy /= finish - begin;
        windows[name] = { rms: Math.sqrt(leftEnergy + rightEnergy), leftEnergy, rightEnergy };
      }
      const expectedSources = scenario.steps.flatMap(entry => entry.events)
        .filter(event => ["noteOn", "noteResume", "playWave"].includes(event.kind)).length;
      results.push({ scenario: scenario.name, maximumError, sources: actual.sources, expectedSources, windows });
    }
    return results;
  });
  for (const result of results) {
    assert.ok(result.maximumError < 0.000001, `${result.scenario}: native reference sample error ${result.maximumError}`);
    assert.equal(result.sources, result.expectedSources, `${result.scenario}: a controller restarted a source`);
    assert.ok(Object.values(result.windows).some(window => window.rms > 0.0001), `${result.scenario}: all measured output was silent`);
    if (result.scenario === "held") {
      const window = result.windows;
      assert.ok(window.before.rms > 0.01, "held reference was silent");
      assert.ok(window.volumeZero.rms < 1e-8 && window.expressionZero.rms < 1e-8, "zero volume or expression was not silent");
      for (const name of ["volumeQuarter", "expressionQuarter"]) {
        assert.ok(Math.abs(window[name].rms / window.before.rms - 32 / 127) < 0.00001, `${name}: incorrect controller multiplication`);
      }
      for (const name of ["volumeRestored", "expressionRestored"]) {
        assert.ok(Math.abs(window[name].rms / window.before.rms - 1) < 0.00001, `${name}: recovery changed the held envelope`);
      }
      assert.ok(window.panLeft.leftEnergy > 0.001 && window.panLeft.rightEnergy < 1e-12, "live pan did not reach the left channel");
      assert.ok(window.panRight.rightEnergy > 0.001 && window.panRight.leftEnergy < window.panRight.rightEnergy * 0.001, "live pan did not reach the right side");
    }
    if (result.scenario === "initial-zero") {
      assert.ok(result.windows.beforeZero.rms < 1e-8 && result.windows.expressionZero.rms < 1e-8, "initial or later zero was not silent");
      assert.ok(result.windows.restored.rms > 0.01, "a source started at zero did not recover");
    }
    console.log(`PASS ${engineName} ${result.scenario}: ${JSON.stringify(result)}`);
  }
} finally {
  await browser.close();
}
