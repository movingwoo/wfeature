import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);
const setup = t => {
  let wall = 0;
  const sources = [];
  t.mock.method(performance, "now", () => wall * 1000);
  const parameter = () => ({
    value: 0, events: [],
    setValueAtTime(value, at) { this.events.push(["set", value, at]); },
    linearRampToValueAtTime(value, at) { this.events.push(["linear", value, at]); },
    exponentialRampToValueAtTime(value, at) { this.events.push(["exponential", value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[2] < at); },
  });
  const node = () => ({ connections: [], disconnects: 0,
    connect(target) { this.connections.push(target); }, disconnect() { this.disconnects++; } });
  class Context {
    currentTime = 1;
    baseLatency = 0.01;
    sampleRate = 48000;
    state = "running";
    destination = {};
    createGain() { return { ...node(), gain: parameter() }; }
    createStereoPanner() { return { ...node(), pan: parameter() }; }
    createBiquadFilter() { return { ...node(), frequency: parameter() }; }
    createSource(kind) {
      const source = { ...node(), kind, frequency: parameter(), starts: [], stops: [],
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
  return { audio, sources, wall: seconds => { wall = seconds; } };
};

const selectRange = (audio, channel = 0, sound = 11) => {
  audio.controlChange(channel, 101, 0, sound);
  audio.controlChange(channel, 100, 0, sound);
};
const range = (audio, channel = 0, sound = 11) => {
  const state = audio.channelsFor(sound)[channel];
  return [state.bendRange, state.bendRangeCents];
};

test("sustain defers note-off, velocity-zero and all-notes-off until pedal release", t => {
  const { audio, sources, wall } = setup(t);
  audio.controlChange(2, 64, 63, 11);
  assert.equal(audio.context, null);
  audio.noteOn(2, 58, 100, 11);
  audio.noteOff(2, 58, 0, 11);
  assert.equal(sources[0].stops.length, 1, "63 leaves sustain disabled");
  audio.controlChange(2, 64, 64, 11);
  for (const note of [60, 62, 64]) audio.noteOn(2, note, 100, 11);
  wall(0.02);
  audio.noteOff(2, 60, 0, 11);
  audio.noteOn(2, 62, 0, 11);
  audio.controlChange(2, 123, 0, 11);
  assert.ok(sources.slice(1).every(source => source.stops.length === 0));
  audio.noteOn(2, 67, 100, 11);
  audio.noteOn(2, 60, 100, 22);
  audio.noteOn(3, 60, 100, 11);
  audio.noteOff(2, 60, 0, 22);
  audio.noteOff(3, 60, 0, 11);
  assert.ok(sources.slice(5).every(source => source.stops.length === 1));
  const started = sources.map(source => structuredClone(source.starts));
  wall(0.04);
  audio.controlChange(2, 64, 63, 11);
  for (const source of sources.slice(1, 4)) {
    close(source.stops[0][0], 1.05 + 0.07);
  }
  assert.equal(sources[4].stops.length, 0, "pedal-up keeps keys still held down");
  assert.ok(audio.voices.has("11:2:67"));
  audio.controlChange(2, 64, 0, 11);
  assert.ok(sources.slice(1, 4).every(source => source.stops.length === 1));
  assert.deepEqual(sources.map(source => source.starts), started);
});

test("restored sustain keeps an aged released key audible until pedal-up without replaying attack", t => {
  const { audio, sources, wall } = setup(t);
  audio.controlChange(0, 64, 127, 11);
  audio.noteResume(0, 69, 100, 350, 11);
  const restored = audio.voices.get("11:0:69");
  const sustain = 100 / 127 * 0.25 * 0.7;
  assert.equal(restored.gain.gain.events.length, 1);
  close(restored.gain.gain.events[0][1], sustain);
  audio.noteOff(0, 69, 0, 11);
  assert.equal(audio.voices.get("11:0:69"), restored);
  assert.equal(sources[0].stops.length, 0);
  audio.noteResume(0, 69, 100, 350, 22);
  wall(0.04);
  audio.controlChange(0, 64, 0, 11);
  close(restored.gain.gain.events.at(-2)[1], sustain);
  close(restored.gain.gain.events.at(-2)[2], 1.05);
  close(sources[0].stops[0][0], 1.12);
  assert.equal(audio.voices.has("11:0:69"), false);
  assert.ok(audio.voices.has("22:0:69"));
  assert.deepEqual(sources.map(source => source.starts.length), [1, 1]);
  assert.equal(sources[1].stops.length, 0);
});

test("percussion ignores note-off and sustain but all-notes-off releases its remaining tail", t => {
  const { audio, sources, wall } = setup(t);
  audio.controlChange(9, 64, 127, 11);
  audio.noteOn(9, 38, 100, 11);
  const voice = audio.voices.get("11:9:38");
  const natural = structuredClone(sources[0].stops);
  audio.noteOff(9, 38, 0, 11);
  audio.noteOn(9, 38, 0, 11);
  audio.controlChange(9, 64, 0, 11);
  assert.equal(audio.voices.get("11:9:38"), voice);
  assert.deepEqual(sources[0].stops, natural);
  audio.controlChange(9, 64, 127, 11);
  wall(0.17);
  audio.context.currentTime = 1.17;
  audio.controlChange(9, 123, 0, 11);
  assert.equal(audio.voices.has("11:9:38"), false);
  close(sources[0].stops.at(-1)[0], natural[0][0]);
  close(voice.gain.gain.events.at(-1)[2], natural[0][0]);
  assert.equal(audio.sources.size, 1, "the source stays tracked until its scheduled end");
  sources[0].onended();
  assert.equal(audio.sources.size, 0);
});

test("all-sound-off schedules an exact cut including release tails without changing their prefix", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  const tail = audio.voices.get("11:0:60");
  audio.noteOn(0, 64, 100, 11);
  audio.noteOn(1, 60, 100, 11);
  audio.noteOn(0, 60, 100, 22);
  audio.playWave(1, 8000, new Float32Array(800), 11);
  wall(0.04);
  audio.noteOff(0, 60, 0, 11);
  const envelope = structuredClone(tail.gain.gain.events);
  const peers = sources.slice(2).map(source => structuredClone(source.stops));
  wall(0.06);
  audio.controlChange(0, 120, 0, 11);
  close(sources[0].stops.at(-1)[0], 1.07);
  close(sources[1].stops.at(-1)[0], 1.07);
  assert.deepEqual(tail.gain.gain.events, envelope, "the pending release ramp remains intact before the cut");
  assert.deepEqual(sources.slice(2).map(source => source.stops), peers);
  assert.ok(sources.every(source => source.disconnects === 0));
  assert.equal(audio.sources.size, 5, "future cuts remain available to a stop/reset before rendering");
  assert.equal(audio.voices.has("11:0:64"), false);
  wall(0.08);
  audio.controlChange(0, 120, 0, 11);
  assert.ok(sources.slice(0, 2).every(source => source.stops.at(-1)[0] <= 1.07));
  sources[0].onended();
  sources[1].onended();
  assert.equal(audio.sources.size, 3);
});

test("all-sound-off includes retriggered drums but preserves peer drums and PCM", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(9, 38, 100, 11);
  audio.noteOn(9, 38, 100, 11);
  audio.noteOn(9, 38, 100, 22);
  audio.playWave(1, 8000, new Float32Array(800), 11);
  const peers = sources.slice(2).map(source => structuredClone(source.stops));
  wall(0.002);
  audio.controlChange(9, 120, 0, 11);
  for (const source of sources.slice(0, 2)) close(source.stops.at(-1)[0], 1.012);
  assert.deepEqual(sources.slice(2).map(source => source.stops), peers);
  assert.equal(audio.voices.has("11:9:38"), false);
  assert.ok(audio.voices.has("22:9:38"));
});

test("reset-controllers releases deferred notes and retains volume, pan, program and bend range", t => {
  const { audio, sources, wall } = setup(t);
  selectRange(audio);
  audio.controlChange(0, 6, 12, 11);
  audio.controlChange(0, 38, 50, 11);
  audio.controlChange(0, 99, 5, 11);
  audio.controlChange(0, 98, 6, 11);
  audio.programChange(0, 56, 11);
  audio.controlChange(0, 7, 64, 11);
  audio.controlChange(0, 10, 0, 11);
  audio.controlChange(0, 11, 32, 11);
  audio.controlChange(0, 64, 127, 11);
  audio.pitchBend(0, 12288, 11);
  audio.noteOn(0, 69, 100, 11);
  audio.noteOn(0, 72, 100, 11);
  audio.noteOff(0, 69, 0, 11);
  audio.noteOn(0, 69, 100, 22);
  const peer = structuredClone(audio.channelsFor(22)[0]);
  wall(0.03);
  audio.controlChange(0, 121, 0, 11);
  const state = audio.channelsFor(11)[0];
  assert.deepEqual([state.expression, state.sustain, state.bend], [127, 0, 8192]);
  assert.deepEqual([state.volume, state.pan, state.program, ...range(audio)], [64, 0, 56, 12, 50]);
  assert.deepEqual([state.rpnMSB, state.rpnLSB, state.nrpnMSB, state.nrpnLSB], [127, 127, 127, 127]);
  assert.equal(state.parameterKind, null);
  assert.equal(sources[0].stops.length, 1);
  assert.equal(sources[1].stops.length, 0);
  assert.equal(sources[2].stops.length, 0);
  close(sources[0].frequency.events.at(-1)[1], 440);
  close(sources[1].frequency.events.at(-1)[1], 440 * 2 ** (3 / 12));
  const output = audio.channelOutputs.get(11)[0];
  close(output.gain.gain.events.at(-1)[1], 64 / 127);
  assert.deepEqual(audio.channelsFor(22)[0], peer);
  audio.controlChange(0, 6, 1, 11);
  assert.deepEqual(range(audio), [12, 50], "reset selectors block stale data entry");
});

test("RPN and NRPN selectors preserve their bytes and gate bend range data entry before activation", t => {
  const { audio } = setup(t);
  assert.deepEqual(range(audio), [2, 0]);
  audio.controlChange(0, 6, 12, 11);
  assert.deepEqual(range(audio), [2, 0]);
  selectRange(audio);
  audio.controlChange(0, 38, 127, 11);
  assert.deepEqual(range(audio), [2, 99], "invalid cent values clamp to 99");
  audio.controlChange(0, 6, 12, 11);
  assert.deepEqual(range(audio), [12, 0], "coarse data entry resets fine tuning");
  audio.controlChange(0, 38, 50, 11);
  audio.controlChange(0, 100, 2, 11);
  audio.controlChange(0, 101, 0, 11);
  audio.controlChange(0, 6, 1, 11);
  assert.deepEqual(range(audio), [12, 50], "selecting MSB must not reset an unsupported LSB");
  audio.controlChange(0, 100, 0, 11);
  audio.controlChange(0, 99, 10, 11);
  audio.controlChange(0, 98, 11, 11);
  for (const [cc, value] of [[6, 1], [38, 1], [96, 0], [97, 0]]) audio.controlChange(0, cc, value, 11);
  assert.deepEqual(range(audio), [12, 50]);
  assert.deepEqual([audio.channelsFor(11)[0].rpnMSB, audio.channelsFor(11)[0].rpnLSB], [0, 0]);
  audio.controlChange(0, 101, 0, 11);
  audio.controlChange(0, 38, 25, 11);
  assert.deepEqual(range(audio), [12, 25]);
  assert.deepEqual([audio.channelsFor(11)[0].nrpnMSB, audio.channelsFor(11)[0].nrpnLSB], [10, 11]);
  audio.controlChange(0, 101, 127, 11);
  audio.controlChange(0, 100, 127, 11);
  audio.controlChange(0, 6, 1, 11);
  audio.controlChange(0, 96, 0, 11);
  assert.deepEqual(range(audio), [12, 25]);
  assert.equal(audio.context, null, "controller-only input must not activate audio");
});

test("RPN zero increment and decrement use one cent with carry and bounded endpoints", t => {
  const { audio } = setup(t);
  selectRange(audio);
  audio.controlChange(0, 38, 99, 11);
  audio.controlChange(0, 96, 127, 11);
  assert.deepEqual(range(audio), [3, 0]);
  audio.controlChange(0, 97, 0, 11);
  assert.deepEqual(range(audio), [2, 99]);
  audio.controlChange(0, 6, 0, 11);
  audio.controlChange(0, 97, 127, 11);
  assert.deepEqual(range(audio), [0, 0]);
  audio.controlChange(0, 6, 127, 11);
  audio.controlChange(0, 38, 99, 11);
  audio.controlChange(0, 96, 0, 11);
  assert.deepEqual(range(audio), [127, 99]);
  audio.controlChange(0, 121, 0, 11);
  audio.controlChange(0, 97, 0, 11);
  assert.deepEqual(range(audio), [127, 99]);
});

test("bend and range changes retune held notes and release tails without retriggering peers", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 69, 100, 11);
  audio.noteOn(0, 72, 100, 11);
  audio.noteOff(0, 72, 0, 11);
  audio.noteOn(1, 69, 100, 11);
  audio.noteOn(0, 69, 100, 22);
  audio.noteOn(9, 38, 100, 11);
  const lifetime = sources.map(source => [structuredClone(source.starts), structuredClone(source.stops)]);
  wall(0.02);
  audio.pitchBend(0, 12288, 11);
  selectRange(audio);
  audio.controlChange(0, 6, 12, 11);
  audio.controlChange(0, 38, 50, 11);
  close(sources[0].frequency.events.at(-1)[1], 440 * 2 ** (6.25 / 12));
  close(sources[1].frequency.events.at(-1)[1], 440 * 2 ** (9.25 / 12));
  close(sources[0].frequency.events.at(-1)[2], 1.03);
  audio.controlChange(0, 96, 0, 11);
  close(sources[0].frequency.events.at(-1)[1], 440 * 2 ** (6.255 / 12));
  assert.ok(sources.slice(2).every(source => source.frequency.events.length === 0));
  assert.deepEqual(sources.map(source => [source.starts, source.stops]), lifetime);
  assert.equal(sources.length, 5);
});
