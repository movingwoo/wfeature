import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);

const setup = t => {
  let wall = 0, created = 0;
  const sources = [], nodes = [];
  t.mock.method(performance, "now", () => wall * 1000);
  const parameter = context => ({
    initial: 1, events: [], cancellations: [],
    get value() { return this.at(context.currentTime); },
    set value(value) { this.initial = value; },
    at(time) {
      let previous = ["set", this.initial, 0];
      for (const event of this.events) {
        if (event[2] > time) {
          const fraction = (time - previous[2]) / (event[2] - previous[2]);
          if (event[0] === "linear") return previous[1] + (event[1] - previous[1]) * fraction;
          if (event[0] === "exponential") return previous[1] * (event[1] / previous[1]) ** fraction;
          return previous[1];
        }
        previous = event;
      }
      return previous[1];
    },
    add(kind, value, at) { this.events.push([kind, value, at]); this.events.sort((left, right) => left[2] - right[2]); },
    setValueAtTime(value, at) { this.add("set", value, at); },
    linearRampToValueAtTime(value, at) { this.add("linear", value, at); },
    exponentialRampToValueAtTime(value, at) { this.add("exponential", value, at); },
    cancelScheduledValues(at) { this.cancellations.push(at); this.events = this.events.filter(event => event[2] < at); },
  });
  const node = kind => {
    const current = { kind, connections: [], disconnects: 0,
      connect(target) { this.connections.push(target); }, disconnect() { this.disconnects++; } };
    nodes.push(current);
    return current;
  };
  class Context {
    constructor() { created++; }
    currentTime = 1;
    baseLatency = 0.01;
    state = "running";
    sampleRate = 48000;
    destination = { kind: "destination" };
    createGain() { return { ...node("gain"), gain: parameter(this) }; }
    createStereoPanner() { return { ...node("panner"), pan: parameter(this) }; }
    createBiquadFilter() { return { ...node("filter"), frequency: parameter(this) }; }
    createSource(kind) {
      const source = { ...node(kind), frequency: parameter(this), starts: [], stops: [],
        start(...args) { this.starts.push(args); }, stop(...args) { this.stops.push(args); } };
      sources.push(source);
      return source;
    }
    createOscillator() { return this.createSource("oscillator"); }
    createBufferSource() { return this.createSource("buffer"); }
    createBuffer(channels, frames) {
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      return { numberOfChannels: channels, getChannelData: channel => data[channel] };
    }
  }
  const previous = Object.getOwnPropertyDescriptor(globalThis, "AudioContext");
  Object.defineProperty(globalThis, "AudioContext", { configurable: true, value: Context });
  t.after(() => {
    if (previous) Object.defineProperty(globalThis, "AudioContext", previous);
    else delete globalThis.AudioContext;
  });
  const audio = new PageAudio();
  audio._noise = {};
  return { audio, sources, nodes, created: () => created,
    wall: seconds => { wall = seconds; },
    advance: seconds => { wall += seconds; audio.context.currentTime += seconds; } };
};

const route = voice => {
  const gain = voice.gain.connections[0], panner = gain.connections[0], owner = panner.connections[0];
  assert.equal(gain.kind, "gain", "a channel gain follows the note envelope");
  assert.equal(panner.kind, "panner", "a shared panner follows the channel gain");
  return { gain, panner, owner };
};
const sourceState = sources => sources.map(source => ({ starts: structuredClone(source.starts), stops: structuredClone(source.stops) }));

test("controllers before activation store state without creating audio nodes", t => {
  const { audio, nodes, created } = setup(t);
  audio.controlChange(2, 7, 32, 11);
  audio.controlChange(2, 11, 64, 11);
  audio.controlChange(2, 10, 0, 11);
  assert.equal(audio.context, null);
  assert.equal(created(), 0);
  assert.equal(nodes.length, 0);

  audio.noteOn(2, 60, 100, 11);
  const voice = audio.voices.get("11:2:60"), output = route(voice);
  close(output.gain.gain.value, 32 / 127 * 64 / 127);
  close(output.panner.pan.value, -1);
  close(voice.peak, 100 / 127 * 0.25);
  assert.equal(output.owner, audio.soundGains.get(11).midi);
  assert.deepEqual(output.gain.gain.events, []);
  assert.deepEqual(output.panner.pan.events, []);
});

