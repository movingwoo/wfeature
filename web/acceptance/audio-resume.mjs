// Render held envelopes and PCM suffixes after pausing one owned sound.
//
// The page pauses a clip by stopping its sound and resumes it from its saved
// age or frame while a peer clip plays on. The reference renders the
// synthesizer itself: the note until the pause, then a note started at the
// saved age, which advances its envelopes without replaying the attack (its
// operator phases start afresh, as the page's do); PCM resumes from the
// complete original buffer at the saved frame offset.
// WFEATURE_AUDIO_NO_WORKLET=1 renders the page's ScriptProcessor fallback.
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [origin, engineName = "chromium"] = process.argv.slice(2);
if (!origin || !process.env.PLAYWRIGHT_MODULE) {
  throw new Error("Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/audio-resume.mjs URL [chromium|webkit]");
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
    const { RATE, modeTiming, pageAudio, renderSteps, channelsOf, wallClock, referenceLanes, referenceMixer,
      maximumError, describeMismatch, energy } = await import("./acceptance/audio-offline.js");
    // The pause stops the clip at once, so the fallback's longer lead moves it
    // with the windows; the start and the resume are scheduled.
    const { baseLatency, lead, shift } = modeTiming(worklet);
    const duration = 1.2 + shift;
    // Percussion plays the stand-in kick: the noise drums draw Math.random,
    // which no independent render can repeat.
    const drumKey = 36;
    const pcm = side => {
      const samples = new Float32Array(RATE * 4);
      for (let frame = 0; frame < RATE * 2; frame++) {
        const at = frame / RATE;
        samples[frame * 2 + side] = 0.16 * Math.sin(2 * Math.PI * (227 * at + 73 * at * at)) + 0.05 * Math.sin(2 * Math.PI * 619 * at);
      }
      return samples;
    };
    const firstPCM = pcm(0), peerPCM = pcm(1);
    const renderPaused = async kind => {
      const context = new OfflineAudioContext(2, Math.round(RATE * duration), RATE);
      const page = await pageAudio(context, { worklet, baseLatency });
      const { audio } = page;
      const wall = wallClock();
      const channel = kind === "percussion" ? 9 : 0;
      const note = kind === "percussion" ? drumKey : 69;
      const restoreChannel = () => {
        audio.setSoundGain(1, 10000);
        audio.programChange(channel, 72, 1);
        audio.controlChange(channel, 10, 0, 1);
      };
      const saved = {};
      const steps = [{ time: 0 }, { time: (kind === "percussion" ? 0.1 : 0.3) + shift }, { time: kind === "percussion" ? 0.4 : 0.65 }];
      try {
        const rendered = await renderSteps(context, page, steps, (entry, index) => {
          wall.set(context.currentTime);
          if (index === 0) {
            saved.firstStart = audio.eventTime();
            audio.playWave(2, RATE, peerPCM, 2);
            restoreChannel();
            if (kind === "PCM") audio.playWave(2, RATE, firstPCM, 1);
            else audio.noteOn(channel, note, 100, 1);
          } else if (index === 1) {
            saved.stoppedAt = context.currentTime;
            saved.savedFrames = Math.floor((saved.stoppedAt - saved.firstStart) * RATE + 1e-6);
            saved.savedAgeMS = Math.floor((saved.stoppedAt - saved.firstStart) * 1000);
            audio.stopSound(1);
          } else {
            saved.resumeStart = audio.eventTime();
            restoreChannel();
            if (kind === "PCM") audio.playWave(2, RATE, firstPCM.subarray(saved.savedFrames * 2), 1);
            else audio.noteResume(channel, note, 100, saved.savedAgeMS, 1);
          }
        });
        return { samples: channelsOf(rendered.buffer), marks: rendered.marks, page, ...saved };
      } finally {
        wall.restore();
      }
    };

    // The reference: the peer's samples throughout; the paused clip on the
    // left (pan 0) at the default channel level 100/127, silent from the pause.
    const renderReference = async (kind, saved) => {
      const frames = saved.samples[0].length;
      const mix = referenceMixer(frames, 2);
      mix.sample(peerPCM, 2, 2, saved.firstStart);
      let fresh = null, tailEnd = null;
      if (kind === "PCM") {
        mix.sample(firstPCM, 2, 1, saved.firstStart, { stopAt: saved.stoppedAt });
        // The complete original buffer, offset independently of the suffix.
        mix.sample(firstPCM, 2, 1, saved.resumeStart, { offset: saved.savedFrames / RATE });
      } else {
        const drum = kind === "percussion", pitch = drum ? drumKey : 69;
        const on = (id, lane, at, age) => ({ op: "on", id, at, lane, pitch, velocity: 100, program: 72, drumKey: drum ? pitch : undefined, age });
        const lanes = referenceLanes(saved.page, frames, [
          { mark: saved.marks[0], commands: [on(1, 0, saved.firstStart, 0)] },
          { mark: saved.marks[2], commands: [on(2, 1, saved.resumeStart, saved.savedAgeMS / 1000),
            // A drum struck again at the resume, for the tail check.
            ...drum ? [on(3, 2, saved.resumeStart, 0)] : []] },
        ]);
        mix.lane(lanes[0], 1, 100 / 127, -1).stop(saved.stoppedAt);
        mix.lane(lanes[1], 1, 100 / 127, -1);
        if (drum) {
          fresh = lanes[2];
          let last = lanes[1].length - 1;
          while (last >= 0 && lanes[1][last] === 0) last--;
          tailEnd = (last + 1) / RATE;
        }
      }
      return { samples: await mix.render(), fresh, tailEnd };
    };
    const rms = (samples, start, end) => Math.sqrt(energy(samples, start, end));
    const results = [];
    for (const kind of ["MIDI", "PCM", "percussion"]) {
      const saved = await renderPaused(kind);
      const reference = await renderReference(kind, saved);
      const expected = reference.samples;
      const onset = saved.resumeStart + 0.0005;
      const tail = reference.tailEnd;
      const clockError = Math.max(Math.abs(saved.firstStart - (saved.marks[0].frame / RATE + lead)),
        Math.abs(saved.resumeStart - (saved.marks[2].frame / RATE + lead)));
      // The pause stops the clip at once, at the render clock.
      const diagnosis = describeMismatch(saved.samples, expected, { steps: [
        { label: "start", mark: saved.marks[0], at: saved.firstStart },
        { label: "pause", mark: saved.marks[1], at: saved.stoppedAt },
        { label: "resume", mark: saved.marks[2], at: saved.resumeStart },
      ] });
      results.push({ kind, mode: saved.page.mode, late: saved.page.late.length, clockError, savedAgeMS: saved.savedAgeMS, savedFrames: saved.savedFrames,
        ...diagnosis && { diagnosis },
        maximumError: maximumError(saved.samples, expected),
        beforeRMS: rms(saved.samples[0], 0.03 + shift, 0.06 + shift),
        pausedRMS: rms(saved.samples[0], saved.stoppedAt + 0.02, saved.resumeStart - 0.02),
        onsetRMS: rms(saved.samples[0], onset, onset + 0.006),
        referenceOnsetRMS: rms(expected[0], onset, onset + 0.006),
        resumeError: maximumError([saved.samples[0]], [expected[0]], onset, saved.resumeStart + 0.08),
        peerError: maximumError([saved.samples[1]], [expected[1]], 0.02 + shift, 1.1 + shift),
        peerPausedRMS: rms(saved.samples[1], saved.stoppedAt + 0.02, saved.resumeStart - 0.02),
        tailEnd: tail,
        tailRMS: tail === null ? null : rms(saved.samples[0], tail + 0.02, tail + 0.05),
        freshTailRMS: tail === null ? null : rms(reference.fresh, tail + 0.02, tail + 0.05) });
    }
    return results;
  }, worklet);
  for (const result of results) {
    const { kind } = result;
    assert.equal(result.mode, worklet ? "worklet" : "script");
    assert.equal(result.late, 0, `${kind}: a command reached the synthesizer after the time it was for`);
    assert.ok(result.clockError < 1e-9, `${kind}: the clip or its resume was not scheduled one lead after the render clock: ${result.clockError}`);
    assert.ok(result.maximumError < 0.000001, `${kind}: pause and resume differ from the synthesizer reference: ${result.maximumError}${result.diagnosis ?? ""}`);
    assert.ok(result.beforeRMS > 0.005, `${kind}: clip did not sound before pause`);
    assert.ok(result.pausedRMS < 0.000001, `${kind}: paused owner remained audible`);
    assert.ok(result.peerPausedRMS > 0.02 && result.peerError < 0.000001, `${kind}: pause/resume changed the peer's output`);
    assert.ok(result.referenceOnsetRMS > 0.0001, `${kind}: resume reference was silent`);
    assert.ok(Math.abs(result.onsetRMS - result.referenceOnsetRMS) < 0.000001, `${kind}: resume replayed an attack or a sample prefix`);
    assert.ok(result.resumeError < 0.000001, `${kind}: resumed output differs from the envelope or PCM suffix reference: ${result.resumeError}`);
    if (kind === "percussion") {
      // The drum rings for as long as its envelope lasts; resumed at its saved
      // age it ends that much sooner than a drum struck again would.
      assert.ok(result.freshTailRMS > 0.00001, `percussion: a drum struck again would be silent after the resumed one ended: ${result.freshTailRMS}`);
      assert.ok(result.tailRMS < 0.0000001, `percussion: resume extended the saved tail: ${result.tailRMS}`);
    }
  }
  const metrics = results.map(result => Object.fromEntries(Object.entries(result).map(([key, value]) =>
    [key, typeof value === "number" ? Number(value.toPrecision(6)) : value])));
  console.log(JSON.stringify({ engine: engineName, status: "pass", metrics }));
} finally {
  await browser.close();
}
