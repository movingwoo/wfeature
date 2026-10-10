import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { AudioStream } from "./audio-stream.js";
import { playAudioEvents } from "./session.js";

const CACHE_BYTES = 16 * 1024 * 1024;
const word = value => [value >>> 24, (value >>> 16) & 255, (value >>> 8) & 255, value & 255];
const join = (...parts) => {
  const bytes = new Uint8Array(parts.reduce((size, part) => size + part.length, 0));
  let offset = 0;
  for (const part of parts) { bytes.set(part, offset); offset += part.length; }
  return bytes;
};
const message = (...operations) => join([0x57, 0x46, 0x41, 0x32], ...operations).buffer;
const pcm = values => {
  const bytes = new Uint8Array(values.length * 2);
  const view = new DataView(bytes.buffer);
  for (let index = 0; index < values.length; index++) view.setInt16(index * 2, values[index], true);
  return bytes;
};
const define = (id, values) => {
  const bytes = pcm(values);
  return join([0x10, ...word(id), ...word(bytes.length)], bytes);
};
const select = sound => [0x07, ...word(sound)];
const wave = (id, channels = 1, rate = 8000) => [0x11, ...word(id), channels, ...word(rate)];
const seconds = value => {
  const bytes = new Uint8Array(8);
  new DataView(bytes.buffer).setFloat64(0, value);
  return bytes;
};
const timing = at => join([0x0b, 1], seconds(at));
const frontier = at => join([0x0c], seconds(at));
const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);

