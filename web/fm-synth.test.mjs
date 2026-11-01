import assert from "node:assert/strict";
import { test } from "node:test";

import { FMSynth, FM_LANES, decodeVoice, fallbackVoice } from "./fm-synth.js";

const RATE = 48000;
const BLOCK = 128;

// operator lays out one operator in the canonical encoding's seventeen bytes.
const operator = ({ multiple = 1, detune = 0, totalLevel = 0, attack = 15, decay = 0, sustainRate = 0, release = 7,
  sustainLevel = 0, keyScaleLevel = 0, keyScaleRate = 0, wave = 0, feedback = 0, tremolo = 0, tremoloDepth = 0,
  vibrato = 0, vibratoDepth = 0, ignoreKeyOff = 0 } = {}) =>
  [multiple, detune, totalLevel, attack, decay, sustainRate, release, sustainLevel, keyScaleLevel, keyScaleRate,
    wave, feedback, tremolo, tremoloDepth, vibrato, vibratoDepth, ignoreKeyOff];
const encode = (algorithm, operators, { lfo = 0, basicOctave = 1 } = {}) =>
  Uint8Array.from([1, algorithm, lfo, basicOctave, ...operators.flat()]);
// A plain sine: two parallel operators, the second at the quietest level
// with the same envelope, so it ends when the first does.
const sine = (carrier = {}) => encode(1, [operator(carrier), operator({ ...carrier, totalLevel: 63 })]);

const lanes = () => Array.from({ length: FM_LANES }, () => new Float32Array(BLOCK));
const render = (synth, seconds, from = 0, lane = 0) => {
  const output = [];
  const buffers = lanes();
  const blocks = Math.round(seconds * RATE / BLOCK);
  for (let block = 0; block < blocks; block++) {
    synth.render(buffers, BLOCK, from + block * BLOCK / RATE);
    output.push(...buffers[lane]);
  }
  return Float32Array.from(output);
};
const crossings = samples => {
  let count = 0;
  for (let index = 1; index < samples.length; index++) if (samples[index - 1] < 0 !== samples[index] < 0) count++;
  return count;
};
const peak = samples => samples.reduce((max, value) => Math.max(max, Math.abs(value)), 0);
const synth = () => {
  const result = new FMSynth(RATE);
  result.ended = [];
  result.onEnded = id => result.ended.push(id);
  return result;
};
const define = (target, data, slot = 1, sound = 5) => target.handle({ op: "define", sound, slot, data });
const on = (target, fields = {}) => target.handle({ op: "on", id: 1, at: 0, lane: 0, sound: 5, slot: 1, pitch: 69, velocity: 127, ...fields });

test("the canonical voice encoding is read and anything out of range refused", () => {
  const two = encode(0, [operator({ multiple: 2, totalLevel: 20, wave: 16 }), operator({ release: 4 })], { lfo: 3, basicOctave: 0 });
  const voice = decodeVoice(two);
  assert.equal(voice.algorithm, 0);
  assert.equal(voice.lfo, 3);
  assert.equal(voice.basicOctave, 0);
  assert.equal(voice.operators.length, 2);
  assert.equal(voice.operators[0].multiple, 2);
  assert.equal(voice.operators[0].wave, 16);
  assert.equal(voice.operators[1].release, 4);
  assert.equal(decodeVoice(encode(5, [operator(), operator(), operator(), operator()])).operators.length, 4);
  for (let position = 0; position < two.length; position++) {
    const bad = Uint8Array.from(two);
    bad[position] = 255;
    assert.equal(decodeVoice(bad), null, `byte ${position} out of range was read`);
  }
  assert.equal(decodeVoice(two.slice(0, -1)), null);
  assert.equal(decodeVoice(Uint8Array.from([...two, 0])), null);
  assert.equal(decodeVoice(encode(2, [operator(), operator()])), null, "a four-operator algorithm needs four operators");
  assert.equal(decodeVoice(null), null);
});

