import assert from "node:assert/strict";
import { test } from "node:test";

import { AudioStream } from "./audio-stream.js";

const word = value => [value >>> 24, (value >>> 16) & 0xff, (value >>> 8) & 0xff, value & 0xff];
const message = (...operations) => Uint8Array.from([0x57, 0x46, 0x41, 0x32, ...operations.flat()]).buffer;
const select = sound => [0x07, ...word(sound)];
const define = (id, bytes) => [0x10, ...word(id), ...word(bytes.length), ...bytes];
const wave = id => [0x11, ...word(id), 1, ...word(8000)];
const seconds = value => { const bytes = new Uint8Array(8); new DataView(bytes.buffer).setFloat64(0, value); return [...bytes]; };

test("presentation selection and frontier retain zero, signed deadlines and resets", () => {
  const stream = new AudioStream();
  assert.deepEqual(stream.decode(message(
    [0x0b, 1, ...seconds(0)], [1, 0, 60, 100],
    [0x0b, 1, ...seconds(.005)], [2, 0, 60, 0],
    [3, 0, 42], [0x0b, 0], [1, 0, 64, 90],
    [0x0b, 1, ...seconds(-.01)], [2, 0, 64, 0], [6],
    [1, 0, 67, 80], [0x0c, ...seconds(.1)],
  )), [
    { kind: "noteOn", channel: 0, note: 60, velocity: 100, at: 0 },
    { kind: "noteOff", channel: 0, note: 60, velocity: 0, at: .005 },
    { kind: "programChange", channel: 0, program: 42, at: .005 },
    { kind: "noteOn", channel: 0, note: 64, velocity: 90 },
    { kind: "noteOff", channel: 0, note: 64, velocity: 0, at: -.01 },
    { kind: "allOff" }, { kind: "noteOn", channel: 0, note: 67, velocity: 80 },
    { kind: "clock", at: .1 },
  ]);
  stream.decode(message([0x0b, 1, ...seconds(1)], [0x0c, ...seconds(1)]));
  assert.deepEqual(stream.decode(message([8])), [{ kind: "stopSound" }]);
});

test("invalid and truncated presentation operands reject the complete batch", () => {
  const stream = new AudioStream();
  for (const value of [NaN, Infinity, -Infinity]) {
    for (const prefix of [[0x0b, 1], [0x0c]]) {
      assert.throws(() => stream.decode(message([1, 0, 60, 100], [...prefix, ...seconds(value)])));
    }
  }
  assert.throws(() => stream.decode(message([0x0b, 2])));
  assert.throws(() => stream.decode(message([0x0b])));
  for (let length = 0; length < 8; length++) {
    for (const prefix of [[0x0b, 1], [0x0c]]) {
      assert.throws(() => stream.decode(message([...prefix, ...seconds(.1).slice(0, length)])), /ends inside an operation/);
    }
  }
});

test("sound selection identifies every operation and preserves unsigned IDs", () => {
  const stream = new AudioStream();
  const sound = 0x89abcdef;
  const events = stream.decode(message(
    select(sound),
    [0x09, 0x09, 0xc4],
    [0x03, 1, 42],
    [0x01, 1, 60, 100],
    [0x04, 1, 7, 90],
    [0x05, 1, 0x23, 0x28],
    [0x02, 1, 60, 0],
    define(5, [0xf0, 0x7e, 0xf7]), [0x12, ...word(5)],
    [0x08], [0x06],
    select(0), [0x01, 0, 67, 80], [0x08],
  ));
  assert.deepEqual(events, [
    { kind: "soundGain", value: 2500, sound },
    { kind: "programChange", channel: 1, program: 42, sound },
    { kind: "noteOn", channel: 1, note: 60, velocity: 100, sound },
    { kind: "controlChange", channel: 1, control: 7, value: 90, sound },
    { kind: "pitchBend", channel: 1, value: 9000, sound },
    { kind: "noteOff", channel: 1, note: 60, velocity: 0, sound },
    { kind: "sysex", data: Uint8Array.of(0xf0, 0x7e, 0xf7), sound },
    { kind: "stopSound", sound },
    { kind: "allOff" },
    { kind: "noteOn", channel: 0, note: 67, velocity: 80 },
    { kind: "stopSound" },
  ]);
});

test("selection resets for each message including empty and rejected batches", () => {
  const stream = new AudioStream();
  assert.deepEqual(stream.decode(message(select(17), [0x08])), [{ kind: "stopSound", sound: 17 }]);
  assert.deepEqual(stream.decode(message()), []);
  assert.deepEqual(stream.decode(message([0x08])), [{ kind: "stopSound" }]);
  assert.throws(() => stream.decode(message(select(23), [0x01, 0])), /ends inside an operation/);
  assert.deepEqual(stream.decode(message([0x01, 0, 60, 100])), [{ kind: "noteOn", channel: 0, note: 60, velocity: 100 }]);
});

