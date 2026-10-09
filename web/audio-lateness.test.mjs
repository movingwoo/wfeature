import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { GameSession, playAudioEvents } from "./session.js";

const prefix = "audio playout timing: ";
const clock = at => ({ kind: "clock", at });
const control = at => ({ kind: "controlChange", sound: 7, channel: 0, control: 7, value: 80, at });
const empty = () => ({ batches: 0, admitted: 0, reanchored: 0, refused: { late: 0, future: 0, range: 0 }, clock_only_batches: 0, events: 0,
  not_late: 0, late_le_5ms: 0, late_le_20ms: 0, late_le_100ms: 0, late_gt_100ms: 0,
  invalid_events: 0, max_event_late_ms: 0, max_frontier_late_ms: 0 });
const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-8, `${actual} != ${expected}`);
const group = (actual, expected) => {
  const { max_event_late_ms, max_frontier_late_ms, ...counts } = actual;
  const { max_event_late_ms: eventMax, max_frontier_late_ms: frontierMax, ...want } = expected;
  assert.deepEqual(counts, want);
  close(max_event_late_ms, eventMax);
  close(max_frontier_late_ms, frontierMax);
};

function setup(t, { enabled = true, defaultDisabled = false } = {}) {
  t.mock.method(performance, "now", () => 0);
  const reports = [], sources = [], state = { enabled };
  const options = { report: message => reports.push(message) };
  if (!defaultDisabled) options.diagnostics = () => state.enabled;
  const audio = new PageAudio(options);
  const node = () => ({ connect() {}, disconnect() {} });
  const parameter = () => ({ value: 1, setValueAtTime() {}, linearRampToValueAtTime() {},
    exponentialRampToValueAtTime() {}, cancelScheduledValues() {} });
  audio.context = {
    state: "running", currentTime: 1, baseLatency: .1,
    createGain: () => ({ ...node(), gain: parameter() }),
    createStereoPanner: () => ({ ...node(), pan: parameter() }),
    createOscillator() {
      const source = { ...node(), frequency: parameter(), starts: [], stops: [],
        start(...args) { this.starts.push(args); }, stop(...args) { this.stops.push(args); } };
      sources.push(source);
      return source;
    },
  };
  audio.midiGain = node();
  audio.waveGain = node();
  t.after(() => audio.stopAll());
  return { audio, reports, sources, state };
}

function flush(audio, reports) {
  const before = reports.length;
  audio.reportTiming();
  assert.equal(reports.length, before + 1, "one flush emits one timing aggregate");
  assert.ok(reports.at(-1).startsWith(prefix));
  return JSON.parse(reports.at(-1).slice(prefix.length));
}

test("timing diagnostics separate fresh anchors from continuing batches and clear after flush", t => {
  const { audio, reports } = setup(t);
  assert.equal(audio.prepareBatch([control(0), clock(0)]), true);
  assert.equal(audio.prepareBatch([control(.01), clock(.02)]), true);
  const snapshot = flush(audio, reports);
  assert.deepEqual(Object.keys(snapshot).sort(), ["anchored", "continuing"]);
  group(snapshot.anchored, { ...empty(), batches: 1, admitted: 1, events: 1, not_late: 1 });
  group(snapshot.continuing, { ...empty(), batches: 1, admitted: 1, events: 1, not_late: 1 });
  audio.reportTiming();
  assert.equal(reports.length, 1, "an empty second flush must not emit");
});

test("lateness buckets include their exact upper bounds and resets preserve pending counts", t => {
  const { audio, reports } = setup(t);
  for (const milliseconds of [0, 5, 20, 100, 101]) {
    audio.stopAll();
    audio.context.currentTime = milliseconds / 1000;
    // Cancellation maps this finite authored time to exactly zero. Its
    // lateness is the context time, avoiding a rounded decimal subtraction.
    const at = -(audio.context.currentTime + .1);
    assert.equal(audio.prepareBatch([control(at), clock(0)]), milliseconds <= 5);
  }
  const snapshot = flush(audio, reports);
  group(snapshot.anchored, { ...empty(), batches: 5, admitted: 2, refused: { late: 3, future: 0, range: 0 },
    events: 5, not_late: 1, late_le_5ms: 1, late_le_20ms: 1, late_le_100ms: 1, late_gt_100ms: 1,
    max_event_late_ms: 101 });
  group(snapshot.continuing, empty());
});

