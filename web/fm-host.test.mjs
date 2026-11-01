import assert from "node:assert/strict";
import { test } from "node:test";

import { FMHost } from "./fm-host.js";
import { FM_LANES } from "./fm-synth.js";

const settle = () => new Promise(resolve => setImmediate(resolve));

const context = ({ worklet = null } = {}) => {
  const node = kind => ({ kind, connections: [], disconnections: [],
    connect(target, output) { this.connections.push([target, output]); },
    disconnect(target, output) { this.disconnections.push([target, output]); } });
  const result = {
    sampleRate: 48000, currentTime: 0, destination: node("destination"),
    createChannelSplitter: count => Object.assign(node("splitter"), { count }),
    createGain: () => Object.assign(node("gain"), { gain: { value: 1 } }),
    createScriptProcessor(frames, inputs, outputs) {
      const processor = Object.assign(node("script"), { frames, inputs, outputs });
      result.processor = processor;
      return processor;
    },
  };
  if (worklet) result.audioWorklet = worklet;
  return result;
};

test("an AudioWorklet runs the synthesizer and receives every queued command in order", async t => {
  const nodes = [];
  const original = globalThis.AudioWorkletNode;
  t.after(() => { globalThis.AudioWorkletNode = original; });
  globalThis.AudioWorkletNode = class {
    constructor(audio, name, options) {
      Object.assign(this, { audio, name, options, sent: [], connections: [] });
      this.port = { postMessage: message => this.sent.push(message) };
      nodes.push(this);
    }
    connect(target) { this.connections.push(target); }
  };
  let loaded;
  const audio = context({ worklet: { addModule: url => { loaded = url; return Promise.resolve(); } } });
  const ended = [];
  const host = new FMHost(audio, { onEnded: ids => ended.push(...ids) });
  host.post({ op: "define", sound: 1, slot: 1, data: new Uint8Array(1) });
  host.post({ op: "on", id: 1, at: 0, lane: 0 });
  assert.equal(nodes.length, 0, "the worklet node exists only after its module loads");
  await settle();
  assert.match(loaded, /fm-synth\.js$/);
  assert.equal(host.mode, "worklet");
  const [node] = nodes;
  assert.equal(node.name, "wfeature-fm");
  assert.deepEqual(node.options.outputChannelCount, [FM_LANES]);
  assert.deepEqual(node.sent.map(batch => batch.map(command => command.op)), [["define", "on"]]);
  host.post({ op: "off", id: 1, at: 1 });
  host.post({ op: "cut", id: 1, at: 2 });
  await settle();
  assert.deepEqual(node.sent.map(batch => batch.map(command => command.op)), [["define", "on"], ["off", "cut"]], "one message per task");
  node.port.onmessage({ data: [1] });
  assert.deepEqual(ended, [1]);
  assert.ok(node.connections.includes(host.splitter) && node.connections.includes(host.keepAlive));
});

