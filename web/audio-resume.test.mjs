import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { fakeFM } from "./audio-test-fm.mjs";

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
  const { fm, createFM } = fakeFM();
  const audio = new PageAudio({ createFM });
  audio.context = {
    state: "running", currentTime: 1, baseLatency: 0.09, sampleRate: 48000,
    createGain: () => ({ ...node(), gain: parameter() }),
    createOscillator: source, createBufferSource: source,
    createStereoPanner: () => ({ ...node(), pan: parameter() }),
    createBiquadFilter: () => ({ ...node(), frequency: parameter() }),
  };
  audio.midiGain = {};
  audio.waveGain = {};
  return { audio, fm, sources, wall: seconds => { wall = seconds; } };
};

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);
const ops = (fm, id) => fm.of(id).map(command => command.op);

// The synthesizer advances a resumed note's envelopes by its age (see
// fm-synth.test.mjs); the page's part is to pass that age along and to keep
// the note's start before its first rendered sample.
for (const age of [5, 10, 65, 120, 1500]) {
  test(`a note resumed ${age} ms into its envelope passes its age and keeps its start`, t => {
    const { audio, fm } = setup(t);
    audio.noteResume(0, 60, 100, age, 11);
    assert.equal(fm.notes().length, 1);
    const [note] = fm.notes();
    const voice = audio.voices.get("11:0:60");
    close(note.age, age / 1000);
    close(voice.sourceStart, note.at);
    close(voice.started, note.at - age / 1000);
    assert.deepEqual(fm.of(note.id), []);
  });
}

test("zero-age resume keeps the ordinary attack and decay", t => {
  const { audio, fm } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  audio.noteResume(0, 60, 100, 0, 22);
  const [first, second] = fm.notes();
  assert.equal(first.age, 0);
  assert.equal(second.age, 0);
  assert.equal(first.at, second.at);
});

test("a later note-off releases at its own time after the resumed start", t => {
  const { audio, fm, wall } = setup(t);
  audio.noteResume(0, 60, 100, 65, 11);
  const [note] = fm.notes();
  wall(0.02);
  audio.noteOff(0, 60, 0, 11);
  assert.equal(audio.voices.size, 0);
  const [release] = fm.of(note.id);
  assert.equal(release.op, "off");
  close(release.at, note.at + 0.02);
});

test("clock correction cannot release a resumed note before its source starts", t => {
  const { audio, fm, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  wall(0.3);
  audio.noteResume(0, 64, 100, 1000, 11);
  const resumed = fm.notes()[1];
  audio.context.baseLatency = 0.01;
  wall(0.301);
  audio.noteOff(0, 64, 0, 11);
  const [release] = fm.of(resumed.id);
  assert.equal(release.op, "off");
  close(release.at, resumed.at);
});

for (const age of [90, 180, 199]) {
  test(`percussion resumes at ${age} ms with its remaining place in the budget`, t => {
    const { audio, fm } = setup(t);
    audio.noteResume(9, 40, 100, age, 11);
    assert.equal(fm.notes().length, 1);
    const [note] = fm.notes();
    const voice = audio.voices.get("11:9:40");
    assert.equal(note.drumKey, 40);
    close(note.age, age / 1000);
    close(voice.slotEnd, note.at + 0.2 - age / 1000);
    fm.end(note.id);
    assert.equal(audio.voices.size, 0);
    assert.equal(audio.sources.size, 0);
  });
}

test("invalid ages and expired percussion consume no voice or source", t => {
  const { audio, fm } = setup(t);
  for (const note of Array.from({ length: 24 }, (_, i) => 40 + i)) audio.noteOn(0, note, 100, 11);
  for (const age of [-1, NaN, Infinity, -Infinity, undefined, "65"]) {
    audio.noteResume(0, 70, 100, age, 22);
    audio.noteResume(9, 40, 100, age, 22);
  }
  for (const age of [200, 201, 1000]) audio.noteResume(9, 40, 100, age, 22);
  assert.equal(fm.notes().length, 24);
  assert.equal(audio.voices.size, 24);
  assert.ok(fm.notes().every(note => fm.of(note.id).length === 0));
  audio.noteResume(0, 70, 100, 1000, 22);
  assert.equal(fm.notes().length, 25);
  assert.equal(audio.voices.size, 24, "resumed notes share the ordinary voice limit");
  assert.deepEqual(ops(fm, fm.notes()[0].id), ["cut"]);
  assert.equal(audio.voices.has("11:0:40"), false);
  assert.equal(audio.voices.has("22:0:70"), true);
});

test("resumed equal pitches retain their owner's gain and independent release", t => {
  const { audio, fm } = setup(t);
  audio.setSoundGain(11, 2500);
  audio.setSoundGain(22, 10000);
  audio.noteResume(0, 60, 100, 65, 11);
  audio.noteResume(0, 60, 100, 1500, 22);
  const second = audio.voices.get("22:0:60");
  const [firstNote, secondNote] = fm.notes();
  const routeIncludes = (node, target) => {
    for (let current = node; current; current = current.connections?.[0]) {
      if (current === target) return true;
    }
    return false;
  };
  const lane = note => fm.lanes[note.lane].destination;
  assert.ok(routeIncludes(lane(firstNote), audio.soundGains.get(11).midi));
  assert.ok(routeIncludes(lane(secondNote), audio.soundGains.get(22).midi));
  assert.equal(routeIncludes(lane(firstNote), audio.soundGains.get(22).midi), false);
  assert.equal(audio.soundGains.get(11).midi.gain.value, 0.25);
  assert.equal(audio.soundGains.get(22).midi.gain.value, 1);
  audio.noteResume(0, 60, 0, 65, 11);
  assert.equal(fm.notes().length, 2, "zero velocity resumes as a note-off");
  assert.deepEqual(ops(fm, firstNote.id), ["off"]);
  assert.deepEqual(fm.of(secondNote.id), []);
  assert.equal(audio.voices.get("22:0:60"), second);
  audio.stopSound(11);
  assert.equal(audio.sources.size, 1);
  assert.equal([...audio.sources.values()][0].sound, 22);
});
