// Sound for the page.
//
// The engine hands us the two things a SMAF file decodes into: MIDI messages
// and sampled waveforms. Waveforms are easy — Web Audio plays a buffer. MIDI
// is not, because MIDI is a score, and playing a score needs instruments.
//
// The obvious answer is a soundfont synthesiser, and the Rust build takes it:
// a worklet plus a 10.5MB sound bank fetched at page load, which on iOS pushes
// the tab past its memory ceiling and gets skipped entirely. This build takes
// the other answer and synthesises the notes here, from oscillators, with no
// download at all. That is a real trade — a soundfont piano sounds like a
// piano and this does not — but these are 2000s handset games whose music was
// written for an FM chip with a handful of operators, so an oscillator with an
// envelope lands closer to the original than it would for most music, and it
// costs nothing to ship and works on every device.

const MAX_VOICES = 24;
const DRUM_CHANNEL = 9;

// General MIDI program families, reduced to the oscillator that carries each
// one best. The program number's top three bits pick the family.
const FAMILY_WAVES = [
  "triangle", // 0-7   piano
  "triangle", // 8-15  chromatic percussion
  "sawtooth", // 16-23 organ
  "triangle", // 24-31 guitar
  "sine", //     32-39 bass
  "sawtooth", // 40-47 strings
  "sawtooth", // 48-55 ensemble
  "square", //   56-63 brass
  "square", //   64-71 reed
  "sine", //     72-79 pipe
  "sawtooth", // 80-87 synth lead
  "triangle", // 88-95 synth pad
  "sine", //     96-103 synth effects
  "triangle", // 104-111 ethnic
  "square", //  112-119 percussive
  "sine", //    120-127 sound effects
];

export class PageAudio {
  constructor() {
    this.context = null;
    this.master = null;
    // Melody and sound effects get their own gain so the page's two sliders
    // can trade them off: on these games the music is continuous and the
    // effects are sharp, and wanting one quieter than the other is normal.
    this.midiGain = null;
    this.waveGain = null;
    // One entry per MIDI channel: its program, volume, expression, pan, and
    // pitch bend, which is the state a note needs at the moment it starts.
    this.channels = Array.from({ length: 16 }, () => ({
      program: 0,
      volume: 100,
      expression: 127,
      pan: 64,
      bend: 8192,
    }));
    // voices are the notes currently sounding, keyed "channel:note" so a
    // note off finds the voice it belongs to.
    this.voices = new Map();
    this.masterVolume = 0.7;
    this.midiVolume = 0.5;
    this.waveVolume = 0.5;
    this.failed = false;
  }

  // ensure builds the audio graph on first use. Browsers refuse to start an
  // AudioContext before a user gesture, so this is called from the same paths
  // that already require one — starting a game, pressing a key.
  ensure() {
    if (this.failed) return null;
    if (!this.context) {
      const AudioContextClass = globalThis.AudioContext || globalThis.webkitAudioContext;
      if (!AudioContextClass) {
        this.failed = true;
        return null;
      }
      try {
        this.context = new AudioContextClass();
        this.master = this.context.createGain();
        this.master.gain.value = this.masterVolume;
        this.master.connect(this.context.destination);
        this.midiGain = this.context.createGain();
        this.midiGain.gain.value = this.midiVolume;
        this.midiGain.connect(this.master);
        this.waveGain = this.context.createGain();
        this.waveGain.gain.value = this.waveVolume;
        this.waveGain.connect(this.master);
      } catch (error) {
        console.warn("audio unavailable, the game will be silent:", error);
        this.failed = true;
        return null;
      }
    }
    if (this.context.state === "suspended") {
      this.context.resume().catch(() => {});
    }
    return this.context;
  }

  setMasterVolume(value) {
    this.masterVolume = Math.max(0, Math.min(1, value));
    if (this.master) this.master.gain.value = this.masterVolume;
  }

  setMIDIVolume(value) {
    this.midiVolume = Math.max(0, Math.min(1, value));
    if (this.midiGain) this.midiGain.gain.value = this.midiVolume;
  }

  setWaveVolume(value) {
    this.waveVolume = Math.max(0, Math.min(1, value));
    if (this.waveGain) this.waveGain.gain.value = this.waveVolume;
  }

  // channelGain answers how loud a note on this channel should be, folding the
  // channel volume and expression the way a MIDI device does.
  channelGain(channel) {
    const state = this.channels[channel & 15];
    return (state.volume / 127) * (state.expression / 127);
  }

  noteFrequency(channel, note) {
    const state = this.channels[channel & 15];
    // Two semitones of bend range either way, which is the MIDI default.
    const bendSemitones = ((state.bend - 8192) / 8192) * 2;
    return 440 * Math.pow(2, (note + bendSemitones - 69) / 12);
  }