test("without a worklet the same synthesizer renders in a ScriptProcessor", async () => {
  const audio = context();
  const ended = [];
  const host = new FMHost(audio, { onEnded: ids => ended.push(...ids) });
  assert.equal(host.mode, "script");
  const processor = audio.processor;
  assert.deepEqual([processor.frames, processor.inputs, processor.outputs], [2048, 0, FM_LANES]);
  const sine = Uint8Array.from([1, 1, 0, 1, 1, 0, 0, 15, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 63, 15, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  host.post({ op: "define", sound: 1, slot: 1, data: sine });
  const lane = host.lane(audio.createGain());
  host.post({ op: "on", id: 9, at: 0.01, lane, sound: 1, slot: 1, pitch: 69, velocity: 127 });
  host.post({ op: "cut", id: 9, at: 0.03, fade: 0.001 });
  const channels = Array.from({ length: FM_LANES }, () => new Float32Array(2048));
  processor.onaudioprocess({ playbackTime: 0, outputBuffer: { numberOfChannels: FM_LANES, length: 2048, getChannelData: index => channels[index] } });
  assert.equal(channels[lane].subarray(0, 470).some(value => value !== 0), false, "the note starts at its own sample");
  assert.ok(channels[lane].subarray(480, 1400).some(value => Math.abs(value) > 0.2));
  assert.equal(channels[lane + 1].some(value => value !== 0), false);
  assert.deepEqual(ended, [9]);
  host.post({ op: "on", id: 10, at: 0.2, lane, sound: 1, slot: 1 });
  host.release(lane);
  assert.deepEqual(ended, [9], "an end caused by a command is reported after the caller's own work");
  await settle();
  assert.deepEqual(ended, [9, 10]);
});

test("a worklet that fails to load falls back to the page and says so", async () => {
  const reports = [];
  const audio = context({ worklet: { addModule: () => Promise.reject(new Error("blocked")) } });
  const original = globalThis.AudioWorkletNode;
  globalThis.AudioWorkletNode = function () {};
  try {
    const host = new FMHost(audio, { report: message => reports.push(message) });
    await settle();
    assert.equal(host.mode, "script");
    assert.ok(audio.processor);
    assert.match(reports[0], /blocked/);
  } finally {
    globalThis.AudioWorkletNode = original;
  }
});

test("lanes connect one splitter output each and come back when released", () => {
  const audio = context();
  const host = new FMHost(audio);
  const destinations = Array.from({ length: FM_LANES + 1 }, () => audio.createGain());
  const lanes = destinations.map(destination => host.lane(destination, 1.5, 0.25));
  assert.deepEqual(lanes.slice(0, FM_LANES), Array.from({ length: FM_LANES }, (_, index) => index));
  assert.equal(lanes[FM_LANES], null, "every lane is in use");
  assert.deepEqual(host.splitter.connections[3], [destinations[3], 3]);
  assert.deepEqual(host.synth.lanes[3], { ratio: 1.5, depth: 0.25 });
  host.release(3);
  assert.deepEqual(host.splitter.disconnections.at(-1), [destinations[3], 3]);
  host.release(3);
  assert.equal(host.splitter.disconnections.length, 1, "a lane is released once");
  assert.equal(host.lane(destinations[FM_LANES]), 3);
});

test("a worklet that stops ends its notes and hands over to the page", async t => {
  const nodes = [];
  const original = globalThis.AudioWorkletNode;
  t.after(() => { globalThis.AudioWorkletNode = original; });
  globalThis.AudioWorkletNode = class {
    constructor() {
      this.port = { postMessage() {} };
      this.connections = [];
      nodes.push(this);
    }
    connect(target) { this.connections.push(target); }
    disconnect() { this.disconnected = true; }
  };
  const audio = context({ worklet: { addModule: () => Promise.resolve() } });
  const ended = [], reports = [];
  const host = new FMHost(audio, { onEnded: ids => ended.push(...ids), report: message => reports.push(message) });
  await settle();
  const voice = Uint8Array.from([1, 1, 0, 1, 1, 0, 0, 15, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 63, 15, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  host.post({ op: "define", sound: 3, slot: 2, data: voice });
  host.post({ op: "define", sound: 4, slot: 1, data: voice });
  host.post({ op: "forget", sound: 4 });
  const lane = host.lane(audio.createGain());
  const released = host.lane(audio.createGain());
  host.post({ op: "bend", lane, at: 0, ratio: 1.5 });
  host.post({ op: "depth", lane, at: 0, depth: 0.25 });
  host.release(released);
  host.post({ op: "on", id: 4, at: 0, lane });
  host.post({ op: "on", id: 5, at: 0, lane });
  nodes[0].port.onmessage({ data: [4] });
  nodes[0].onprocessorerror();
  assert.deepEqual(ended, [4, 5], "the page learns every note is over");
  assert.equal(nodes[0].disconnected, true);
  assert.equal(host.mode, "script");
  assert.ok(audio.processor);
  assert.deepEqual([...host.synth.patches.keys()], ["3:2"], "the voices still defined carry over");
  assert.deepEqual(host.synth.lanes[lane], { ratio: 1.5, depth: 0.25 }, "a claimed lane keeps its bend and depth");
  assert.deepEqual(host.synth.lanes[released], { ratio: 1, depth: 0 });
  assert.match(reports[0], /worklet stopped/);
});

// **A buffer renders for the time it really plays.** Chromium and WebKit
// report a callback's playbackTime as the render clock when they posted it
// plus one buffer, late by however long the posting waited, and a buffer they
// silenced without a callback shows only as a jump in the next report.
test("the page's ScriptProcessor renders each buffer for when it plays, not when it was reported", () => {
  const sine = Uint8Array.from([1, 1, 0, 1, 1, 0, 0, 15, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 63, 15, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  const exact = context(), reported = context();
  const hosts = [exact, reported].map(audio => new FMHost(audio));
  for (const host of hosts) {
    const lane = host.lane(host.context.createGain());
    host.post({ op: "define", sound: 1, slot: 1, data: sine });
    host.post({ op: "on", id: 1, at: 0.1, lane, sound: 1, slot: 1, pitch: 69, velocity: 127 });
    host.post({ op: "cut", id: 1, at: 0.2, fade: 0.001 });
  }
  const block = 2048 / 48000;
  const buffer = () => {
    const channels = Array.from({ length: FM_LANES }, () => new Float32Array(2048));
    return { channels, outputBuffer: { numberOfChannels: FM_LANES, length: 2048, getChannelData: index => channels[index] } };
  };
  const waits = [0, 0.03, 0.005, 0.021, 0.0001, 0.04, 0.012];
  waits.forEach((wait, index) => {
    const playbackTime = index * block;
    const expected = buffer(), played = buffer();
    exact.processor.onaudioprocess({ playbackTime, outputBuffer: expected.outputBuffer });
    reported.processor.onaudioprocess({ playbackTime: playbackTime + wait, outputBuffer: played.outputBuffer });
    assert.deepEqual(played.channels[0], expected.channels[0], `buffer ${index}`);
  });
});

test("a ScriptProcessor's buffer clock steps over buffers the engine silenced", async () => {
  const { bufferClock } = await import("./fm-host.js");
  const clock = bufferClock({ sampleRate: 48000, currentTime: 0 });
  const block = 2048 / 48000;
  const call = playbackTime => clock({ playbackTime, outputBuffer: { length: 2048 } });
  assert.equal(call(0.004), 0.004, "the first report is all there is to go on");
  assert.ok(Math.abs(call(block + 0.03) - (block + 0.004)) < 1e-12, "a longer wait does not move it");
  assert.ok(Math.abs(call(2 * block + 0.001) - (2 * block + 0.001)) < 1e-12, "a shorter one moves it earlier");
  // Buffer 3 was silenced without a callback; buffer 4's report comes late too.
  assert.ok(Math.abs(call(4 * block + 0.01) - (4 * block + 0.001)) < 1e-12, "buffer 4, not 3");
});