test("held and overlapping notes share live volume, expression and pan without changing envelopes", t => {
  const { audio, sources, advance } = setup(t);
  for (const [sound, channel, note] of [[11, 2, 60], [11, 2, 64], [11, 3, 60], [22, 2, 60]]) {
    audio.noteOn(channel, note, 100, sound);
  }
  const voices = [...audio.voices.values()], outputs = voices.map(route);
  assert.equal(outputs[0].gain, outputs[1].gain);
  assert.equal(outputs[0].panner, outputs[1].panner);
  assert.notEqual(outputs[0].gain, outputs[2].gain);
  assert.notEqual(outputs[0].gain, outputs[3].gain);
  const envelopes = voices.map(voice => structuredClone(voice.gain.gain.events));
  const initial = sourceState(sources), gain = outputs[0].gain.gain, pan = outputs[0].panner.pan;
  advance(0.2);
  const at = audio.eventTime();
  audio.controlChange(2, 7, 64, 11);
  close(gain.at(at), 100 / 127);
  close(gain.at(at + 0.0025), 82 / 127);
  close(gain.at(at + 0.005), 64 / 127);
  advance(0.002);
  audio.controlChange(2, 11, 32, 11);
  close(gain.at(at + 0.001), (100 - 36 * 0.2) / 127);
  close(gain.at(at + 0.002), (100 - 36 * 0.4) / 127);
  close(gain.at(at + 0.007), 64 / 127 * 32 / 127);
  audio.controlChange(2, 10, 0, 11);
  close(pan.at(at + 0.002), 0);
  close(pan.at(at + 0.0045), -0.5);
  close(pan.at(at + 0.007), -1);
  for (const output of outputs.slice(2)) {
    assert.deepEqual(output.gain.gain.events, []);
    assert.deepEqual(output.panner.pan.events, []);
  }
  assert.deepEqual(voices.map(voice => voice.gain.gain.events), envelopes);
  assert.deepEqual(sourceState(sources), initial);
  assert.equal(sources.length, 4);
});

test("same-time controls retain the unfinished fade and zero recovers without another attack", t => {
  const { audio, sources, advance } = setup(t);
  audio.controlChange(0, 7, 0, 11);
  audio.noteOn(0, 60, 100, 11);
  const voice = audio.voices.get("11:0:60"), gain = route(voice).gain.gain;
  const envelope = structuredClone(voice.gain.gain.events), initial = sourceState(sources);
  close(voice.peak, 100 / 127 * 0.25);
  advance(0.2);
  const at = audio.eventTime();
  audio.controlChange(0, 7, 127, 11);
  advance(0.002);
  audio.controlChange(0, 11, 64, 11);
  audio.controlChange(0, 7, 64, 11);
  close(gain.at(at + 0.001), 0.2);
  close(gain.at(at + 0.002), 0.4);
  close(gain.at(at + 0.007), (64 / 127) ** 2);
  advance(0.02);
  audio.controlChange(0, 11, 0, 11);
  close(gain.at(audio.eventTime() + 0.005), 0);
  advance(0.02);
  audio.controlChange(0, 11, 127, 11);
  close(gain.at(audio.eventTime() + 0.005), 64 / 127);
  assert.deepEqual(voice.gain.gain.events, envelope);
  assert.deepEqual(sourceState(sources), initial);
  assert.equal(audio.voices.get("11:0:60"), voice);
});

test("release tails and percussion after note-off remain on live channel controls", t => {
  const { audio, sources, advance } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  audio.noteOn(9, 40, 100, 11);
  const melody = audio.voices.get("11:0:60"), drum = audio.voices.get("11:9:40");
  advance(0.03);
  audio.noteOff(0, 60, 0, 11);
  audio.noteOff(9, 40, 0, 11);
  assert.equal(audio.voices.size, 1);
  assert.equal(audio.voices.get("11:9:40"), drum);
  assert.equal(audio.sources.size, 2);
  const envelopes = [melody, drum].map(voice => structuredClone(voice.gain.gain.events));
  const initial = sourceState(sources);
  for (const [channel, voice] of [[0, melody], [9, drum]]) {
    audio.controlChange(channel, 7, 0, 11);
    audio.controlChange(channel, 10, 127, 11);
    const output = route(voice), end = audio.eventTime() + 0.005;
    close(output.gain.gain.at(end), 0);
    close(output.panner.pan.at(end), 63 / 64);
  }
  assert.deepEqual([melody, drum].map(voice => voice.gain.gain.events), envelopes);
  assert.deepEqual(sourceState(sources), initial, "percussion keeps its natural end and melody its release end");
});

