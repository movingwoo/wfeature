import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { playAudioEvents } from "./session.js";

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);
const clock = at => ({ kind: "clock", at });
const control = (pcmChannel, control, value, sound = 7) => ({ kind: "pcmControl", pcmChannel, control, value, sound });
const wave = (samples, pcmChannel = 1, sound = 7) => ({ kind: "playWave", channels: 1, rate: 8000, samples, sound, pcmChannel, cacheable: true });
const levels = (volume = 127, expression = 127, pan = 64) => {
  const gain = (volume / 127) ** 2 * (expression / 127) ** 2;
  return [gain * Math.cos(Math.PI * pan / 254), gain * Math.sin(Math.PI * pan / 254)];
};
const finalLevel = parameter => parameter.events.at(-1)?.[1] ?? parameter.value;

const setup = t => {
  t.mock.method(performance, "now", () => 0);
  const sources = [], buffers = [], nodes = [];
  const parameter = () => ({
    value: 1, events: [],
    setValueAtTime(value, at) { this.events.push(["set", value, at]); },
    linearRampToValueAtTime(value, at) { this.events.push(["linear", value, at]); },
    exponentialRampToValueAtTime(value, at) { this.events.push(["exponential", value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[2] < at); },
  });
  const node = kind => {
    const result = { kind, connections: [], disconnects: 0,
      connect(target, output = 0, input = 0) { this.connections.push({ target, output, input }); },
      disconnect() { this.disconnects++; } };
    nodes.push(result);
    return result;
  };
  const source = () => {
    const result = Object.assign(node("source"), { frequency: parameter(), starts: [], stops: [],
      start(...args) { this.starts.push(args); }, stop(...args) { this.stops.push(args); } });
    sources.push(result);
    return result;
  };
  const audio = new PageAudio();
  audio.context = {
    state: "running", currentTime: 1, baseLatency: .01, sampleRate: 48000,
    createGain: () => Object.assign(node("gain"), { gain: parameter() }),
    createChannelMerger: inputs => Object.assign(node("merger"), { numberOfInputs: inputs }),
    createBufferSource: source, createOscillator: source,
    createBuffer(channels, frames, rate) {
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      const buffer = { numberOfChannels: channels, length: frames, sampleRate: rate, getChannelData: channel => data[channel] };
      buffers.push(buffer);
      return buffer;
    },
  };
  audio.waveGain = node("waveOutput");
  audio.midiGain = node("midiOutput");
  const send = events => assert.equal(playAudioEvents(audio, events), true);
  return { audio, sources, buffers, nodes, send };
};

// Inspect the resulting speaker routes, independently of PCM state-map names.
const route = source => {
  const individual = source.connections[0].target;
  assert.equal(individual.connections.length, 2, "grouped mono PCM must reach independently controlled left and right routes");
  const sides = individual.connections.map(connection => connection.target)
    .sort((a, b) => a.connections[0].input - b.connections[0].input);
  const merger = sides[0].connections[0].target;
  assert.equal(sides[1].connections[0].target, merger);
  assert.equal(merger.numberOfInputs, 2);
  assert.deepEqual(sides.map(side => side.connections[0].input), [0, 1]);
  return { individual, left: sides[0], right: sides[1], merger, owner: merger.connections[0].target };
};
const assertLevels = (output, expected) => {
  close(finalLevel(output.left.gain), expected[0]);
  close(finalLevel(output.right.gain), expected[1]);
};

test("live PCM mute and expression change existing output without restarting or rewriting a wave", t => {
  const { audio, sources, buffers, send } = setup(t);
  const samples = new Float32Array(8000);
  samples.set([.25, -.5, .75, -.25]);
  send([{ ...wave(samples), at: 0 }, clock(0)]);
  const output = route(sources[0]);
  assertLevels(output, levels());
  close(output.individual.gain.value, .8);
  send([{ ...control(1, 7, 0), at: .020 }, clock(.020)]);
  assertLevels(output, [0, 0]);
  close(output.left.gain.events.at(-1)[2], 1.125);
  send([{ ...control(1, 7, 64), at: .040 }, { ...control(1, 11, 64), at: .060 }, clock(.060)]);
  assertLevels(output, levels(64, 64));
  close(output.right.gain.events.at(-1)[2], 1.165);
  assert.equal(sources.length, 1);
  assert.deepEqual(sources[0].starts, [[1.1]]);
  assert.deepEqual(sources[0].stops, []);
  assert.equal(buffers.length, 1);
  assert.deepEqual([...buffers[0].getChannelData(0)], [...samples]);
  send([{ ...wave(samples), at: .080 }, { kind: "soundGain", sound: 7, value: 5000, at: .090 }, clock(.090)]);
  assert.equal(sources[1].buffer, sources[0].buffer);
  assert.equal(route(sources[1]).left, output.left);
  assert.equal(finalLevel(output.owner.gain), .5);
  assertLevels(output, levels(64, 64));
  assert.equal(audio.sources.size, 2);
});

test("pre-wave volume, expression and exact pan endpoints remain distinct for every group and owner zero", t => {
  const { sources, send } = setup(t);
  for (const [index, pan] of [0, 64, 127].entries()) {
    const group = [1, 17, 65535][index];
    send([control(group, 7, 64, 0), control(group, 11, 96, 0), control(group, 10, pan, 0), wave(Float32Array.of(.5), group, 0)]);
  }
  for (const [index, pan] of [0, 64, 127].entries()) assertLevels(route(sources[index]), levels(64, 96, pan));
  assert.equal(new Set(sources.map(source => route(source).left)).size, 3);
});

test("PCM controls isolate groups, owners, ungrouped samples and MIDI channels", t => {
  const { audio, sources, send } = setup(t);
  const samples = Float32Array.of(.5, -.5);
  send([wave(samples), wave(samples, 2), wave(samples, 1, 8), wave(samples, 0),
    { kind: "noteOn", sound: 7, channel: 0, note: 60, velocity: 100 }]);
  const outputs = sources.slice(0, 3).map(route);
  const legacy = sources[3].connections[0].target;
  const midi = audio.channelOutputs.get(7)[0].gain.gain;
  audio.controlChange(0, 7, 0, 7);
  const midiEvents = structuredClone(midi.events);
  send([control(1, 7, 0), control(1, 10, 127)]);
  assertLevels(outputs[0], [0, 0]);
  assertLevels(outputs[1], levels());
  assertLevels(outputs[2], levels());
  assert.deepEqual(outputs[1].left.gain.events, []);
  assert.deepEqual(outputs[2].right.gain.events, []);
  assert.deepEqual(midi.events, midiEvents);
  assert.equal(legacy.connections.length, 1, "group zero retains its original direct wave route");
  assert.deepEqual(legacy.gain.events, []);
  assert.ok(sources.every(source => source.stops.length === 0));
});

test("immediate clip stops reset PCM controls and retain cached payloads until all-off", t => {
  const { audio, sources, buffers, send } = setup(t);
  const samples = Float32Array.of(.25, -.25);
  send([control(1, 7, 32), control(1, 10, 0), wave(samples)]);
  const old = route(sources[0]);
  send([{ kind: "stopSound", sound: 7 }, wave(samples)]);
  assertLevels(route(sources[1]), levels());
  for (const node of [old.left, old.right, old.merger]) assert.equal(node.disconnects, 1);
  assert.equal(buffers.length, 1);
  assert.equal(sources[0].buffer, sources[1].buffer);
  send([{ kind: "allOff" }, wave(samples)]);
  assertLevels(route(sources[2]), levels());
  assert.equal(buffers.length, 2);
  sources[0].onended();
  assert.equal(audio.sources.size, 1);
});

test("scheduled stop retains old PCM routes until all retired sources end and isolates a restart", t => {
  const { audio, sources, send } = setup(t);
  const samples = new Float32Array(8000);
  send([{ ...wave(samples), at: 0 }, { ...wave(samples, 2), at: 0 }, { ...wave(samples, 1, 8), at: 0 }, clock(0)]);
  const old = [route(sources[0]), route(sources[1])], peer = route(sources[2]);
  send([{ kind: "stopSound", sound: 7, at: .030 }, { ...wave(samples), at: .040 },
    { ...control(1, 7, 0), at: .050 }, clock(.050)]);
  for (const source of sources.slice(0, 2)) close(source.stops[0][0], 1.130);
  for (const output of old) {
    assert.equal(output.left.disconnects, 0);
    assert.deepEqual(output.left.gain.events, []);
    assertLevels(output, levels());
  }
  const current = route(sources[3]);
  assert.notEqual(current.left, old[0].left);
  assertLevels(current, [0, 0]);
  assert.deepEqual(sources[2].stops, []);
  sources[0].onended();
  assert.equal(old[0].left.disconnects, 0, "a retired generation stays connected until its last source ends");
  sources[1].onended();
  for (const output of old) for (const node of [output.left, output.right, output.merger]) assert.equal(node.disconnects, 1);
  sources[0].onended();
  assert.equal(old[0].left.disconnects, 1);
  assert.equal(current.left.disconnects, 0);
  assert.equal(peer.left.disconnects, 0);
  assert.equal(audio.retiredOutputs.size, 0);
});

test("pause reconstruction and quick load apply saved controls before the trimmed PCM starts", t => {
  const { audio, sources, buffers, send } = setup(t);
  const samples = Float32Array.of(.1, .2, .3, .4);
  send([wave(samples), wave(samples, 1, 8)]);
  const original = [...buffers[0].getChannelData(0)];
  send([{ kind: "stopSound", sound: 7 }]);
  const trimmed = samples.slice(2);
  const replay = [control(1, 7, 64), control(1, 11, 32), control(1, 10, 127), wave(trimmed)];
  send(replay);
  assertLevels(route(sources[2]), levels(64, 32, 127));
  assert.deepEqual([...sources[2].buffer.getChannelData(0)], [...trimmed]);
  assert.deepEqual([...buffers[0].getChannelData(0)], original);
  assert.deepEqual(sources[1].stops, [], "the peer keeps playing through one clip's pause");
  send([{ kind: "allOff" }, ...replay]);
  assertLevels(route(sources[3]), levels(64, 32, 127));
  assert.notEqual(sources[2].buffer, sources[3].buffer);
  assert.equal(sources[3].starts.length, 1);
  assert.equal(sources[3].starts[0].length, 1);
  assert.equal(audio.sources.size, 1);
});

test("invalid PCM metadata refuses an entire untimed batch before MIDI, gain or source mutation", t => {
  const { sources, nodes, send, audio } = setup(t);
  const samples = Float32Array.of(.5);
  send([wave(samples)]);
  const output = route(sources[0]);
  const invalid = [
    ...[0, -1, 65536, 1.5, "1"].map(group => control(group, 7, 64)),
    ...[-1, 128, 1.5, NaN].map(value => control(1, 7, value)), control(1, 8, 64),
    ...[-1, 65536, .5].map(group => wave(samples, group)), { ...wave(samples), channels: 2 },
  ];
  for (const event of invalid) {
    const allocations = nodes.length;
    assert.equal(playAudioEvents(audio, [
      { kind: "noteOn", note: 60, velocity: 100 }, control(1, 7, 0), event, wave(samples),
    ]), false, `invalid PCM event accepted: ${JSON.stringify(event)}`);
    assert.equal(nodes.length, allocations);
    assert.equal(sources.length, 1);
    assert.deepEqual(output.left.gain.events, []);
    assert.deepEqual(output.right.gain.events, []);
    assert.deepEqual(sources[0].stops, []);
  }
});

test("the 2048-group budget is global across owners and preflights stops before admitting a replacement", t => {
  const { audio, sources, nodes, send } = setup(t);
  const samples = Float32Array.of(.5);
  send([wave(samples)]);
  send(Array.from({ length: 2047 }, (_, index) => control(index + 2, 11, 127)));
  const output = route(sources[0]), allocations = nodes.length;
  assert.equal(playAudioEvents(audio, [control(1, 7, 0), control(1, 11, 127, 8), wave(samples, 1, 8)]), false);
  assert.equal(sources.length, 1);
  assert.equal(nodes.length, allocations);
  assert.deepEqual(output.left.gain.events, []);
  send([{ kind: "stopSound", sound: 7 }, control(1, 7, 64, 8), wave(samples, 1, 8)]);
  assertLevels(route(sources[1]), levels(64));
  send([{ kind: "allOff" }, wave(samples)]);
  assertLevels(route(sources[2]), levels());
  assert.equal(audio.sources.size, 1);
});

test("idle groups retain controls without retaining native nodes or allocating before activation", t => {
  const { audio, sources, nodes, send } = setup(t);
  const context = audio.context;
  audio.context = null;
  send([control(1, 7, 64), control(1, 10, 127)]);
  assert.equal(nodes.length, 2, "idle controller state cannot activate or allocate an audio graph");
  audio.context = context;
  const samples = Float32Array.of(.5);
  send([wave(samples), wave(samples)]);
  const first = route(sources[0]);
  assert.equal(route(sources[1]).left, first.left);
  sources[0].onended();
  assert.equal(first.left.disconnects, 0);
  sources[1].onended();
  for (const node of [first.left, first.right, first.merger]) assert.equal(node.disconnects, 1);
  send([wave(samples)]);
  const next = route(sources[2]);
  assert.notEqual(first.left, next.left);
  assertLevels(next, levels(64, 127, 127));
  send([{ kind: "pcmControl", pcmChannel: 1, control: 7, sound: 7 }]);
  assertLevels(next, [0, 0], "JSON omits a zero-valued controller field");
});

test("invalid reset batches preserve old PCM output and failed source allocation releases unused routes", t => {
  const { audio, sources, nodes, send } = setup(t);
  const samples = Float32Array.of(.5);
  send([{ ...wave(samples), at: 0 }, clock(0)]);
  const output = route(sources[0]), anchor = audio.presentationAnchor;
  assert.equal(playAudioEvents(audio, [{ kind: "allOff" }, control(0, 7, 0), wave(samples)]), false);
  assert.deepEqual(sources[0].stops, []);
  assert.equal(output.left.disconnects, 0);
  assert.equal(audio.presentationAnchor, anchor);
  audio.stopAll();
  const before = nodes.length;
  const create = audio.context.createBufferSource;
  audio.context.createBufferSource = () => { throw new Error("native source allocation failed"); };
  assert.throws(() => audio.playWave(1, 8000, samples, 7, true, 1), /allocation failed/);
  const groupNodes = nodes.slice(before, before + 3);
  assert.deepEqual(groupNodes.map(node => node.kind), ["gain", "gain", "merger"]);
  assert.ok(groupNodes.every(node => node.disconnects === 1));
  audio.context.createBufferSource = create;
  send([wave(samples)]);
  assertLevels(route(sources[1]), levels());
});
