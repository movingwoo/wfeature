import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { GameSession, playAudioEvents } from "./session.js";

const pcm = Buffer.alloc(4000);
pcm.writeInt16LE(8192, 0);
const wave = (sound = 1) => ({ kind: "playWave", sound, pcmChannel: 1,
  channels: 1, rate: 8000, samples: pcm.toString("base64") });
const clock = at => ({ kind: "clock", at });
const atTime = (events, at) => [...events.map(event => ({ ...event, at })), clock(at)];
const word = value => [value >>> 24, (value >>> 16) & 255, (value >>> 8) & 255, value & 255];
const seconds = value => {
  const bytes = new Uint8Array(8);
  new DataView(bytes.buffer).setFloat64(0, value);
  return [...bytes];
};

// Independent wire fixture: one immutable definition shared by every PCM start.
const binaryAudio = events => {
  const bytes = [0x57, 0x46, 0x41, 0x32];
  if (events.some(event => event.kind === "playWave")) bytes.push(0x10, ...word(1), ...word(pcm.length), ...pcm);
  for (const event of events) {
    if (event.kind === "allOff") { bytes.push(0x06); continue; }
    if (event.kind === "clock") { bytes.push(0x0c, ...seconds(event.at)); continue; }
    bytes.push(0x07, ...word(event.sound ?? 0));
    if (event.at === undefined) bytes.push(0x0b, 0);
    else bytes.push(0x0b, 1, ...seconds(event.at));
    switch (event.kind) {
      case "playWave":
        bytes.push(0x14, ...word(1), 1, ...word(8000), 0, 1);
        break;
      case "noteResume":
        bytes.push(0x0a, event.channel ?? 0, event.note, event.velocity, ...word(event.age));
        break;
      case "soundGain":
        bytes.push(0x09, event.value >>> 8, event.value & 255);
        break;
      default:
        assert.fail(`unhandled audio fixture event ${event.kind}`);
    }
  }
  return Uint8Array.from(bytes).buffer;
};

