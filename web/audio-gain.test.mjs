import assert from "node:assert/strict";
import { test } from "node:test";
import { PageAudio } from "./audio.js";

const setup = t => {
  let wall = 0, created = 0;
  t.mock.method(performance, "now", () => wall * 1000);
  const sources = [];
  const parameter = () => ({
    value: 1, events: [],
    setValueAtTime(value, at) { this.events.push(["set", value, at]); },
    exponentialRampToValueAtTime(value, at) { this.events.push(["ramp", value, at]); },
    cancelScheduledValues(at) { this.events = this.events.filter(event => event[2] < at); },
  });
  const node = () => ({
    connections: [], disconnects: 0,
    connect(target) { this.connections.push(target); },
    disconnect() { this.disconnects++; },
  });
  const source = () => {
    const current = {
      ...node(), frequency: parameter(), starts: [], stops: [],
      start(...args) { this.starts.push(args); },
      stop(...args) { this.stops.push(args); },
    };
    sources.push(current);
    return current;
  };
  class Context {
    constructor() { created++; }
    state = "running";
    currentTime = 1;
    baseLatency = 0.01;
    sampleRate = 48000;
    destination = {};
    createGain() { return { ...node(), gain: parameter() }; }
    createOscillator = source;
    createBufferSource = source;
    createStereoPanner() { return { ...node(), pan: parameter() }; }
    createBiquadFilter() { return { ...node(), frequency: parameter() }; }
    createBuffer(channels, frames) {
      const data = Array.from({ length: channels }, () => new Float32Array(frames));
      return { numberOfChannels: channels, getChannelData: channel => data[channel] };
    }
  }
  const previous = Object.getOwnPropertyDescriptor(globalThis, "AudioContext");
  Object.defineProperty(globalThis, "AudioContext", { configurable: true, value: Context });
  t.after(() => {
    if (previous) Object.defineProperty(globalThis, "AudioContext", previous);
    else delete globalThis.AudioContext;
  });
  const audio = new PageAudio();
  audio._noise = {};
  return { audio, sources, created: () => created, advance: seconds => { wall += seconds; audio.context.currentTime += seconds; } };
};

const sourceState = sources => sources.map(source => ({
  starts: structuredClone(source.starts), stops: structuredClone(source.stops), disconnects: source.disconnects,
}));

const pathFrom = source => {
  const path = [];
  for (let node = source; node; node = node.connections?.[0]) path.push(node);
  return path;
};

test("a clock reset cannot let an older queued gain undo the latest mute", t => {
  const { audio } = setup(t);
  audio.noteOn(0, 60, 100, 11);
  audio.playWave(1, 8000, new Float32Array(8000), 11);
  audio.context.baseLatency = 0.09;
  audio.scheduleAnchor = { audio: 1.17, wall: 0 };
  audio.setSoundGain(11, 2500);
  const gains = audio.soundGains.get(11);
  assert.equal(gains.midi.gain.events.at(-1)[2], 1.17);
  audio.context.state = "suspended";
  audio.context.onstatechange();
  audio.setSoundGain(11, 0);
  for (const node of [gains.midi, gains.wave]) {
    assert.deepEqual(node.gain.events, [["set", 0, 1.09]], "the stale future gain must be cancelled");
  }
});

test("gain received before the first gesture applies when MIDI and PCM start", t => {
  const { audio, sources, created } = setup(t);
  audio.setSoundGain(11, 5000);
  audio.setSoundGain(11, 2500);
  audio.setSoundGain(22, 0);
  assert.equal(audio.context, null);
  assert.equal(created(), 0, "a guest level alone must not activate the speaker");

  audio.noteOn(0, 60, 100, 11);
  audio.playWave(1, 8000, new Float32Array([0.2, -0.2]), 11);
  audio.noteOn(0, 60, 100, 22);
  audio.playWave(1, 8000, new Float32Array([0.2, -0.2]), 22);
  assert.equal(created(), 1);
  for (const [sound, level] of [[11, 0.25], [22, 0]]) {
    const gains = audio.soundGains.get(sound);
    assert.equal(gains.midi.gain.value, level);
    assert.equal(gains.wave.gain.value, level);
  }
  assert.equal(sources.length, 4, "muted sources still advance for a future unmute");
  assert.equal(audio.voices.size, 2);
  assert.ok(sources.every(source => source.starts.length === 1));
});

