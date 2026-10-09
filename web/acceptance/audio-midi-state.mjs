// Render MIDI state transitions against an independently scheduled native graph.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-midi-state.mjs URL [chromium|webkit]");
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
    const packet = (events = [], actions = []) => ({ events: [events].flat(), actions: [actions].flat() });
    const step = (time, ...packets) => ({
      time,
      events: packets.flatMap(value => value.events || []),
      actions: packets.flatMap(value => value.actions || []),
      notes: packets.find(value => value.notes)?.notes,
    });
    const retained = (...notes) => ({ notes: notes.sort() });
    const key = (note, sound = 11, channel = 0) => `${sound}:${channel}:${note}`;
    const cc = (control, value, sound = 11, channel = 0) => packet({ kind: "controlChange", sound, channel, control, value });
    const off = (note = 69, sound = 11, channel = 0) => packet({ kind: "noteOff", sound, channel, note, velocity: 0 });
    const release = (...ids) => packet([], ids.map(id => ({ kind: "release", id })));
    const cut = (...ids) => packet([], ids.map(id => ({ kind: "cut", id })));
    const tune = (semitones, sound = 11, channel = 0) => packet([], { kind: "tune", sound, channel, semitones });
    const bend = (value, semitones, sound = 11, channel = 0) => packet(
      { kind: "pitchBend", sound, channel, value }, { kind: "tune", sound, channel, semitones });
    const rpn = (msb = 0, lsb = 0) => [cc(101, msb), cc(100, lsb)];
    const channel = (sound = 11, channel = 0, values = {}) => {
      const state = { program: 72, volume: 127, expression: 127, pan: 64, ...values };
      return packet([
        { kind: "programChange", sound, channel, program: state.program },
        { kind: "controlChange", sound, channel, control: 7, value: state.volume },
        { kind: "controlChange", sound, channel, control: 11, value: state.expression },
        { kind: "controlChange", sound, channel, control: 10, value: state.pan },
      ], { kind: "channel", sound, channel, ...state });
    };
    const note = (id, note = 69, velocity = 100, sound = 11, channel = 0, age = 0) => packet(
      { kind: age ? "noteResume" : "noteOn", sound, channel, note, velocity, age },
      { kind: "note", id, sound, channel, note, velocity, age: age / 1000 });
    const pcm = sound => {
      const samples = new Float32Array(rate * 4);
      const side = sound === 11 ? 0 : 1, frequency = sound === 11 ? 227 : 379;
      for (let frame = 0; frame < samples.length / 2; frame++) {
        const at = frame / rate;
        samples[frame * 2 + side] = 0.025 * Math.sin(2 * Math.PI * (frequency * at + 31 * at * at));
      }
      return samples;
    };
    const wave = sound => packet({ kind: "playWave", sound, channels: 2, rate, samples: pcm(sound) }, { kind: "wave", sound });

    // Expected actions explicitly identify which sources release, stop or change
    // pitch. The reference graph never interprets a MIDI controller or reads the
    // page's channel state, so a controller defect cannot duplicate itself here.
    const cases = [
      { name: "sustain-released-key-and-held-peer", duration: 0.8, steps: [
        step(0, channel(), cc(64, 127), note("released"), note("held", 76, 70), retained(key(69), key(76))),
        step(0.15, off(), retained(key(69), key(76))),
        step(0.38, cc(64, 0), release("released"), retained(key(76))),
        step(0.52, off(76), release("held"), retained()),
      ], windows: [["pedalHeld", 0.23, 0.28], ["heldPeer", 0.47, 0.51], ["silent", 0.67, 0.73]] },
      { name: "all-notes-off-sustain-and-percussion", duration: 0.7, steps: [
        step(0, channel(11, 0, { pan: 0 }), channel(11, 9, { pan: 127 }),
          cc(64, 127), cc(64, 127, 11, 9), note("first"), note("second", 76, 70), note("drum-off", 40, 110, 11, 9)),
        step(0.02, off(40, 11, 9)),
        step(0.04, note("drum-held", 42, 100, 11, 9)),
        step(0.065, cc(123, 0), cc(123, 0, 11, 9), release("drum-off", "drum-held"), retained(key(69), key(76))),
        step(0.15, note("new-held", 79, 85), retained(key(69), key(76), key(79))),
        step(0.28, cc(64, 0), release("first", "second"), retained(key(79))),
        step(0.48, off(79), release("new-held"), retained()),
      ], windows: [["percussionBefore", 0.052, 0.063], ["pedalStillHeld", 0.2, 0.24], ["heldPeer", 0.4, 0.44], ["silent", 0.61, 0.66]] },
      { name: "all-sound-off-tails-drums-peers-and-pcm", duration: 0.55, steps: [
        step(0, channel(11, 0, { pan: 0 }), channel(11, 1), channel(22, 0, { pan: 127 }),
          channel(11, 9, { pan: 0 }), channel(22, 9, { pan: 127 }),
          note("tail"), note("held", 72, 80), note("channel-peer", 76, 65, 11, 1), note("owner-peer", 81, 75, 22),
          note("drum-off", 40, 110, 11, 9), note("drum-peer", 44, 110, 22, 9), wave(11), wave(22)),
        step(0.025, off(40, 11, 9)),
        step(0.045, note("drum-held", 42, 100, 11, 9)),
        step(0.055, off(), release("tail")),
        step(0.075, cc(120, 0), cut("tail", "held"), retained(key(76, 11, 1), key(81, 22))),
        step(0.09, cc(120, 0, 11, 9), cut("drum-off", "drum-held")),
        step(0.24, off(76, 11, 1), off(81, 22), release("channel-peer", "owner-peer"), retained()),
      ], windows: [["before", 0.03, 0.05], ["peersRemain", 0.15, 0.19], ["pcmOnly", 0.39, 0.45]] },
      { name: "bend-range-updates-held-and-release-tail", duration: 0.9, steps: [
        step(0, channel(11, 0, { pan: 127 }), channel(22, 0, { pan: 0 }),
          bend(12288, 1), note("bent"), note("peer", 76, 80, 22)),
        step(0.2, ...rpn(), cc(6, 12), cc(38, 25), tune(6.125)),
        step(0.4, cc(38, 75), tune(6.375)),
        step(0.55, off(), release("bent"), retained(key(76, 22))),
        step(0.57, cc(6, 3), tune(1.5)),
        step(0.58, bend(0, -3)),
        step(0.61, cc(38, 99), tune(-3.99)),
        step(0.74, off(76, 22), release("peer"), retained()),
      ], windows: [["defaultRange", 0.14, 0.18], ["twelveAndCents", 0.3, 0.35], ["bentTail", 0.593, 0.603], ["silent", 0.85, 0.89]] },
      { name: "rpn-cents-carry-reset-and-ignored-data", duration: 1.6, steps: [
        step(0, channel(), bend(12288, 1), note("bent")),
        step(0.14, ...rpn(), cc(38, 99), tune(1.495)),
        step(0.28, cc(96, 0), tune(1.5)),
        step(0.42, cc(97, 127), tune(1.495)),
        step(0.56, cc(6, 12), tune(6)),
        step(0.7, cc(38, 25), tune(6.125)),
        step(0.84, ...rpn(127, 127), cc(6, 50), cc(38, 99), cc(96, 127)),
        step(0.98, ...rpn(0, 1), cc(6, 4), cc(38, 10), cc(97, 127)),
        step(1.12, cc(99, 0), cc(98, 0), cc(6, 1), cc(38, 40), cc(96, 127)),
        step(1.26, ...rpn(), cc(97, 127), tune(6.12)),
        step(1.4, off(), release("bent"), retained()),
      ], windows: [["defaultRange", 0.07, 0.12], ["centCarry", 0.34, 0.39], ["cc6ClearedCents", 0.62, 0.67],
        ["nullIgnored", 0.9, 0.95], ["unsupportedIgnored", 1.04, 1.09], ["nrpnIgnored", 1.18, 1.23], ["silent", 1.52, 1.57]] },
      { name: "reset-controllers-preserves-volume-pan-program-range", duration: 1, steps: [
        step(0, channel(11, 0, { program: 40, volume: 64, expression: 32, pan: 0 }),
          ...rpn(), cc(6, 12), cc(38, 25), cc(64, 127), bend(12288, 6.125), note("released"), note("held", 76, 70)),
        step(0.16, off(), retained(key(69), key(76))),
        step(0.3, cc(121, 0), release("released"), tune(0),
          packet([], { kind: "channel", sound: 11, channel: 0, expression: 127 }),
          cc(6, 24), cc(38, 50), cc(96, 127), retained(key(76))),
        step(0.43, bend(12288, 6.125)),
        step(0.48, note("new-program", 81, 90), retained(key(76), key(81))),
        step(0.6, ...rpn(), cc(38, 75), tune(6.375)),
        step(0.7, off(76), release("held"), retained(key(81))),
        step(0.78, off(81), release("new-program"), retained()),
      ], windows: [["beforeReset", 0.2, 0.25], ["expressionReset", 0.39, 0.42], ["programAndRangeRetained", 0.55, 0.59], ["silent", 0.91, 0.96]] },
    ];
    for (const [name, age] of [["attack", 5], ["decay", 65], ["sustain", 1500]]) {
      cases.push({ name: `resume-pedal-${name}`, duration: 0.43, steps: [
        step(0, channel(), cc(64, 127), note("resumed", 69, 100, 11, 0, age), off(),
          note("held", 76, 70), retained(key(69), key(76))),
        step(0.12, cc(64, 0), release("resumed"), retained(key(76))),
        step(0.25, off(76), release("held"), retained()),
      ], windows: [["pedalHeld", 0.06, 0.1], ["heldPeer", 0.21, 0.24], ["silent", 0.37, 0.41]] });
    }

    const makeBuffer = (context, samples, channels) => {
      const buffer = context.createBuffer(channels, samples.length / channels, rate);
      for (let channel = 0; channel < channels; channel++) {
        const output = buffer.getChannelData(channel);
        for (let frame = 0; frame < output.length; frame++) output[frame] = samples[frame * channels + channel];
      }
      return buffer;
    };
    const noise = context => {
      const samples = new Float32Array(rate / 4);
      let seed = 7;
      for (let index = 0; index < samples.length; index++) {
        seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
        samples[index] = seed / 2147483648 - 1;
      }
      return makeBuffer(context, samples, 1);
    };
    const samplesOf = buffer => [buffer.getChannelData(0), buffer.getChannelData(1)];
    const renderPage = async scenario => {
      const context = new OfflineAudioContext(2, Math.ceil(rate * scenario.duration), rate);
      const audio = new PageAudio();
      const clock = { state: "running", get currentTime() { return context.currentTime; }, baseLatency: 0, sampleRate: rate };
      let sources = 0;
      for (const name of ["createGain", "createOscillator", "createStereoPanner", "createBuffer", "createBufferSource", "createBiquadFilter"]) {
        clock[name] = (...args) => {
          if (name === "createOscillator" || name === "createBufferSource") sources++;
          return context[name](...args);
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
      const times = [], noteChecks = [];
      const apply = entry => {
        times.push(audio.eventTime());
        playAudioEvents(audio, entry.events);
        if (entry.notes) noteChecks.push({
          time: entry.time, expected: entry.notes,
          actual: [...audio.voices.values()].filter(voice => !voice.drum)
            .map(voice => key(voice.note, voice.sound, voice.channel)).sort(),
        });
      };
      try {
        apply(scenario.steps[0]);
        const pauses = scenario.steps.slice(1).map(entry => context.suspend(entry.time));
        const rendering = context.startRendering();
        for (let index = 1; index < scenario.steps.length; index++) {
          await pauses[index - 1];
          wall = context.currentTime;
          apply(scenario.steps[index]);
          await context.resume();
        }
        return { samples: samplesOf(await rendering), times, sources, noteChecks };
      } finally {
        if (previous) Object.defineProperty(performance, "now", previous);
        else delete performance.now;
      }
    };

    const renderReference = async (scenario, times) => {
      const context = new OfflineAudioContext(2, Math.ceil(rate * scenario.duration), rate);
      const channels = new Map(), owners = new Map(), voices = new Map();
      const drumNoise = noise(context);
      const ownerOutput = (sound, kind) => {
        const id = `${sound}:${kind}`;
        if (!owners.has(id)) {
          const gain = context.createGain();
          gain.connect(context.destination);
          owners.set(id, gain);
        }
        return owners.get(id);
      };
      const stateFor = (sound, channel) => {
        const id = `${sound}:${channel}`;
        if (!channels.has(id)) channels.set(id, { program: 72, volume: 127, expression: 127, pan: 64, semitones: 0 });
        return channels.get(id);
      };
      const outputFor = (sound, channel) => {
        const state = stateFor(sound, channel);
        if (!state.gain) {
          state.gain = context.createGain();
          state.panner = context.createStereoPanner();
          state.gain.gain.value = state.volume * state.expression / (127 * 127);
          state.panner.pan.value = (state.pan - 64) / 64;
          state.gain.connect(state.panner);
          state.panner.connect(ownerOutput(sound, "midi"));
        }
        return state.gain;
      };
      const melodicLevel = (peak, elapsed) => elapsed < 0.01
        ? 0.0001 * (peak / 0.0001) ** (elapsed / 0.01)
        : elapsed < 0.12 ? peak * 0.7 ** ((elapsed - 0.01) / 0.11) : peak * 0.7;
      const drumLevel = (peak, elapsed) => elapsed < 0.18 ? peak * (0.0001 / peak) ** (elapsed / 0.18) : 0.0001;
      const frequency = (note, semitones) => 440 * 2 ** ((note + semitones - 69) / 12);
      const apply = (action, at) => {
        if (action.kind === "channel") {
          const state = stateFor(action.sound, action.channel);
          const previousLevel = state.volume * state.expression / (127 * 127), previousPan = (state.pan - 64) / 64;
          for (const name of ["program", "volume", "expression", "pan"]) if (action[name] !== undefined) state[name] = action[name];
          if (state.gain && (action.volume !== undefined || action.expression !== undefined)) {
            state.gain.gain.setValueAtTime(previousLevel, at);
            state.gain.gain.linearRampToValueAtTime(state.volume * state.expression / (127 * 127), at + 0.005);
          }
          if (state.panner && action.pan !== undefined) {
            state.panner.pan.setValueAtTime(previousPan, at);
            state.panner.pan.linearRampToValueAtTime((state.pan - 64) / 64, at + 0.005);
          }
          return;
        }
        if (action.kind === "tune") {
          stateFor(action.sound, action.channel).semitones = action.semitones;
          for (const voice of voices.values()) {
            if (!voice.drum && voice.sound === action.sound && voice.channel === action.channel && at < voice.stopAt) {
              voice.source.frequency.setValueAtTime(frequency(voice.note, action.semitones), at);
            }
          }
          return;
        }
        if (action.kind === "release" || action.kind === "cut") {
          const voice = voices.get(action.id);
          if (!voice) throw new Error(`Native reference has no voice ${action.id}`);
          if (at >= voice.stopAt) return;
          if (action.kind === "cut") {
            voice.stopAt = at;
            voice.source.stop(at);
            return;
          }
          const elapsed = at - voice.at + voice.age;
          const level = voice.drum ? drumLevel(voice.peak, elapsed) : melodicLevel(voice.peak, elapsed);
          voice.envelope.gain.cancelScheduledValues(at);
          if (elapsed > 0) voice.envelope.gain.exponentialRampToValueAtTime(level, at);
          else voice.envelope.gain.setValueAtTime(level, at);
          voice.envelope.gain.exponentialRampToValueAtTime(0.0001, at + 0.06);
          voice.stopAt = Math.min(voice.stopAt, at + 0.07);
          voice.source.stop(voice.stopAt);
          return;
        }
        if (action.kind === "wave") {
          const source = context.createBufferSource(), gain = context.createGain();
          source.buffer = makeBuffer(context, pcm(action.sound), 2);
          gain.gain.value = 0.8;
          source.connect(gain);
          gain.connect(ownerOutput(action.sound, "wave"));
          source.start(at);
          return;
        }
        if (action.kind !== "note") throw new Error(`Unknown native reference action ${action.kind}`);
        const { sound, channel, note, velocity, age } = action;
        const drum = channel === 9, peak = velocity / 127 * 0.25;
        const envelope = context.createGain();
        envelope.connect(outputFor(sound, channel));
        let source, stopAt = Infinity;
        if (drum) {
          source = context.createBufferSource();
          source.buffer = drumNoise;
          const filter = context.createBiquadFilter();
          filter.type = "bandpass";
          filter.frequency.value = 200 + (note % 24) * 180;
          source.connect(filter);
          filter.connect(envelope);
          envelope.gain.setValueAtTime(drumLevel(peak, age), at);
          if (age < 0.18) envelope.gain.exponentialRampToValueAtTime(0.0001, at + 0.18 - age);
          source.start(at, age);
          stopAt = at + 0.2 - age;
          source.stop(stopAt);
        } else {
          source = context.createOscillator();
          const state = stateFor(sound, channel);
          source.type = state.program === 40 ? "sawtooth" : "sine";
          source.frequency.value = frequency(note, state.semitones);
          source.connect(envelope);
          envelope.gain.setValueAtTime(melodicLevel(peak, age), at);
          if (age < 0.01) envelope.gain.exponentialRampToValueAtTime(peak, at + 0.01 - age);
          if (age < 0.12) envelope.gain.exponentialRampToValueAtTime(peak * 0.7, at + 0.12 - age);
          source.start(at);
        }
        voices.set(action.id, { source, envelope, sound, channel, note, drum, peak, age, at, stopAt });
      };
      // Adding all future automation before rendering changes native oscillator
      // rounding compared with live updates. Apply the independent expectations
      // at the same render-clock boundaries as the page's incoming messages.
      for (const action of scenario.steps[0].actions) apply(action, times[0]);
      const pauses = scenario.steps.slice(1).map(entry => context.suspend(entry.time));
      const rendering = context.startRendering();
      for (let index = 1; index < scenario.steps.length; index++) {
        await pauses[index - 1];
        for (const action of scenario.steps[index].actions) apply(action, times[index]);
        await context.resume();
      }
      return samplesOf(await rendering);
    };

    const results = [];
    for (const scenario of cases) {
      const actual = await renderPage(scenario);
      const expected = await renderReference(scenario, actual.times);
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
      results.push({ scenario: scenario.name, maximumError, sources: actual.sources, expectedSources, windows, noteChecks: actual.noteChecks });
    }
    return results;
  });
  for (const result of results) {
    assert.ok(result.maximumError < 0.000001, `${result.scenario}: native reference sample error ${result.maximumError}`);
    assert.equal(result.sources, result.expectedSources, `${result.scenario}: a controller restarted a source`);
    for (const check of result.noteChecks) {
      assert.deepEqual(check.actual, check.expected, `${result.scenario} at ${check.time}: incorrect retained notes`);
    }
    const windows = result.windows;
    assert.ok(Object.values(windows).some(window => window.rms > 0.0001), `${result.scenario}: all measured output was silent`);
    if (windows.silent) assert.ok(windows.silent.rms < 1e-8, `${result.scenario}: released notes remained audible`);
    if (windows.pedalHeld) {
      assert.ok(windows.pedalHeld.rms > 0.01 && windows.heldPeer.rms > 0.01, `${result.scenario}: pedal hold or unreleased peer was silent`);
    }
    if (result.scenario === "all-notes-off-sustain-and-percussion") {
      assert.ok(windows.percussionBefore.rightEnergy > 1e-7, "percussion reference was silent before All Notes Off");
      assert.ok(windows.pedalStillHeld.leftEnergy > 0.001 && windows.pedalStillHeld.rightEnergy < 1e-12,
        "All Notes Off released pedal-held melody or retained percussion");
    }
    if (result.scenario === "all-sound-off-tails-drums-peers-and-pcm") {
      assert.ok(windows.peersRemain.rms > windows.pcmOnly.rms * 2, "All Sound Off silenced a peer channel or owner");
      assert.ok(windows.pcmOnly.leftEnergy > 1e-5 && windows.pcmOnly.rightEnergy > 1e-5, "All Sound Off silenced owned or peer PCM");
    }
    if (result.scenario === "bend-range-updates-held-and-release-tail") {
      assert.ok(windows.bentTail.rightEnergy > 1e-7, "the bent release tail was not audible");
    }
    if (result.scenario === "reset-controllers-preserves-volume-pan-program-range") {
      assert.ok(windows.expressionReset.rms > windows.beforeReset.rms * 1.5, "Reset All Controllers did not restore expression");
      assert.ok(windows.programAndRangeRetained.leftEnergy > 0.001 && windows.programAndRangeRetained.rightEnergy < 1e-12,
        "Reset All Controllers changed channel pan or silenced the retained program");
    }
    const { noteChecks, ...report } = result;
    console.log(`PASS ${engineName} ${result.scenario}: ${JSON.stringify({ ...report, noteCounts: noteChecks.map(check => [check.time, check.actual.length]) })}`);
  }
} finally {
  await browser.close();
}