const setup = t => {
  t.mock.method(performance, "now", () => 0);
  const buffers = [], sources = [];
  const parameter = () => ({
    value: 1, events: [],
    setValueAtTime(value, at) { this.events.push([value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[1] < at); },
  });
  const node = () => ({
    connections: [], disconnects: 0,
    connect(target) { this.connections.push(target); },
    disconnect() { this.disconnects++; },
  });
  const context = () => ({
    state: "running", currentTime: 1, baseLatency: 0.01, sampleRate: 48000,
    createGain: () => ({ ...node(), gain: parameter() }),
    createBuffer(channels, frames, rate) {
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      const buffer = { numberOfChannels: channels, length: frames, sampleRate: rate,
        getChannelData: channel => data[channel] };
      buffers.push(buffer);
      return buffer;
    },
    createBufferSource() {
      const source = { ...node(), starts: [], stops: [],
        start(...args) { this.starts.push(args); },
        stop(...args) { this.stops.push(args); } };
      sources.push(source);
      return source;
    },
  });
  const audio = new PageAudio();
  audio.context = context();
  audio.waveGain = node();
  audio.midiGain = node();
  const stream = new AudioStream();
  const send = (...operations) => {
    const events = stream.decode(message(...operations));
    assert.equal(playAudioEvents(audio, events), true);
    return events;
  };
  return { audio, buffers, sources, stream, send, context };
};

test("512 immutable protocol 2 plays share one buffer and retain independent sources, owners and times", t => {
  const { audio, buffers, sources, send } = setup(t);
  send(define(1, [-32768, 0, 16384, 32767]), frontier(0));
  let last;
  for (let index = 0; index < 512; index++) {
    const sound = [0, 11, 22][index % 3], at = index / 10000;
    last = send(select(sound), timing(at), wave(1), frontier(at));
  }
  assert.equal(sources.length, 512, "every playback needs its own single-use source");
  assert.equal(buffers.length, 1, "repeated decoded definitions must avoid repeated native allocation and copying");
  assert.equal(last[0].cacheable, true, "the binary decoder identifies its immutable sample storage");
  assert.deepEqual([...buffers[0].getChannelData(0)], [-1, 0, 0.5, 32767 / 32768]);
  assert.equal(new Set(sources.map(source => source.connections[0])).size, 512, "source gain and cursor state cannot be shared");
  for (const [index, source] of sources.entries()) {
    assert.equal(source.buffer, buffers[0]);
    assert.equal(source.starts.length, 1);
    close(source.starts[0][0], 1.1 + index / 10000);
    assert.equal(source.starts[0].length, 1, "a replay starts from its own first frame");
    const voice = audio.sources.get(source);
    assert.equal(voice.sound, [0, 11, 22][index % 3]);
    close(voice.end - voice.sourceStart, 4 / 8000);
  }
  audio.stopSound(11);
  for (const [index, source] of sources.entries()) {
    assert.equal(source.stops.length, index % 3 === 1 ? 1 : 0, "stopping one owner cannot stop another user's shared payload");
  }
  assert.equal(audio.waveBuffers.size, 1);
  assert.equal(audio.waveBufferBytes, 16);
});

test("the same samples keep only their latest channel and rate variant", t => {
  const { audio, buffers, sources, send } = setup(t);
  send(define(1, [8192, -8192, 16384, -16384, 24576, -24576]), wave(1));
  send(wave(1));
  assert.equal(buffers.length, 1);
  send(wave(1, 2));
  assert.equal(buffers.length, 2);
  assert.equal(sources.at(-1).buffer.numberOfChannels, 2);
  assert.equal(sources.at(-1).buffer.length, 3);
  assert.deepEqual([...sources.at(-1).buffer.getChannelData(0)], [0.25, 0.5, 0.75]);
  assert.deepEqual([...sources.at(-1).buffer.getChannelData(1)], [-0.25, -0.5, -0.75]);
  send(wave(1, 2));
  assert.equal(buffers.length, 2);
  send(wave(1, 2, 16000));
  assert.equal(buffers.length, 3);
  assert.equal(sources.at(-1).buffer.sampleRate, 16000);
  close(audio.sources.get(sources.at(-1)).end - audio.sources.get(sources.at(-1)).sourceStart, 3 / 16000);
  send(wave(1));
  assert.equal(buffers.length, 4, "changing back cannot retain every historic format variant");
  assert.equal(audio.waveBuffers.size, 1);
  assert.equal(audio.waveBufferBytes, 24);
  assert.deepEqual([...buffers[0].getChannelData(0)], [0.25, -0.25, 0.5, -0.5, 0.75, -0.75], "a new format must not modify an already playing buffer");
});

test("clip stops retain the payload while an all-off timeline reset drops it", t => {
  const { audio, buffers, sources, send } = setup(t);
  send(define(1, [8192, -8192]), select(11), wave(1));
  const first = sources[0];
  send(select(11), [0x08], wave(1));
  assert.equal(buffers.length, 1);
  assert.equal(first.disconnects, 1);
  assert.notEqual(sources[1], first);
  send([0x06]);
  assert.equal(audio.sources.size, 0);
  assert.equal(audio.waveBuffers.size, 0);
  assert.equal(audio.waveBufferBytes, 0);
  send(wave(1));
  assert.equal(buffers.length, 2, "even a still-defined sample must acquire a fresh buffer after quick load or reset");
  assert.notEqual(sources[2].buffer, first.buffer);
  first.onended();
  assert.equal(audio.sources.size, 1, "old end callbacks cannot erase new playback");
  assert.equal(audio.waveBuffers.size, 1);
});

test("forgotten or replaced definitions and equal-content arrays never alias stale buffers", t => {
  const { buffers, sources, send } = setup(t);
  send(define(1, [8192, -8192]), wave(1));
  const first = sources[0].buffer;
  send([0x13], wave(1));
  assert.equal(sources.length, 1, "a forgotten definition cannot replay a cached payload");
  send(define(1, [24576, -24576]), wave(1));
  const replacement = sources.at(-1).buffer;
  assert.equal(buffers.length, 2);
  assert.notEqual(replacement, first);
  assert.deepEqual([...replacement.getChannelData(0)], [0.75, -0.75]);
  send(define(2, [24576, -24576]), wave(2));
  assert.equal(buffers.length, 3, "equal content in separately decoded arrays does not establish immutable identity");
  send(wave(1));
  assert.equal(buffers.length, 3);
  assert.equal(sources.at(-1).buffer, replacement);
  send(define(1, [24576, -24576]), wave(1));
  assert.equal(buffers.length, 4, "reusing a definition number cannot reuse its previous sample identity");
});

test("mutable direct and ordinary event samples remain fresh and JSON is not retained", t => {
  const { audio, buffers } = setup(t);
  const samples = Float32Array.of(0.25, -0.5);
  audio.playWave(1, 8000, samples);
  samples[0] = 0.75;
  audio.playWave(1, 8000, samples);
  assert.equal(buffers.length, 2);
  assert.equal(buffers[0].getChannelData(0)[0], 0.25);
  assert.equal(buffers[1].getChannelData(0)[0], 0.75);
  const event = { kind: "playWave", channels: 1, rate: 8000, samples };
  playAudioEvents(audio, [event]);
  samples[0] = -0.25;
  playAudioEvents(audio, [event]);
  assert.equal(buffers[2].getChannelData(0)[0], 0.75);
  assert.equal(buffers[3].getChannelData(0)[0], -0.25);
  const encoded = { ...event, samples: Buffer.from(pcm([8192, -8192])).toString("base64") };
  playAudioEvents(audio, [encoded]);
  playAudioEvents(audio, [encoded]);
  assert.equal(buffers.length, 6);
  assert.equal(audio.waveBuffers.size, 0);
  assert.equal(audio.waveBufferBytes, 0);
});

test("entry pressure evicts the least recently played payload without changing live sources", t => {
  const { audio, buffers, sources } = setup(t);
  const samples = Array.from({ length: 257 }, (_, index) => Float32Array.of(index / 512));
  for (const data of samples.slice(0, 256)) audio.playWave(1, 8000, data, 7, true);
  const first = sources[0].buffer, second = sources[1].buffer;
  audio.playWave(1, 8000, samples[0], 7, true);
  assert.equal(buffers.length, 256);
  audio.playWave(1, 8000, samples[256], 7, true);
  assert.equal(audio.waveBuffers.size, 256);
  assert.equal(audio.waveBufferBytes, 256 * 4);
  audio.playWave(1, 8000, samples[0], 7, true);
  assert.equal(sources.at(-1).buffer, first, "a warm payload must survive an older untouched entry");
  assert.equal(buffers.length, 257);
  audio.playWave(1, 8000, samples[1], 7, true);
  assert.equal(buffers.length, 258);
  assert.notEqual(sources.at(-1).buffer, second);
  assert.equal(sources[1].buffer, second);
  assert.deepEqual(sources[1].stops, [], "cache eviction must not stop a source already using that buffer");
  assert.equal(second.getChannelData(0)[0], 1 / 512);
  assert.equal(audio.waveBuffers.size, 256);
});

test("native float payload pressure obeys the byte ceiling and promotes warm entries", t => {
  const { audio, buffers, sources } = setup(t);
  const blockBytes = 1024 * 1024;
  const samples = Array.from({ length: 17 }, (_, index) => {
    const data = new Float32Array(blockBytes / 4);
    data[0] = index / 32;
    return data;
  });
  for (const data of samples.slice(0, 16)) audio.playWave(1, 8000, data, 7, true);
  const first = sources[0].buffer;
  audio.playWave(1, 8000, samples[0], 7, true);
  assert.equal(buffers.length, 16);
  audio.playWave(1, 8000, samples[16], 7, true);
  assert.equal(audio.waveBuffers.size, 16);
  assert.equal(audio.waveBufferBytes, CACHE_BYTES);
  audio.playWave(1, 8000, samples[0], 7, true);
  assert.equal(sources.at(-1).buffer, first);
  assert.equal(buffers.length, 17);
  audio.playWave(1, 8000, samples[1], 7, true);
  assert.equal(buffers.length, 18, "the oldest cold payload must be reconstructed after byte eviction");
  assert.equal(sources.at(-1).buffer.getChannelData(0)[0], 1 / 32);
  assert.equal(audio.waveBufferBytes, CACHE_BYTES);
  audio.stopAll();
  assert.equal(audio.waveBuffers.size, 0);
  assert.equal(audio.waveBufferBytes, 0);
});

test("an oversized waveform still plays without entering or flushing the bounded cache", t => {
  const { audio, buffers, sources } = setup(t);
  const small = Float32Array.of(0.5);
  audio.playWave(1, 8000, small, 7, true);
  const retained = sources[0].buffer;
  const large = new Float32Array(CACHE_BYTES / 4 + 1);
  large[0] = 0.25;
  large[large.length - 1] = -0.75;
  audio.playWave(1, 8000, large, 7, true);
  audio.playWave(1, 8000, large, 7, true);
  assert.equal(buffers.length, 3);
  for (const source of sources.slice(1)) {
    assert.equal(source.starts.length, 1);
    assert.equal(source.buffer.length, large.length);
    assert.equal(source.buffer.getChannelData(0)[0], 0.25);
    assert.equal(source.buffer.getChannelData(0).at(-1), -0.75);
  }
  assert.equal(audio.waveBuffers.size, 1);
  assert.equal(audio.waveBufferBytes, 4);
  audio.playWave(1, 8000, small, 7, true);
  assert.equal(buffers.length, 3);
  assert.equal(sources.at(-1).buffer, retained);
});

test("a replacement output context builds its own native buffer", t => {
  const { audio, buffers, sources, context } = setup(t);
  const samples = Float32Array.of(0.25, -0.25);
  audio.playWave(1, 8000, samples, 7, true);
  audio.stopSound(7);
  audio.context = context();
  audio.playWave(1, 8000, samples, 7, true);
  assert.equal(buffers.length, 2);
  assert.notEqual(sources[0].buffer, sources[1].buffer);
  assert.equal(audio.waveBuffers.size, 1);
  assert.equal(audio.waveBufferBytes, 8);
});