test("a refused batch records its entire distribution after the first late event", t => {
  const { audio, reports } = setup(t);
  assert.equal(audio.prepareBatch([clock(0)]), true);
  audio.context.currentTime = 1.5;
  assert.equal(audio.prepareBatch([.15, .32, .385, .396, .4, .405].map(control).concat(clock(.405))), false);
  const snapshot = flush(audio, reports);
  group(snapshot.anchored, { ...empty(), batches: 1, admitted: 1, clock_only_batches: 1 });
  group(snapshot.continuing, { ...empty(), batches: 1, refused: { late: 1, future: 0, range: 0 },
    events: 6, not_late: 2, late_le_5ms: 1, late_le_20ms: 1, late_le_100ms: 1, late_gt_100ms: 1,
    max_event_late_ms: 250 });
});

test("first window failure determines the reason while invalid and future events remain accounted", t => {
  const { audio, reports } = setup(t);
  assert.equal(audio.prepareBatch([control(-2), control(0), clock(0)]), false);
  assert.equal(audio.prepareBatch([control(-Number.MAX_VALUE), clock(Number.MAX_VALUE)]), false);
  assert.equal(audio.prepareBatch([clock(0)]), true);
  audio.context.currentTime = 1.25;
  assert.equal(audio.prepareBatch([control(0), control(1), clock(1)]), false, "late precedes future in this batch");
  assert.equal(audio.prepareBatch([control(1), clock(1)]), false);
  const snapshot = flush(audio, reports);
  group(snapshot.anchored, { ...empty(), batches: 3, admitted: 1, clock_only_batches: 1,
    refused: { late: 0, future: 0, range: 2 }, events: 3, invalid_events: 2, not_late: 1 });
  group(snapshot.continuing, { ...empty(), batches: 2, refused: { late: 1, future: 1, range: 0 },
    events: 3, not_late: 2, late_gt_100ms: 1, max_event_late_ms: 150 });
});

// A batch holding nothing but its frontier loses nothing when it arrives late:
// the server fell behind the render clock and has caught up. Live sessions
// showed two thirds of all late refusals were such batches, each answered by
// asking for the current output, which cuts every sounding voice to replay it.
// The anchor moves to the frontier instead; a late batch with events is still
// refused, since it would otherwise have to play them late.
test("a late frontier without events moves the anchor and keeps playing", t => {
  const { audio, reports, sources } = setup(t);
  assert.equal(audio.prepareBatch([clock(0)]), true);
  audio.context.currentTime = 1.3;
  assert.equal(playAudioEvents(audio, [clock(.1)]), true);
  close(audio.presentationAnchor.audio, 1.4);
  assert.equal(audio.presentationAnchor.presentation, .1);
  assert.equal(audio.presentationFrontier, .1);
  let snapshot = flush(audio, reports);
  group(snapshot.anchored, { ...empty(), batches: 1, admitted: 1, clock_only_batches: 1 });
  group(snapshot.continuing, { ...empty(), batches: 1, reanchored: 1, clock_only_batches: 1, max_frontier_late_ms: 100 });

  // Later events are placed from the moved anchor, a lead ahead of the clock.
  assert.equal(playAudioEvents(audio, [{ kind: "noteOn", sound: 7, channel: 0, note: 60, velocity: 90, at: .12 }, clock(.12)]), true);
  close(sources.at(-1).starts[0][0], 1.42);
  snapshot = flush(audio, reports);
  group(snapshot.continuing, { ...empty(), batches: 1, admitted: 1, events: 1, not_late: 1 });

  audio.context.currentTime = 1.7;
  assert.equal(audio.prepareBatch([control(.2), clock(.2)]), false, "a late batch with events still asks for the current output");
  snapshot = flush(audio, reports);
  group(snapshot.continuing, { ...empty(), batches: 1, refused: { late: 1, future: 0, range: 0 },
    events: 1, late_gt_100ms: 1, max_event_late_ms: 200, max_frontier_late_ms: 200 });
});

