import assert from "node:assert/strict";
import { test } from "node:test";

import { DEFAULT_VOLUME, createAudioSettings, initAudioSettings } from "./audio-settings.js";

const storage = entries => {
  const values = new Map(Object.entries(entries ?? {}));
  return {
    values,
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, String(value)),
  };
};

test("audio volumes survive a new page controller", () => {
  const backing = storage();
  const first = createAudioSettings(backing);

  assert.equal(first.remember("midi", 0), true);
  assert.equal(first.remember("pcm", 73), true);

  const reloaded = createAudioSettings(backing);
  assert.equal(reloaded.stored("midi"), 0);
  assert.equal(reloaded.stored("pcm"), 73);
});

test("absent or invalid audio volumes use the slider default", () => {
  const settings = createAudioSettings(storage({
    "wfeature:volume:midi": "loud",
    "wfeature:volume:pcm": "101",
  }));

  assert.equal(settings.stored("midi"), DEFAULT_VOLUME);
  assert.equal(settings.stored("pcm"), DEFAULT_VOLUME);
  assert.equal(settings.stored("unknown"), DEFAULT_VOLUME);
  assert.equal(settings.remember("midi", -1), false);
  assert.equal(settings.remember("unknown", 50), false);
  assert.equal(createAudioSettings(null).remember("midi", 50), false);
});

test("denied storage leaves audio at the default without throwing", () => {
  const denied = {
    getItem: () => { throw new Error("storage denied"); },
    setItem: () => { throw new Error("storage denied"); },
  };
  const settings = createAudioSettings(denied);

  assert.equal(settings.stored("midi"), DEFAULT_VOLUME);
  assert.equal(settings.remember("midi", 25), false);
});

test("the controls restore, apply and remember both sound sources", () => {
  const backing = storage({
    "wfeature:volume:midi": "0",
    "wfeature:volume:pcm": "73",
  });
  const control = () => {
    const listeners = new Map();
    return {
      value: "50",
      addEventListener: (name, listener) => listeners.set(name, listener),
      input(value) {
        this.value = String(value);
        listeners.get("input")();
      },
    };
  };
  const midi = control();
  const pcm = control();
  const document = {
    getElementById: id => ({ "volume-midi": midi, "volume-pcm": pcm })[id] ?? null,
  };
  const applied = { midi: null, pcm: null };
  const audio = {
    setMIDIVolume: value => { applied.midi = value; },
    setWaveVolume: value => { applied.pcm = value; },
  };

  assert.equal(initAudioSettings({ document, audio, storage: backing }), true);
  assert.equal(midi.value, "0");
  assert.equal(pcm.value, "73");
  assert.deepEqual(applied, { midi: 0, pcm: 0.73 });

  midi.input(25);
  pcm.input(90);
  assert.equal(backing.values.get("wfeature:volume:midi"), "25");
  assert.equal(backing.values.get("wfeature:volume:pcm"), "90");
  assert.deepEqual(applied, { midi: 0.25, pcm: 0.9 });
});
