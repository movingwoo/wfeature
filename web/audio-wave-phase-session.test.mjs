import assert from "node:assert/strict";
import { test } from "node:test";
import { AudioStream } from "./audio-stream.js";
import { PageAudio } from "./audio.js";
import { GameSession, playAudioEvents } from "./session.js";

const pcm = Buffer.from([0xe8, 3, 0xd0, 7, 0xb8, 11, 0xa0, 15]);
const word = value => [value >>> 24, value >>> 16 & 255, value >>> 8 & 255, value & 255];
const seconds = value => {
  const bytes = new Uint8Array(8);
  new DataView(bytes.buffer).setFloat64(0, value);
  return [...bytes];
};
const wave = (framePhase, pcmChannel = 35) => ({ kind: "playWave", sound: 7,
  channels: 1, rate: 4, samples: pcm.toString("base64"), framePhase, pcmChannel, at: 0 });
const batch = (framePhase, reset = false, pcmChannel = 35) => [
  ...(reset ? [{ kind: "allOff" }] : []), wave(framePhase, pcmChannel), { kind: "clock", at: 0 },
];

// Independent authored wire layout, including the complete new operation.
const binary = (phase, { reset = false, group = 35, define = true, channels = 1 } = {}) => Uint8Array.from([
  0x57, 0x46, 0x41, 0x32,
  ...(reset ? [0x06] : []),
  ...(define ? [0x10, ...word(1), ...word(pcm.length), ...pcm] : []),
  0x07, ...word(7), 0x0b, 1, ...seconds(0),
  0x16, ...word(1), channels, ...word(4), group >>> 8, group & 255, ...word(phase),
  0x0c, ...seconds(0),
]).buffer;

const setup = async t => {
  t.mock.method(performance, "now", () => 0);
  const sources = [], deliveries = [], listeners = new Map();
  let buffers = 0;
  const node = () => ({ connect() {}, disconnect() {} });
  const parameter = () => ({ value: 1, setValueAtTime() {}, linearRampToValueAtTime() {}, cancelScheduledValues() {} });
  const audio = new PageAudio();
  audio.context = {
    state: "running", currentTime: 1, baseLatency: .01,
    createGain: () => ({ ...node(), gain: parameter() }), createChannelMerger: node,
    createBufferSource() {
      const source = { ...node(), starts: [], stops: [], start(...args) { this.starts.push(args); }, stop(...args) { this.stops.push(args); } };
      sources.push(source);
      return source;
    },
    createBuffer(channels, frames, rate) {
      buffers++;
      const samples = Array.from({ length: channels }, () => new Float32Array(frames));
      return { numberOfChannels: channels, length: frames, sampleRate: rate, getChannelData: channel => samples[channel] };
    },
  };
  audio.waveGain = node();
  const socket = {
    readyState: 1, sent: [], close() {},
    addEventListener(kind, handler) { listeners.set(kind, [...(listeners.get(kind) || []), handler]); },
    send(message) { this.sent.push(JSON.parse(message)); },
    deliver(value) {
      const data = value instanceof ArrayBuffer ? value : JSON.stringify(value);
      for (const handler of listeners.get("message") || []) handler({ data });
    },
  };
  const original = { WebSocket: globalThis.WebSocket, location: globalThis.location };
  globalThis.WebSocket = function () { return socket; };
  globalThis.location = { protocol: "https:", host: "example.test" };
  t.after(() => Object.assign(globalThis, original));
  const session = new GameSession({ onAudio(events) {
    const accepted = playAudioEvents(audio, events);
    deliveries.push({ events, accepted });
    return accepted;
  } });
  t.after(() => { session.close(); audio.stopAll(); });
  const opening = session.open();
  while (!session.socket) await new Promise(resolve => setImmediate(resolve));
  socket.deliver({ kind: "ready", profile: "debug" });
  await opening;
  socket.deliver({ kind: "started", epoch: 7, started: {} });
  return { socket, session, audio, sources, deliveries, buffers: () => buffers };
};

for (const format of ["JSON", "WFA2"]) {
  for (const group of [0, 35]) {
    test(`${format} fractional PCM reaches the scheduled source with group ${group}`, async t => {
      const { socket, audio, sources, buffers } = await setup(t);
      for (const phase of [500000000, 999999999]) {
        socket.deliver(format === "WFA2" ? binary(phase, { group, define: phase === 500000000 }) :
          { kind: "audio", epoch: 7, audio: batch(phase, false, group) });
      }
      assert.equal(sources.length, 2);
      assert.deepEqual(sources[0].starts, [[1.1, .125]]);
      assert.deepEqual(sources[1].starts, [[1.1, 999999999 / 4e9]]);
      assert.equal(audio.sources.get(sources[0]).end, 1.1 + 1 - .125);
      assert.equal(audio.liveWaveBytes, 32, "fractional offsets do not reduce retained buffer charges");
      if (format === "WFA2") {
        assert.equal(buffers(), 1);
        assert.equal(sources[0].buffer, sources[1].buffer, "phase must not split immutable PCM cache identity");
      }
      assert.equal(audio.sources.get(sources[0]).pcm?.channel ?? 0, group);
      sources[0].onended(); sources[1].onended();
      assert.equal(audio.sources.size, 0);
      assert.equal(audio.liveWaveBytes, 0);
      assert.deepEqual(socket.sent, []);
    });
  }

  test(`${format} malformed PCM phase requests one recovery and a fresh phased replay succeeds`, async t => {
    const { socket, session, audio, sources } = await setup(t);
    const send = (phase, reset = false) => socket.deliver(format === "WFA2" ? binary(phase, { reset }) :
      { kind: "audio", epoch: 7, audio: batch(phase, reset) });
    send(1000000000);
    assert.equal(sources.length, 0);
    if (format === "WFA2") {
      assert.deepEqual(socket.sent, [], "a malformed first binary packet cannot establish server timing support");
      assert.equal(session.audioDecodeLost, true);
      socket.deliver({ kind: "audio", epoch: 7, audio: [{ kind: "clock", at: 0 }] });
    }
    assert.deepEqual(socket.sent, [{ kind: "audioResume", epoch: 7 }]);
    send(500000000);
    assert.equal(sources.length, 0, "stale phased output must wait for reconstruction");
    send(500000000, true);
    assert.equal(session.audioResetPending, false);
    assert.deepEqual(sources[0].starts, [[1.1, .125]]);
    assert.equal(audio.sources.size, 1);
    assert.equal(socket.sent.length, 1);
  });
}

test("WFA2 fractional PCM rejects every truncated operand and invalid metadata before delivery", () => {
  const packet = new Uint8Array(binary(500000000));
  const operationAt = packet.length - 9 - 16;
  for (let length = 1; length < 16; length++) {
    assert.throws(() => new AudioStream().decode(packet.slice(0, operationAt + length).buffer), /sound message/);
  }
  for (const phase of [1000000000, 0xffffffff]) {
    assert.throws(() => new AudioStream().decode(binary(phase)), /phase/);
  }
  assert.throws(() => new AudioStream().decode(binary(1, { channels: 2 })), /route/);
  // Metadata is still untrusted when its sample definition is missing.
  assert.throws(() => new AudioStream().decode(binary(1000000000, { define: false })), /phase/);
  assert.deepEqual(new AudioStream().decode(binary(1, { define: false })), [{ kind: "clock", at: 0 }]);
});