  noteOn(channel, note, velocity) {
    if (velocity === 0) {
      this.noteOff(channel, note, 0);
      return;
    }
    const context = this.ensure();
    if (!context) return;
    if (this.voices.size >= MAX_VOICES) {
      // Steal the oldest voice rather than refusing the note: a dropped note
      // is more noticeable than a shortened one.
      const oldest = this.voices.keys().next().value;
      this.stopVoice(oldest, 0.01);
    }

    const key = `${channel}:${note}`;
    this.stopVoice(key, 0.005);

    const now = context.currentTime;
    const gain = context.createGain();
    const peak = Math.max(0.0001, (velocity / 127) * this.channelGain(channel) * 0.25);
    const panner = context.createStereoPanner ? context.createStereoPanner() : null;
    if (panner) {
      panner.pan.value = (this.channels[channel & 15].pan - 64) / 64;
      gain.connect(panner);
      panner.connect(this.midiGain);
    } else {
      gain.connect(this.midiGain);
    }

    let source;
    if ((channel & 15) === DRUM_CHANNEL) {
      // Percussion is noise, not pitch: a short burst whose length stands in
      // for the kit piece. Anything else would need samples.
      source = context.createBufferSource();
      source.buffer = this.noiseBuffer(context);
      const bandpass = context.createBiquadFilter();
      bandpass.type = "bandpass";
      // Higher drum keys are the smaller pieces, so they ring higher.
      bandpass.frequency.value = 200 + (note % 24) * 180;
      source.connect(bandpass);
      bandpass.connect(gain);
      gain.gain.setValueAtTime(peak, now);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.18);
      source.start(now);
      source.stop(now + 0.2);
      this.voices.set(key, { source, gain, drum: true });
      source.onended = () => this.voices.delete(key);
      return;
    }

    source = context.createOscillator();
    source.type = FAMILY_WAVES[(this.channels[channel & 15].program >> 3) & 15];
    source.frequency.value = this.noteFrequency(channel, note);
    source.connect(gain);
    // A short attack and a decay to a sustain level: enough shape that notes
    // are distinguishable without clicking at the edges.
    gain.gain.setValueAtTime(0.0001, now);
    gain.gain.exponentialRampToValueAtTime(peak, now + 0.01);
    gain.gain.exponentialRampToValueAtTime(Math.max(0.0001, peak * 0.7), now + 0.12);
    source.start(now);
    this.voices.set(key, { source, gain, drum: false });
  }

  noteOff(channel, note) {
    this.stopVoice(`${channel}:${note}`, 0.06);
  }

  // stopVoice releases a note over a short ramp. Cutting the gain outright
  // would click.
  stopVoice(key, release) {
    const voice = this.voices.get(key);
    if (!voice) return;
    this.voices.delete(key);
    if (voice.drum) return;
    const now = this.context.currentTime;
    try {
      voice.gain.gain.cancelScheduledValues(now);
      voice.gain.gain.setValueAtTime(Math.max(0.0001, voice.gain.gain.value), now);
      voice.gain.gain.exponentialRampToValueAtTime(0.0001, now + release);
      voice.source.stop(now + release + 0.01);
    } catch {
      // A voice already stopped throws; nothing to do about it.
    }
  }

  noiseBuffer(context) {
    if (!this._noise) {
      const length = Math.floor(context.sampleRate * 0.25);
      this._noise = context.createBuffer(1, length, context.sampleRate);
      const samples = this._noise.getChannelData(0);
      for (let index = 0; index < length; index += 1) samples[index] = Math.random() * 2 - 1;
    }
    return this._noise;
  }

  programChange(channel, program) {
    this.channels[channel & 15].program = program & 127;
  }

  controlChange(channel, control, value) {
    const state = this.channels[channel & 15];
    switch (control) {
      case 7:
        state.volume = value;
        break;
      case 10:
        state.pan = value;
        break;
      case 11:
        state.expression = value;
        break;
      case 120:
      case 123:
        // All sound off and all notes off both mean "stop this channel now".
        for (const key of [...this.voices.keys()]) {
          if (key.startsWith(`${channel}:`)) this.stopVoice(key, 0.02);
        }
        break;
      default:
        break;
    }
  }

  pitchBend(channel, value) {
    const state = this.channels[channel & 15];
    state.bend = value;
    for (const [key, voice] of this.voices) {
      if (!key.startsWith(`${channel}:`) || voice.drum) continue;
      const note = Number(key.split(":")[1]);
      try {
        voice.source.frequency.setValueAtTime(this.noteFrequency(channel, note), this.context.currentTime);
      } catch {
        // Ignore a voice that ended between the lookup and the write.
      }
    }
  }

  sysex() {
    // Device-specific setup with no device to configure.
  }

  // playWave plays a decoded sample. The engine hands it over already
  // normalised, so this only has to wrap it in a buffer.
  playWave(channels, samplingRate, samples) {
    const context = this.ensure();
    if (!context || !samples || samples.length === 0) return;
    const frameCount = Math.floor(samples.length / Math.max(1, channels));
    if (frameCount === 0) return;
    const buffer = context.createBuffer(Math.max(1, channels), frameCount, samplingRate);
    for (let channel = 0; channel < buffer.numberOfChannels; channel += 1) {
      const target = buffer.getChannelData(channel);
      for (let frame = 0; frame < frameCount; frame += 1) {
        target[frame] = samples[frame * buffer.numberOfChannels + channel];
      }
    }
    const source = context.createBufferSource();
    source.buffer = buffer;
    const gain = context.createGain();
    gain.gain.value = 0.8;
    source.connect(gain);
    gain.connect(this.waveGain);
    source.start();
  }

  // stopAll silences everything, which the page does when a game ends.
  stopAll() {
    for (const key of [...this.voices.keys()]) this.stopVoice(key, 0.02);
  }
}
