// The two page-owned volume controls and where they are remembered.
//
// These are Host preferences, not volume values a game writes through its
// media API. They belong to the browser that produces the sound, so they stay
// in local storage beside the keypad and vibration settings.

import { local } from "./storage.js";

export const DEFAULT_VOLUME = 50;

const keys = {
  midi: "wfeature:volume:midi",
  pcm: "wfeature:volume:pcm",
};

const volume = value => {
  if (value === null || value === "") return null;
  const number = Number(value);
  return Number.isFinite(number) && number >= 0 && number <= 100 ? number : null;
};

export const createAudioSettings = (storage = local) => ({
  stored(source) {
    const key = keys[source];
    if (!key) return DEFAULT_VOLUME;
    try {
      return volume(storage?.getItem(key)) ?? DEFAULT_VOLUME;
    } catch {
      return DEFAULT_VOLUME;
    }
  },

  remember(source, value) {
    const key = keys[source];
    const level = volume(value);
    if (!storage || !key || level === null) return false;
    try {
      return storage?.setItem(key, String(level)) !== false;
    } catch {
      return false;
    }
  },
});

// initAudioSettings restores the controls before applying them to the audio
// graph. The graph itself is lazy, but PageAudio keeps these values until its
// first user gesture creates that graph.
export const initAudioSettings = ({ document: doc, audio, storage = local } = {}) => {
  const midi = doc?.getElementById("volume-midi");
  const pcm = doc?.getElementById("volume-pcm");
  if (!midi || !pcm || !audio) return false;

  const settings = createAudioSettings(storage);
  midi.value = String(settings.stored("midi"));
  pcm.value = String(settings.stored("pcm"));

  const apply = () => {
    audio.setMIDIVolume(Number(midi.value) / 100);
    audio.setWaveVolume(Number(pcm.value) / 100);
  };
  midi.addEventListener("input", () => {
    apply();
    settings.remember("midi", midi.value);
  });
  pcm.addEventListener("input", () => {
    apply();
    settings.remember("pcm", pcm.value);
  });
  apply();
  return true;
};