test("live gain changes hold MIDI and PCM positions and leave another clip untouched", t => {
  const { audio, sources, advance } = setup(t);
  for (const sound of [11, 22]) {
    audio.noteOn(0, 60, 100, sound);
    audio.playWave(1, 8000, new Float32Array([0.2, -0.2, 0.4, -0.4]), sound);
  }
  const current = sourceState(sources);
  const voices = [...audio.voices.values()];
  const envelopes = voices.map(voice => structuredClone(voice.gain.gain.events));
  const first = audio.soundGains.get(11), second = audio.soundGains.get(22);
  const buffers = [sources[1].buffer, sources[3].buffer];
  for (const value of [0, 2500, 10000]) {
    advance(0.125);
    audio.setSoundGain(11, value);
    assert.equal(first.midi.gain.events.at(-1)[1], value / 10000);
    assert.deepEqual(first.wave.gain.events.at(-1), first.midi.gain.events.at(-1), "both routes change at one playback instant");
    assert.equal(second.midi.gain.value, 1);
    assert.equal(second.wave.gain.value, 1);
    assert.deepEqual(second.midi.gain.events, []);
    assert.deepEqual(second.wave.gain.events, []);
    assert.equal(sources.length, 4);
    assert.deepEqual(sourceState(sources), current, "gain changes cannot restart or stop a source");
    assert.deepEqual([...audio.voices.values()], voices);
    assert.deepEqual(voices.map(voice => voice.gain.gain.events), envelopes, "gain changes cannot rewrite the envelope");
    assert.equal(sources[1].buffer, buffers[0]);
    assert.equal(sources[3].buffer, buffers[1]);
  }
  assert.ok(pathFrom(sources[0]).includes(first.midi));
  assert.ok(pathFrom(sources[1]).includes(first.wave));
  assert.ok(pathFrom(sources[2]).includes(second.midi));
  assert.ok(pathFrom(sources[3]).includes(second.wave));
});

test("released melodic tails and percussion share their clip's live gain", t => {
  const { audio, sources, advance } = setup(t);
  for (const sound of [11, 22]) {
    audio.noteOn(0, 60, 100, sound);
    advance(0.01);
    audio.noteOff(0, 60, 0, sound);
    audio.noteOn(9, 40, 100, sound);
    audio.playWave(1, 8000, new Float32Array(8000), sound);
  }
  assert.equal(audio.voices.size, 2, "released melodic tails are outside the note map");
  assert.equal(audio.sources.size, 6);
  const initial = sourceState(sources);
  const release = structuredClone(sources[0].connections[0].gain.events);
  const gains = audio.soundGains.get(11);
  assert.ok(pathFrom(sources[0]).includes(gains.midi));
  assert.ok(pathFrom(sources[1]).includes(gains.midi));
  assert.ok(pathFrom(sources[2]).includes(gains.wave));
  for (const value of [2500, 0, 10000]) {
    advance(0.005);
    audio.setSoundGain(11, value);
    assert.equal(gains.midi.gain.events.at(-1)[1], value / 10000);
    assert.equal(gains.wave.gain.events.at(-1)[1], value / 10000);
    assert.equal(audio.sources.size, 6);
    assert.deepEqual(sourceState(sources), initial, "natural percussion and release stops stay in place");
    assert.deepEqual(sources[0].connections[0].gain.events, release);
    assert.deepEqual(audio.soundGains.get(22).midi.gain.events, []);
    assert.deepEqual(audio.soundGains.get(22).wave.gain.events, []);
  }
});

