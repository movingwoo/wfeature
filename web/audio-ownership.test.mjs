import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";

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
  const audio = new PageAudio();
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
  audio._noise = {};
  return { audio, sources, panners };
};

test("equal channel and pitch in separate sounds have independent note-offs", t => {
  const { audio, sources } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  audio.noteOn(0, 60, 100, 22);
  assert.equal(audio.voices.size, 2);
  assert.deepEqual(sources.map(source => source.stops), [[], []]);

  audio.noteOff(0, 60, 0, 11);
  assert.equal(sources[0].stops.length, 1);
  assert.equal(sources[1].stops.length, 0);
  assert.equal(audio.voices.size, 1);
  audio.noteOn(0, 60, 0, 22);
  assert.equal(sources[1].stops.length, 1, "zero-velocity note-on stops its own sound");
  assert.equal(audio.voices.size, 0);
});

test("program, gain, pan and live bend remain scoped to their sound", t => {
  const { audio, sources, panners } = setup(t);
  audio.programChange(2, 16, 11);
  audio.controlChange(2, 7, 32, 11);
  audio.controlChange(2, 10, 0, 11);
  audio.programChange(2, 56, 22);
  audio.controlChange(2, 7, 96, 22);
  audio.controlChange(2, 10, 127, 22);
  audio.noteOn(2, 69, 100, 11);
  audio.noteOn(2, 69, 100, 22);
  assert.deepEqual(sources.map(source => source.type), ["sawtooth", "square"]);
  assert.deepEqual(panners.map(panner => panner.pan.value), [-1, 63 / 64]);
  const peaks = sources.map(source => {
    const envelope = source.connections[0];
    return envelope.gain.events[1][1] * envelope.connections[0].gain.value;
  });
  assert.ok(Math.abs(peaks[1] / peaks[0] - 3) < 1e-12);

  audio.pitchBend(2, 12288, 11);
  assert.ok(Math.abs(sources[0].frequency.events[0][1] - 440 * 2 ** (1 / 12)) < 1e-9);
  assert.deepEqual(sources[1].frequency.events, []);
  assert.equal(sources[1].frequency.value, 440);

  audio.programChange(2, 72, 11);
  audio.controlChange(2, 10, 64, 11);
  audio.noteOn(2, 70, 100, 11);
  audio.noteOn(2, 70, 100, 22);
  assert.deepEqual(sources.slice(2).map(source => source.type), ["sine", "square"]);
  assert.equal(panners.length, 2, "notes on one channel share its live panner");
  assert.equal(panners[0].pan.events.at(-1)[1], 0);
  assert.equal(panners[1].pan.value, 63 / 64);
  assert.deepEqual(panners[1].pan.events, []);
});

for (const control of [120, 123]) {
  test(`channel controller ${control} does not release another sound`, t => {
    const { audio, sources } = setup(t);
    audio.noteOn(0, 60, 100, 11);
    audio.noteOn(0, 60, 100, 22);
    audio.controlChange(0, control, 0, 11);
    assert.equal(sources[0].stops.length, 1);
    assert.equal(sources[1].stops.length, 0);
    assert.equal(audio.voices.size, 1);
  });
}

test("stopping one sound cancels its PCM, percussion and released notes", t => {
  const { audio, sources } = setup(t);
  for (const sound of [11, 22]) {
    audio.noteOn(0, 60, 100, sound);
    audio.noteOff(0, 60, 0, sound);
    audio.noteOn(9, 40, 100, sound);
    audio.playWave(1, 8000, new Float32Array(8000), sound);
  }
  assert.equal(audio.voices.size, 2, "released melodic notes are outside the note map");
  assert.equal(audio.sources.size, 6);
  const otherStops = sources.slice(3).map(source => structuredClone(source.stops));
  const anchor = audio.scheduleAnchor;
  audio.stopSound(11);
  for (const source of sources.slice(0, 3)) {
    assert.deepEqual(source.stops.at(-1), [], "cancel even a future scheduled or released source immediately");
    assert.equal(source.disconnects, 1);
  }
  assert.deepEqual(sources.slice(3).map(source => source.stops), otherStops);
  assert.ok(sources.slice(3).every(source => source.disconnects === 0));
  assert.equal(audio.sources.size, 3);
  assert.equal(audio.voices.size, 1);
  assert.equal(audio.scheduleAnchor, anchor, "another sound keeps its scheduling clock");
  audio.stopSound(11);
  assert.ok(sources.slice(0, 3).every(source => source.disconnects === 1));
});

test("stale percussion callbacks preserve a retrigger after a sound stop", t => {
  const { audio, sources } = setup(t);
  audio.noteOn(9, 40, 100, 11);
  audio.noteOn(9, 40, 110, 11);
  audio.noteOn(9, 40, 100, 22);
  sources[0].onended();
  assert.equal(audio.voices.size, 2);
  assert.ok([...audio.voices.values()].some(voice => voice.source === sources[1]));

  audio.stopSound(11);
  audio.stopSound(11);
  audio.noteOn(9, 40, 100, 11);
  sources[1].onended();
  assert.equal(audio.voices.size, 2);
  assert.ok([...audio.voices.values()].some(voice => voice.source === sources[3]));
  assert.equal(sources[2].stops.length, 1, "only the peer percussion's natural end is scheduled");
  audio.stopSound(11);
  assert.deepEqual(sources[3].stops.at(-1), []);
  assert.equal(audio.voices.size, 1);
});

test("legacy owner zero coexists with owned output and has its own stop", t => {
  const { audio, sources } = setup(t);
  audio.noteOn(0, 69, 100);
  audio.noteOn(0, 69, 100, 22);
  audio.playWave(1, 8000, new Float32Array(8000));
  audio.noteOff(0, 69);
  assert.equal(sources[0].stops.length, 1);
  assert.equal(sources[1].stops.length, 0);
  audio.stopSound();
  assert.deepEqual(sources[0].stops.at(-1), []);
  assert.deepEqual(sources[2].stops.at(-1), []);
  assert.equal(sources[1].disconnects, 0);
  assert.equal(audio.sources.size, 1);
});

test("global stop cancels all owners and resets channels without changing user volume", t => {
  const { audio, sources } = setup(t);
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
  assert.equal(audio.sources.size, 0);
  assert.equal(audio.voices.size, 0);
  assert.equal(audio.soundChannels.size, 0);
  assert.equal(audio.scheduleAnchor, null);
  assert.deepEqual(audio.channels[0], { program: 0, volume: 100, expression: 127, pan: 64, bend: 8192,
    sustain: 0, bendRange: 2, bendRangeCents: 0, rpnMSB: 127, rpnLSB: 127,
    nrpnMSB: 127, nrpnLSB: 127, parameterKind: null });
  assert.equal(audio.midiVolume, 0.3);
  assert.equal(audio.waveVolume, 0.4);
  audio.stopAll();
  assert.ok(sources.every(source => source.disconnects === 1));
});