test("a sine voice sounds its pitch at its level and the lane it was given", () => {
  const target = synth();
  define(target, sine());
  on(target, { lane: 3 });
  const samples = render(target, 1, 0, 3);
  assert.ok(Math.abs(crossings(samples) - 880) <= 2, `crossings ${crossings(samples)}`);
  assert.ok(Math.abs(peak(samples) - 0.25) < 0.01, `peak ${peak(samples)}`);
  const other = render(synth(), 0.01, 0, 0);
  assert.equal(peak(other), 0);
});

test("basic octave, multiple and lane bend move the pitch", () => {
  for (const [voice, ratio, expected] of [
    [encode(1, [operator(), operator({ totalLevel: 63 })], { basicOctave: 0 }), 1, 1760],
    [sine({ multiple: 2 }), 1, 1760],
    [sine({ multiple: 0 }), 1, 440],
    [sine(), 2, 1760],
  ]) {
    const target = synth();
    define(target, voice);
    target.handle({ op: "lane", lane: 0, ratio, depth: 0 });
    on(target);
    const count = crossings(render(target, 1));
    assert.ok(Math.abs(count - expected) <= 3, `${count} crossings, want ${expected}`);
  }
});

test("a held voice sustains, releases at key-off and reports its end once", () => {
  const target = synth();
  define(target, sine({ release: 10 }));
  on(target);
  target.handle({ op: "off", id: 1, at: 0.5 });
  const held = render(target, 0.5);
  assert.ok(peak(held.subarray(held.length - 4800)) > 0.24, "a sustained voice holds its level");
  const released = render(target, 1, 0.5);
  assert.ok(peak(released.subarray(0, 480)) > 0.1, "the release starts from the held level");
  assert.ok(peak(released.subarray(released.length - 4800)) === 0, "the release reaches silence");
  assert.deepEqual(target.ended, [1]);
});

test("a percussive voice decays without a key-off and ends by itself", () => {
  const target = synth();
  define(target, sine({ decay: 9, sustainLevel: 4, sustainRate: 9 }));
  on(target);
  const samples = render(target, 2);
  assert.ok(peak(samples.subarray(0, 480)) > 0.2);
  assert.equal(peak(samples.subarray(samples.length - 4800)), 0);
  assert.deepEqual(target.ended, [1]);
});

test("an aged note continues its envelopes where rendering them would have left them", () => {
  for (const voice of [
    encode(0, [operator({ attack: 9, decay: 6, sustainLevel: 5, sustainRate: 3, totalLevel: 20 }), operator({ attack: 8, decay: 4, sustainLevel: 3 })]),
    encode(4, [operator({ attack: 7 }), operator({ attack: 12, decay: 7, sustainLevel: 2 }), operator({ decay: 3, sustainRate: 2, sustainLevel: 6 }), operator({ attack: 10, decay: 5, sustainLevel: 4 })]),
  ]) {
    const rendered = synth(), aged = synth();
    define(rendered, voice);
    define(aged, voice);
    on(rendered);
    render(rendered, 0.3);
    on(aged, { age: 0.3, at: 0.3 });
    render(aged, BLOCK / RATE, 0.3);
    render(rendered, BLOCK / RATE, 0.3);
    const count = decodeVoice(voice).operators.length;
    for (let index = 0; index < count; index++) {
      const left = rendered.voices.find(voice => voice.active).operators[index];
      const right = aged.voices.find(voice => voice.active).operators[index];
      assert.equal(left.stage, right.stage, `operator ${index} stage`);
      assert.ok(Math.abs(left.attenuation - right.attenuation) < 0.1, `operator ${index}: ${left.attenuation} vs ${right.attenuation}`);
    }
  }
});

test("a cut fades to silence and a cut before the start cancels the note", () => {
  const target = synth();
  define(target, sine());
  on(target);
  target.handle({ op: "cut", id: 1, at: 0.1, fade: 0.01 });
  const samples = render(target, 0.2);
  assert.ok(peak(samples.subarray(4800 - 480, 4800)) > 0.2);
  assert.equal(peak(samples.subarray(4800 + 500)), 0);
  assert.deepEqual(target.ended, [1]);

  const cancelled = synth();
  define(cancelled, sine());
  on(cancelled, { at: 0.2 });
  cancelled.handle({ op: "cut", id: 1, at: 0.1 });
  assert.deepEqual(cancelled.ended, [1], "the page learns the note will never sound");
  assert.equal(peak(render(cancelled, 0.4)), 0);
});