test("guest gain is applied before the user's unchanged mixer sliders", t => {
  const { audio, sources } = setup(t);
  audio.setMasterVolume(0.7);
  audio.setMIDIVolume(0.3);
  audio.setWaveVolume(0.4);
  audio.noteOn(0, 60, 100, 11);
  audio.playWave(1, 8000, new Float32Array(8000), 11);
  const gains = audio.soundGains.get(11);
  assert.deepEqual(pathFrom(sources[0]).slice(-4), [gains.midi, audio.midiGain, audio.master, audio.context.destination]);
  assert.deepEqual(pathFrom(sources[1]).slice(-4), [gains.wave, audio.waveGain, audio.master, audio.context.destination]);
  for (const value of [0, 2500, 10000]) audio.setSoundGain(11, value);
  assert.deepEqual([audio.masterVolume, audio.midiVolume, audio.waveVolume], [0.7, 0.3, 0.4]);
  assert.deepEqual([audio.master.gain.value, audio.midiGain.gain.value, audio.waveGain.gain.value], [0.7, 0.3, 0.4]);
  const guestEvents = structuredClone(gains.midi.gain.events);
  audio.setMasterVolume(0.2);
  audio.setMIDIVolume(0.1);
  audio.setWaveVolume(0.8);
  assert.equal(gains.level, 1);
  assert.deepEqual(gains.midi.gain.events, guestEvents);
  assert.deepEqual(gains.wave.gain.events, guestEvents);
});

test("clip stop and timeline reset discard gain nodes and pending levels", t => {
  const { audio } = setup(t);
  audio.setMIDIVolume(0.3);
  audio.setWaveVolume(0.4);
  for (const sound of [11, 22]) {
    audio.setSoundGain(sound, 2500);
    audio.noteOn(0, 60, 100, sound);
    audio.playWave(1, 8000, new Float32Array(8000), sound);
  }
  audio.setSoundGain(33, 0);
  const first = audio.soundGains.get(11), second = audio.soundGains.get(22);
  audio.stopSound(11);
  audio.stopSound(11);
  assert.equal(first.midi.disconnects, 1);
  assert.equal(first.wave.disconnects, 1);
  assert.equal(second.midi.disconnects, 0);
  assert.equal(second.wave.disconnects, 0);
  assert.equal(audio.soundGains.has(11), false);
  audio.stopSound(33);
  assert.equal(audio.soundGains.has(33), false, "pending levels end with the clip");

  audio.noteOn(0, 60, 100, 11);
  audio.playWave(1, 8000, new Float32Array(8000), 11);
  const replacement = audio.soundGains.get(11);
  assert.notEqual(replacement.midi, first.midi);
  assert.notEqual(replacement.wave, first.wave);
  assert.equal(replacement.midi.gain.value, 1);
  assert.equal(replacement.wave.gain.value, 1);
  audio.setSoundGain(33, 5000);
  audio.stopAll();
  audio.stopAll();
  assert.equal(audio.soundGains.size, 0);
  for (const gains of [first, second, replacement]) {
    assert.equal(gains.midi.disconnects, 1);
    assert.equal(gains.wave.disconnects, 1);
  }
  assert.equal(audio.midiVolume, 0.3);
  assert.equal(audio.waveVolume, 0.4);
});

test("guest gain clamps to silence and unity for legacy owner zero too", t => {
  const { audio } = setup(t);
  audio.noteOn(0, 60, 100);
  audio.playWave(1, 8000, new Float32Array(8000));
  for (const [value, expected] of [[-1, 0], [10001, 1]]) {
    audio.setSoundGain(0, value);
    const gains = audio.soundGains.get(0);
    assert.equal(gains.level, expected);
    assert.equal(gains.midi.gain.events.at(-1)[1], expected);
    assert.equal(gains.wave.gain.events.at(-1)[1], expected);
  }
});
