// Compare live MIDI controllers with an independent synthesizer reference.
//
// The page renders each scenario through its real FM synthesizer host. The
// reference reads the authored events itself: it renders the synthesizer with
// one lane per channel and routes each lane through native gain and panner
// nodes moved the way the controllers say, (volume / 127) * (expression / 127)
// and (pan - 64) / 64 over 5 ms. WFEATURE_AUDIO_NO_WORKLET=1 renders the page's
// ScriptProcessor fallback.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-controls.mjs URL [chromium|webkit]");
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
    // Every step here schedules its events, so the fallback's longer lead moves
    // only the measured windows and the render's length.
    const { baseLatency, lead, shift } = modeTiming(worklet);
    // Percussion plays the stand-in kick: the noise drums draw Math.random,
    // which no independent render can repeat.
    const drumKey = 36;
    const cc = (control, value, sound = 11, channel = 0) => ({ kind: "controlChange", sound, channel, control, value });
    const on = (note = 69, velocity = 100, sound = 11, channel = 0) => ({ kind: "noteOn", sound, channel, note, velocity });
    const off = (note = 69, sound = 11, channel = 0) => ({ kind: "noteOff", sound, channel, note, velocity: 0 });
    const setupChannel = (sound = 11, channel = 0, pan = 64, volume = 127) => [
      { kind: "programChange", sound, channel, program: 72 }, cc(7, volume, sound, channel),
      cc(11, 127, sound, channel), cc(10, pan, sound, channel),
    ];
    const step = (time, ...events) => ({ time, events: events.flat() });
    const pcm = side => {
      const samples = new Float32Array(RATE * 3);
      for (let frame = 0; frame < samples.length / 2; frame++) {
        const at = frame / RATE;
        samples[frame * 2 + side] = 0.026 * Math.sin(2 * Math.PI * (227 * at + 73 * at * at)) + 0.008 * Math.sin(2 * Math.PI * 619 * at);
      }
      return samples;
    };
    const wave = sound => ({ kind: "playWave", sound, channels: 2, rate: RATE, samples: pcm(sound === 11 ? 0 : 1) });
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
        step(0.08, on(drumKey, 90, 11, 9), on(drumKey, 100, 22, 9)),
        step(0.12, cc(7, 32, 11, 9), cc(10, 127, 11, 9)),
        step(0.14, off(drumKey, 11, 9)),
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
      const note = channel === 9 ? drumKey : 69;
      cases.push({ name: `resume-${name}`, duration: 0.3, steps: [
        step(0, setupChannel(11, channel, 0, 32), cc(11, 100, 11, channel),
          { kind: "noteResume", sound: 11, channel, note, velocity: 100, age }),
        step(0.025, cc(7, 127, 11, channel), cc(11, 64, 11, channel), cc(10, 127, 11, channel)),
        step(0.07, off(note, 11, channel)), step(0.085, cc(7, 0, 11, channel)), step(0.1, cc(7, 127, 11, channel)),
      ], windows: [["audible", 0.035, 0.065]] });
    }

    const renderPage = async scenario => {
      const frames = Math.ceil(RATE * (scenario.duration + shift));
      const context = new OfflineAudioContext(2, frames, RATE);
      const page = await pageAudio(context, { worklet, baseLatency });
      const wall = wallClock();
      const timeline = [];
      try {
        const rendered = await renderSteps(context, page, scenario.steps, entry => {
          wall.set(context.currentTime);
          timeline.push({ at: page.audio.eventTime(), events: entry.events });
          playAudioEvents(page.audio, entry.events);
        });
        return { samples: channelsOf(rendered.buffer), timeline, rendered, page };
      } finally {
        wall.restore();
      }
    };

    // The reference never reads the page's channel state: it follows the
    // authored events, with each step's commands reaching the synthesizer at
    // the quantum the page's did and timed by the page's scheduling clock.
    const renderReference = async (scenario, actual) => {
      const frames = Math.ceil(RATE * (scenario.duration + shift));
      const channels = new Map(), held = new Map(), waves = [], entries = [];
      let lanes = 0, ids = 0;
      const channelState = (sound, channel) => {
        const key = `${sound}:${channel}`;
        if (!channels.has(key)) channels.set(key, { sound, program: 0, volume: 100, expression: 127, pan: 64, lane: null, changes: [] });
        return channels.get(key);
      };
      const level = state => state.volume * state.expression / (127 * 127);
      const position = state => (state.pan - 64) / 64;
      for (const [index, { at, events }] of actual.timeline.entries()) {
        const commands = [];
        for (const event of events) {
          const { sound, channel, note } = event;
          const state = channelState(sound, channel), key = `${sound}:${channel}:${note}`;
          if (event.kind === "programChange") {
            state.program = event.program;
          } else if (event.kind === "controlChange") {
            const name = { 7: "volume", 11: "expression", 10: "pan" }[event.control];
            state[name] = event.value;
            if (state.lane !== null) {
              state.changes.push(event.control === 10 ? { at, pan: position(state) } : { at, level: level(state) });
            }
          } else if (event.kind === "noteOn" || event.kind === "noteResume") {
            if (state.lane === null) Object.assign(state, { lane: lanes++, level: level(state), position: position(state) });
            const id = ++ids, drum = channel === 9;
            commands.push({ op: "on", id, at, lane: state.lane, pitch: note, velocity: event.velocity, program: state.program,
              drumKey: drum ? note : undefined, age: event.kind === "noteResume" ? event.age / 1000 : 0 });
            if (!drum) held.set(key, id);
          } else if (event.kind === "noteOff") {
            // A stand-in drum is a one-shot; a melodic note follows its release.
            if (held.has(key)) commands.push({ op: "off", id: held.get(key), at });
            held.delete(key);
          } else if (event.kind === "playWave") {
            waves.push({ at, event });
          }
        }
        entries.push({ mark: actual.rendered.marks[index], commands });
      }
      const mix = referenceMixer(frames, 2);
      const rendered = referenceLanes(actual.page, frames, entries);
      for (const state of channels.values()) {
        if (state.lane === null) continue;
        const lane = mix.lane(rendered[state.lane], state.sound, state.level, state.position);
        let gain = steady(state.level), pan = steady(state.position);
        for (const change of state.changes) {
          if (change.level !== undefined) gain = transition(lane.gain.gain, gain, change.level, change.at);
          else pan = transition(lane.panner.pan, pan, change.pan, change.at);
        }
      }
      for (const { at, event } of waves) mix.sample(event.samples, event.channels, event.sound, at);
      return { samples: await mix.render(), lanes: rendered, channels };
    };

    const results = [];
    for (const scenario of cases) {
      const actual = await renderPage(scenario);
      const expected = await renderReference(scenario, actual);
      const windows = {};
      for (const [name, from, to] of scenario.windows) {
        const start = from + shift, end = to + shift;
        const leftEnergy = energy(actual.samples[0], start, end), rightEnergy = energy(actual.samples[1], start, end);
        // What the synthesizer rendered for the scenario's first channel,
        // before any routing: a held envelope's own level in this window.
        const laneEnergy = energy(expected.lanes[0], start, end);
        windows[name] = { rms: Math.sqrt(leftEnergy + rightEnergy), leftEnergy, rightEnergy, laneRMS: Math.sqrt(laneEnergy) };
      }
      const notes = scenario.steps.flatMap(entry => entry.events).filter(event => ["noteOn", "noteResume"].includes(event.kind)).length;
      const waves = scenario.steps.flatMap(entry => entry.events).filter(event => event.kind === "playWave").length;
      const clockError = Math.max(...actual.timeline.map(({ at }, index) => Math.abs(at - (actual.rendered.marks[index].frame / RATE + lead))));
      const diagnosis = describeMismatch(actual.samples, expected.samples, { steps: actual.rendered.marks.map((mark, index) =>
        ({ label: `requested ${scenario.steps[index].time} s`, mark, at: actual.timeline[index].at })) });
      results.push({ scenario: scenario.name, mode: actual.page.mode, late: actual.page.late.length, clockError,
        maximumError: maximumError(actual.samples, expected.samples),
        notes, starts: actual.page.commands.filter(command => command.op === "on").length,
        waves, bufferSources: actual.page.bufferSources(),
        cuts: actual.page.commands.filter(command => command.op === "cut").length, windows, ...diagnosis && { diagnosis } });
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
    assert.equal(result.cuts, 0, `${result.scenario}: a controller cut a note`);
    assert.ok(Object.values(result.windows).some(window => window.rms > 0.0001), `${result.scenario}: all measured output was silent`);
    if (result.scenario === "held") {
      const window = result.windows;
      assert.ok(window.before.rms > 0.01, "held note was silent");
      assert.ok(window.volumeZero.rms < 1e-8 && window.expressionZero.rms < 1e-8, "zero volume or expression was not silent");
      // A centred pan keeps the sum of both sides' energy, so the output's
      // level over the synthesizer's own is the channel gain.
      for (const [name, gain] of [["before", 1], ["volumeQuarter", 32 / 127], ["volumeRestored", 1],
        ["expressionQuarter", 32 / 127], ["expressionRestored", 1]]) {
        const ratio = window[name].rms / window[name].laneRMS;
        assert.ok(Math.abs(ratio - gain) < 0.00001, `${name}: channel gain ${ratio} differs from ${gain}`);
      }
      assert.ok(window.panLeft.leftEnergy > 0.001 && window.panLeft.rightEnergy < 1e-12, "live pan did not reach the left channel");
      assert.ok(window.panRight.rightEnergy > 0.001 && window.panRight.leftEnergy < window.panRight.rightEnergy * 0.001, "live pan did not reach the right side");
    }
    if (result.scenario === "initial-zero") {
      assert.ok(result.windows.beforeZero.rms < 1e-8 && result.windows.expressionZero.rms < 1e-8, "initial or later zero was not silent");
      assert.ok(result.windows.restored.rms > 0.01, "a note started at zero did not recover");
    }
    console.log(`PASS ${engineName} ${result.mode} ${result.scenario}: ${JSON.stringify(result)}`);
  }
} finally {
  await browser.close();
}
