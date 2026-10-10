import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";

const setup = t => {
  let wall = 0;
  t.mock.method(performance, "now", () => wall * 1000);
  const sources = [];
  const parameter = () => ({
    value: 1, events: [],
    setValueAtTime(value, at) { this.events.push(["set", value, at]); },
    exponentialRampToValueAtTime(value, at) { this.events.push(["ramp", value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[2] < at); },
  });
  const node = () => ({ connections: [], connect(target) { this.connections.push(target); }, disconnect() {} });
  const source = () => {
    const current = { ...node(), frequency: parameter(), starts: [], stops: [],
      start(...args) { this.starts.push(args); }, stop(...args) { this.stops.push(args); } };
    sources.push(current);
    return current;
  };
  const audio = new PageAudio();
  audio.context = {
    state: "running", currentTime: 1, baseLatency: 0.09, sampleRate: 48000,
    createGain: () => ({ ...node(), gain: parameter() }),
    createOscillator: source, createBufferSource: source,
    createStereoPanner: () => ({ ...node(), pan: parameter() }),
    createBiquadFilter: () => ({ ...node(), frequency: parameter() }),
  };
  audio.midiGain = {};
  audio.waveGain = {};
  audio._noise = {};
  return { audio, sources, wall: seconds => { wall = seconds; } };
};

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);
const peak = 100 / 127 * 0.25;
const sustain = peak * 0.7;

for (const [name, age, initial, ramps] of [
  ["attack", 5, Math.sqrt(0.0001 * peak), [[peak, 0.005], [sustain, 0.115]]],
  ["attack boundary", 10, peak, [[sustain, 0.11]]],
  ["decay", 65, peak * Math.sqrt(0.7), [[sustain, 0.055]]],
  ["sustain boundary", 120, sustain, []],
  ["sustain", 1500, sustain, []],
]) {
  test(`resume in ${name} starts at the elapsed level and keeps only remaining ramps`, t => {
    const { audio, sources } = setup(t);
    audio.noteResume(0, 60, 100, age, 11);
    assert.equal(sources.length, 1);
    const voice = audio.voices.get("11:0:60");
    const start = sources[0].starts[0][0];
    assert.equal(voice.gain.gain.events[0][0], "set");
    close(voice.gain.gain.events[0][1], initial);
    close(voice.gain.gain.events[0][2], start);
    close(voice.started, start - age / 1000);
    assert.equal(voice.gain.gain.events.length, ramps.length + 1);
    for (const [index, [level, remaining]] of ramps.entries()) {
      const event = voice.gain.gain.events[index + 1];
      assert.equal(event[0], "ramp");
      close(event[1], level);
      close(event[2], start + remaining);
    }
  });
}

test("zero-age resume keeps the ordinary attack and decay", t => {
  const { audio, sources } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  audio.noteResume(0, 60, 100, 0, 22);
  assert.deepEqual(audio.voices.get("11:0:60").gain.gain.events, audio.voices.get("22:0:60").gain.gain.events);
  assert.deepEqual(sources[0].starts, sources[1].starts);
});

test("a later note-off uses the envelope age from before resume", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteResume(0, 60, 100, 65, 11);
  const voice = audio.voices.get("11:0:60");
  const start = sources[0].starts[0][0];
  wall(0.02);
  audio.noteOff(0, 60, 0, 11);
  assert.equal(audio.voices.size, 0);
  const release = voice.gain.gain.events.at(-2);
  assert.equal(release[0], "ramp");
  close(release[1], peak * 0.7 ** (0.075 / 0.11));
  close(release[2], start + 0.02);
  close(voice.gain.gain.events.at(-1)[2], start + 0.08);
  close(sources[0].stops[0][0], start + 0.09);
});

test("clock correction cannot release a resumed note before its source starts", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  wall(0.3);
  audio.noteResume(0, 64, 100, 1000, 11);
  const voice = audio.voices.get("11:0:64");
  const start = sources[1].starts[0][0];
  audio.context.baseLatency = 0.01;
  wall(0.301);
  audio.noteOff(0, 64, 0, 11);
  close(voice.gain.gain.events.at(-2)[2], start);
  close(voice.gain.gain.events.at(-2)[1], sustain);
  close(sources[1].stops[0][0], start + 0.07);
});

