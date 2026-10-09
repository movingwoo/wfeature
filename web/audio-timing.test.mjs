import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";
import { playAudioEvents } from "./session.js";

const setup = t => {
  let wall = 0;
  t.mock.method(performance, "now", () => wall * 1000);
  const sources = [];
  const parameter = () => ({
    value: 1, events: [],
    setValueAtTime(value, at) { this.events.push(["set", value, at]); },
    exponentialRampToValueAtTime(value, at) { this.events.push(["ramp", value, at]); },
    linearRampToValueAtTime(value, at) { this.events.push(["linear", value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[2] < at); },
  });
  const node = () => ({
    connections: [], disconnects: 0,
    connect(target) { this.connections.push(target); },
    disconnect() { this.disconnects++; },
  });
  const source = () => {
    const current = { ...node(), frequency: parameter(), stops: [],
      start(at) { this.started = at; }, stop(at) { this.stopped = at; this.stops.push(at); } };
    sources.push(current);
    return current;
  };
  const audio = new PageAudio();
  audio.context = {
    state: "running", currentTime: 1, baseLatency: 0.09, sampleRate: 48000,
    createGain: () => ({ ...node(), gain: parameter() }),
    createStereoPanner: () => ({ ...node(), pan: parameter() }),
    createOscillator: source, createBufferSource: source,
    createBuffer: (channels, frames) => ({ numberOfChannels: channels, getChannelData: () => new Float32Array(frames) }),
    createBiquadFilter: () => ({ ...node(), frequency: parameter() }),
  };
  audio.midiGain = {};
  audio.waveGain = {};
  audio._noise = {};
  return { audio, sources, wall: value => { wall = value; } };
};

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} != ${expected}`);

test("an overdue post-restore gate recovers without replaying stale output", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, [
    { kind: "allOff" },
    { kind: "noteResume", sound: 1, note: 60, velocity: 100, age: 100, at: 0 },
    { kind: "clock", at: 0 },
  ]), true);
  const old = sources[0];
  let stopped = false;
  const stop = old.stop;
  old.stop = function (at) { stopped = true; stop.call(this, at); };
  audio.context.currentTime += .01;
  assert.equal(playAudioEvents(audio, [
    { kind: "noteOff", sound: 1, note: 60, at: -.005839489 },
    { kind: "clock", at: .01 },
  ]), false);
  assert.equal(sources.length, 1, "rejected history must not create new sources");
  assert.equal(stopped, false, "rejected history must not partially change output");
  assert.equal(playAudioEvents(audio, [
    { kind: "allOff" },
    { kind: "noteResume", sound: 2, note: 64, velocity: 90, age: 110, at: .01 },
    { kind: "clock", at: .01 },
  ]), true);
  assert.equal(audio.voices.size, 1);
  assert.equal([...audio.voices.values()][0].sound, 2);
  assert.equal(stopped, true, "global reset must cancel old sources immediately");
  close(sources[1].started, audio.context.currentTime + .1);
});

test("separate arrivals retain their spacing within one large output buffer", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  wall(0.025);
  audio.noteOn(0, 64, 100);
  wall(0.050);
  audio.noteOff(0, 60);
  close(sources[1].started - sources[0].started, 0.025);
  close(sources[0].stopped - sources[0].started, 0.050 + 0.060 + 0.010);
  assert.ok(sources[0].started > audio.context.currentTime);
  wall(0.090);
  audio.context.currentTime += 0.09;
  audio.noteOn(0, 67, 100);
  close(sources[2].started - sources[0].started, 0.090);
});

test("MIDI, percussion, PCM and bends share the scheduling clock", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  const start = sources[0].started;
  wall(0.025);
  audio.pitchBend(0, 9000);
  close(sources[0].frequency.events.at(-1)[2] - start, 0.025);
  wall(0.05);
  audio.noteOn(9, 40, 100);
  close(sources[1].started - start, 0.05);
  wall(0.075);
  audio.playWave(1, 8000, new Float32Array(800));
  close(sources[2].started - start, 0.075);
});

test("a short note released before rendering retains its attack and decay", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  const voice = audio.voices.get("0:60");
  const start = sources[0].started;
  const peak = 100 / 127 * 0.25;
  wall(0.005);
  audio.noteOff(0, 60);
  const hold = voice.gain.gain.events.find(event => Math.abs(event[2] - (start + 0.005)) < 1e-9);
  assert.equal(hold[0], "ramp", "retain the attack segment before the release");
  close(hold[1], Math.sqrt(0.0001 * peak));
  assert.ok(hold[1] > 0.0001 && hold[1] < 1, "use the envelope, not the initial AudioParam.value");
});

test("a release within decay holds the scheduled envelope level", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  const voice = audio.voices.get("0:60");
  const start = sources[0].started;
  const peak = 100 / 127 * 0.25;
  wall(0.065);
  audio.noteOff(0, 60);
  const hold = voice.gain.gain.events.find(event => Math.abs(event[2] - (start + 0.065)) < 1e-9);
  close(hold[1], peak * Math.sqrt(0.7));
  assert.equal(voice.gain.gain.events[1][1], peak, "retain the completed attack");
});

test("an output stall and a reset cannot leave scheduling far ahead", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  wall(60);
  audio.noteOn(0, 64, 100);
  assert.ok(sources[1].started - audio.context.currentTime <= 0.2);
  audio.stopAll();
  audio.context.currentTime = 20;
  wall(100);
  audio.noteOn(0, 67, 100);
  assert.ok(sources[2].started >= 20 && sources[2].started <= 20.2);
  assert.equal(sources[0].stopped, undefined, "reset cancels even a source scheduled in the future");
});

test("a clock that falls behind is reanchored and low latency stays bounded", t => {
  const { audio, sources, wall } = setup(t);
  audio.context.baseLatency = 0.005;
  audio.noteOn(0, 60, 100);
  assert.ok(sources[0].started - 1 <= 0.02);
  wall(0.5);
  audio.context.currentTime = 1.6;
  audio.noteOn(0, 64, 100);
  assert.ok(sources[1].started >= 1.6 && sources[1].started <= 1.62);
});

test("trimming clock drift does not move a release back before its attack", t => {
  const { audio, sources, wall } = setup(t);
  audio.noteOn(0, 60, 100);
  wall(0.08);
  audio.noteOn(0, 64, 100);
  wall(0.105);
  audio.noteOff(0, 64);
  assert.ok(sources[1].stopped - sources[1].started >= 0.089,
    "a 5 ms clock correction must not erase the entire 25 ms note");
});

const clock = at => ({ kind: "clock", at });
const attack = (at, note = 60, sound = 7, channel = 0) => ({ kind: "noteOn", at, note, sound, channel, velocity: 100 });
const release = (at, note = 60, sound = 7) => ({ kind: "noteOff", at, note, sound });
const controller = (at, control, value, sound = 7) => ({ kind: "controlChange", at, control, value, sound });

test("coarse and fine packets schedule the same short gates, PCM and controller automation", t => {
  const events = [
    { kind: "programChange", at: 0, sound: 7, program: 72 },
    { kind: "soundGain", at: 0, sound: 7, value: 10000 },
    attack(0), release(0.005), attack(0.010, 64),
    { kind: "playWave", at: 0.020, sound: 7, channels: 1, rate: 8000, samples: new Float32Array(800) },
    { kind: "pitchBend", at: 0.025, sound: 7, value: 10000 },
    { kind: "soundGain", at: 0.030, sound: 7, value: 5000 },
    release(0.045, 64), attack(0.050, 67),
    controller(0.055, 64, 127), release(0.060, 67),
    controller(0.065, 7, 64), controller(0.070, 11, 100),
    controller(0.075, 10, 0), controller(0.080, 64, 0),
    { kind: "soundGain", at: 0.085, sound: 7, value: 0 },
    { kind: "soundGain", at: 0.090, sound: 7, value: 10000 },
  ];
  const run = coarse => {
    const result = setup(t);
    assert.equal(playAudioEvents(result.audio, [clock(0)]), true);
    if (coarse) {
      result.wall(0.091);
      assert.equal(playAudioEvents(result.audio, [...events, clock(0.09)]), true);
    } else {
      for (const [index, event] of events.entries()) {
        result.wall(index * 0.003);
        assert.equal(playAudioEvents(result.audio, [event, clock(event.at)]), true);
      }
    }
    return result;
  };
  const coarse = run(true), fine = run(false);
  const output = ({ audio, sources }) => ({
    sources: sources.map(source => ({
      start: source.started, stops: source.stops, type: source.type,
      frequency: source.frequency.events, envelope: source.connections[0].gain.events,
    })),
    channelGain: audio.channelOutputs.get(7)[0].gain.gain.events,
    pan: audio.channelOutputs.get(7)[0].panner.pan.events,
    midiGain: audio.soundGains.get(7).midi.gain.events,
    waveGain: audio.soundGains.get(7).wave.gain.events,
  });
  assert.deepEqual(output(coarse), output(fine), "packet grouping and arrival spacing cannot change the authored timeline");
  const [short, longer, pcm, sustained] = coarse.sources;
  close(short.started, 1.1);
  close(short.stopped - short.started, 0.005 + 0.060 + 0.010);
  close(longer.stopped - longer.started, 0.035 + 0.060 + 0.010);
  close(pcm.started - short.started, 0.020);
  close(sustained.stopped - sustained.started, 0.030 + 0.060 + 0.010);
  const attackRelease = short.connections[0].gain.events.find(event => Math.abs(event[2] - 1.105) < 1e-9);
  assert.equal(attackRelease[0], "ramp");
  close(attackRelease[1], Math.sqrt(0.0001 * (100 / 127 * 0.25)));
  const gain = coarse.audio.soundGains.get(7);
  assert.deepEqual(gain.midi.gain.events, gain.wave.gain.events);
  assert.deepEqual(gain.midi.gain.events.map(event => event[1]), [0.5, 0, 1]);
  close(gain.midi.gain.events[0][2], 1.130);
  close(short.frequency.events[0][2], 1.125);
  close(coarse.audio.channelOutputs.get(7)[0].panner.pan.events.at(-1)[2], 1.180);
});

test("a timed stop preserves preceding envelopes and isolates the restarted owner's graph", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, [clock(0)]), true);
  assert.equal(playAudioEvents(audio, [
    attack(0), attack(0, 69, 8),
    { kind: "playWave", at: 0.005, sound: 7, channels: 1, rate: 8000, samples: new Float32Array(8000) },
    attack(0.008, 40, 7, 9), release(0.010), clock(0.010),
  ]), true);
  const oldSources = [sources[0], sources[2], sources[3]];
  const oldGains = audio.soundGains.get(7);
  const oldChannels = audio.channelOutputs.get(7);
  const envelope = structuredClone(sources[0].connections[0].gain.events);
  assert.equal(playAudioEvents(audio, [
    { kind: "stopSound", at: 0.040, sound: 7 },
    { kind: "programChange", at: 0.041, sound: 7, program: 72 },
    { kind: "soundGain", at: 0.041, sound: 7, value: 5000 },
    attack(0.045, 62),
    { kind: "playWave", at: 0.046, sound: 7, channels: 1, rate: 8000, samples: new Float32Array(8000) },
    clock(0.046),
  ]), true);
  for (const source of oldSources) {
    close(source.stopped, 1.14);
    assert.equal(source.disconnects, 0, "a future stop must leave earlier scheduled audio connected");
  }
  assert.deepEqual(sources[0].connections[0].gain.events, envelope, "stop cannot erase earlier attack or release ramps");
  assert.equal(oldGains.midi.disconnects, 0);
  assert.equal(oldGains.wave.disconnects, 0);
  assert.equal(oldChannels[0].gain.disconnects, 0);
  assert.notEqual(audio.soundGains.get(7), oldGains);
  assert.notEqual(audio.channelOutputs.get(7), oldChannels);
  assert.equal(sources[4].type, "sine");
  assert.equal(audio.soundGains.get(7).midi.gain.value, 0.5);
  const oldStops = oldSources.map(source => [...source.stops]);
  assert.equal(playAudioEvents(audio, [
    { kind: "pitchBend", at: 0.047, sound: 7, value: 9999 },
    controller(0.048, 120, 0), clock(0.048),
  ]), true);
  assert.deepEqual(oldSources.map(source => source.stops), oldStops, "new controllers cannot retarget a retired generation");
  assert.deepEqual(sources[0].frequency.events, []);
  assert.deepEqual(sources[1].stops, [], "the peer remains held");
  oldSources[0].onended();
  oldSources[1].onended();
  assert.equal(oldGains.midi.disconnects, 0, "retain the shared graph while its percussion is still pending");
  oldSources[2].onended();
  for (const source of oldSources) {
    assert.equal(source.disconnects, 1);
    source.onended();
    assert.equal(source.disconnects, 1, "stale callbacks cannot disconnect twice");
  }
  assert.equal(oldGains.midi.disconnects, 1);
  assert.equal(oldGains.wave.disconnects, 1);
  assert.equal(oldChannels[0].gain.disconnects, 1);
  assert.equal(oldChannels[9].gain.disconnects, 1);
  assert.equal(audio.soundGains.get(7).midi.disconnects, 0);
  assert.equal(audio.retiredOutputs.size, 0);
});

test("repeated timed restarts reclaim each retired graph", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, [clock(0)]), true);
  for (let index = 0; index < 100; index++) {
    const at = index / 1000;
    assert.equal(playAudioEvents(audio, [attack(at), { kind: "stopSound", at: at + 0.0005, sound: 7 }, clock(at + 0.0005)]), true);
    assert.equal(audio.retiredOutputs.size, 1);
    sources.at(-1).onended();
    assert.equal(audio.retiredOutputs.size, 0);
    assert.equal(audio.sources.size, 0);
    assert.equal(audio.channelOutputs.size, 0);
    assert.equal(audio.soundGains.size, 0);
  }
});

test("expired percussion cannot steal a held melody while onended still awaits the render clock", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, [clock(0)]), true);
  const events = [attack(0, 69, 8)];
  for (let note = 30; note < 53; note++) events.push(attack(0.001, note, 7, 9));
  events.push(attack(0.202, 70, 7, 9), clock(0.202));
  assert.equal(playAudioEvents(audio, events), true);
  assert.equal(sources.length, 25);
  assert.deepEqual(sources[0].stops, [], "expired drum slots must be removed before stealing the oldest live melody");
  assert.equal(audio.voices.size, 2);
  assert.equal(audio.voices.get("8:0:69").source, sources[0]);
  assert.equal(audio.voices.get("7:9:70").source, sources[24]);
  assert.equal(audio.sources.size, 25, "scheduled sources remain connected until their real end callbacks");
  for (const source of sources.slice(1, 24)) source.onended();
  assert.equal(audio.sources.size, 2);
});

test("a timed stop never postpones a source's earlier natural or released end", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, [clock(0)]), true);
  assert.equal(playAudioEvents(audio, [attack(0), release(0.005), attack(0.010, 40, 7, 9), clock(0.010)]), true);
  const ends = sources.map(source => source.stopped);
  assert.equal(playAudioEvents(audio, [{ kind: "stopSound", at: 0.220, sound: 7 }, clock(0.220)]), true);
  assert.deepEqual(sources.map(source => source.stopped), ends);
  assert.ok(sources.every(source => source.disconnects === 0), "logical expiry cannot disconnect audio still ahead of the render clock");
  for (const source of sources) source.onended();
  assert.equal(audio.retiredOutputs.size, 0);
});

const invalidBatches = [
  ["late", () => [attack(0.060, 62), clock(0.060)], 1.3],
  ["too far ahead", () => [attack(0.300, 62), clock(0.300)]],
  ["backward frontier", () => [clock(0.040)]],
  ["backward event between batches", () => [attack(0.040, 62), clock(0.060)]],
  ["backward event within a batch", () => [attack(0.060, 62), release(0.055, 62), clock(0.070)]],
  ["missing event time", () => [attack(0.060, 62), { kind: "soundGain", sound: 7, value: 0 }, clock(0.070)]],
  ["event beyond frontier", () => [attack(0.080, 62), clock(0.070)]],
  ["missing frontier", () => [attack(0.060, 62)]],
  ["negative frontier", () => [clock(-1)]],
  ["nonfinite frontier", () => [attack(0.060, 62), clock(Infinity)]],
  ["nonfinite event", () => [attack(0.060, 62), attack(NaN, 64), clock(0.070)]],
  ["duplicate frontier", () => [attack(0.060, 62), clock(0.060), clock(0.070)]],
  ["oversized", () => [attack(0.060, 62), ...Array(65536).fill(controller(0.060, 7, 0)), clock(0.060)]],
];
for (const [name, events, current] of invalidBatches) {
  test(`${name}: a timed batch is refused before any sound or controller is applied`, t => {
    const { audio, sources } = setup(t);
    assert.equal(playAudioEvents(audio, [clock(0)]), true);
    assert.equal(playAudioEvents(audio, [attack(0.050), clock(0.050)]), true);
    const anchor = audio.presentationAnchor;
    const channel = audio.channelOutputs.get(7)[0];
    const envelope = structuredClone(sources[0].connections[0].gain.events);
    if (current !== undefined) audio.context.currentTime = current;
    assert.equal(playAudioEvents(audio, events()), false);
    assert.equal(sources.length, 1);
    assert.deepEqual(sources[0].stops, []);
    assert.deepEqual(sources[0].connections[0].gain.events, envelope);
    assert.deepEqual(channel.gain.gain.events, []);
    assert.equal(audio.presentationAnchor, anchor);
    assert.equal(audio.presentationFrontier, 0.050);
  });
}

test("clock-only batches anchor later notes without creating or retiming sources", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, []), true);
  assert.equal(playAudioEvents(audio, [clock(0)]), true);
  const anchor = audio.presentationAnchor;
  assert.equal(playAudioEvents(audio, [clock(0.080)]), true);
  assert.equal(sources.length, 0);
  assert.equal(audio.presentationAnchor, anchor);
  assert.equal(playAudioEvents(audio, [attack(0.085), clock(0.090)]), true);
  close(sources[0].started, 1.185);
});

test("signed pending deadlines after a reset retain their original gate", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, [{ kind: "allOff" }, attack(-0.040), release(-0.005), clock(0)]), true);
  close(sources[0].started, 1.060);
  close(sources[0].stopped - sources[0].started, 0.035 + 0.060 + 0.010);
});

for (const state of ["suspended", "interrupted"]) {
  test(`${state}: timed output is skipped and requires reconstruction after returning`, t => {
    const { audio, sources } = setup(t);
    assert.equal(playAudioEvents(audio, [attack(0), clock(0)]), true);
    audio.context.state = state;
    assert.equal(playAudioEvents(audio, [attack(0.010, 62), clock(0.010)]), undefined);
    assert.equal(sources.length, 1);
    assert.equal(sources[0].stopped, undefined);
    assert.equal(sources[0].disconnects, 1);
    assert.equal(audio.sources.size, 0);
    assert.equal(audio.presentationAnchor, null);
    audio.context.state = "running";
    assert.equal(playAudioEvents(audio, [attack(0.020, 64), clock(0.020)]), false);
    assert.equal(sources.length, 1);
    assert.equal(playAudioEvents(audio, [
      { kind: "allOff" },
      { kind: "noteResume", at: 0.020, sound: 7, note: 67, velocity: 100, age: 80 },
      clock(0.020),
    ]), true);
    assert.equal(sources.length, 2);
    close(sources[1].started, 1.1);
    close(audio.voices.get("7:0:67").started, 1.02);
  });
}

test("all-off immediately cancels future and retired sources before replacing the timeline", t => {
  const { audio, sources } = setup(t);
  assert.equal(playAudioEvents(audio, [clock(0)]), true);
  assert.equal(playAudioEvents(audio, [attack(0), { kind: "stopSound", at: 0.030, sound: 7 }, attack(0.040, 64), clock(0.040)]), true);
  const obsolete = [...sources];
  assert.equal(audio.retiredOutputs.size, 1);
  assert.ok(obsolete.every(source => source.started > audio.context.currentTime && source.disconnects === 0));
  assert.equal(playAudioEvents(audio, [attack(0.050, 62), { kind: "allOff" }, attack(0, 67), clock(0)]), true);
  assert.equal(sources.length, 3, "events preceding the last all-off belong to the obsolete timeline");
  for (const source of obsolete) {
    assert.equal(source.stopped, undefined);
    assert.equal(source.disconnects, 1);
    source.onended();
    assert.equal(source.disconnects, 1);
  }
  assert.equal(audio.sources.size, 1);
  assert.equal(audio.voices.get("7:0:67").source, sources[2]);
  assert.equal(audio.retiredOutputs.size, 0);
  close(sources[2].started, 1.1);
  assert.equal(playAudioEvents(audio, [{ kind: "allOff" }]), true);
  assert.equal(audio.sources.size, 0);
  assert.equal(audio.presentationAnchor, null);
});
