import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { fakeFM } from "./audio-test-fm.mjs";

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
  const { fm, createFM } = fakeFM();
  const audio = new PageAudio({ createFM });
  return { audio, fm, sources, wall: seconds => { wall = seconds; } };
};

const selectRange = (audio, channel = 0, sound = 11) => {
  audio.controlChange(channel, 101, 0, sound);
  audio.controlChange(channel, 100, 0, sound);
};
const ids = fm => fm.notes().map(note => note.id);
const ops = (fm, id) => fm.of(id).map(command => command.op);
const laneOf = (audio, sound, channel) => audio.channelOutputs.get(sound)[channel].lane;
const bends = (fm, lane) => fm.commands.filter(command => command.op === "bend" && command.lane === lane);

const range = (audio, channel = 0, sound = 11) => {
  const state = audio.channelsFor(sound)[channel];
  return [state.bendRange, state.bendRangeCents];
};

test("sustain defers note-off, velocity-zero and all-notes-off until pedal release", t => {
  const { audio, fm, wall } = setup(t);
  audio.controlChange(2, 64, 63, 11);
  assert.equal(audio.context, null);
  audio.noteOn(2, 58, 100, 11);
  audio.noteOff(2, 58, 0, 11);
  assert.deepEqual(ops(fm, ids(fm)[0]), ["off"], "63 leaves sustain disabled");
  audio.controlChange(2, 64, 64, 11);
  for (const note of [60, 62, 64]) audio.noteOn(2, note, 100, 11);
  wall(0.02);
  audio.noteOff(2, 60, 0, 11);
  audio.noteOn(2, 62, 0, 11);
  audio.controlChange(2, 123, 0, 11);
  assert.ok(ids(fm).slice(1).every(id => fm.of(id).length === 0));
  audio.noteOn(2, 67, 100, 11);
  audio.noteOn(2, 60, 100, 22);
  audio.noteOn(3, 60, 100, 11);
  audio.noteOff(2, 60, 0, 22);
  audio.noteOff(3, 60, 0, 11);
  assert.ok(ids(fm).slice(5).every(id => ops(fm, id).join() === "off"));
  const started = structuredClone(fm.notes());
  wall(0.04);
  audio.controlChange(2, 64, 63, 11);
  for (const id of ids(fm).slice(1, 4)) {
    const [release] = fm.of(id);
    assert.equal(release.op, "off");
    close(release.at, 1.05);
  }
  assert.equal(fm.of(ids(fm)[4]).length, 0, "pedal-up keeps keys still held down");
  assert.ok(audio.voices.has("11:2:67"));
  audio.controlChange(2, 64, 0, 11);
  assert.ok(ids(fm).slice(1, 4).every(id => fm.of(id).length === 1));
  assert.deepEqual(fm.notes(), started);
});

test("restored sustain keeps an aged released key audible until pedal-up without replaying attack", t => {
  const { audio, fm, wall } = setup(t);
  audio.controlChange(0, 64, 127, 11);
  audio.noteResume(0, 69, 100, 350, 11);
  const restored = audio.voices.get("11:0:69");
  close(fm.notes()[0].age, 0.35);
  audio.noteOff(0, 69, 0, 11);
  assert.equal(audio.voices.get("11:0:69"), restored);
  assert.deepEqual(fm.of(restored.fm), []);
  audio.noteResume(0, 69, 100, 350, 22);
  wall(0.04);
  audio.controlChange(0, 64, 0, 11);
  const [release] = fm.of(restored.fm);
  assert.equal(release.op, "off");
  close(release.at, 1.05);
  assert.equal(audio.voices.has("11:0:69"), false);
  assert.ok(audio.voices.has("22:0:69"));
  assert.equal(fm.notes().length, 2, "neither key replays its attack");
  assert.deepEqual(fm.of(ids(fm)[1]), []);
});

test("percussion ignores note-off and sustain but all-notes-off releases its remaining tail", t => {
  const { audio, fm, wall } = setup(t);
  audio.controlChange(9, 64, 127, 11);
  audio.noteOn(9, 38, 100, 11);
  const voice = audio.voices.get("11:9:38");
  audio.noteOff(9, 38, 0, 11);
  audio.noteOn(9, 38, 0, 11);
  audio.controlChange(9, 64, 0, 11);
  assert.equal(audio.voices.get("11:9:38"), voice);
  assert.deepEqual(fm.of(voice.fm), []);
  audio.controlChange(9, 64, 127, 11);
  wall(0.17);
  audio.context.currentTime = 1.17;
  audio.controlChange(9, 123, 0, 11);
  assert.equal(audio.voices.has("11:9:38"), false);
  const [release] = fm.of(voice.fm);
  assert.equal(release.op, "off");
  close(release.at, 1.18);
  assert.equal(audio.sources.size, 1, "the voice stays tracked until the synthesizer ends it");
  fm.end(voice.fm);
  assert.equal(audio.sources.size, 0);
});

