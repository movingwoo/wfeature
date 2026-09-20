import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";

const setup = (t, state = "running") => {
  const timers = [];
  const reports = [];
  let created = 0;
  class Context {
    constructor() { created++; }
    state = state;
    currentTime = 0;
    destination = {};
    resumes = 0;
    suspends = 0;
    createGain() { return { gain: { value: 0 }, connect() {} }; }
    async resume() { this.resumes++; this.state = "running"; }
    async suspend() { this.suspends++; this.state = "suspended"; }
  }
  t.mock.method(globalThis, "setTimeout", callback => { timers.push(callback); return timers.length; });
  const oldContext = Object.getOwnPropertyDescriptor(globalThis, "AudioContext");
  const oldDocument = Object.getOwnPropertyDescriptor(globalThis, "document");
  Object.defineProperty(globalThis, "AudioContext", { configurable: true, value: Context });
  Object.defineProperty(globalThis, "document", { configurable: true, value: { hidden: false } });
  t.after(() => {
    if (oldContext) Object.defineProperty(globalThis, "AudioContext", oldContext);
    else delete globalThis.AudioContext;
    if (oldDocument) Object.defineProperty(globalThis, "document", oldDocument);
    else delete globalThis.document;
  });
  const audio = new PageAudio({ report: message => reports.push(message) });
  const check = async () => { timers.shift()(); for (let i = 0; i < 8; i++) await Promise.resolve(); };
  return { audio, timers, reports, check, created: () => created };
};

for (const state of ["suspended", "interrupted"]) {
  test(`first use resumes a ${state} context`, t => {
    const { audio } = setup(t, state);
    audio.ensure();
    assert.equal(audio.context.resumes, 1);
    assert.equal(audio.context.state, "running");
  });
}

test("a stalled running clock gets one restart without rebuilding the graph", async t => {
  const { audio, timers, reports, check, created } = setup(t);
  audio.setMIDIVolume(0.23);
  audio.activate();
  audio.activate();
  assert.equal(timers.length, 1);
  const master = audio.master;
  await check();
  assert.equal(audio.context.suspends, 1);
  assert.equal(audio.context.resumes, 1);
  assert.equal(created(), 1);
  assert.equal(audio.master, master);
  assert.equal(audio.midiGain.gain.value, 0.23);
  assert.equal(timers.length, 0, "a persistently frozen device must not cause a retry loop");
  assert.ok(reports.some(message => message.includes("clock stalled")));
});

test("a running clock is left alone even with both sources muted", async t => {
  const { audio, check } = setup(t);
  audio.setMIDIVolume(0);
  audio.setWaveVolume(0);
  audio.activate();
  audio.context.currentTime = 0.4;
  await check();
  assert.equal(audio.context.suspends, 0);
  assert.equal(audio.context.resumes, 0);
});

test("foreground return resumes interruption without creating audio in the library", t => {
  const { audio, created } = setup(t);
  audio.foreground();
  assert.equal(created(), 0);
  audio.activate();
  audio.context.state = "interrupted";
  audio.foreground();
  assert.equal(audio.context.resumes, 1);
});

test("background and closed contexts are not restarted", async t => {
  const { audio, check } = setup(t);
  audio.activate();
  document.hidden = true;
  await check();
  assert.equal(audio.context.suspends, 0);
  document.hidden = false;
  audio.context.state = "closed";
  audio.activate();
  await check();
  assert.equal(audio.context.suspends, 0);
  assert.equal(audio.context.resumes, 0);
});

test("a rejected recovery is reported and a later gesture can retry", async t => {
  const { audio, reports, check } = setup(t);
  audio.activate();
  audio.context.suspend = async () => { throw new Error("device unavailable"); };
  await check();
  assert.equal(audio.recovering, false);
  assert.ok(reports.includes("audio recovery failed: device unavailable"));
  audio.context.suspend = async () => { audio.context.state = "suspended"; };
  audio.activate();
  await check();
  assert.equal(audio.context.resumes, 1);
});
