import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { playAudioEvents } from "./session.js";

const BILLION = 1_000_000_000;
const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-14, `${actual} != ${expected}`);
const invalidPhases = [-1, null, NaN, Infinity, -Infinity, .5, BILLION, 0xffffffff, 0x100000000, "1", false, {}];
const wave = (samples, framePhase) => ({ kind: "playWave", sound: 7, pcmChannel: 1,
  channels: 1, rate: 8000, samples, cacheable: true, framePhase });

function setup(t) {
  t.mock.method(performance, "now", () => 0);
  const audio = new PageAudio(), sources = [], buffers = [], nodes = [];
  const parameter = () => ({ value: 1, events: [],
    setValueAtTime(value, at) { this.events.push(["set", value, at]); },
    linearRampToValueAtTime(value, at) { this.events.push(["linear", value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[2] < at); } });
  const node = () => {
    const result = { connections: [], disconnects: 0,
      connect(target) { this.connections.push(target); }, disconnect() { this.disconnects++; } };
    nodes.push(result);
    return result;
  };
  audio.context = {
    state: "running", currentTime: 1, baseLatency: .01, sampleRate: 48000,
    createGain: () => Object.assign(node(), { gain: parameter() }),
    createChannelMerger: () => node(),
    createBufferSource() {
      const source = Object.assign(node(), { starts: [], stops: [],
        start(...args) { this.starts.push(args); }, stop(...args) { this.stops.push(args); } });
      sources.push(source);
      return source;
    },
    createBuffer(channels, frames, rate) {
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      const buffer = { numberOfChannels: channels, length: frames, sampleRate: rate,
        getChannelData: channel => data[channel] };
      buffers.push(buffer);
      return buffer;
    },
  };
  audio.waveGain = node();
  audio.midiGain = node();
  return { audio, sources, buffers, nodes };
}

test("fractional PCM starts share immutable buffers but retain independent offsets and full byte charges", t => {
  const { audio, sources, buffers } = setup(t);
  const samples = Float32Array.of(.25, -.5, .75, -.25);
  assert.equal(audio.prepareBatch([{ kind: "clock", at: 0 }]), true);
  const phases = [0, 1, 500_000_000, BILLION - 1];
  for (const [index, phase] of phases.entries()) {
    audio.playTimed(0, () => audio.playWave(1, 8000, samples, index < 2 ? 7 : 8, true, index % 2 + 1, phase));
  }
  assert.equal(buffers.length, 1);
  assert.equal(new Set(sources.map(source => source.buffer)).size, 1);
  assert.deepEqual([...samples], [.25, -.5, .75, -.25]);
  assert.deepEqual([...buffers[0].getChannelData(0)], [...samples]);
  assert.equal(audio.liveWaveBytes, 4 * samples.byteLength);
  for (const [index, phase] of phases.entries()) {
    const offset = phase / (BILLION * 8000), source = sources[index];
    assert.deepEqual(source.starts, [phase ? [1.1, offset] : [1.1]]);
    close(audio.sources.get(source).end, 1.1 + samples.length / 8000 - offset);
  }
  assert.equal(new Set([...audio.sources.values()].map(voice => voice.pcm)).size, 4);
  sources[1].onended();
  sources[1].onended();
  assert.equal(audio.liveWaveBytes, 3 * samples.byteLength);
  assert.equal(audio.sources.size, 3);
});

test("phase offsets use each wave's sample rate and frames rather than context rate or channel count", t => {
  const { audio, sources } = setup(t);
  const samples = Float32Array.of(.5, -.5, .25, -.25, .75, -.75);
  const phase = 234_567_891;
  for (const rate of [8000, 11025, 22050, 44100, 48000]) {
    for (const channels of [1, 2]) {
      audio.playWave(channels, rate, samples, 7, true, 0, phase);
      const source = sources.at(-1), offset = phase / (BILLION * rate);
      assert.deepEqual(source.starts, [[1.01, offset]]);
      close(audio.sources.get(source).end, 1.01 + samples.length / channels / rate - offset);
    }
  }
});

test("omitted, undefined and zero phase preserve the existing one-argument start", t => {
  const { audio, sources } = setup(t);
  const samples = Float32Array.of(.25, -.25);
  audio.playWave(1, 8000, samples);
  audio.playWave(1, 8000, samples, 7, true, 0, undefined);
  audio.playWave(1, 8000, samples, 7, true, 0, 0);
  assert.deepEqual(sources.map(source => source.starts), [[[1.01]], [[1.01]], [[1.01]]]);
  for (const framePhase of [undefined, 0]) assert.equal(audio.prepareBatch([wave(samples, framePhase)]), true);
});

test("invalid phases refuse direct calls before samples, context, group or source allocation", t => {
  const { audio, sources, buffers, nodes } = setup(t);
  let ensures = 0;
  t.mock.method(audio, "ensure", () => { ensures++; throw new Error("invalid phase reached context allocation"); });
  const nodeCount = nodes.length;
  for (const phase of invalidPhases) {
    for (const samples of [Float32Array.of(.25), new Float32Array(), undefined]) {
      assert.equal(audio.playWave(1, 8000, samples, 7, true, 1, phase), false);
    }
  }
  assert.equal(ensures, 0);
  assert.equal(sources.length, 0);
  assert.equal(buffers.length, 0);
  assert.equal(nodes.length, nodeCount);
  assert.equal(audio.pcmChannelCount, 0);
  assert.equal(audio.liveWaveBytes, 0);
});

test("invalid phases reject whole batches before controllers, all-off resets or JSON decoding", t => {
  const { audio, sources, buffers, nodes } = setup(t);
  const samples = Float32Array.of(.25, -.25);
  assert.equal(playAudioEvents(audio, [{ ...wave(samples, 0), at: 0 }, { kind: "clock", at: 0 }]), true);
  const source = sources[0], output = audio.pcmOutputs.get(7).get(1);
  const nodeCount = nodes.length, anchor = audio.presentationAnchor;
  let decodes = 0;
  t.mock.method(globalThis, "atob", () => { decodes++; throw new Error("invalid phase reached JSON decoding"); });
  const changes = [
    { kind: "controlChange", sound: 7, channel: 0, control: 7, value: 0 },
    { kind: "pcmControl", sound: 7, pcmChannel: 1, control: 7, value: 0 },
  ];
  for (const phase of invalidPhases) {
    for (const payload of [samples, new Float32Array(), undefined, "AAA="]) {
      for (const reset of [[], [{ kind: "allOff" }]]) {
        assert.equal(playAudioEvents(audio, [...reset, ...changes, wave(payload, phase)]), false);
        assert.equal(audio.channelsFor(7)[0].volume, 100);
        assert.equal(audio.pcmChannels.get(7).get(1).volume, 127);
        assert.deepEqual(output.left.gain.events, []);
        assert.deepEqual(output.right.gain.events, []);
        assert.deepEqual(source.stops, []);
        assert.equal(source.disconnects, 0);
        assert.equal(audio.presentationAnchor, anchor);
        assert.equal(audio.liveWaveBytes, samples.byteLength);
        assert.equal(audio.sources.size, 1);
        assert.equal(sources.length, 1);
        assert.equal(buffers.length, 1);
        assert.equal(nodes.length, nodeCount);
      }
    }
  }
  assert.equal(decodes, 0);
});

test("a phased start failure releases only its own source and byte reservation", t => {
  const { audio, sources, buffers } = setup(t);
  const samples = Float32Array.of(.25, -.25), phase = 500_000_000;
  audio.playWave(1, 8000, samples, 7, true, 1, phase);
  const peer = sources[0], output = audio.sources.get(peer).pcm;
  const create = audio.context.createBufferSource;
  audio.context.createBufferSource = () => {
    const source = create();
    source.start = (...args) => { source.starts.push(args); throw new Error("deliberate phased start failure"); };
    return source;
  };
  assert.throws(() => audio.playWave(1, 8000, samples, 7, true, 1, phase), /deliberate phased start failure/);
  audio.context.createBufferSource = create;
  const failed = sources[1];
  assert.deepEqual(failed.starts, [[1.01, phase / (BILLION * 8000)]]);
  assert.equal(failed.disconnects, 1);
  assert.equal(failed.connections[0].disconnects, 1);
  assert.equal(peer.disconnects, 0);
  assert.equal(output.references, 1);
  assert.equal(output.left.disconnects, 0);
  assert.equal(audio.sources.size, 1);
  assert.equal(audio.liveWaveBytes, samples.byteLength);
  failed.onended();
  assert.equal(audio.liveWaveBytes, samples.byteLength);
  audio.playWave(1, 8000, samples, 7, true, 1, phase);
  assert.equal(buffers.length, 1);
  assert.equal(output.references, 2);
  sources[2].onended();
  peer.onended();
  assert.equal(audio.liveWaveBytes, 0);
  assert.equal(audio.sources.size, 0);
  assert.equal(output.left.disconnects, 1);
  assert.equal(output.right.disconnects, 1);
  assert.equal(output.merger.disconnects, 1);
});

test("timed retirement respects a phased wave's earlier end and releases every generation exactly once", t => {
  const { audio, sources, buffers } = setup(t);
  const samples = Float32Array.of(.25), phase = 750_000_000;
  assert.equal(audio.prepareBatch([{ kind: "clock", at: 0 }]), true);
  audio.playTimed(0, () => audio.playWave(1, 8000, samples, 7, true, 1, phase));
  const first = sources[0], output = audio.sources.get(first).pcm;
  const end = 1.1 + .25 / 8000;
  audio.playTimed(.010, () => audio.stopSound(7));
  close(first.stops[0][0], end);
  assert.equal(first.disconnects, 0);
  assert.equal(output.left.disconnects, 0);
  assert.equal(audio.liveWaveBytes, samples.byteLength);
  audio.playTimed(.020, () => audio.playWave(1, 8000, samples, 7, true, 1, phase));
  assert.equal(buffers.length, 1);
  assert.notEqual(audio.sources.get(sources[1]).pcm, output);
  assert.equal(audio.liveWaveBytes, 2 * samples.byteLength);
  first.onended();
  first.onended();
  assert.equal(output.left.disconnects, 1);
  assert.equal(audio.retiredOutputs.size, 0);
  assert.equal(audio.liveWaveBytes, samples.byteLength);
  audio.playTimed(.030, () => audio.stopSound(7));
  audio.playTimed(.040, () => audio.playWave(1, 8000, samples, 7, true, 1, phase));
  audio.stopAll();
  for (const source of sources) { source.onended(); assert.equal(source.disconnects, 1); }
  assert.equal(audio.sources.size, 0);
  assert.equal(audio.retiredOutputs.size, 0);
  assert.equal(audio.liveWaveBytes, 0);
  assert.equal(audio.pcmOutputs.size, 0);
});
