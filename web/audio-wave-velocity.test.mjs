import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { AudioStream } from "./audio-stream.js";
import { playAudioEvents, sessionURL } from "./session.js";

const word = value => [value >>> 24, (value >>> 16) & 255, (value >>> 8) & 255, value & 255];
const message = (...operations) => Uint8Array.from([..."WFA2"].map(letter => letter.charCodeAt(0)).concat(operations.flat())).buffer;
const define = [0x10, ...word(1), ...word(4), 0, 1, 0, 2];
const play = [0x11, ...word(1), 1, ...word(8000)];

function setup(t) {
  t.mock.method(performance, "now", () => 0);
  const audio = new PageAudio(), gains = [];
  const node = () => ({ connect() {}, disconnect() {} });
  audio.context = {
    state: "running", currentTime: 1, baseLatency: .01, sampleRate: 48000,
    createGain: () => {
      const gain = Object.assign(node(), { gain: { value: 1, setValueAtTime() {}, linearRampToValueAtTime() {}, cancelScheduledValues() {} } });
      gains.push(gain);
      return gain;
    },
    createBufferSource: () => Object.assign(node(), { start() {}, stop() {} }),
    createBuffer: (channels, frames, rate) => {
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      return { numberOfChannels: channels, length: frames, sampleRate: rate, getChannelData: channel => data[channel] };
    },
  };
  audio.waveGain = node();
  audio.midiGain = node();
  return { audio, gains };
}

test("the page asks for wave velocities", () => {
  const original = globalThis.location;
  globalThis.location = { protocol: "https:", host: "example.test" };
  try {
    assert.match(sessionURL(), /[?&]wavevelocity=1(&|$)/);
  } finally {
    globalThis.location = original;
  }
});

test("a wave velocity reaches only the wave operation after it", () => {
  const events = new AudioStream().decode(message(define, [0x1a, 20], play, play));
  assert.equal(events.length, 2);
  assert.equal(events[0].velocity, 20);
  assert.equal("velocity" in events[1], false, "the next wave has none of its own");
});

test("a wave velocity that is zero, too large or without its wave refuses the message", () => {
  for (const [name, bytes] of [
    ["zero", message(define, [0x1a, 0], play)],
    ["too large", message(define, [0x1a, 128], play)],
    ["followed by a note", message(define, [0x1a, 20], [0x01, 0, 60, 100], play)],
    ["at the end", message(define, play, [0x1a, 20])],
    ["truncated", message(define, [0x1a])],
  ]) {
    assert.throws(() => new AudioStream().decode(bytes), /sound message/, name);
  }
  // A velocity whose wave definition never arrived is consumed with it.
  assert.deepEqual(new AudioStream().decode(message([0x1a, 20], play, [0x01, 0, 60, 100])), [{ kind: "noteOn", channel: 0, note: 60, velocity: 100 }]);
});

test("a stream wave's level follows the square root of its velocity, 6 dB up, never past full", t => {
  const { audio, gains } = setup(t);
  const samples = Float32Array.of(.25, -.25);
  for (const [velocity, level] of [[0, 1], [127, 1], [52, 1], [51, Math.sqrt(51 / 127) * 200 / 127], [20, Math.sqrt(20 / 127) * 200 / 127], [1, Math.sqrt(1 / 127) * 200 / 127]]) {
    audio.playWave(1, 8000, samples, 7, true, 0, 0, velocity);
    // The sound's own output gain is unity; the wave's source gain is not.
    const gain = gains.filter(node => node.gain.value !== 1).at(-1).gain.value;
    assert.ok(Math.abs(gain - 0.8 * level) < 1e-12, `velocity ${velocity}: gain ${gain}, want ${0.8 * level}`);
  }
  const before = gains.length;
  for (const velocity of [-1, 128, 1.5, "20", null]) assert.equal(audio.playWave(1, 8000, samples, 7, true, 0, 0, velocity), false);
  assert.equal(gains.length, before, "an invalid velocity plays nothing");
});

test("a delivered wave keeps its velocity and one without plays at full level", t => {
  const { audio, gains } = setup(t);
  const samples = Float32Array.of(.25, -.25);
  playAudioEvents(audio, [
    { kind: "playWave", channels: 1, rate: 8000, samples, cacheable: true, sound: 7, velocity: 20 },
    { kind: "playWave", channels: 1, rate: 8000, samples, cacheable: true, sound: 7 },
  ]);
  const levels = gains.filter(gain => gain.gain.value !== 1).map(gain => gain.gain.value);
  assert.equal(levels.length, 2);
  assert.ok(Math.abs(levels[0] - 0.8 * Math.sqrt(20 / 127) * 200 / 127) < 1e-12);
  assert.equal(levels[1], 0.8);
});

test("a batch with an invalid wave velocity is refused whole before anything plays", t => {
  const { audio, gains } = setup(t);
  const samples = Float32Array.of(.25, -.25);
  for (const velocity of [-1, 128, 1.5, "20", null]) {
    assert.equal(playAudioEvents(audio, [
      { kind: "playWave", channels: 1, rate: 8000, samples, cacheable: true, sound: 7 },
      { kind: "playWave", channels: 1, rate: 8000, samples, cacheable: true, sound: 7, velocity },
    ]), false, `velocity ${velocity}`);
  }
  assert.equal(gains.filter(gain => gain.gain.value !== 1).length, 0, "nothing of the refused batches played");
});
