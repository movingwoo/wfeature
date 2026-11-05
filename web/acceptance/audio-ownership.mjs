// Cancel one of two simultaneous clips and compare with independent references.
//
// The page plays two clips, one panned left and one right, and stops the first
// at once. The reference renders the synthesizer (or the samples) itself and
// routes them through native nodes at the default channel level 100/127, with
// the first clip silent from the instant it was stopped; the surviving clip is
// also rendered alone.
// WFEATURE_AUDIO_NO_WORKLET=1 renders the page's ScriptProcessor fallback.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-ownership.mjs URL [chromium|webkit]");
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
      maximumError, describeMismatch, energy } = await import("./acceptance/audio-offline.js");
    // The stop acts at once, so the fallback's longer lead moves it with the
    // windows; the clips' starts are scheduled.
    const { baseLatency, lead, shift } = modeTiming(worklet);
    const frames = Math.round(RATE * (1 + shift));
    const pan = sound => sound === 1 ? 0 : 127;
    const samplesFor = sound => {
      const samples = new Float32Array(RATE * 2);
      for (let frame = 0; frame < RATE; frame++) samples[frame * 2 + sound - 1] = 0.2 * Math.sin(2 * Math.PI * 440 * frame / RATE);
      return samples;
    };
    const render = async kind => {
      const context = new OfflineAudioContext(2, frames, RATE);
      const page = await pageAudio(context, { worklet, baseLatency });
      const wall = wallClock();
      const saved = {};
      try {
        const rendered = await renderSteps(context, page, [{ time: 0 }, { time: 0.35 + shift }], (entry, index) => {
          wall.set(context.currentTime);
          if (index === 1) {
            saved.stoppedAt = context.currentTime;
            playAudioEvents(page.audio, [{ kind: "stopSound", sound: 1 }]);
            return;
          }
          saved.start = page.audio.eventTime();
          const events = [];
          for (const sound of [1, 2]) {
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
          playAudioEvents(page.audio, events);
        });
        return { samples: channelsOf(rendered.buffer), marks: rendered.marks, page, ...saved };
      } finally {
        wall.restore();
      }
    };
    const renderReference = async (kind, actual, owners) => {
      const mix = referenceMixer(frames, 2);
      const stops = sound => sound === 1 ? actual.stoppedAt : null;
      if (kind === "MIDI") {
        const lanes = referenceLanes(actual.page, frames, [{ mark: actual.marks[0], commands: owners.map(sound =>
          ({ op: "on", id: sound, at: actual.start, lane: sound - 1, pitch: 69, velocity: 100, program: 72 })) }]);
        for (const sound of owners) {
          const lane = mix.lane(lanes[sound - 1], sound, 100 / 127, (pan(sound) - 64) / 64);
          if (stops(sound) !== null) lane.stop(stops(sound));
        }
      } else {
        for (const sound of owners) mix.sample(samplesFor(sound), 2, sound, actual.start, { stopAt: stops(sound) });
      }
      return mix.render();
    };
    const rms = (samples, start, end) => Math.sqrt(energy(samples, start + shift, end + shift));
    const windows = samples => ({ before: [rms(samples[0], 0.2, 0.3), rms(samples[1], 0.2, 0.3)],
      after: [rms(samples[0], 0.5, 0.7), rms(samples[1], 0.5, 0.7)] });
    const results = [];
    for (const kind of ["MIDI", "PCM"]) {
      const actual = await render(kind);
      const expected = await renderReference(kind, actual, [1, 2]);
      const surviving = await renderReference(kind, actual, [2]);
      // The stop acts at once, at the render clock.
      const diagnosis = describeMismatch(actual.samples, expected, { steps: [
        { label: "clips start", mark: actual.marks[0], at: actual.start },
        { label: "first clip stops", mark: actual.marks[1], at: actual.stoppedAt },
      ] });
      results.push({ kind, mode: actual.page.mode, late: actual.page.late.length, clockError: Math.abs(actual.start - lead),
        maximumError: maximumError(actual.samples, expected), ...diagnosis && { diagnosis },
        afterError: maximumError(actual.samples, surviving, 0.36 + shift),
        concurrent: windows(actual.samples), reference: windows(surviving) });
    }
    return results;
  }, worklet);
  for (const { kind, mode, late, clockError, maximumError, diagnosis, afterError, concurrent, reference } of results) {
    assert.equal(mode, worklet ? "worklet" : "script");
    assert.equal(late, 0, `${kind}: a command reached the synthesizer after the time it was for`);
    assert.ok(clockError < 1e-9, `${kind}: the clips were not scheduled one lead after the render clock: ${clockError}`);
    assert.ok(maximumError < 0.000001, `${kind}: cancelling one clip differs from the synthesizer reference: ${maximumError}${diagnosis ?? ""}`);
    assert.ok(concurrent.before[0] > 0.02, `${kind}: first clip never sounded`);
    assert.ok(concurrent.after[1] > 0.02, `${kind}: stopping first clip silenced second clip`);
    assert.ok(afterError < 0.000001, `${kind}: output after cancellation differs from the surviving clip alone: ${afterError}`);
    for (let channel = 0; channel < 2; channel++) {
      assert.ok(Math.abs(concurrent.after[channel] - reference.after[channel]) < 0.000001,
        `${kind}: output after cancellation differs from surviving clip on channel ${channel}`);
    }
    console.log(`PASS ${engineName} ${mode} ${kind}: first clip cancelled; surviving clip RMS ${concurrent.after[1].toFixed(6)} matches reference; ` +
      JSON.stringify({ maximumError, afterError }));
  }
} finally {
  await browser.close();
}