test("a reset anchor after a refusal is counted apart from continuing batches", t => {
  const { audio, reports } = setup(t);
  assert.equal(audio.prepareBatch([clock(0)]), true);
  audio.context.currentTime = 1.3;
  assert.equal(audio.prepareBatch([control(.1), clock(.1)]), false);
  assert.equal(playAudioEvents(audio, [{ kind: "allOff" }, control(.1), clock(.1)]), true);
  let snapshot = flush(audio, reports);
  group(snapshot.anchored, { ...empty(), batches: 2, admitted: 2, clock_only_batches: 1, events: 1, not_late: 1 });
  group(snapshot.continuing, { ...empty(), batches: 1, refused: { late: 1, future: 0, range: 0 },
    events: 1, late_le_100ms: 1, max_event_late_ms: 100, max_frontier_late_ms: 100 });
  assert.equal(audio.prepareBatch([clock(.15)]), true);
  snapshot = flush(audio, reports);
  group(snapshot.anchored, empty());
  group(snapshot.continuing, { ...empty(), batches: 1, admitted: 1, clock_only_batches: 1 });
});

test("diagnostics default off and disabling drops buffered data without changing playback timing", t => {
  const disabled = setup(t, { defaultDisabled: true });
  assert.equal(disabled.audio.prepareBatch([control(0), clock(0)]), true);
  disabled.audio.reportTiming();
  assert.deepEqual(disabled.reports, []);
  const { audio, reports, state } = setup(t, { enabled: false });
  assert.equal(audio.prepareBatch([clock(0)]), true);
  state.enabled = true;
  assert.equal(audio.prepareBatch([clock(.01)]), true);
  state.enabled = false;
  audio.reportTiming();
  assert.deepEqual(reports, [], "a release-profile flush discards earlier debug data");
  state.enabled = true;
  audio.reportTiming();
  assert.deepEqual(reports, []);
  assert.equal(audio.prepareBatch([clock(.02)]), true);
  state.enabled = false;
  assert.equal(audio.prepareBatch([clock(.03)]), true);
  state.enabled = true;
  audio.reportTiming();
  assert.deepEqual(reports, [], "disabled reception must not retain a pending recorder");
  assert.equal(audio.prepareBatch([clock(.04)]), true);
  const snapshot = flush(audio, reports);
  group(snapshot.anchored, empty());
  group(snapshot.continuing, { ...empty(), batches: 1, admitted: 1, clock_only_batches: 1 });
});

test("legacy, malformed, backward, resource and unavailable-context refusals do not enter timing statistics", t => {
  const { audio, reports } = setup(t);
  assert.equal(audio.prepareBatch([control(1), clock(1)]), true);
  flush(audio, reports);
  const legacy = { kind: "controlChange", control: 7, value: 80 };
  assert.equal(audio.prepareBatch([legacy]), true);
  const refused = [null, [control(1)], [control(NaN), clock(1)], [legacy, clock(1)],
    [clock(1), clock(1)], [control(.9), clock(1.1)], [clock(.9)],
    [{ kind: "playWave", at: 1, channels: 1, rate: 0, samples: Float32Array.of(1) }, clock(1)],
    [{ kind: "playWave", at: 1, framePhase: 1e9, samples: Float32Array.of(1) }, clock(1)],
    [...Array.from({ length: 513 }, () => ({ kind: "noteOn", at: 1, velocity: 100 })), clock(1)],
  ];
  for (const events of refused) assert.equal(audio.prepareBatch(events), false);
  const context = audio.context;
  audio.context = null;
  t.mock.method(audio, "ensure", () => null);
  assert.equal(audio.prepareBatch([clock(1.1)]), null);
  audio.context = context;
  audio.context.state = "suspended";
  assert.equal(audio.prepareBatch([clock(1.1)]), null);
  audio.context.state = "running";
  assert.equal(audio.prepareBatch([clock(1.1)]), false, "interrupted output must await reconstruction");
  audio.reportTiming();
  assert.equal(reports.length, 1, "non-window outcomes must not fabricate diagnostic batches");
});

