// Compare live guest gain changes with an independent synthesizer reference
// and with uninterrupted reference clips.
//
// Two clips play at once, one panned left and one right; the first is muted,
// set to a quarter and restored while both play. The reference renders the
// synthesizer (or the samples) itself and routes them through native nodes:
// the channel level 100/127 and pan, then each clip's gain, which steps to its
// new level at the page's scheduled time, then the MIDI or wave slider and the
// master. Each clip is also rendered alone at full gain.
// WFEATURE_AUDIO_NO_WORKLET=1 renders the page's ScriptProcessor fallback.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-gain.mjs URL [chromium|webkit]");
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
    const { RATE, modeTiming, pageAudio, renderSteps, channelsOf, wallClock, referenceLanes, referenceMixer,
      maximumError, describeMismatch } = await import("./acceptance/audio-offline.js");
    // Gain changes are scheduled, so the fallback's longer lead moves only the
    // measured windows and the render's length.
    const { baseLatency, lead, shift } = modeTiming(worklet);
    const frames = Math.round(RATE * (1.3 + shift));
    const sliders = { master: 0.8, midi: 0.6, wave: 0.7 };
    const pan = sound => sound === 1 ? 0 : 127;
    const samplesFor = sound => {
      const samples = new Float32Array(RATE * 4);
      for (let frame = 0; frame < RATE * 2; frame++) {
        const at = frame / RATE;
        // A changing pitch makes replaying a sample prefix detectable.
        samples[frame * 2 + sound - 1] = 0.16 * Math.sin(2 * Math.PI * (220 * at + 55 * at * at)) + 0.05 * Math.sin(2 * Math.PI * 613 * at);
      }
      return samples;
    };
    const changes = [{ time: 0.3, value: 0 }, { time: 0.55, value: 2500 }, { time: 0.85, value: 10000 }];
    const render = async kind => {
      const context = new OfflineAudioContext(2, frames, RATE);
      const page = await pageAudio(context, { worklet, baseLatency, master: true });
      const { audio } = page;
      audio.setMasterVolume(sliders.master);
      audio.setMIDIVolume(sliders.midi);
      audio.setWaveVolume(sliders.wave);
      const wall = wallClock(), times = [];
      try {
        const steps = [{ time: 0 }, ...changes];
        const rendered = await renderSteps(context, page, steps, (entry, index) => {
          wall.set(context.currentTime);
          times.push(audio.eventTime());
          if (index > 0) {
            playAudioEvents(audio, [{ kind: "soundGain", sound: 1, value: entry.value }]);
            return;
          }
          const events = [];
          for (const sound of [1, 2]) {
            events.push({ kind: "soundGain", sound, value: 10000 });
            if (kind === "MIDI") {
              events.push(
                { kind: "programChange", sound, channel: 0, program: 72 },
                { kind: "controlChange", sound, channel: 0, control: 10, value: pan(sound) },
                { kind: "noteOn", sound, channel: 0, note: 69, velocity: 100 },
              );
            } else {
              events.push({ kind: "playWave", sound, channels: 2, rate: RATE, samples: samplesFor(sound) });
            }
          }
          playAudioEvents(audio, events);
        });
        return { samples: channelsOf(rendered.buffer), times, marks: rendered.marks, page };
      } finally {
        wall.restore();
      }
    };

    // owners lists the clips the reference plays; levels, when given, are the
    // first clip's gain changes as [time, level].
    const renderReference = async (kind, actual, owners, levels = []) => {
      const mix = referenceMixer(frames, 2);
      mix.master.gain.value = sliders.master;
      mix.midi.gain.value = sliders.midi;
      mix.wave.gain.value = sliders.wave;
      const at = actual.times[0];
      if (kind === "MIDI") {
        const lanes = referenceLanes(actual.page, frames, [{ mark: actual.marks[0], commands: owners.map(sound =>
          ({ op: "on", id: sound, at, lane: sound - 1, pitch: 69, velocity: 100, program: 72 })) }]);
        for (const sound of owners) mix.lane(lanes[sound - 1], sound, 100 / 127, (pan(sound) - 64) / 64);
      } else {
        for (const sound of owners) mix.sample(samplesFor(sound), 2, sound, at);
      }
      for (const [time, level] of levels) {
        for (const node of [mix.owner(1).midi, mix.owner(1).wave]) node.gain.setValueAtTime(level, time);
      }
      return mix.render();
    };

    const results = [];
    for (const kind of ["MIDI", "PCM"]) {
      const actual = await render(kind);
      const levels = changes.map((change, index) => [actual.times[index + 1], change.value / 10000]);
      const expected = await renderReference(kind, actual, [1, 2], levels);
      const first = await renderReference(kind, actual, [1]);
      const second = await renderReference(kind, actual, [2]);
      const windows = [];
      for (const [name, start, end, level] of [
        ["initial", 0.18, 0.25, 1],
        ["muted", 0.4, 0.48, 0],
        ["quarter", 0.67, 0.75, 0.25],
        ["restored", 0.93, 1.1, 1],
      ]) {
        const begin = Math.round((start + shift) * RATE), finish = Math.round((end + shift) * RATE);
        let maximum = 0, firstEnergy = 0, secondEnergy = 0, observedEnergy = 0;
        for (let frame = begin; frame < finish; frame++) {
          for (let channel = 0; channel < 2; channel++) {
            maximum = Math.max(maximum, Math.abs(actual.samples[channel][frame] - first[channel][frame] * level - second[channel][frame]));
          }
          firstEnergy += first[0][frame] ** 2;
          secondEnergy += second[1][frame] ** 2;
          observedEnergy += (actual.samples[0][frame] - second[0][frame]) ** 2;
        }
        windows.push({ name, level, maximumError: maximum, firstRMS: Math.sqrt(firstEnergy / (finish - begin)),
          secondRMS: Math.sqrt(secondEnergy / (finish - begin)), observedRatio: Math.sqrt(observedEnergy / firstEnergy) });
      }
      const clockError = Math.max(...actual.times.map((at, index) => Math.abs(at - (actual.marks[index].frame / RATE + lead))));
      const labels = ["clips start", ...changes.map(change => `clip gain ${change.value}`)];
      const diagnosis = describeMismatch(actual.samples, expected,
        { steps: actual.marks.map((mark, index) => ({ label: labels[index], mark, at: actual.times[index] })) });
      results.push({ kind, mode: actual.page.mode, late: actual.page.late.length, clockError, maximumError: maximumError(actual.samples, expected),
        ...diagnosis && { diagnosis },
        starts: actual.page.commands.filter(command => command.op === "on").length,
        stops: actual.page.commands.filter(command => command.op === "cut" || command.op === "off").length,
        bufferSources: actual.page.bufferSources(), windows });
    }
    return results;
  }, worklet);
  for (const { kind, mode, late, clockError, maximumError, diagnosis, starts, stops, bufferSources, windows } of results) {
    assert.equal(mode, worklet ? "worklet" : "script");
    assert.equal(late, 0, `${kind}: a command reached the synthesizer after the time it was for`);
    assert.ok(clockError < 1e-9, `${kind}: a gain change was not scheduled one lead after the render clock: ${clockError}`);
    assert.ok(maximumError < 0.000001, `${kind}: live gain differs from the synthesizer reference: ${maximumError}${diagnosis ?? ""}`);
    assert.deepEqual([starts, stops, bufferSources], kind === "MIDI" ? [2, 0, 0] : [0, 0, 2], `${kind}: a gain change restarted or stopped a source`);
    for (const window of windows) {
      assert.ok(window.firstRMS > 0.01 && window.secondRMS > 0.01, `${kind} ${window.name}: reference clips were silent`);
      assert.ok(window.maximumError < 0.000001, `${kind} ${window.name}: sample phase, envelope or peer output changed: ${window.maximumError}`);
      assert.ok(Math.abs(window.observedRatio - window.level) < 0.00001, `${kind} ${window.name}: level ${window.observedRatio} differs from ${window.level}`);
    }
    console.log(`PASS ${engineName} ${mode} ${kind}: live mute, quarter gain and unmute preserve phase and peer output; ` +
      JSON.stringify({ maximumError, windows: windows.map(window => [window.name, window.maximumError, window.observedRatio]) }));
  }
} finally {
  await browser.close();
}
