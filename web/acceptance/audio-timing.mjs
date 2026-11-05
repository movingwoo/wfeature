// Render short notes against the coarse output clock observed in Android WebView.
//
// The page's FM synthesizer renders twelve 35 ms notes that arrive 0.18 s apart
// while the output clock moves in 90 ms steps; the reference renders the same
// synthesizer with each note started and keyed off at its arrival spacing.
// WFEATURE_AUDIO_NO_WORKLET=1 renders the page's ScriptProcessor fallback.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-timing.mjs URL [chromium|webkit]");
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
  const result = await page.evaluate(async worklet => {
    const { RATE, pageAudio, wallClock, channelsOf, referenceLanes, referenceMixer, maximumError, describeMismatch, energy } =
      await import("./acceptance/audio-offline.js");
    const frames = RATE * 3, notes = 12, spacing = 0.18, gate = 0.035, block = 0.09;
    const context = new OfflineAudioContext(1, frames, RATE);
    const clock = { state: "running", currentTime: 0, baseLatency: block };
    const page = await pageAudio(context, { worklet, clock });
    const { audio } = page;
    audio.programChange(0, 72);
    const wall = wallClock(), times = [];
    try {
      for (let index = 0; index < notes; index++) {
        wall.set(index * spacing);
        clock.currentTime = Math.floor((wall.get() + 1e-9) / block) * block;
        const on = audio.eventTime();
        audio.noteOn(0, 69, 100);
        wall.set(wall.get() + gate);
        const off = audio.eventTime();
        audio.noteOff(0, 69);
        times.push({ on, off });
      }
    } finally {
      wall.restore();
    }
    await page.sync();
    const actual = channelsOf(await context.startRendering());

    // The reference: one lane of the synthesizer, every note keyed off at its
    // own time, through the default channel level 100/127 and a centred pan.
    const commands = times.flatMap(({ on, off }, index) => [
      { op: "on", id: index + 1, at: on, lane: 0, pitch: 69, velocity: 100, program: 72 },
      { op: "off", id: index + 1, at: off },
    ]);
    const lanes = referenceLanes(page, frames, [{ mark: { frame: 0, calls: 0 }, commands }]);
    const mix = referenceMixer(frames, 1);
    mix.lane(lanes[0], 0, 100 / 127, 0);
    const expected = await mix.render();
    const rms = (samples, start, end) => Math.sqrt(energy(samples, start, end));
    // Every note reached the synthesizer before rendering began.
    const diagnosis = describeMismatch(actual, expected, { steps: times.flatMap(({ on, off }, index) => [
      { label: `note ${index} on`, mark: { frame: 0, calls: 0 }, at: on },
      { label: `note ${index} off`, mark: { frame: 0, calls: 0 }, at: off },
    ]) });
    return {
      mode: page.mode,
      late: page.late.length,
      maximumError: maximumError(actual, expected),
      ...diagnosis && { diagnosis },
      ons: page.commands.filter(command => command.op === "on").length,
      offs: page.commands.filter(command => command.op === "off").length,
      cuts: page.commands.filter(command => command.op === "cut").length,
      notes: times.map(({ on, off }, index) => ({
        on, off, expectedOn: block + index * spacing,
        body: rms(actual[0], on + 0.015, on + 0.030),
        rest: rms(actual[0], on + 0.11, on + 0.17),
      })),
    };
  }, worklet);
  assert.equal(result.mode, worklet ? "worklet" : "script");
  assert.equal(result.late, 0, "a command reached the synthesizer after the time it was for");
  assert.ok(result.maximumError < 0.000001, `rendered notes differ from the synthesizer reference: ${result.maximumError}${result.diagnosis ?? ""}`);
  assert.deepEqual([result.ons, result.offs, result.cuts], [12, 12, 0], "every note starts once and is keyed off once");
  for (const [index, note] of result.notes.entries()) {
    assert.ok(Math.abs(note.on - note.expectedOn) < 1e-9, `short note ${index} lost its arrival spacing: ${note.on}`);
    assert.ok(Math.abs(note.off - note.on - 0.035) < 1e-9, `short note ${index} was keyed off at ${note.off - note.on}`);
    assert.ok(note.body > 0.02, `short note ${index} lost its envelope: ${note.body}`);
    // The stand-in voice releases over about 0.36 s, longer than the rest, so
    // the rest is not silent as it was with the oscillator's 60 ms release. A
    // note keyed off at its own time is about 20 dB down there; a lost key-off
    // holds it at the body's level, and one a 90 ms block late is 2 dB down.
    assert.ok(note.rest < note.body / 8, `short note ${index} rang through its rest: ${note.rest}`);
  }
  console.log(`PASS ${engineName} ${result.mode}: all 12 short notes render with their attacks and rests on a 90 ms clock; ` +
    JSON.stringify({ maximumError: result.maximumError, body: Math.min(...result.notes.map(note => note.body)),
      rest: Math.max(...result.notes.map(note => note.rest)) }));
} finally {
  await browser.close();
}