for (const [age, initial, rampLeft, lifeLeft] of [
  [90, Math.sqrt(peak * 0.0001), 0.09, 0.11],
  [180, 0.0001, 0, 0.02],
  [199, 0.0001, 0, 0.001],
]) {
  test(`percussion resumes at ${age} ms with its noise offset and remaining tail`, t => {
    const { audio, sources } = setup(t);
    audio.noteResume(9, 40, 100, age, 11);
    assert.equal(sources.length, 1);
    const voice = audio.voices.get("11:9:40");
    const start = sources[0].starts[0][0];
    assert.equal(sources[0].buffer, audio._noise);
    close(sources[0].starts[0][1], age / 1000);
    close(voice.gain.gain.events[0][1], initial);
    close(voice.gain.gain.events[0][2], start);
    assert.equal(voice.gain.gain.events.length, rampLeft ? 2 : 1);
    if (rampLeft) {
      close(voice.gain.gain.events[1][1], 0.0001);
      close(voice.gain.gain.events[1][2], start + rampLeft);
    }
    close(sources[0].stops[0][0], start + lifeLeft);
    sources[0].onended();
    assert.equal(audio.voices.size, 0);
    assert.equal(audio.sources.size, 0);
  });
}

test("invalid ages and expired percussion consume no voice or source", t => {
  const { audio, sources } = setup(t);
  for (const note of Array.from({ length: 24 }, (_, i) => 40 + i)) audio.noteOn(0, note, 100, 11);
  for (const age of [-1, NaN, Infinity, -Infinity, undefined, "65"]) {
    audio.noteResume(0, 70, 100, age, 22);
    audio.noteResume(9, 40, 100, age, 22);
  }
  for (const age of [200, 201, 1000]) audio.noteResume(9, 40, 100, age, 22);
  assert.equal(sources.length, 24);
  assert.equal(audio.voices.size, 24);
  assert.ok(sources.every(source => source.stops.length === 0));
  audio.noteResume(0, 70, 100, 1000, 22);
  assert.equal(sources.length, 25);
  assert.equal(audio.voices.size, 24, "resumed notes share the ordinary voice limit");
  assert.equal(sources[0].stops.length, 1);
  assert.equal(audio.voices.has("11:0:40"), false);
  assert.equal(audio.voices.has("22:0:70"), true);
});

test("resumed equal pitches retain their owner's gain and independent release", t => {
  const { audio, sources } = setup(t);
  audio.setSoundGain(11, 2500);
  audio.setSoundGain(22, 10000);
  audio.noteResume(0, 60, 100, 65, 11);
  audio.noteResume(0, 60, 100, 1500, 22);
  const first = audio.voices.get("11:0:60"), second = audio.voices.get("22:0:60");
  const routeIncludes = (node, target) => {
    for (let current = node; current; current = current.connections?.[0]) {
      if (current === target) return true;
    }
    return false;
  };
  assert.ok(routeIncludes(first.gain, audio.soundGains.get(11).midi));
  assert.ok(routeIncludes(second.gain, audio.soundGains.get(22).midi));
  assert.equal(routeIncludes(first.gain, audio.soundGains.get(22).midi), false);
  assert.equal(audio.soundGains.get(11).midi.gain.value, 0.25);
  assert.equal(audio.soundGains.get(22).midi.gain.value, 1);
  audio.noteResume(0, 60, 0, 65, 11);
  assert.equal(sources.length, 2, "zero velocity resumes as a note-off");
  assert.equal(sources[0].stops.length, 1);
  assert.equal(sources[1].stops.length, 0);
  assert.equal(audio.voices.get("22:0:60"), second);
  audio.stopSound(11);
  assert.equal(audio.sources.size, 1);
  assert.equal(audio.sources.get(sources[1]).sound, 22);
});