test("all-sound-off schedules an exact cut including release tails without changing their prefix", t => {
  const { audio, fm, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  audio.noteOn(0, 64, 100, 11);
  audio.noteOn(1, 60, 100, 11);
  audio.noteOn(0, 60, 100, 22);
  audio.playWave(1, 8000, new Float32Array(800), 11);
  const [tail, held, other, peer] = ids(fm);
  wall(0.04);
  audio.noteOff(0, 60, 0, 11);
  const waves = sources.map(source => structuredClone(source.stops));
  wall(0.06);
  audio.controlChange(0, 120, 0, 11);
  assert.deepEqual(ops(fm, tail), ["off", "cut"], "the release keeps sounding until the cut");
  close(fm.of(tail)[0].at, 1.05);
  close(fm.of(tail)[1].at, 1.07);
  assert.deepEqual(ops(fm, held), ["cut"]);
  close(fm.of(held)[0].at, 1.07);
  assert.deepEqual([...fm.of(other), ...fm.of(peer)], []);
  assert.deepEqual(sources.map(source => source.stops), waves);
  assert.ok(sources.every(source => source.disconnects === 0));
  assert.equal(audio.sources.size, 5, "future cuts remain available to a stop/reset before rendering");
  assert.equal(audio.voices.has("11:0:64"), false);
  wall(0.08);
  audio.controlChange(0, 120, 0, 11);
  assert.ok([tail, held].every(id => fm.of(id).filter(command => command.op === "cut").every(command => command.at <= 1.07 + 1e-9)));
  fm.end(tail, held);
  assert.equal(audio.sources.size, 3);
});

test("all-sound-off includes retriggered drums but preserves peer drums and PCM", t => {
  const { audio, fm, sources, wall } = setup(t);
  audio.noteOn(9, 38, 100, 11);
  audio.noteOn(9, 38, 100, 11);
  audio.noteOn(9, 38, 100, 22);
  audio.playWave(1, 8000, new Float32Array(800), 11);
  const waves = sources.map(source => structuredClone(source.stops));
  const [first, second, peer] = ids(fm);
  wall(0.002);
  audio.controlChange(9, 120, 0, 11);
  for (const id of [first, second]) close(fm.of(id).at(-1).at, 1.012);
  assert.deepEqual(fm.of(peer), []);
  assert.deepEqual(sources.map(source => source.stops), waves);
  assert.equal(audio.voices.has("11:9:38"), false);
  assert.ok(audio.voices.has("22:9:38"));
});

test("reset-controllers releases deferred notes and retains volume, pan, program and bend range", t => {
  const { audio, fm, wall } = setup(t);
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
  const lane = laneOf(audio, 11, 0);
  close(fm.lanes[lane].ratio, 2 ** (6.25 / 12));
  const peer = structuredClone(audio.channelsFor(22)[0]);
  wall(0.03);
  audio.controlChange(0, 121, 0, 11);
  const state = audio.channelsFor(11)[0];
  assert.deepEqual([state.expression, state.sustain, state.bend], [127, 0, 8192]);
  assert.deepEqual([state.volume, state.pan, state.program, ...range(audio)], [64, 0, 56, 12, 50]);
  assert.deepEqual([state.rpnMSB, state.rpnLSB, state.nrpnMSB, state.nrpnLSB], [127, 127, 127, 127]);
  assert.equal(state.parameterKind, null);
  const [deferred, held, other] = ids(fm);
  assert.deepEqual(ops(fm, deferred), ["off"]);
  assert.deepEqual(fm.of(held), []);
  assert.deepEqual(fm.of(other), []);
  close(bends(fm, lane).at(-1).ratio, 1);
  assert.deepEqual(bends(fm, laneOf(audio, 22, 0)), []);
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
  const { audio, fm, wall } = setup(t);
  audio.noteOn(0, 69, 100, 11);
  audio.noteOn(0, 72, 100, 11);
  audio.noteOff(0, 72, 0, 11);
  audio.noteOn(1, 69, 100, 11);
  audio.noteOn(0, 69, 100, 22);
  audio.noteOn(9, 38, 100, 11);
  const lifetime = structuredClone(fm.commands.filter(command => command.op !== "bend"));
  wall(0.02);
  audio.pitchBend(0, 12288, 11);
  selectRange(audio);
  audio.controlChange(0, 6, 12, 11);
  audio.controlChange(0, 38, 50, 11);
  const lane = laneOf(audio, 11, 0);
  close(bends(fm, lane).at(-1).ratio, 2 ** (6.25 / 12));
  close(bends(fm, lane).at(-1).at, 1.03);
  audio.controlChange(0, 96, 0, 11);
  close(bends(fm, lane).at(-1).ratio, 2 ** (6.255 / 12));
  for (const [sound, channel] of [[11, 1], [22, 0], [11, 9]]) assert.deepEqual(bends(fm, laneOf(audio, sound, channel)), []);
  assert.deepEqual(fm.commands.filter(command => command.op !== "bend"), lifetime, "a bend neither restarts nor releases a note");
  assert.equal(fm.notes().length, 5);
});