test("a released lane silences its voices and forgets what was queued for it", () => {
  const target = synth();
  define(target, sine());
  on(target, { id: 1, lane: 2 });
  on(target, { id: 2, lane: 2, at: 0.5 });
  on(target, { id: 3, lane: 4 });
  render(target, 0.1, 0, 2);
  target.handle({ op: "release", lane: 2 });
  assert.deepEqual(target.ended.sort(), [1, 2]);
  assert.equal(peak(render(target, 0.6, 0.1, 2)), 0);
  assert.ok(peak(render(target, 0.1, 0.7, 4)) > 0.2, "another lane plays on");
});

test("a note resolves its voice when posted, so a later forget cannot change it", () => {
  const target = synth();
  define(target, sine({ multiple: 2 }));
  on(target, { at: 0.1 });
  target.handle({ op: "forget", sound: 5 });
  const count = crossings(render(target, 1.1).subarray(4800));
  assert.ok(Math.abs(count - 1760) <= 3, `${count} crossings: the defined voice should still play`);
  on(target, { id: 2, at: 1.1, program: 0 });
  assert.equal(target.queue.at(-1).patch, fallbackVoice(0), "after the forget the stand-in plays");
});

test("feedback, modulation and every algorithm render finite, bounded sound", () => {
  for (let algorithm = 0; algorithm < 8; algorithm++) {
    const count = algorithm < 2 ? 2 : 4;
    const operators = Array.from({ length: count }, (_, index) => operator({ multiple: index + 1, feedback: 7, wave: index * 3, totalLevel: index === 0 ? 0 : 10 }));
    const target = synth();
    define(target, encode(algorithm, operators));
    on(target);
    const samples = render(target, 0.2);
    assert.ok(samples.every(Number.isFinite), `algorithm ${algorithm} produced a non-finite sample`);
    assert.ok(peak(samples) > 0.01 && peak(samples) <= 0.25 * count + 1e-6, `algorithm ${algorithm} peak ${peak(samples)}`);
  }
});

test("modulation brightens a carrier without moving its pitch", () => {
  const plain = synth(), modulated = synth();
  define(plain, encode(0, [operator({ totalLevel: 63 }), operator()]));
  define(modulated, encode(0, [operator({ totalLevel: 12, multiple: 1 }), operator()]));
  on(plain);
  on(modulated);
  const left = render(plain, 0.5), right = render(modulated, 0.5);
  assert.ok(Math.abs(crossings(left) - 440) <= 2);
  const energy = samples => samples.reduce((sum, value, index) => index ? sum + (value - samples[index - 1]) ** 2 : sum, 0);
  assert.ok(energy(right) > energy(left) * 2, "a modulated carrier has more high-frequency energy");
});

test("stand-in voices exist for every program and drum key and they sound", () => {
  for (let program = 0; program < 128; program++) {
    const voice = fallbackVoice(program);
    assert.ok(voice && voice.operators.length === (voice.algorithm < 2 ? 2 : 4), `program ${program}`);
  }
  for (let key = 0; key < 128; key++) assert.ok(fallbackVoice(0, key).drum, `drum key ${key}`);
  for (const [program, drumKey] of [[0, undefined], [40, undefined], [120, undefined], [0, 36], [0, 38], [0, 42], [0, 49]]) {
    const target = synth();
    target.handle({ op: "on", id: 1, at: 0, lane: 0, sound: 5, slot: 0, pitch: drumKey ?? 60, velocity: 100, program, drumKey });
    const samples = render(target, 0.3);
    assert.ok(samples.every(Number.isFinite));
    assert.ok(peak(samples) > 0.01, `program ${program} drum ${drumKey} is silent`);
  }
});