const setup = async t => {
  t.mock.method(performance, "now", () => 0);
  const sources = [], deliveries = [], listeners = new Map();
  const parameter = () => ({ value: 1,
    setValueAtTime() {}, linearRampToValueAtTime() {}, exponentialRampToValueAtTime() {}, cancelScheduledValues() {} });
  const node = () => ({ disconnects: 0, connect() {}, disconnect() { this.disconnects++; } });
  const source = () => {
    const result = { ...node(), frequency: parameter(), starts: [], stops: [],
      start(...args) { this.starts.push(args); }, stop(...args) { this.stops.push(args); } };
    sources.push(result);
    return result;
  };
  const audio = new PageAudio();
  audio.context = {
    state: "running", currentTime: 1, baseLatency: .01, sampleRate: 48000,
    createGain: () => ({ ...node(), gain: parameter() }),
    createChannelMerger: node, createBufferSource: source, createOscillator: source,
    createBuffer(channels, frames, rate) {
      const samples = Array.from({ length: channels }, () => new Float32Array(frames));
      return { numberOfChannels: channels, length: frames, sampleRate: rate, getChannelData: channel => samples[channel] };
    },
  };
  audio.waveGain = node();
  audio.midiGain = node();
  const socket = {
    readyState: 1, sent: [],
    addEventListener(kind, handler) { listeners.set(kind, [...(listeners.get(kind) ?? []), handler]); },
    send(message) { this.sent.push(JSON.parse(message)); },
    close() {},
    deliver(message) {
      const data = message instanceof ArrayBuffer ? message : JSON.stringify(message);
      for (const handler of listeners.get("message") ?? []) handler({ data });
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
  return { session, socket, audio, sources, deliveries };
};

for (const protocol of ["JSON", "binary"]) {
  test(`${protocol} source pressure requests one epoch-bound reset and admits the complete output replay`, async t => {
    const { session, socket, audio, sources, deliveries } = await setup(t);
    const epoch = 7;
    socket.deliver({ kind: "started", epoch, started: {} });
    const send = events => socket.deliver(protocol === "binary" ? binaryAudio(events) : { kind: "audio", epoch, audio: events });

    send(atTime(Array.from({ length: 512 }, () => wave()), 0));
    assert.equal(sources.length, 512);
    assert.equal(audio.sources.size, 512);
    const oldSources = [...sources];
    send(atTime([{ kind: "soundGain", sound: 1, value: 0 }, wave()], .01));
    assert.deepEqual(socket.sent, [{ kind: "audioResume", epoch }], "pressure must recover on the current timeline exactly once");
    assert.equal(deliveries.at(-2).accepted, false, "the over-budget batch must be refused before dispatch");
    assert.deepEqual(deliveries.at(-1).events, [{ kind: "allOff" }]);
    assert.equal(sources.length, 512, "refusal must not allocate a partial extra source");
    assert.equal(audio.sources.size, 0);
    assert.ok(oldSources.every(source => source.stops.length === 1 && source.disconnects === 1));

    const pendingDeliveries = deliveries.length;
    send(atTime([wave()], .02));
    send(atTime([wave()], .03));
    socket.deliver({ kind: "audio", epoch: epoch - 1, audio: [{ kind: "allOff" }, ...atTime([wave()], .03)] });
    assert.equal(deliveries.length, pendingDeliveries, "pending output and old epochs cannot revive stale sources");
    assert.equal(socket.sent.length, 1);
    assert.equal(sources.length, 512);

    const replay = [
      ...Array.from({ length: 256 }, (_, index) => wave(1000 + index)),
      ...Array.from({ length: 24 }, (_, index) => ({ kind: "noteResume", sound: 1000,
        channel: 0, note: 40 + index, velocity: 100, age: 150 })),
    ];
    send([{ kind: "allOff" }, ...atTime(replay, .04)]);
    assert.equal(deliveries.at(-1).accepted, true);
    assert.equal(session.audioResetPending, false);
    assert.equal(audio.sources.size, 280);
    assert.equal(audio.voices.size, 24);
    assert.equal([...audio.sources.values()].filter(voice => voice.kind === "wave").length, 256);
    assert.ok(sources.slice(512).every(source => source.starts.length === 1));
    for (const source of oldSources) source.onended();
    assert.equal(audio.sources.size, 280, "late ends from the discarded timeline cannot consume replay sources");

    send(atTime(Array.from({ length: 232 }, () => wave(1000)), .05));
    assert.equal(deliveries.at(-1).accepted, true, "reconstruction must release the old source reservation");
    assert.equal(audio.sources.size, 512);
    assert.equal(socket.sent.length, 1);
    send([{ kind: "allOff" }, ...atTime([wave()], .06)]);
    assert.equal(audio.sources.size, 1, "a later reset must make the capacity reusable again");
    assert.equal(socket.sent.length, 1);
  });
}

test("an untimed server's over-budget batch leaves output usable without requesting recovery", async t => {
  const { session, socket, audio, sources, deliveries } = await setup(t);
  const send = events => socket.deliver({ kind: "audio", audio: events });
  const legacyWave = (sound = 1) => {
    const event = wave(sound);
    delete event.pcmChannel;
    return event;
  };
  send([legacyWave()]);
  const sounding = sources[0];
  send([{ kind: "soundGain", sound: 1, value: 0 }, ...Array.from({ length: 512 }, () => legacyWave())]);
  assert.equal(deliveries.at(-1).accepted, false);
  assert.deepEqual(socket.sent, [], "legacy servers do not understand audioResume");
  assert.equal(session.audioResetPending, false);
  assert.equal(audio.sources.size, 1);
  assert.equal(sources.length, 1, "a refused untimed batch cannot allocate a playable prefix");
  assert.deepEqual(sounding.stops, []);
  assert.equal(audio.soundGains.get(1).level, 1, "refusal must preserve earlier owner gain");
  send([legacyWave(2)]);
  assert.equal(deliveries.at(-1).accepted, true);
  assert.equal(audio.sources.size, 2, "the next ordinary batch does not need an all-off response");
  assert.deepEqual(socket.sent, []);
});
