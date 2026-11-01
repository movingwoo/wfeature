import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { fakeFM } from "./audio-test-fm.mjs";
import { playAudioEvents, sessionURL } from "./session.js";

const setup = t => {
  t.mock.method(performance, "now", () => 0);
  const parameter = () => ({ value: 1, setValueAtTime() {}, linearRampToValueAtTime() {}, cancelScheduledValues() {} });
  const node = () => ({ connections: [], connect(target) { this.connections.push(target); }, disconnect() {} });
  const { fm, createFM } = fakeFM();
  const audio = new PageAudio({ createFM });
  audio.context = {
    state: "running", currentTime: 1, baseLatency: 0.01, sampleRate: 48000,
    createGain: () => ({ ...node(), gain: parameter() }),
    createStereoPanner: () => ({ ...node(), pan: parameter() }),
  };
  audio.midiGain = node();
  audio.waveGain = node();
  return { audio, fm };
};
const voiceBytes = Uint8Array.of(1, 1, 0, 1);

test("a sound's own voices reach the synthesizer and its notes name them", t => {
  const { audio, fm } = setup(t);
  audio.defineVoice(3, voiceBytes, 11);
  for (const slot of [0, 256, 1.5, -1]) audio.defineVoice(slot, voiceBytes, 11);
  assert.deepEqual(fm.commands.filter(command => command.op === "define"), [{ op: "define", sound: 11, slot: 3, data: voiceBytes }]);
  audio.programChange(0, 40, 11);
  audio.noteOn(0, 60, 100, 11, 0, 3, 48);
  audio.noteOn(0, 62, 100, 11);
  const [voiced, plain] = fm.notes();
  assert.deepEqual([voiced.slot, voiced.pitch, voiced.program, voiced.drumKey], [3, 48, 40, undefined]);
  assert.deepEqual([plain.slot, plain.pitch], [0, 62], "without a voice the stand-in plays the note itself");
  audio.noteOn(0, 64, 100, 11, 0, 3, 200);
  assert.equal(fm.notes()[2].pitch, 64, "an impossible pitch falls back to the note");
});

test("a file's drum voice is keyed off by its note-off; a stand-in drum is a one-shot", t => {
  const { audio, fm } = setup(t);
  audio.noteOn(9, 36, 100, 11, 0, 2, 39);
  audio.noteOn(9, 38, 100, 11);
  const [voiced, standIn] = fm.notes();
  assert.equal(voiced.drumKey, 36);
  assert.equal(standIn.drumKey, 38);
  audio.noteOff(9, 36, 0, 11);
  audio.noteOff(9, 38, 0, 11);
  assert.deepEqual(fm.of(voiced.id).map(command => command.op), ["off"]);
  assert.deepEqual(fm.of(standIn.id), []);
  assert.equal(audio.voices.size, 2, "both keep their place in the budget");
  audio.noteOff(9, 36, 0, 11);
  assert.equal(fm.of(voiced.id).length, 1, "a second note-off releases nothing more");
});

test("a file's drum voice struck again after its budget slot releases the earlier hit", t => {
  const { audio, fm } = setup(t);
  let wall = 0;
  t.mock.method(performance, "now", () => wall * 1000);
  audio.noteOn(9, 36, 100, 11, 0, 2, 39);
  wall = 0.3;
  audio.context.currentTime = 1.3;
  audio.noteOn(9, 36, 100, 11, 0, 2, 39);
  const [first, second] = fm.notes();
  assert.deepEqual(fm.of(first.id).map(command => command.op), ["off"]);
  assert.equal(fm.of(first.id)[0].at, second.at);
  audio.noteOff(9, 36, 0, 11);
  assert.deepEqual(fm.of(second.id).map(command => command.op), ["off"], "the later note-off releases the later hit");
});

test("modulation and reset reach the channel's lane; a stop forgets the sound's voices", t => {
  const { audio, fm } = setup(t);
  audio.controlChange(0, 1, 64, 11);
  audio.noteOn(0, 60, 100, 11);
  const lane = fm.notes()[0].lane;
  assert.equal(fm.lanes[lane].depth, 64 / 127, "a lane starts with its channel's modulation");
  audio.controlChange(0, 1, 127, 11);
  audio.controlChange(0, 121, 0, 11);
  assert.deepEqual(fm.commands.filter(command => command.op === "depth").map(command => [command.lane, command.depth]), [[lane, 1], [lane, 0]]);
  audio.stopSound(11);
  assert.ok(fm.commands.some(command => command.op === "forget" && command.sound === 11));
});

test("the session asks for voices and dispatches them with their notes", t => {
  const { audio, fm } = setup(t);
  const location = globalThis.location;
  globalThis.location = { protocol: "http:", host: "example.test" };
  try {
    assert.match(sessionURL(), /[?&]voice=1(&|$)/);
  } finally {
    globalThis.location = location;
  }
  assert.equal(playAudioEvents(audio, [
    { kind: "voice", voice: 4, data: voiceBytes, sound: 11 },
    { kind: "noteOn", channel: 0, note: 60, velocity: 100, voice: 4, pitch: 48, sound: 11 },
    { kind: "noteResume", channel: 1, note: 62, velocity: 90, age: 40, voice: 4, pitch: 50, sound: 11 },
  ]), true);
  assert.ok(fm.commands.some(command => command.op === "define" && command.slot === 4 && command.sound === 11));
  const [note, resumed] = fm.notes();
  assert.deepEqual([note.slot, note.pitch, note.sound], [4, 48, 11]);
  assert.deepEqual([resumed.slot, resumed.pitch, resumed.age], [4, 50, 0.04]);
});