test("the voice pool steals the quietest voice and reports it ended", () => {
  const target = synth();
  define(target, sine());
  for (let id = 1; id <= 64; id++) on(target, { id, pitch: 40 + (id % 40) });
  render(target, BLOCK / RATE);
  target.handle({ op: "off", id: 7, at: BLOCK / RATE });
  render(target, 0.2, BLOCK / RATE);
  on(target, { id: 65, at: 0.3 });
  render(target, BLOCK / RATE, 0.3);
  assert.ok(target.ended.includes(7), "the released voice is the one stolen");
  assert.equal(target.voices.filter(voice => voice.active).length, 64);
});

test("percussion keeps its pitch while a lane bends", () => {
  const target = synth();
  define(target, sine());
  target.handle({ op: "on", id: 1, at: 0, lane: 0, sound: 5, slot: 1, pitch: 69, velocity: 127, drumKey: 69 });
  target.handle({ op: "bend", lane: 0, at: 0, ratio: 2 });
  assert.ok(Math.abs(crossings(render(target, 1)) - 880) <= 2);
});

test("an operator stepping more than a cycle a sample keeps its phase within one cycle", () => {
  const target = synth();
  // Pitch 108 with a fifteen-times operator is about 63 kHz.
  define(target, encode(1, [operator({ multiple: 15 }), operator({ totalLevel: 63 })]));
  on(target, { pitch: 108 });
  const samples = render(target, 0.05);
  assert.ok(samples.every(Number.isFinite));
  const [operator0] = target.voices.find(voice => voice.active).operators;
  assert.ok(operator0.step > 1, `step ${operator0.step}`);
  assert.ok(operator0.phase >= 0 && operator0.phase < 1, `phase ${operator0.phase}`);
});

test("velocity scales a voice by the square root of its fraction", () => {
  for (const velocity of [127, 64, 16]) {
    const target = synth();
    define(target, sine());
    on(target, { velocity });
    const level = peak(render(target, 0.2));
    const want = 0.25 * Math.sqrt(velocity / 127);
    assert.ok(Math.abs(level - want) < 0.005, `velocity ${velocity}: peak ${level}, want ${want}`);
  }
});

// Attack 3 at this pitch is effective rate 14: half a second to full.
const ATTACK_SECONDS = 5.652 / 2 ** (14 / 4);
const amplitude = operator => 10 ** (-operator.attenuation / 20);

test("an attack raises the amplitude in a straight line over its rate's time", () => {
  const target = synth();
  define(target, sine({ attack: 3 }));
  on(target);
  let elapsed = 0;
  for (const fraction of [0.25, 0.5, 0.75]) {
    const until = fraction * ATTACK_SECONDS;
    render(target, until - elapsed, elapsed);
    elapsed = Math.round(until * RATE / BLOCK) * BLOCK / RATE;
    const [carrier] = target.voices.find(voice => voice.active).operators;
    assert.equal(carrier.stage, 1, `attack still running at ${fraction}`);
    const want = elapsed / ATTACK_SECONDS;
    assert.ok(Math.abs(amplitude(carrier) - want) < 0.01, `at ${fraction}: amplitude ${amplitude(carrier)}, want ${want}`);
  }
  render(target, ATTACK_SECONDS, elapsed);
  const [carrier] = target.voices.find(voice => voice.active).operators;
  assert.equal(carrier.attenuation, 0, "the attack ends at full level");
});

test("a note resumed during its attack continues the same straight line", () => {
  const rendered = synth(), aged = synth();
  define(rendered, sine({ attack: 3 }));
  define(aged, sine({ attack: 3 }));
  on(rendered);
  render(rendered, 0.2);
  on(aged, { age: 0.2, at: 0.2 });
  render(aged, BLOCK / RATE, 0.2);
  render(rendered, BLOCK / RATE, 0.2);
  const [left] = rendered.voices.find(voice => voice.active).operators;
  const [right] = aged.voices.find(voice => voice.active).operators;
  assert.equal(left.stage, 1);
  assert.equal(right.stage, 1);
  assert.ok(Math.abs(amplitude(left) - amplitude(right)) < 0.002, `${amplitude(left)} vs ${amplitude(right)}`);
});
