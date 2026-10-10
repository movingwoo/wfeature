import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { AudioStream } from "./audio-stream.js";
import { playAudioEvents } from "./session.js";

const SOURCE_LIMIT = 512, WAVE_BYTES = 128 << 20;
const wave = (samples = Float32Array.of(.25, -.25, .5, -.5)) => ({ kind: "playWave", channels: 1, rate: 8000, samples, cacheable: true, sound: 7 });
const note = (extra = {}) => ({ kind: "noteOn", channel: 0, note: 60, velocity: 100, sound: 7, ...extra });
const volume = { kind: "controlChange", channel: 0, control: 7, value: 0, sound: 7 };
const repeated = (count, event) => Array.from({ length: count }, () => event);
const timed = (events, at) => [...events.map(event => ({ ...event, at })), { kind: "clock", at }];

function setup(t) {
  t.mock.method(performance, "now", () => 0);
  const audio = new PageAudio(), sources = [], buffers = [];
  const parameter = () => ({ value: 1, events: [],
    setValueAtTime(value, at) { this.events.push([value, at]); },
    linearRampToValueAtTime(value, at) { this.events.push([value, at]); },
    exponentialRampToValueAtTime(value, at) { this.events.push([value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[1] < at); } });
  const node = () => ({ connections: [], disconnects: 0,
    connect(other) { this.connections.push(other); }, disconnect() { this.disconnects++; } });
  const source = kind => {
    const value = Object.assign(node(), { kind, frequency: parameter(), starts: [], stops: [],
      start(...at) { this.starts.push(at); }, stop(...at) { this.stops.push(at); } });
    sources.push(value);
    return value;
  };
  audio.context = {
    state: "running", currentTime: 1, baseLatency: .01, sampleRate: 48000,
    createGain: () => Object.assign(node(), { gain: parameter() }),
    createChannelMerger: () => node(),
    createBiquadFilter: () => Object.assign(node(), { frequency: parameter() }),
    createBufferSource: () => source("buffer"), createOscillator: () => source("oscillator"),
    createBuffer(channels, frames, rate) {
      assert.ok(frames <= 1 << 20, "a refused huge input must not reach native allocation");
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      const buffer = { numberOfChannels: channels, length: frames, sampleRate: rate, getChannelData: channel => data[channel] };
      buffers.push(buffer);
      return buffer;
    },
  };
  audio.waveGain = node();
  audio.midiGain = node();
  const send = events => assert.equal(playAudioEvents(audio, events), true);
  return { audio, sources, buffers, send };
}

test("512 sources fit and a 513th binary wave refuses its entire batch before changing MIDI", t => {
  const { audio, sources, buffers, send } = setup(t);
  const word = value => [value >>> 24, value >>> 16 & 255, value >>> 8 & 255, value & 255];
  const stream = new AudioStream();
  const packet = (...ops) => Uint8Array.from([0x57, 0x46, 0x41, 0x32, ...ops.flat()]).buffer;
  const play = [0x11, ...word(1), 1, ...word(8000)];
  const events = stream.decode(packet([0x10, ...word(1), ...word(8), 0, 32, 0, 224, 0, 64, 0, 192], ...repeated(511, play)));
  send([note(), ...events]);
  assert.equal(audio.sources.size, SOURCE_LIMIT);
  assert.equal(sources.length, SOURCE_LIMIT);
  assert.equal(buffers.length, 1);
  const output = audio.channelOutputs.get(7)[0].gain.gain;
  assert.equal(playAudioEvents(audio, [volume, ...stream.decode(packet(play))]), false);
  assert.equal(audio.channelsFor(7)[0].volume, 100);
  assert.deepEqual(output.events, []);
  assert.equal(sources.length, SOURCE_LIMIT);
  assert.equal(buffers.length, 1);
});

for (const [name, expectedKeys, events] of [
  ["same-key melody retriggers", 1, () => repeated(512, note())],
  ["released melody tails", 0, () => repeated(512, [note(), { kind: "noteOff", channel: 0, note: 60, sound: 7 }]).flat()],
  ["percussion tails", 24, () => Array.from({ length: 512 }, (_, index) => note({ channel: 9, note: index % 128 }))],
]) {
  test(`${name} consume the source budget independently of the key map`, t => {
    const { audio, sources, send } = setup(t);
    send(timed(events(), 0));
    assert.equal(audio.voices.size, expectedKeys);
    assert.equal(audio.sources.size, SOURCE_LIMIT);
    const before = sources.map(source => source.stops.length);
    assert.equal(playAudioEvents(audio, timed([volume, note()], .001)), false);
    assert.equal(audio.channelsFor(7)[0].volume, 100);
    assert.equal(sources.length, SOURCE_LIMIT);
    assert.deepEqual(sources.map(source => source.stops.length), before);
  });
}

test("scheduled clip stops retain capacity until onended and admit exactly the released slots", t => {
  const { audio, sources, send } = setup(t);
  const event = { ...wave(new Float32Array(8000)), pcmChannel: 1 };
  send(timed(repeated(512, event), 0));
  send(timed([{ kind: "stopSound", sound: 7 }], .010));
  assert.equal(audio.sources.size, SOURCE_LIMIT);
  assert.equal(audio.retiredOutputs.size, 1);
  assert.equal(playAudioEvents(audio, timed([event], .020)), false);
  assert.equal(sources.length, SOURCE_LIMIT);
  assert.ok(sources.every(source => source.disconnects === 0));
  audio.context.currentTime = 1.111;
  for (const source of sources.slice(0, 3)) source.onended();
  assert.equal(audio.sources.size, 509);
  send(timed(repeated(3, event), .020));
  assert.equal(audio.sources.size, SOURCE_LIMIT);
  assert.equal(sources.length, 515);
  sources[0].onended();
  assert.equal(playAudioEvents(audio, timed([event], .030)), false, "a stale callback cannot release a second slot");
  audio.stopAll();
  assert.equal(audio.sources.size, 0);
  assert.equal(audio.retiredOutputs.size, 0);
});

for (const [budget, count, frames] of [["source", SOURCE_LIMIT, 4], ["byte", 128, (1 << 20) / 4]]) {
  test(`untimed owner replacement reclaims its ${budget} budget without freeing peers or double-crediting stops`, t => {
    const { audio, sources, send } = setup(t);
    const event = wave(new Float32Array(frames)), stop = { kind: "stopSound", sound: 7 };
    send([event, ...repeated(count - 1, { ...event, sound: 8 })]);
    const original = sources[0], peers = sources.slice(1);
    assert.equal(playAudioEvents(audio, [stop, stop, event, event]), false);
    assert.ok(sources.every(source => source.stops.length === 0 && source.disconnects === 0));
    send([stop, event]);
    assert.equal(original.disconnects, 1);
    assert.equal(audio.sources.size, count);
    assert.ok(peers.every(source => source.stops.length === 0 && source.disconnects === 0));
    const replacement = sources.at(-1);
    send([stop, event, stop, event]);
    assert.equal(replacement.disconnects, 1);
    assert.equal(sources.at(-2).disconnects, 1, "a stop must account for the source created earlier in this same batch");
    assert.equal(audio.sources.size, count);
    assert.ok(peers.every(source => source.stops.length === 0 && source.disconnects === 0));
    assert.equal(playAudioEvents(audio, [stop, stop, event, event]), false);
    assert.equal(audio.sources.size, count);
  });
}

test("malformed explicit owners refuse the whole batch before controls, stops or allocations", t => {
  const { audio, sources, buffers, send } = setup(t);
  const event = wave();
  send([note(), event, { ...event, sound: 0 }, { ...event, sound: 0xffffffff }]);
  const count = sources.length, bufferCount = buffers.length, output = audio.channelOutputs.get(7)[0].gain.gain;
  for (const sound of [null, -1, 0x100000000, .5, "7", NaN, {}, false]) {
    for (const invalid of [{ ...event, sound }, note({ sound }), { kind: "stopSound", sound }, { ...volume, sound }]) {
      assert.equal(playAudioEvents(audio, [volume, invalid, event]), false, `invalid owner accepted: ${JSON.stringify(invalid)}`);
      assert.equal(sources.length, count);
      assert.equal(buffers.length, bufferCount);
      assert.equal(audio.channelsFor(7)[0].volume, 100);
      assert.deepEqual(output.events, []);
      assert.ok(sources.every(source => source.stops.length === 0 && source.disconnects === 0));
    }
  }
  assert.equal(playAudioEvents(audio, [{ kind: "allOff" }, { ...event, sound: null }]), false);
  assert.equal(audio.sources.size, count);
  assert.ok(sources.every(source => source.stops.length === 0 && source.disconnects === 0));
});

test("omitted owners remain zero and uint32 owner boundaries stay independent during replacement", t => {
  const { audio, sources, send } = setup(t);
  const event = wave();
  send([{ ...event, sound: undefined }, { ...event, sound: 0x80000000 }, { ...event, sound: 0xffffffff }]);
  assert.deepEqual([...audio.sources.values()].map(voice => voice.sound), [0, 0x80000000, 0xffffffff]);
  send([{ kind: "stopSound" }, { ...event, sound: 0 }, { kind: "stopSound", sound: 0xffffffff }, { ...event, sound: 0xffffffff }]);
  assert.equal(sources[0].disconnects, 1);
  assert.equal(sources[2].disconnects, 1);
  assert.deepEqual(sources[1].stops, []);
  assert.deepEqual([...audio.sources.values()].map(voice => voice.sound), [0x80000000, 0, 0xffffffff]);
});

test("zero-velocity notes and expired drum reconstruction allocate nothing at the limit", t => {
  const { audio, sources, send } = setup(t);
  send(repeated(SOURCE_LIMIT, wave()));
  send([note({ velocity: 0 }), { kind: "noteResume", channel: 9, note: 36, velocity: 100, age: 200, sound: 7 },
    { kind: "noteResume", channel: 0, note: 60, velocity: 0, age: 50, sound: 7 }]);
  assert.equal(sources.length, SOURCE_LIMIT);
  assert.equal(playAudioEvents(audio, [{ kind: "noteResume", channel: 0, note: 60, velocity: 100, age: 50, sound: 7 }]), false);
});

test("an oversized all-off reconstruction is atomic and a full 280-source replay fits", t => {
  const { audio, sources, buffers, send } = setup(t);
  const event = wave();
  send(timed([note(), ...repeated(511, event)], 0));
  const anchor = audio.presentationAnchor;
  assert.equal(playAudioEvents(audio, [{ kind: "allOff" }, ...repeated(513, event)]), false);
  assert.equal(audio.presentationAnchor, anchor);
  assert.equal(audio.sources.size, SOURCE_LIMIT);
  assert.ok(sources.every(source => source.stops.length === 0 && source.disconnects === 0));
  assert.equal(buffers.length, 1);
  const old = [...sources];
  send([{ kind: "allOff" }, ...Array.from({ length: 24 }, (_, index) => ({ ...note({ note: index + 60 }), kind: "noteResume", age: 50 })),
    ...repeated(256, event)]);
  assert.equal(audio.sources.size, 280);
  assert.equal(audio.voices.size, 24);
  assert.ok(old.every(source => source.disconnects === 1));
  old[0].onended();
  assert.equal(audio.sources.size, 280);
});

test("every live reference spends the float-byte budget even when one immutable buffer is reused", t => {
  const { audio, sources, buffers, send } = setup(t);
  const event = wave(new Float32Array((1 << 20) / 4));
  send(repeated(128, event));
  assert.equal(sources.length, 128);
  assert.equal(buffers.length, 1);
  assert.equal(new Set(sources.map(source => source.buffer)).size, 1);
  assert.equal(playAudioEvents(audio, [volume, event]), false);
  assert.equal(audio.channelsFor(7)[0].volume, 100);
  assert.equal(sources.length, 128);
  let decodes = 0;
  t.mock.method(globalThis, "atob", () => { decodes++; throw new Error("over-budget JSON was decoded"); });
  assert.equal(playAudioEvents(audio, [volume, { ...wave(), samples: "AAA=", cacheable: false }]), false);
  assert.equal(decodes, 0, "JSON size must be checked before allocating decoded samples");
  sources[0].onended();
  send([event]);
  assert.equal(audio.sources.size, 128);
  assert.equal(buffers.length, 1);
  sources[0].onended();
  assert.equal(playAudioEvents(audio, [event]), false);
});

test("the exact float-byte boundary reaches allocation and larger declared input is refused cheaply", t => {
  const { audio, sources, send } = setup(t);
  send([wave()]); // Four floats leave exactly WAVE_BYTES - 16 bytes.
  let allocations = 0;
  t.mock.method(audio.context, "createBuffer", () => { allocations++; throw new Error("deliberate native allocation failure"); });
  const exact = { length: (WAVE_BYTES - 16) / 4 };
  assert.throws(() => audio.playWave(1, 8000, exact, 7), /deliberate native allocation failure/);
  assert.equal(allocations, 1);
  const tooLarge = { length: exact.length + 1 };
  assert.doesNotThrow(() => audio.playWave(1, 8000, tooLarge, 7));
  assert.equal(playAudioEvents(audio, [volume, wave(tooLarge)]), false);
  assert.equal(allocations, 1);
  assert.equal(sources.length, 1);
  assert.equal(audio.channelsFor(7)[0].volume, 100);
});

test("direct wave and note entry points guard capacity before allocating or stealing a voice", t => {
  const { audio, sources, buffers, send } = setup(t);
  send([note(), ...repeated(511, wave())]);
  const held = sources[0];
  audio.playWave(1, 8000, Float32Array.of(.75), 8);
  audio.noteOn(0, 60, 100, 7);
  audio.noteResume(0, 62, 100, 40, 8);
  assert.equal(sources.length, SOURCE_LIMIT);
  assert.equal(buffers.length, 1);
  assert.deepEqual(held.stops, []);
  assert.equal(audio.voices.get("7:0:60").source, held);
  held.onended();
  audio.noteOn(0, 62, 100, 8);
  assert.equal(audio.sources.size, SOURCE_LIMIT);
  assert.equal(sources.length, SOURCE_LIMIT + 1);
});

for (const kind of ["buffer", "oscillator"]) {
  test(`failed ${kind} creation does not spend a source or PCM byte reservation`, t => {
    const { audio, sources, send } = setup(t);
    const event = wave(new Float32Array(kind === "buffer" ? (1 << 20) / 4 : 4));
    send(repeated(kind === "buffer" ? 127 : 511, event));
    const method = kind === "buffer" ? "createBufferSource" : "createOscillator";
    const create = audio.context[method];
    audio.context[method] = () => { throw new Error("deliberate source creation failure"); };
    const play = () => kind === "buffer" ? audio.playWave(1, 8000, event.samples, 7, true) : audio.noteOn(0, 60, 100, 7);
    assert.throws(play, /deliberate source creation failure/);
    audio.context[method] = create;
    assert.doesNotThrow(play);
    const count = kind === "buffer" ? 128 : SOURCE_LIMIT;
    assert.equal(audio.sources.size, count);
    assert.equal(sources.length, count);
    assert.equal(playAudioEvents(audio, [kind === "buffer" ? event : note()]), false);
  });
}

test("canonical base64 padding admits an exact byte-budget fit", t => {
  for (const occupied of [0, 8]) {
    const { audio, sources, send } = setup(t);
    send(repeated(127, wave(new Float32Array((1 << 20) / 4))));
    if (occupied) send([wave(new Float32Array(occupied / 4))]);
    const frames = ((1 << 20) - occupied) / 4;
    const encoded = Buffer.alloc(frames * 2).toString("base64");
    assert.ok(encoded.endsWith(occupied ? "==" : "="));
    send([{ ...wave(), samples: encoded, cacheable: false }]);
    assert.equal(audio.sources.size, occupied ? 129 : 128);
    assert.equal(sources.at(-1).buffer.length, frames);
    assert.equal(playAudioEvents(audio, [{ ...wave(), samples: "AAA=", cacheable: false }]), false);
  }
});

test("a 280-source reconstruction fits both replay maxima and retired waves keep their byte charges", t => {
  const { audio, sources, buffers, send } = setup(t);
  const event = wave(new Float32Array((512 << 10) / 4));
  const notes = Array.from({ length: 24 }, (_, index) => ({ ...note({ note: index + 60 }), kind: "noteResume", age: 50 }));
  send(timed([...notes, ...repeated(256, event)], 0));
  assert.equal(audio.sources.size, 280);
  assert.equal(buffers.length, 1);
  assert.equal(sources[24].buffer, sources.at(-1).buffer);
  const extraFrame = wave(Float32Array.of(.5));
  assert.equal(playAudioEvents(audio, timed([volume, extraFrame], .005)), false);
  assert.equal(audio.channelsFor(7)[0].volume, 100);
  send(timed([{ kind: "stopSound", sound: 7 }], .010));
  assert.equal(playAudioEvents(audio, timed([extraFrame], .020)), false, "retiring a wave does not reclaim its samples");
  audio.context.currentTime = 1.111;
  sources[0].onended();
  assert.equal(playAudioEvents(audio, timed([extraFrame], .020)), false, "a MIDI end releases no PCM bytes");
  sources[24].onended();
  send(timed([event], .020));
  assert.equal(audio.sources.size, 279);
  assert.equal(buffers.length, 1);
  assert.equal(playAudioEvents(audio, timed([extraFrame], .030)), false);

  const json = setup(t);
  const encoded = Buffer.alloc((512 << 10) / 2).toString("base64");
  assert.ok(encoded.endsWith("=="));
  let decodes = 0;
  t.mock.method(globalThis, "atob", () => { decodes++; throw new Error("preflight decoded JSON"); });
  const replay = [...notes, ...repeated(256, { ...wave(), samples: encoded, cacheable: false })];
  assert.equal(json.audio.prepareBatch(replay), true);
  assert.equal(json.audio.prepareBatch([...replay, { ...wave(), samples: "AAA=", cacheable: false }]), false);
  assert.equal(decodes, 0);
  assert.equal(json.buffers.length, 0);
  assert.equal(json.sources.length, 0);
});

for (const kind of ["buffer", "oscillator"]) {
  test(`failed ${kind} start releases registered nodes and restores capacity`, t => {
    const { audio, sources, send } = setup(t);
    const event = wave();
    send(repeated(511, event));
    const method = kind === "buffer" ? "createBufferSource" : "createOscillator";
    const create = audio.context[method];
    audio.context[method] = () => {
      const source = create();
      source.start = () => { throw new Error("deliberate source start failure"); };
      return source;
    };
    const play = () => kind === "buffer" ? audio.playWave(1, 8000, event.samples, 7, true) : audio.noteOn(0, 60, 100, 7);
    assert.throws(play, /deliberate source start failure/);
    const failed = sources.at(-1);
    assert.equal(failed.disconnects, 1);
    assert.equal(failed.connections[0].disconnects, 1);
    assert.equal(audio.sources.size, 511);
    audio.context[method] = create;
    play();
    assert.equal(audio.sources.size, SOURCE_LIMIT);
    assert.equal(sources.length, SOURCE_LIMIT + 1, "one unsuccessful native request is distinct from live sources");
  });
}
