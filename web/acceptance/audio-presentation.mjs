// Render one authored score under different packet sizes without an outside server.
//
// The same timed score of notes, controllers, clip gains, a clip stop and
// cached PCM is delivered once in 5 ms packets and once in 70 ms packets on a
// render clock that moves in 90 ms steps; the two renders must match. The
// notes play through the page's real FM synthesizer host, in its AudioWorklet
// or, with WFEATURE_AUDIO_NO_WORKLET=1, its ScriptProcessor fallback, and the
// run fails when the synthesizer is missing, or a note does not reach it at
// its presentation time, instead of passing on the PCM alone.
//
// The page and its modules come from a small server of this script's own on a
// loopback port: a loopback origin is a secure context, which an AudioWorklet
// needs, and Chromium does not hand a worklet's module fetch to request
// interception.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const engineName = process.argv[2] || "chromium";
if (!process.env.PLAYWRIGHT_MODULE) throw new Error("Set PLAYWRIGHT_MODULE to playwright/index.mjs");
const worklet = process.env.WFEATURE_AUDIO_NO_WORKLET !== "1";
const { [engineName]: engine } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE)));
assert.ok(engine && ["chromium", "webkit"].includes(engineName), "unsupported browser engine");
const web = new URL("../", import.meta.url);
const server = createServer(async (request, response) => {
  const path = new URL(request.url, "http://127.0.0.1").pathname;
  try {
    if (path === "/") {
      response.writeHead(200, { "content-type": "text/html" });
      response.end("<!doctype html><title>Audio presentation acceptance</title>");
      return;
    }
    // The page's own modules and the acceptance helpers, nothing else.
    if (!/^\/(acceptance\/)?[a-z0-9-]+\.js$/.test(path)) throw new Error("not served");
    const body = await readFile(new URL(`.${path}`, web));
    response.writeHead(200, { "content-type": "text/javascript" });
    response.end(body);
  } catch {
    response.writeHead(404);
    response.end();
  }
});
await new Promise(done => server.listen(0, "127.0.0.1", done));
const browser = await engine.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.goto(`http://127.0.0.1:${server.address().port}/`);
  const result = await page.evaluate(async worklet => {
    const { playAudioEvents } = await import("./session.js");
    const { RATE, pageAudio, channelsOf, describeMismatch, energy } = await import("./acceptance/audio-offline.js");
    const rate = RATE;
    // The first batch anchors the score one lead after the render clock; the
    // page's lead for timed batches is at least 100 ms.
    const lead = 0.1;
    const samples = new Float32Array(rate / 4);
    for (let i = 0; i < samples.length; i++) samples[i] = Math.sin(i * 2 * Math.PI * 330 / rate) * .15;
    const score = cacheable => {
      const events = [
        { kind: "programChange", at: 0, sound: 1, program: 72 },
        { kind: "playWave", at: 0, sound: 2, channels: 1, rate, samples, cacheable },
        { kind: "soundGain", at: .04, sound: 2, value: 5000 },
        { kind: "stopSound", at: .08, sound: 2 },
        { kind: "soundGain", at: .085, sound: 2, value: 2500 },
        { kind: "playWave", at: .085, sound: 2, channels: 1, rate, samples, cacheable },
        { kind: "controlChange", at: .3, sound: 1, control: 64, value: 127 },
        { kind: "pitchBend", at: .33, sound: 1, value: 10000 },
        { kind: "controlChange", at: .4, sound: 1, control: 64, value: 0 },
        { kind: "controlChange", at: .5, sound: 1, control: 11, value: 75 },
        { kind: "controlChange", at: .55, sound: 1, control: 10, value: 100 },
      ];
      // Later intervals model a continuous backend rate change. Pitch and PCM
      // sampling rate stay unchanged; only the presentation spacing changes.
      // Percussion plays the stand-in kick: the noise drums draw Math.random,
      // so two renders of one score would differ in their noise.
      let at = 0;
      for (let i = 0; i < 10; i++) {
        events.push({ kind: "noteOn", at, sound: 1, note: 60 + i, velocity: 100 });
        events.push({ kind: "noteOff", at: at + (i % 2 ? .035 : .005), sound: 1, note: 60 + i });
        if (i % 3 === 0) events.push({ kind: "noteOn", at, sound: 3, channel: 9, note: 36, velocity: 80 });
        at += i < 4 ? .14 : .07;
      }
      return events.sort((a, b) => a.at - b.at);
    };
    const render = async (packetMilliseconds, cacheable) => {
      const context = new OfflineAudioContext(2, rate * 2, rate);
      const clock = { state: "running", currentTime: 0, baseLatency: .09 };
      const page = await pageAudio(context, { worklet, clock });
      const { audio } = page;
      const events = score(cacheable);
      if (!playAudioEvents(audio, [{ kind: "clock", at: 0 }])) throw new Error("initial clock refused");
      let cursor = 0;
      for (let milliseconds = packetMilliseconds; milliseconds <= 1400; milliseconds += packetMilliseconds) {
        const frontier = milliseconds / 1000;
        clock.currentTime = Math.floor((frontier + 1e-9) / .09) * .09;
        const batch = [];
        while (cursor < events.length && events[cursor].at <= frontier) batch.push(events[cursor++]);
        batch.push({ kind: "clock", at: frontier });
        if (!playAudioEvents(audio, batch)) throw new Error(`packet ${packetMilliseconds}/${frontier} refused`);
      }
      if (cursor !== events.length) throw new Error("score was not fully delivered");
      // Every command has reached the synthesizer before rendering starts.
      await page.sync();
      const rendered = await context.startRendering();
      const channels = channelsOf(rendered);
      // The PCM ends by 0.44 s; this window holds notes only.
      const midiRMS = Math.sqrt(energy(channels[0], 0.6, 1.1) + energy(channels[1], 0.6, 1.1));
      const expected = events.filter(event => event.kind === "noteOn")
        .map(event => ({ at: lead + event.at, pitch: event.note, drum: event.channel === 9 }));
      const started = page.commands.filter(command => command.op === "on")
        .map(command => ({ at: command.at, pitch: command.pitch, drum: command.drumKey !== undefined }));
      const order = (left, right) => left.at - right.at || left.pitch - right.pitch;
      expected.sort(order);
      started.sort(order);
      const moved = expected.length !== started.length ? Infinity : Math.max(...expected.map((note, index) =>
        note.pitch === started[index].pitch && note.drum === started[index].drum ? Math.abs(note.at - started[index].at) : Infinity));
      return { channels, started, mode: page.mode, late: page.late.length, waveBufferRequests: page.created("createBuffer"),
        notes: expected.length, starts: started.length, moved, midiRMS,
        offs: page.commands.filter(command => command.op === "off").length,
        cuts: page.commands.filter(command => command.op === "cut").length,
        commands: JSON.stringify(page.commands) };
    };
    const fine = await render(5, false), coarse = await render(70, true);
    if (fine.waveBufferRequests !== 2 || coarse.waveBufferRequests !== 1) throw new Error("PCM cache did not reuse exactly one immutable buffer");
    let maximumError = 0, energySum = 0;
    for (let channel = 0; channel < 2; channel++) {
      for (let i = 0; i < fine.channels[channel].length; i++) {
        maximumError = Math.max(maximumError, Math.abs(fine.channels[channel][i] - coarse.channels[channel][i]));
        energySum += fine.channels[channel][i] ** 2;
      }
    }
    // Here the 5 ms render stands where the page does and the 70 ms render
    // where the reference does; every note reached its synthesizer before
    // rendering began.
    const diagnosis = describeMismatch(fine.channels, coarse.channels, { steps: fine.started.map(note =>
      ({ label: `${note.drum ? "drum" : "note"} ${note.pitch} on`, at: note.at })) });
    const summary = ({ channels, commands, started, ...rest }) => rest;
    return { maximumError, energy: energySum, sameCommands: fine.commands === coarse.commands,
      fine: summary(fine), coarse: summary(coarse), ...diagnosis && { diagnosis } };
  }, worklet);
  for (const [name, render] of [["5 ms", result.fine], ["70 ms", result.coarse]]) {
    assert.equal(render.mode, worklet ? "worklet" : "script", `${name}: the synthesizer runs in ${render.mode}`);
    assert.equal(render.late, 0, `${name}: a command reached the synthesizer after the time it was for`);
    assert.equal(render.starts, render.notes, `${name}: ${render.notes - render.starts} of ${render.notes} notes never reached the synthesizer`);
    assert.ok(render.moved < 1e-9, `${name}: a note reached the synthesizer away from its presentation time: ${render.moved}`);
    assert.deepEqual([render.offs, render.cuts], [10, 0], `${name}: each melodic note is keyed off once and none is cut`);
    assert.ok(render.midiRMS > 0.01, `${name}: the notes rendered silence: ${render.midiRMS}`);
  }
  assert.ok(result.sameCommands, "packet size changed what the synthesizer was told");
  assert.ok(result.energy > 1, "the authored score rendered silence");
  assert.ok(result.maximumError < 1e-6, `packet size changed rendered output (page: the 5 ms render, reference: the 70 ms one): ` +
    `${result.maximumError}${result.diagnosis ?? ""}`);
  console.log(`PASS ${engineName} ${result.fine.mode}: 5/70 ms packets and PCM buffer reuse preserve rendered notes, controls and stops; ` +
    `max error ${result.maximumError}; ${JSON.stringify({ notes: result.fine.starts, midiRMS: result.fine.midiRMS, energy: result.energy })}`);
} finally {
  await browser.close();
  server.closeAllConnections();
  server.close();
}
