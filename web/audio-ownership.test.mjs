import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { fakeFM } from "./audio-test-fm.mjs";

const setup = t => {
  t.mock.method(performance, "now", () => 0);
  const sources = [], panners = [];
  const parameter = () => ({
    value: 1, events: [],
    setValueAtTime(value, at) { this.events.push(["set", value, at]); },
    exponentialRampToValueAtTime(value, at) { this.events.push(["ramp", value, at]); },
    linearRampToValueAtTime(value, at) { this.events.push(["linear", value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[2] < at); },
  });
  const routedNode = () => ({ connections: [], connect(target) { this.connections.push(target); }, disconnect() {} });
  const source = () => {
    const node = {
      frequency: parameter(), connections: [], stops: [], disconnects: 0,
      connect(target) { this.connections.push(target); },
      disconnect() { this.disconnects++; },
      start(at) { this.started = at; },
      stop(...args) { this.stops.push(args); },
    };
    sources.push(node);
    return node;
  };
  const { fm, createFM } = fakeFM();
  const audio = new PageAudio({ createFM });
  audio.context = {
    state: "running", currentTime: 1, baseLatency: 0.01, sampleRate: 48000,
    createGain: () => ({ ...routedNode(), gain: parameter() }),
    createOscillator: source, createBufferSource: source,
    createStereoPanner: () => {
      const node = { ...routedNode(), pan: parameter() };
      panners.push(node);
      return node;
    },
    createBiquadFilter: () => ({ frequency: parameter(), connect() {} }),
    createBuffer: (channels, frames) => {
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      return { numberOfChannels: channels, getChannelData: channel => data[channel] };
    },
  };
  audio.midiGain = { gain: parameter() };
  audio.waveGain = { gain: parameter() };
  return { audio, fm, sources, panners };
};

const ids = fm => fm.notes().map(note => note.id);
const ops = (fm, id) => fm.of(id).map(command => command.op);
const laneOf = (audio, sound, channel) => audio.channelOutputs.get(sound)[channel].lane;

test("equal channel and pitch in separate sounds have independent note-offs", t => {
  const { audio, fm } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  audio.noteOn(0, 60, 100, 22);
  assert.equal(audio.voices.size, 2);
  const [first, second] = ids(fm);
  assert.deepEqual([fm.of(first), fm.of(second)], [[], []]);

  audio.noteOff(0, 60, 0, 11);
  assert.deepEqual(ops(fm, first), ["off"]);
  assert.deepEqual(fm.of(second), []);
  assert.equal(audio.voices.size, 1);
  audio.noteOn(0, 60, 0, 22);
  assert.deepEqual(ops(fm, second), ["off"], "zero-velocity note-on releases its own sound");
  assert.equal(audio.voices.size, 0);
});

test("program, gain, pan and live bend remain scoped to their sound", t => {
  const { audio, fm, panners } = setup(t);
  audio.programChange(2, 16, 11);
  audio.controlChange(2, 7, 32, 11);
  audio.controlChange(2, 10, 0, 11);
  audio.programChange(2, 56, 22);
  audio.controlChange(2, 7, 96, 22);
  audio.controlChange(2, 10, 127, 22);
  audio.noteOn(2, 69, 100, 11);
  audio.noteOn(2, 69, 100, 22);
  assert.deepEqual(fm.notes().map(note => note.program), [16, 56]);
  assert.deepEqual(panners.map(panner => panner.pan.value), [-1, 63 / 64]);
  const levels = fm.notes().map(note => note.velocity * fm.lanes[note.lane].destination.gain.value);
  assert.ok(Math.abs(levels[1] / levels[0] - 3) < 1e-12);

  audio.pitchBend(2, 12288, 11);
  const bends = lane => fm.commands.filter(command => command.op === "bend" && command.lane === lane);
  assert.ok(Math.abs(bends(laneOf(audio, 11, 2))[0].ratio - 2 ** (1 / 12)) < 1e-9);
  assert.deepEqual(bends(laneOf(audio, 22, 2)), []);
  assert.equal(fm.lanes[laneOf(audio, 22, 2)].ratio, 1);

  audio.programChange(2, 72, 11);
  audio.controlChange(2, 10, 64, 11);
  audio.noteOn(2, 70, 100, 11);
  audio.noteOn(2, 70, 100, 22);
  assert.deepEqual(fm.notes().slice(2).map(note => note.program), [72, 56]);
  assert.equal(panners.length, 2, "notes on one channel share its live panner");
  assert.equal(panners[0].pan.events.at(-1)[1], 0);
  assert.equal(panners[1].pan.value, 63 / 64);
  assert.deepEqual(panners[1].pan.events, []);
});

for (const [control, op] of [[120, "cut"], [123, "off"]]) {
  test(`channel controller ${control} does not release another sound`, t => {
    const { audio, fm } = setup(t);
    audio.noteOn(0, 60, 100, 11);
    audio.noteOn(0, 60, 100, 22);
    audio.controlChange(0, control, 0, 11);
    const [first, second] = ids(fm);
    assert.deepEqual(ops(fm, first), [op]);
    assert.deepEqual(fm.of(second), []);
    assert.equal(audio.voices.size, 1);
  });
}

test("stopping one sound cancels its PCM, percussion and released notes", t => {
  const { audio, fm, sources } = setup(t);
  for (const sound of [11, 22]) {
    audio.noteOn(0, 60, 100, sound);
    audio.noteOff(0, 60, 0, sound);
    audio.noteOn(9, 40, 100, sound);
    audio.playWave(1, 8000, new Float32Array(8000), sound);
  }
  assert.equal(audio.voices.size, 2, "released melodic notes are outside the note map");
  assert.equal(audio.sources.size, 6);
  const [melody, drum, peerMelody, peerDrum] = ids(fm);
  const peerCommands = structuredClone([fm.of(peerMelody), fm.of(peerDrum)]);
  const otherStops = structuredClone(sources[1].stops);
  const anchor = audio.scheduleAnchor;
  audio.stopSound(11);
  assert.deepEqual(ops(fm, melody), ["off", "cut"], "cancel even a released note immediately");
  assert.deepEqual(ops(fm, drum), ["cut"]);
  for (const id of [melody, drum]) assert.equal(fm.of(id).at(-1).at, audio.context.currentTime);
  assert.deepEqual(sources[0].stops.at(-1), [], "cancel even a future scheduled sample immediately");
  assert.equal(sources[0].disconnects, 1);
  assert.equal(fm.released.length, 2, "the stopped sound's lanes are returned");
  assert.deepEqual([fm.of(peerMelody), fm.of(peerDrum)], peerCommands);
  assert.deepEqual(sources[1].stops, otherStops);
  assert.equal(sources[1].disconnects, 0);
  assert.equal(audio.sources.size, 3);
  assert.equal(audio.voices.size, 1);
  assert.equal(audio.scheduleAnchor, anchor, "another sound keeps its scheduling clock");
  audio.stopSound(11);
  assert.equal(sources[0].disconnects, 1);
});

test("stale percussion callbacks preserve a retrigger after a sound stop", t => {
  const { audio, fm } = setup(t);
  audio.noteOn(9, 40, 100, 11);
  audio.noteOn(9, 40, 110, 11);
  audio.noteOn(9, 40, 100, 22);
  const [first, second, peer] = ids(fm);
  fm.end(first);
  assert.equal(audio.voices.size, 2);
  assert.ok([...audio.voices.values()].some(voice => voice.fm === second));

  audio.stopSound(11);
  audio.stopSound(11);
  audio.noteOn(9, 40, 100, 11);
  const replacement = ids(fm)[3];
  fm.end(second);
  assert.equal(audio.voices.size, 2);
  assert.ok([...audio.voices.values()].some(voice => voice.fm === replacement));
  assert.deepEqual(fm.of(peer), [], "the peer percussion keeps its natural end");
  audio.stopSound(11);
  assert.deepEqual(ops(fm, replacement), ["cut"]);
  assert.equal(audio.voices.size, 1);
});

test("legacy owner zero coexists with owned output and has its own stop", t => {
  const { audio, fm, sources } = setup(t);
  audio.noteOn(0, 69, 100);
  audio.noteOn(0, 69, 100, 22);
  audio.playWave(1, 8000, new Float32Array(8000));
  audio.noteOff(0, 69);
  const [legacy, owned] = ids(fm);
  assert.deepEqual(ops(fm, legacy), ["off"]);
  assert.deepEqual(fm.of(owned), []);
  audio.stopSound();
  assert.deepEqual(ops(fm, legacy), ["off", "cut"]);
  assert.deepEqual(sources[0].stops.at(-1), []);
  assert.deepEqual(fm.of(owned), []);
  assert.equal(audio.sources.size, 1);
});

test("global stop cancels all owners and resets channels without changing user volume", t => {
  const { audio, fm, sources } = setup(t);
  audio.setMIDIVolume(0.3);
  audio.setWaveVolume(0.4);
  for (const sound of [0, 11, 22]) {
    audio.programChange(0, 56, sound);
    audio.controlChange(0, 10, 0, sound);
    audio.noteOn(0, 69, 100, sound);
    audio.playWave(1, 8000, new Float32Array(8000), sound);
  }
  audio.stopAll();
  assert.ok(sources.every(source => source.stops.length === 1 && source.stops[0].length === 0 && source.disconnects === 1));
  assert.ok(fm.commands.some(command => command.op === "reset"));
  assert.ok(ids(fm).every(id => ops(fm, id).join() === "cut"));
  assert.equal(audio.sources.size, 0);
  assert.equal(audio.voices.size, 0);
  assert.equal(audio.soundChannels.size, 0);
  assert.equal(audio.scheduleAnchor, null);
  assert.deepEqual(audio.channels[0], { program: 0, volume: 100, expression: 127, pan: 64, bend: 8192,
    sustain: 0, modulation: 0, bendRange: 2, bendRangeCents: 0, rpnMSB: 127, rpnLSB: 127,
    nrpnMSB: 127, nrpnLSB: 127, parameterKind: null });
  assert.equal(audio.midiVolume, 0.3);
  assert.equal(audio.waveVolume, 0.4);
  audio.stopAll();
  assert.ok(sources.every(source => source.disconnects === 1));
});