test("different owners reuse one PCM definition and stopping leaves it cached", () => {
  const stream = new AudioStream();
  const events = stream.decode(message(
    select(7), define(1, [0, 0x40, 0, 0xc0]), wave(1),
    [0x08], select(9), wave(1),
  ));
  assert.equal(events.length, 3);
  assert.deepEqual(events[1], { kind: "stopSound", sound: 7 });
  assert.equal(events[0].sound, 7);
  assert.equal(events[2].sound, 9);
  assert.deepEqual([...events[0].samples], [0.5, -0.5]);
  assert.equal(events[0].samples, events[2].samples);
  const next = stream.decode(message(wave(1)));
  assert.equal(next[0].samples, events[0].samples);
  assert.equal(Object.hasOwn(next[0], "sound"), false);
});

test("forget affects definitions without changing the selected sound", () => {
  const stream = new AudioStream();
  stream.decode(message(define(1, [0, 0x40])));
  const events = stream.decode(message(
    select(0xffffffff), [0x13], wave(1),
    define(2, [0, 0xc0]), wave(2), [0x08],
  ));
  assert.equal(events.length, 2);
  assert.equal(events[0].sound, 0xffffffff);
  assert.deepEqual([...events[0].samples], [-0.5]);
  assert.deepEqual(events[1], { kind: "stopSound", sound: 0xffffffff });
  assert.deepEqual(stream.decode(message(wave(1))), []);
  assert.equal(Object.hasOwn(stream.decode(message(wave(2)))[0], "sound"), false);
});

test("truncated selectors and unknown operations reject the whole batch", () => {
  const stream = new AudioStream();
  for (let length = 0; length < 4; length++) {
    const truncated = [0x07, ...word(19).slice(0, length)];
    assert.throws(() => stream.decode(message([0x01, 0, 60, 100], truncated)), /ends inside an operation/);
  }
  for (const truncated of [[0x09], [0x09, 0]]) {
    assert.throws(() => stream.decode(message(select(19), truncated)), /ends inside an operation/);
  }
  for (let length = 0; length < 7; length++) {
    const operands = [1, 60, 100, ...word(65)];
    assert.throws(() => stream.decode(message(select(19), [0x0a, ...operands.slice(0, length)])), /ends inside an operation/);
  }
  assert.throws(() => stream.decode(message(select(19), [0x08], [0xff])), /unknown sound operation/);
  assert.deepEqual(stream.decode(message([0x08])), [{ kind: "stopSound" }]);
});

test("PCM controls and routed mono waves preserve ownership, timing and immutable definitions", () => {
  const stream = new AudioStream();
  const sound = 0x89abcdef, pcmChannel = 65535;
  const routed = [0x14, ...word(1), 1, ...word(8000), 0xff, 0xff];
  const events = stream.decode(message(
    define(1, [0, 0x40, 0, 0xc0]), select(sound), [0x0b, 1, ...seconds(.025)],
    [0x15, 0xff, 0xff, 7, 0], [0x15, 0xff, 0xff, 11, 64], [0x15, 0xff, 0xff, 10, 127],
    routed, wave(1),
  ));
  assert.deepEqual(events.slice(0, 3), [
    { kind: "pcmControl", pcmChannel, control: 7, value: 0, sound, at: .025 },
    { kind: "pcmControl", pcmChannel, control: 11, value: 64, sound, at: .025 },
    { kind: "pcmControl", pcmChannel, control: 10, value: 127, sound, at: .025 },
  ]);
  assert.deepEqual(events[3], { kind: "playWave", channels: 1, rate: 8000, pcmChannel, sound, at: .025,
    samples: Float32Array.of(.5, -.5), cacheable: true });
  assert.equal(events[3].samples, events[4].samples);
  assert.equal(Object.hasOwn(events[4], "pcmChannel"), false, "legacy waves cannot inherit a PCM route");
  const next = stream.decode(message(routed))[0];
  assert.equal(Object.hasOwn(next, "sound"), false);
  assert.equal(Object.hasOwn(next, "at"), false);
  assert.equal(next.pcmChannel, pcmChannel);
});

test("truncated or invalid PCM operands reject every earlier event in the message", () => {
  const stream = new AudioStream();
  const operands = [[0x14, ...word(1), 1, ...word(8000), 0, 1], [0x15, 0, 1, 7, 127]];
  for (const bytes of operands) {
    for (let length = 1; length < bytes.length; length++) {
      assert.throws(() => stream.decode(message([1, 0, 60, 100], bytes.slice(0, length))), /ends inside an operation/);
    }
  }
  for (const invalid of [
    [0x14, ...word(1), 1, ...word(8000), 0, 0], [0x14, ...word(1), 2, ...word(8000), 0, 1],
    [0x15, 0, 0, 7, 127], [0x15, 0, 1, 8, 127], [0x15, 0, 1, 7, 128],
  ]) assert.throws(() => stream.decode(message([1, 0, 60, 100], invalid)), /invalid PCM/);
  assert.deepEqual(stream.decode(message([0x15, 0, 1, 7, 127])), [{ kind: "pcmControl", pcmChannel: 1, control: 7, value: 127 }]);
  assert.deepEqual(stream.decode(message(operands[0])), [], "a missing definition cannot create a source");
});