test("channel controls leave PCM, owner gains and user sliders unchanged", t => {
  const { audio, sources } = setup(t);
  audio.setSoundGain(11, 2500);
  audio.noteOn(0, 60, 100, 11);
  audio.playWave(1, 8000, new Float32Array([0.25, -0.25]), 11);
  const wave = sources[1], envelope = wave.connections[0], gains = audio.soundGains.get(11);
  assert.equal(envelope.connections[0], gains.wave);
  const initial = sourceState(sources), buffer = wave.buffer;
  for (const [control, value] of [[7, 0], [11, 32], [10, 127]]) audio.controlChange(0, control, value, 11);
  assert.equal(wave.buffer, buffer);
  assert.deepEqual(envelope.gain.events, []);
  assert.deepEqual(gains.wave.gain.events, []);
  assert.deepEqual(gains.midi.gain.events, []);
  assert.equal(gains.wave.gain.value, 0.25);
  assert.equal(gains.midi.gain.value, 0.25);
  assert.deepEqual([audio.masterVolume, audio.midiVolume, audio.waveVolume], [0.7, 0.5, 0.5]);
  assert.deepEqual(sourceState(sources), initial);
});

test("a resumed envelope keeps its age while current channel controls change", t => {
  const { audio, advance } = setup(t);
  audio.controlChange(0, 7, 32, 11);
  audio.controlChange(0, 11, 64, 11);
  audio.noteResume(0, 60, 100, 65, 11);
  const voice = audio.voices.get("11:0:60"), output = route(voice);
  close(voice.gain.gain.events[0][1], 100 / 127 * 0.25 * Math.sqrt(0.7));
  close(voice.sourceStart - voice.started, 0.065);
  close(output.gain.gain.value, 32 / 127 * 64 / 127);
  const envelope = structuredClone(voice.gain.gain.events);
  advance(0.02);
  audio.controlChange(0, 7, 127, 11);
  assert.deepEqual(voice.gain.gain.events, envelope);
  audio.noteOff(0, 60, 0, 11);
  close(voice.gain.gain.events.at(-2)[1], 100 / 127 * 0.25 * 0.7 ** (0.075 / 0.11));
});

test("a clock reset cancels older pending controls before applying the latest ramp", t => {
  const { audio, wall } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  const output = route(audio.voices.get("11:0:60"));
  audio.context.baseLatency = 0.09;
  audio.scheduleAnchor = { audio: 1.17, wall: 0 };
  audio.controlChange(0, 7, 32, 11);
  audio.controlChange(0, 10, 127, 11);
  wall(0.002);
  audio.controlChange(0, 11, 64, 11);
  audio.controlChange(0, 10, 0, 11);
  audio.context.state = "suspended";
  audio.context.onstatechange();
  audio.controlChange(0, 7, 0, 11);
  audio.controlChange(0, 10, 64, 11);
  close(output.gain.gain.at(1.09), 100 / 127);
  close(output.gain.gain.at(1.0925), 50 / 127);
  close(output.gain.gain.at(1.095), 0);
  close(output.gain.gain.at(2), 0);
  close(output.panner.pan.at(1.09), 0);
  close(output.panner.pan.at(2), 0);
  for (const parameter of [output.gain.gain, output.panner.pan]) {
    assert.ok(parameter.events.every(event => event[2] <= 1.095 + 1e-9), "old future automation cannot undo the reset");
    assert.ok(parameter.cancellations.length > 0);
  }
});

test("stopping owners including zero disconnects cached channel nodes exactly once", t => {
  const { audio } = setup(t);
  const outputs = new Map();
  for (const sound of [0, 11, 22]) {
    audio.controlChange(0, 7, 32, sound);
    audio.noteOn(0, 60, 100, sound);
    audio.noteOn(1, 64, 100, sound);
    outputs.set(sound, [...audio.voices.values()].filter(voice => voice.sound === sound).map(route));
  }
  audio.stopSound();
  audio.stopSound();
  for (const output of outputs.get(0)) {
    assert.equal(output.gain.disconnects, 1);
    assert.equal(output.panner.disconnects, 1);
  }
  for (const output of [...outputs.get(11), ...outputs.get(22)]) {
    assert.equal(output.gain.disconnects, 0);
    assert.equal(output.panner.disconnects, 0);
  }
  audio.noteOn(0, 60, 100);
  const replacement = route(audio.voices.get("0:60"));
  assert.notEqual(replacement.gain, outputs.get(0)[0].gain);
  close(replacement.gain.gain.value, 100 / 127);
  audio.stopSound(11);
  audio.stopAll();
  audio.stopAll();
  for (const output of [...outputs.values()].flat().concat(replacement)) {
    assert.equal(output.gain.disconnects, 1);
    assert.equal(output.panner.disconnects, 1);
  }
  assert.equal(audio.sources.size, 0);
  assert.equal(audio.voices.size, 0);
});
