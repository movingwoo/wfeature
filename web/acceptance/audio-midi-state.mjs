// Render MIDI state transitions against an independently scheduled synthesizer.
//
// Each step names what the synthesizer should do — start a note, release or
// cut one, retune a channel — next to the MIDI it sends the page. The page
// renders the MIDI through its real FM synthesizer host; the reference renders
// the synthesizer from the named actions alone and routes each lane through
// native nodes, so a controller defect cannot repeat itself in the reference.
// WFEATURE_AUDIO_NO_WORKLET=1 renders the page's ScriptProcessor fallback.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-midi-state.mjs URL [chromium|webkit]");
}
const worklet = process.env.WFEATURE_AUDIO_NO_WORKLET !== "1";
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const browser = await engine.launch({ headless: true });
try {
  // A blank page of the server's origin: the harness imports the page's audio
  // modules itself and needs none of the app around them, whose own forty-odd
  // module loads and server calls only add requests that can fail.
  const page = await browser.newPage();
  await page.route(url => url.href === new URL(origin).href,
    route => route.fulfill({ contentType: "text/html", body: "<!doctype html><title>Audio acceptance</title>" }));
  await page.goto(origin);
  const results = await page.evaluate(async worklet => {
    const { playAudioEvents } = await import("./session.js");
    const { RATE, modeTiming, pageAudio, renderSteps, channelsOf, wallClock, referenceLanes, referenceMixer, transition, steady,
      maximumError, describeMismatch, energy } = await import("./acceptance/audio-offline.js");
    // Every step schedules its events, so the fallback's longer lead moves only
    // the measured windows and the render's length.
    const { baseLatency, lead, shift } = modeTiming(worklet);
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
      const samples = new Float32Array(RATE * 4);
      const side = sound === 11 ? 0 : 1, frequency = sound === 11 ? 227 : 379;
      for (let frame = 0; frame < samples.length / 2; frame++) {
        const at = frame / RATE;
        samples[frame * 2 + side] = 0.025 * Math.sin(2 * Math.PI * (frequency * at + 31 * at * at));
      }
      return samples;
    };
    const wave = sound => packet({ kind: "playWave", sound, channels: 2, rate: RATE, samples: pcm(sound) }, { kind: "wave", sound });

    // Expected actions explicitly identify which notes release, stop or change
    // pitch. The reference never interprets a MIDI controller or reads the
    // page's channel state, so a controller defect cannot duplicate itself here.
    // The synthesizer's release tails are longer than the old oscillator's
    // 60 ms, so every scenario runs until its last tail has ended, and a
    // window that must be silent starts after it. Percussion plays tonal
    // stand-ins (kicks 36 and 35, cowbell 56): the noise drums draw
    // Math.random, which no independent render can repeat. A stand-in drum
    // rings for as long as its envelope lasts, so the drums All Notes Off
    // releases are checked for silence once their release has ended
    // (percussionReleased), where left alone they would still sound.
    const cases = [
      { name: "sustain-released-key-and-held-peer", duration: 1, steps: [
        step(0, channel(), cc(64, 127), note("released"), note("held", 76, 70), retained(key(69), key(76))),
        step(0.15, off(), retained(key(69), key(76))),
        step(0.38, cc(64, 0), release("released"), retained(key(76))),
        step(0.52, off(76), release("held"), retained()),
      ], windows: [["pedalHeld", 0.23, 0.28], ["heldPeer", 0.47, 0.51], ["silent", 0.93, 0.98]] },
      { name: "all-notes-off-sustain-and-percussion", duration: 1, steps: [
        step(0, channel(11, 0, { pan: 0 }), channel(11, 9, { pan: 127 }),
          cc(64, 127), cc(64, 127, 11, 9), note("first"), note("second", 76, 70), note("drum-off", 36, 110, 11, 9)),
        step(0.02, off(36, 11, 9)),
        step(0.04, note("drum-held", 35, 100, 11, 9)),
        step(0.065, cc(123, 0), cc(123, 0, 11, 9), release("drum-off", "drum-held"), retained(key(69), key(76))),
        step(0.15, note("new-held", 79, 85), retained(key(69), key(76), key(79))),
        step(0.28, cc(64, 0), release("first", "second"), retained(key(79))),
        step(0.48, off(79), release("new-held"), retained()),
      ], windows: [["percussionBefore", 0.052, 0.063], ["pedalStillHeld", 0.2, 0.24], ["percussionReleased", 0.34, 0.38],
        ["heldPeer", 0.4, 0.44], ["silent", 0.9, 0.95]] },
      { name: "all-sound-off-tails-drums-peers-and-pcm", duration: 0.8, steps: [
        step(0, channel(11, 0, { pan: 0 }), channel(11, 1), channel(22, 0, { pan: 127 }),
          channel(11, 9, { pan: 0 }), channel(22, 9, { pan: 127 }),
          note("tail"), note("held", 72, 80), note("channel-peer", 76, 65, 11, 1), note("owner-peer", 81, 75, 22),
          note("drum-off", 36, 110, 11, 9), note("drum-peer", 56, 110, 22, 9), wave(11), wave(22)),
        step(0.025, off(36, 11, 9)),
        step(0.045, note("drum-held", 35, 100, 11, 9)),
        step(0.055, off(), release("tail")),
        step(0.075, cc(120, 0), cut("tail", "held"), retained(key(76, 11, 1), key(81, 22))),
        step(0.09, cc(120, 0, 11, 9), cut("drum-off", "drum-held")),
        step(0.24, off(76, 11, 1), off(81, 22), release("channel-peer", "owner-peer"), retained()),
      ], windows: [["before", 0.03, 0.05], ["peersRemain", 0.15, 0.19], ["pcmOnly", 0.68, 0.74]] },
      { name: "bend-range-updates-held-and-release-tail", duration: 1.2, steps: [
        step(0, channel(11, 0, { pan: 127 }), channel(22, 0, { pan: 0 }),
          bend(12288, 1), note("bent"), note("peer", 76, 80, 22)),
        step(0.2, ...rpn(), cc(6, 12), cc(38, 25), tune(6.125)),
        step(0.4, cc(38, 75), tune(6.375)),
        step(0.55, off(), release("bent"), retained(key(76, 22))),
        step(0.57, cc(6, 3), tune(1.5)),
        step(0.58, bend(0, -3)),
        step(0.61, cc(38, 99), tune(-3.99)),
        step(0.74, off(76, 22), release("peer"), retained()),
      ], windows: [["defaultRange", 0.14, 0.18], ["twelveAndCents", 0.3, 0.35], ["bentTail", 0.593, 0.603], ["silent", 1.14, 1.18]] },
      { name: "rpn-cents-carry-reset-and-ignored-data", duration: 1.9, steps: [
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
        ["nullIgnored", 0.9, 0.95], ["unsupportedIgnored", 1.04, 1.09], ["nrpnIgnored", 1.18, 1.23], ["silent", 1.8, 1.85]] },
      { name: "reset-controllers-preserves-volume-pan-program-range", duration: 1.65, steps: [
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
      ], windows: [["beforeReset", 0.2, 0.25], ["expressionReset", 0.39, 0.42], ["programAndRangeRetained", 0.55, 0.59], ["silent", 1.55, 1.6]] },
    ];
    for (const [name, age] of [["attack", 5], ["decay", 65], ["sustain", 1500]]) {
      cases.push({ name: `resume-pedal-${name}`, duration: 0.72, steps: [
        step(0, channel(), cc(64, 127), note("resumed", 69, 100, 11, 0, age), off(),
          note("held", 76, 70), retained(key(69), key(76))),
        step(0.12, cc(64, 0), release("resumed"), retained(key(76))),
        step(0.25, off(76), release("held"), retained()),
      ], windows: [["pedalHeld", 0.06, 0.1], ["heldPeer", 0.21, 0.24], ["silent", 0.66, 0.7]] });
    }

    const renderPage = async scenario => {
      const frames = Math.ceil(RATE * (scenario.duration + shift));
      const context = new OfflineAudioContext(2, frames, RATE);
      const page = await pageAudio(context, { worklet, baseLatency });
      const wall = wallClock();
      const times = [], noteChecks = [];
      try {
        const rendered = await renderSteps(context, page, scenario.steps, entry => {
          wall.set(context.currentTime);
          times.push(page.audio.eventTime());
          playAudioEvents(page.audio, entry.events);
          if (entry.notes) noteChecks.push({
            time: entry.time, expected: entry.notes,
            actual: [...page.audio.voices.values()].filter(voice => !voice.drum)
              .map(voice => key(voice.note, voice.sound, voice.channel)).sort(),
          });
        });
        return { samples: channelsOf(rendered.buffer), times, marks: rendered.marks, noteChecks, page };
      } finally {
        wall.restore();
      }
    };

    // The reference applies each step's actions where the page's step reached
    // its synthesizer, timed by the page's scheduling clock.
    const renderReference = async (scenario, actual) => {
      const frames = Math.ceil(RATE * (scenario.duration + shift));
      const channels = new Map(), ids = new Map(), waves = [], entries = [];
      let lanes = 0;
      const stateFor = (sound, channel) => {
        const id = `${sound}:${channel}`;
        if (!channels.has(id)) {
          channels.set(id, { sound, channel, program: 72, volume: 127, expression: 127, pan: 64, semitones: 0, lane: null, changes: [] });
        }
        return channels.get(id);
      };
      const level = state => state.volume * state.expression / (127 * 127);
      const position = state => (state.pan - 64) / 64;
      const ratio = semitones => Math.pow(2, semitones / 12);
      for (const [index, entry] of scenario.steps.entries()) {
        const at = actual.times[index], commands = [];
        for (const action of entry.actions) {
          if (action.kind === "channel") {
            const state = stateFor(action.sound, action.channel);
            for (const name of ["program", "volume", "expression", "pan"]) if (action[name] !== undefined) state[name] = action[name];
            if (state.lane !== null && (action.volume !== undefined || action.expression !== undefined)) state.changes.push({ at, level: level(state) });
            if (state.lane !== null && action.pan !== undefined) state.changes.push({ at, pan: position(state) });
          } else if (action.kind === "tune") {
            const state = stateFor(action.sound, action.channel);
            state.semitones = action.semitones;
            if (state.lane !== null) commands.push({ op: "bend", lane: state.lane, at, ratio: ratio(state.semitones) });
          } else if (action.kind === "note") {
            const state = stateFor(action.sound, action.channel), drum = action.channel === 9;
            if (state.lane === null) {
              Object.assign(state, { lane: lanes++, level: level(state), position: position(state) });
              commands.push({ op: "lane", lane: state.lane, ratio: drum ? 1 : ratio(state.semitones), depth: 0 });
            }
            ids.set(action.id, ids.size + 1);
            commands.push({ op: "on", id: ids.get(action.id), at, lane: state.lane, pitch: action.note, velocity: action.velocity,
              program: state.program, drumKey: drum ? action.note : undefined, age: action.age });
          } else if (action.kind === "release" || action.kind === "cut") {
            if (!ids.has(action.id)) throw new Error(`the reference has no note ${action.id}`);
            commands.push(action.kind === "release" ? { op: "off", id: ids.get(action.id), at }
              : { op: "cut", id: ids.get(action.id), at, fade: 0.005 });
          } else if (action.kind === "wave") {
            waves.push({ at, sound: action.sound });
          } else {
            throw new Error(`unknown reference action ${action.kind}`);
          }
        }
        entries.push({ mark: actual.marks[index], commands });
      }
      const rendered = referenceLanes(actual.page, frames, entries);
      // The same drums never keyed off, to show a window where released drums
      // are silent is one where drums left alone still sound.
      const drums = new Set([...ids.entries()].filter(([name]) => scenario.steps.some(entry =>
        entry.actions.some(action => action.kind === "note" && action.id === name && action.channel === 9))).map(([, id]) => id));
      const unreleased = referenceLanes(actual.page, frames, entries.map(entry =>
        ({ ...entry, commands: entry.commands.filter(command => command.op !== "off" || !drums.has(command.id)) })));
      const drumLanes = [...channels.values()].filter(state => state.channel === 9 && state.lane !== null).map(state => state.lane);
      const unreleasedDrums = (start, end) => drumLanes.reduce((sum, lane) => sum + energy(unreleased[lane], start, end), 0);
      const mix = referenceMixer(frames, 2);
      for (const state of channels.values()) {
        if (state.lane === null) continue;
        const lane = mix.lane(rendered[state.lane], state.sound, state.level, state.position);
        let gain = steady(state.level), pan = steady(state.position);
        for (const change of state.changes) {
          if (change.level !== undefined) gain = transition(lane.gain.gain, gain, change.level, change.at);
          else pan = transition(lane.panner.pan, pan, change.pan, change.at);
        }
      }
      for (const { at, sound } of waves) mix.sample(pcm(sound), 2, sound, at);
      // When each channel's synthesizer lane last sounds, in scenario time.
      const ends = {};
      for (const state of channels.values()) {
        if (state.lane === null) continue;
        const samples = rendered[state.lane];
        let last = samples.length - 1;
        while (last >= 0 && samples[last] === 0) last--;
        ends[`${state.sound}:${state.channel}`] = Number(((last + 1) / RATE - shift).toFixed(4));
      }
      return { samples: await mix.render(), ends, unreleasedDrums };
    };

    const results = [];
    for (const scenario of cases) {
      const actual = await renderPage(scenario);
      const expected = await renderReference(scenario, actual);
      const windows = {};
      for (const [name, from, to] of scenario.windows) {
        const leftEnergy = energy(actual.samples[0], from + shift, to + shift), rightEnergy = energy(actual.samples[1], from + shift, to + shift);
        windows[name] = { rms: Math.sqrt(leftEnergy + rightEnergy), leftEnergy, rightEnergy,
          unreleasedDrumEnergy: expected.unreleasedDrums(from + shift, to + shift) };
      }
      const events = scenario.steps.flatMap(entry => entry.events);
      const clockError = Math.max(...actual.times.map((at, index) => Math.abs(at - (actual.marks[index].frame / RATE + lead))));
      const diagnosis = describeMismatch(actual.samples, expected.samples, { steps: actual.marks.map((mark, index) =>
        ({ label: `requested ${scenario.steps[index].time} s`, mark, at: actual.times[index] })) });
      results.push({ scenario: scenario.name, mode: actual.page.mode, late: actual.page.late.length, clockError,
        maximumError: maximumError(actual.samples, expected.samples),
        notes: events.filter(event => ["noteOn", "noteResume"].includes(event.kind)).length,
        starts: actual.page.commands.filter(command => command.op === "on").length,
        waves: events.filter(event => event.kind === "playWave").length, bufferSources: actual.page.bufferSources(),
        laneEnds: expected.ends, windows, noteChecks: actual.noteChecks, ...diagnosis && { diagnosis } });
    }
    return results;
  }, worklet);
  for (const result of results) {
    assert.equal(result.mode, worklet ? "worklet" : "script");
    assert.equal(result.late, 0, `${result.scenario}: a command reached the synthesizer after the time it was for`);
    assert.ok(result.clockError < 1e-9, `${result.scenario}: a step was not scheduled one lead after the render clock: ${result.clockError}`);
    assert.ok(result.maximumError < 0.000001, `${result.scenario}: synthesizer reference sample error ${result.maximumError}${result.diagnosis ?? ""}`);
    assert.equal(result.starts, result.notes, `${result.scenario}: a controller restarted a note`);
    assert.equal(result.bufferSources, result.waves, `${result.scenario}: a controller restarted a sample`);
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
      assert.ok(windows.percussionBefore.rightEnergy > 1e-7, "percussion was silent before All Notes Off");
      assert.ok(windows.pedalStillHeld.leftEnergy > 0.001, "All Notes Off released pedal-held melody");
      assert.ok(windows.percussionReleased.unreleasedDrumEnergy > 1e-9, "drums left alone would be silent where released ones are checked");
      assert.ok(windows.percussionReleased.rightEnergy < 1e-12, "All Notes Off retained percussion");
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
    console.log(`PASS ${engineName} ${result.mode} ${result.scenario}: ${JSON.stringify({ ...report, noteCounts: noteChecks.map(check => [check.time, check.actual.length]) })}`);
  }
} finally {
  await browser.close();
}