test("clearTiming discards the log aggregate while preserving existing sources and their anchor", t => {
  const { audio, reports, sources } = setup(t);
  const note = at => ({ kind: "noteOn", note: 60 + Math.round(at * 100), velocity: 100, at });
  assert.equal(playAudioEvents(audio, [note(0), clock(0)]), true);
  audio.clearTiming();
  audio.reportTiming();
  assert.deepEqual(reports, []);
  assert.equal(playAudioEvents(audio, [note(.01), clock(.01)]), true);
  assert.equal(sources.length, 2);
  assert.deepEqual(sources[0].stops, []);
  close(sources[1].starts[0][0] - sources[0].starts[0][0], .01);
  const snapshot = flush(audio, reports);
  group(snapshot.anchored, empty());
  group(snapshot.continuing, { ...empty(), batches: 1, admitted: 1, events: 1, not_late: 1 });
});

const word = value => [value >>> 24, value >>> 16 & 255, value >>> 8 & 255, value & 255];
const seconds = value => {
  const bytes = new Uint8Array(8);
  new DataView(bytes.buffer).setFloat64(0, value);
  return [...bytes];
};
const wire = events => {
  const bytes = [0x57, 0x46, 0x41, 0x32, 0x07, ...word(7)];
  for (const event of events) {
    if (event.kind === "allOff") bytes.push(0x06);
    else if (event.kind === "clock") bytes.push(0x0c, ...seconds(event.at));
    else {
      bytes.push(0x0b, 1, ...seconds(event.at));
      if (event.kind === "noteOn") bytes.push(0x01, 0, event.note, event.velocity);
      else bytes.push(0x04, 0, event.control, event.value);
    }
  }
  return Uint8Array.from(bytes).buffer;
};

async function openSession(t, audio) {
  const listeners = new Map();
  const socket = { readyState: 1, sent: [], close() {},
    addEventListener(kind, callback) { listeners.set(kind, [...(listeners.get(kind) || []), callback]); },
    send(message) { this.sent.push(JSON.parse(message)); },
    deliver(message) {
      const data = message instanceof ArrayBuffer ? message : JSON.stringify(message);
      for (const callback of listeners.get("message") || []) callback({ data });
    } };
  const previous = { WebSocket: globalThis.WebSocket, location: globalThis.location };
  globalThis.WebSocket = function () { return socket; };
  globalThis.location = { protocol: "https:", host: "example.test" };
  t.after(() => Object.assign(globalThis, previous));
  const session = new GameSession({ onAudio: events => playAudioEvents(audio, events) });
  t.after(() => session.close());
  const opening = session.open();
  while (!session.socket) await new Promise(resolve => setImmediate(resolve));
  socket.deliver({ kind: "ready", profile: "debug" });
  await opening;
  socket.deliver({ kind: "started", epoch: 7, started: {} });
  return { socket, session };
}

for (const format of ["JSON", "WFA2"]) {
  test(`${format} late reception records one refusal through recovery and suppresses stale batches`, async t => {
    const { audio, reports, sources } = setup(t);
    const { socket, session } = await openSession(t, audio);
    const send = events => socket.deliver(format === "WFA2" ? wire(events) : { kind: "audio", epoch: 7, audio: events });
    const note = at => ({ kind: "noteOn", sound: 7, channel: 0, note: 60, velocity: 100, at });
    send([note(0), clock(0)]);
    assert.equal(sources.length, 1);
    audio.context.currentTime = 1.3;
    const late = [note(.01), control(.02), clock(.02)];
    send(late);
    assert.deepEqual(socket.sent, [{ kind: "audioResume", epoch: 7 }]);
    assert.equal(sources.length, 1, "rejected history must not start a partial source");
    assert.equal(sources[0].stops.length, 1, "recovery must cancel old output");
    send(late);
    send(late);
    assert.equal(socket.sent.length, 1);
    assert.equal(sources.length, 1);
    send([{ kind: "allOff" }, note(.02), clock(.02)]);
    assert.equal(session.audioResetPending, false);
    assert.equal(sources.length, 2);
    close(sources[1].starts[0][0], 1.4);
    const snapshot = flush(audio, reports);
    group(snapshot.anchored, { ...empty(), batches: 2, admitted: 2, events: 2, not_late: 2 });
    group(snapshot.continuing, { ...empty(), batches: 1, refused: { late: 1, future: 0, range: 0 },
      events: 2, late_gt_100ms: 2, max_event_late_ms: 190, max_frontier_late_ms: 180 });
  });
}
