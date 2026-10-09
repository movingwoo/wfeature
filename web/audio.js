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
const MAX_AUDIO_EVENTS = 65536;
const MAX_PCM_CHANNELS = 2048;
// Retained native sources include scheduled starts and release/retired tails.
// A full output reconstruction needs at most 256 PCM sources and 24 notes.
const MAX_AUDIO_SOURCES = 512;
// Charge float payload per live wave reference, even when its buffer is shared.
// This covers the backend's 32 MiB PCM replay with mono-to-stereo fallback.
const MAX_LIVE_WAVE_BYTES = 128 << 20;
// Cache policy, separate from live source limits. The byte ceiling is the
// wire's 8 MiB of signed 16-bit definitions represented as Float32 samples.
const MAX_WAVE_BUFFERS = 256;
const MAX_WAVE_BUFFER_BYTES = 16 << 20;
const DRUM_CHANNEL = 9;
const CONTROL_RAMP = 0.005;
const voiceKey = (sound, channel, note) => sound ? `${sound}:${channel}:${note}` : `${channel}:${note}`;
const channelDefaults = () => ({
  program: 0, volume: 100, expression: 127, pan: 64, bend: 8192, sustain: 0,
  bendRange: 2, bendRangeCents: 0,
  rpnMSB: 127, rpnLSB: 127, nrpnMSB: 127, nrpnLSB: 127, parameterKind: null,
});
const pcmDefaults = () => ({ volume: 127, expression: 127, pan: 64 });
const validPCMChannel = channel => Number.isInteger(channel) && channel > 0 && channel <= 0xffff;
const validPCMOwner = sound => Number.isInteger(sound) && sound >= 0 && sound <= 0xffffffff;
const validPCMControl = (control, value) => [7, 10, 11].includes(control) && Number.isInteger(value) && value >= 0 && value <= 127;
const wavePayloadBytes = (channels, length) => {
  if (!Number.isInteger(channels) || channels < 1 || channels > 32 ||
      !Number.isSafeInteger(length) || length < 0) return null;
  return Math.floor(length / channels) * channels * Float32Array.BYTES_PER_ELEMENT;
};
const validWaveRate = rate => Number.isInteger(rate) && rate > 0 && rate <= 0xffffffff;
const validFramePhase = phase => Number.isInteger(phase) && phase >= 0 && phase < 1_000_000_000;
const waveSampleLength = samples => {
  if (typeof samples !== "string") return samples?.length ?? 0;
  // The JSON wire carries base64 int16 samples. Account without decoding or
  // allocating them; optional whitespace only makes this estimate larger.
  let bytes = Math.floor(samples.length * 3 / 4);
  if (samples.endsWith("=")) bytes--;
  if (samples.endsWith("==")) bytes--;
  return Math.floor(bytes / 2);
};
const pcmLevels = state => {
  const gain = (state.volume / 127) ** 2 * (state.expression / 127) ** 2;
  const angle = Math.PI * state.pan / 254;
  return [gain * Math.cos(angle), gain * Math.sin(angle)];
};
const controlRamp = level => ({ from: level, level, at: -Infinity, linear: false });
const rampControl = (parameter, previous, level, at, current) => {
  let from, linear = false;
  if (at < previous.at) {
    // A clock correction can place this change before older queued changes.
    // Discard unrendered automation and hold the actual output-clock value.
    from = parameter.value;
    parameter.cancelScheduledValues(current);
    parameter.setValueAtTime(from, current);
  } else {
    const progress = Math.min(1, (at - previous.at) / CONTROL_RAMP);
    from = previous.from + (previous.level - previous.from) * progress;
    // Retain a ramp's elapsed portion when cancelling its future endpoint,
    // including another controller arriving at this same scheduled instant.
    linear = at <= previous.at + CONTROL_RAMP && (at > previous.at || previous.linear);
    parameter.cancelScheduledValues(at);
  }
  if (linear) parameter.linearRampToValueAtTime(from, at);
  else parameter.setValueAtTime(from, at);
  parameter.linearRampToValueAtTime(level, at + CONTROL_RAMP);
  return { from, level, at, linear };
};
const melodyLevel = (peak, elapsed) => {
  const sustain = Math.max(0.0001, peak * 0.7);
  return elapsed < 0.01 ? 0.0001 * Math.pow(peak / 0.0001, elapsed / 0.01)
    : elapsed < 0.12 ? peak * Math.pow(sustain / peak, (elapsed - 0.01) / 0.11) : sustain;
};
const drumLevel = (peak, elapsed) => elapsed < 0.18 ? peak * Math.pow(0.0001 / peak, elapsed / 0.18) : 0.0001;
const timingCounts = () => ({
  batches: 0, clock_only_batches: 0, admitted: 0, refused: { late: 0, future: 0, range: 0 },
  events: 0, not_late: 0, late_le_5ms: 0, late_le_20ms: 0,
  late_le_100ms: 0, late_gt_100ms: 0, invalid_events: 0,
  max_event_late_ms: 0, max_frontier_late_ms: 0,
});

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
  constructor({ report = () => {}, diagnostics = () => false } = {}) {
    this.report = report;
    this.diagnostics = diagnostics;
    this.timing = null;
    this.activationCheck = null;
    this.recovering = false;
    this.context = null;
    this.scheduleAnchor = null;
    this.presentationAnchor = null;
    this.presentationFrontier = null;
    this.presentationLastTime = null;
    this.presentationInterrupted = false;
    this.scheduledTime = null;
    this.master = null;
    // Melody and sound effects get their own gain so the page's two sliders
    // can trade them off: on these games the music is continuous and the
    // effects are sharp, and wanting one quieter than the other is normal.
    this.midiGain = null;
    this.waveGain = null;
    // Channel state is available before activation. Output nodes are lazy
    // and shared by held notes, percussion and melodic release tails.
    this.channels = Array.from({ length: 16 }, channelDefaults);
    this.soundChannels = new Map();
    this.channelOutputs = new Map();
    // ATR groups are independent of MIDI channels and physical PCM channels.
    // Keep controller state while idle, but retain nodes only for live sources.
    this.pcmChannels = new Map();
    this.pcmChannelCount = 0;
    this.pcmOutputs = new Map();
    this.soundGains = new Map();
    // Sound identity scopes channels and note keys, including equal pitches
    // played by separate clips. Owner zero supports legacy streams.
    this.voices = new Map();
    // Includes PCM, percussion and melodic release tails after noteOff.
    this.sources = new Map();
    this.liveWaveBytes = 0;
    this.retiredOutputs = new Set();
    this.waveBufferKeys = new WeakMap();
    this.waveBuffers = new Map();
    this.waveBufferBytes = 0;
    this.waveBufferContext = null;
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
        this.context.onstatechange = () => {
          this.scheduleAnchor = null;
          if (this.presentationAnchor && this.context.state !== "running") {
            this.stopAll();
            this.presentationInterrupted = true;
          }
          this.reportState("state changed");
        };
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
    if (!this.recovering && (this.context.state === "suspended" || this.context.state === "interrupted")) {
      this.context.resume().catch(error => this.report(`audio resume failed: ${error.message}`));
    }
    return this.context;
  }

  reportState(reason) {
    this.report(`audio ${reason}: state ${this.context.state}, time ${this.context.currentTime}`);
  }

  // Sample the render deadline at batch admission, before source creation or
  // JSON PCM decoding. This does not measure network or audible output delay.
  // Only fixed counters survive the call; no event/sample payload is retained.
  recordTiming(events, anchor, current, fresh, refused) {
    if (!this.diagnostics()) {
      this.timing = null;
      return;
    }
    this.timing ??= { anchored: timingCounts(), continuing: timingCounts() };
    const counts = fresh ? this.timing.anchored : this.timing.continuing;
    counts.batches++;
    if (events.length === 1) counts.clock_only_batches++;
    if (refused) counts.refused[refused]++;
    else counts.admitted++;
    // Visit the entire valid batch even when its first event missed a bound.
    // The final clock is a frontier, not another musical/controller event.
    for (const event of events) {
      const at = anchor.audio + (event.at - anchor.presentation);
      const late = Math.max(0, (current - at) * 1000);
      if (event.kind === "clock") {
        if (Number.isFinite(late)) counts.max_frontier_late_ms = Math.max(counts.max_frontier_late_ms, late);
        continue;
      }
      counts.events++;
      if (!Number.isFinite(at) || at < 0 || !Number.isFinite(late)) {
        counts.invalid_events++;
        continue;
      }
      counts.max_event_late_ms = Math.max(counts.max_event_late_ms, late);
      if (late === 0) counts.not_late++;
      else if (late <= 5) counts.late_le_5ms++;
      else if (late <= 20) counts.late_le_20ms++;
      else if (late <= 100) counts.late_le_100ms++;
      else counts.late_gt_100ms++;
    }
  }

  // The existing session statistics/report actions flush this into the debug
  // page log. Release capture is disabled and keeps no timing accumulator.
  reportTiming() {
    const timing = this.timing;
    this.timing = null;
    if (this.diagnostics() && timing) this.report(`audio playout timing: ${JSON.stringify(timing)}`);
  }

  clearTiming() {
    this.timing = null;
  }

  // Called on gestures and foreground return, never for each MIDI event.
  // WebKit can report running while its render clock is stalled. Check the
  // clock, not the signal level: an intentional rest or muted volume is valid.
  activate() {
    const context = this.ensure();
    if (!context || this.activationCheck !== null || this.recovering) return;
    this.reportState("activation");
    const time = context.currentTime;
    this.activationCheck = setTimeout(() => {
      this.activationCheck = null;
      if (globalThis.document?.hidden || context.state !== "running" || context.currentTime !== time) return;
      this.reportState("clock stalled; restarting output");
      this.recovering = true;
      // One attempt per activation. Do not loop if the OS still owns audio,
      // replace the graph, or replay notes accumulated while it was stopped.
      context.suspend().then(() => {
        if (!globalThis.document?.hidden) return context.resume();
      }).catch(error => this.report(`audio recovery failed: ${error.message}`))
        .finally(() => { this.recovering = false; });
    }, 500);
  }

  foreground() {
    // A page opened only to browse the library must not create an audio graph.
    if (this.context) this.activate();
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

  channelsFor(sound = 0) {
    if (!sound) return this.channels;
    let channels = this.soundChannels.get(sound);
    if (!channels) {
      channels = Array.from({ length: 16 }, channelDefaults);
      this.soundChannels.set(sound, channels);
    }
    return channels;
  }

  channelGain(channel, sound = 0) {
    const state = this.channelsFor(sound)[channel & 15];
    return (state.volume / 127) * (state.expression / 127);
  }

  channelOutputFor(channel, sound) {
    channel &= 15;
    let outputs = this.channelOutputs.get(sound);
    if (!outputs) {
      outputs = new Array(16);
      this.channelOutputs.set(sound, outputs);
    }
    if (!outputs[channel]) {
      const gain = this.context.createGain();
      const level = this.channelGain(channel, sound);
      gain.gain.value = level;
      const pan = (this.channelsFor(sound)[channel].pan - 64) / 64;
      const panner = this.context.createStereoPanner ? this.context.createStereoPanner() : null;
      if (panner) {
        panner.pan.value = pan;
        gain.connect(panner);
        panner.connect(this.outputFor(sound, "midi"));
      } else {
        gain.connect(this.outputFor(sound, "midi"));
      }
      outputs[channel] = { gain, panner, gainRamp: controlRamp(level), panRamp: controlRamp(pan) };
    }
    return outputs[channel];
  }

  disconnectChannelOutputs(sound) {
    for (const output of this.channelOutputs.get(sound) || []) {
      output?.gain.disconnect?.();
      output?.panner?.disconnect?.();
    }
    this.channelOutputs.delete(sound);
  }

  pcmStateFor(channel, sound) {
    let channels = this.pcmChannels.get(sound);
    if (channels?.has(channel)) return channels.get(channel);
    if (this.pcmChannelCount >= MAX_PCM_CHANNELS) return null;
    if (!channels) this.pcmChannels.set(sound, channels = new Map());
    const state = pcmDefaults();
    channels.set(channel, state);
    this.pcmChannelCount++;
    return state;
  }

  clearPCMChannels(sound) {
    this.pcmChannelCount -= this.pcmChannels.get(sound)?.size || 0;
    this.pcmChannels.delete(sound);
  }

  pcmOutputFor(channel, sound) {
    let outputs = this.pcmOutputs.get(sound);
    if (outputs?.has(channel)) return outputs.get(channel);
    const state = this.pcmStateFor(channel, sound);
    if (!state) return null;
    const levels = pcmLevels(state);
    let left, right, merger;
    try {
      left = this.context.createGain();
      right = this.context.createGain();
      merger = this.context.createChannelMerger(2);
      left.gain.value = levels[0];
      right.gain.value = levels[1];
      left.connect(merger, 0, 0);
      right.connect(merger, 0, 1);
      merger.connect(this.outputFor(sound, "wave"));
    } catch (error) {
      left?.disconnect?.();
      right?.disconnect?.();
      merger?.disconnect?.();
      throw error;
    }
    const output = { left, right, merger, sound, channel, references: 0,
      leftRamp: controlRamp(levels[0]), rightRamp: controlRamp(levels[1]) };
    if (!outputs) this.pcmOutputs.set(sound, outputs = new Map());
    outputs.set(channel, output);
    return output;
  }

  disconnectPCMOutput(output) {
    if (output.disconnected) return;
    output.disconnected = true;
    output.left.disconnect?.();
    output.right.disconnect?.();
    output.merger.disconnect?.();
    const outputs = this.pcmOutputs.get(output.sound);
    // A retired generation must not remove its replacement's group.
    if (outputs?.get(output.channel) === output) {
      outputs.delete(output.channel);
      if (outputs.size === 0) this.pcmOutputs.delete(output.sound);
    }
  }

  pcmControl(channel, control, value, sound = 0) {
    if (!validPCMChannel(channel) || !validPCMOwner(sound) || !validPCMControl(control, value)) return;
    const state = this.pcmStateFor(channel, sound);
    if (!state) return;
    state[control === 7 ? "volume" : control === 11 ? "expression" : "pan"] = value;
    const output = this.pcmOutputs.get(sound)?.get(channel);
    if (!output) return;
    const [left, right] = pcmLevels(state);
    const now = this.eventTime();
    output.leftRamp = rampControl(output.left.gain, output.leftRamp, left, now, this.context.currentTime);
    output.rightRamp = rampControl(output.right.gain, output.rightRamp, right, now, this.context.currentTime);
  }

  // Preflight controller metadata and the complete resulting group budget,
  // including stops. Refuse a bad batch before changing any MIDI or PCM state.
  preparePCMChannels(events, reset) {
    const owners = new Map();
    let count = 0;
    if (!reset) {
      for (const [sound, channels] of this.pcmChannels) {
        owners.set(sound, new Set(channels.keys()));
        count += channels.size;
      }
    }
    for (const event of events) {
      if (!event || typeof event !== "object") return false;
      const sound = event.sound ?? 0;
      if (event.kind === "stopSound") {
        count -= owners.get(sound)?.size || 0;
        owners.delete(sound);
        continue;
      }
      if (event.kind === "allOff") {
        owners.clear();
        count = 0;
        continue;
      }
      const channel = event.pcmChannel;
      if (event.kind === "pcmControl") {
        const value = event.value === undefined ? 0 : event.value;
        if (!validPCMChannel(channel) || !validPCMOwner(sound) || !validPCMControl(event.control, value)) return false;
      } else if (event.kind === "playWave" && channel !== undefined) {
        if (channel === 0) continue;
        if (!validPCMChannel(channel) || !validPCMOwner(sound) || (event.channels ?? 1) !== 1) return false;
      } else continue;
      let channels = owners.get(sound);
      if (!channels) owners.set(sound, channels = new Set());
      if (!channels.has(channel)) {
        if (++count > MAX_PCM_CHANNELS) return false;
        channels.add(channel);
      }
    }
    return true;
  }

  // A whole-batch refusal leaves controllers and the previous output intact.
  // Only immediate owner stops reclaim capacity inside a batch. Scheduled
  // sources can still be audible until their onended callbacks release them.
  prepareSources(events, reset, timed) {
    let count = 0, bytes = 0;
    const owners = new Map();
    const reserve = (sound, payload) => {
      const owner = owners.get(sound) || { count: 0, bytes: 0 };
      owner.count++;
      owner.bytes += payload;
      owners.set(sound, owner);
      count++;
      bytes += payload;
    };
    if (!reset) {
      for (const voice of this.sources.values()) reserve(voice.sound, voice.waveBytes || 0);
    }
    for (const event of events) {
      const sound = event.sound === undefined ? 0 : event.sound;
      if (!validPCMOwner(sound)) return false;
      let payload = 0;
      if (event.kind === "allOff") {
        owners.clear();
        count = bytes = 0;
        continue;
      } else if (event.kind === "stopSound") {
        if (!timed) {
          const owner = owners.get(sound);
          count -= owner?.count || 0;
          bytes -= owner?.bytes || 0;
          owners.delete(sound);
        }
        continue;
      } else if (event.kind === "noteOn" || event.kind === "noteResume") {
        const age = event.kind === "noteResume" ? (event.age ?? 0) : 0;
        if (!Number.isFinite(age) || age < 0) return false;
        if ((event.velocity ?? 0) === 0 || ((event.channel ?? 0) & 15) === DRUM_CHANNEL && age >= 200) continue;
      } else if (event.kind === "playWave") {
        if (!validFramePhase(event.framePhase === undefined ? 0 : event.framePhase)) return false;
        if (!event.samples) continue;
        payload = wavePayloadBytes(event.channels ?? 1, waveSampleLength(event.samples));
        if (payload === null || !validWaveRate(event.rate ?? 8000)) return false;
        if (payload === 0) continue;
      } else continue;
      reserve(sound, payload);
      if (count > MAX_AUDIO_SOURCES || bytes > MAX_LIVE_WAVE_BYTES) return false;
    }
    return true;
  }

  // Guest device and clip gain changes apply after each source's envelope,
  // before the user's separate MIDI/PCM sliders. Muting keeps sources alive
  // so raising the level does not replay an attack or sample prefix.
  setSoundGain(sound, value) {
    const level = Math.max(0, Math.min(10000, Number(value) || 0)) / 10000;
    const state = this.soundGains.get(sound) || { level: 1 };
    state.level = level;
    this.soundGains.set(sound, state);
    if (!this.context || (!state.midi && !state.wave)) return;
    const now = this.eventTime();
    for (const node of [state.midi, state.wave]) {
      if (node) {
        node.gain.cancelScheduledValues(now);
        node.gain.setValueAtTime(level, now);
      }
    }
  }

  outputFor(sound, kind) {
    const state = this.soundGains.get(sound) || { level: 1 };
    if (!state[kind]) {
      state[kind] = this.context.createGain();
      state[kind].gain.value = state.level;
      state[kind].connect(kind === "midi" ? this.midiGain : this.waveGain);
    }
    this.soundGains.set(sound, state);
    return state[kind];
  }

  noteFrequency(channel, note, sound = 0) {
    const state = this.channelsFor(sound)[channel & 15];
    const bendSemitones = ((state.bend - 8192) / 8192) * (state.bendRange + state.bendRangeCents / 100);
    return 440 * Math.pow(2, (note + bendSemitones - 69) / 12);
  }

  // A timed batch ends with the server's current presentation clock. Check
  // the whole batch before scheduling any node: a delayed packet must recover
  // current output instead of compressing its history onto one render block.
  prepareBatch(events, { reset = false } = {}) {
    if (!Array.isArray(events) || events.length > MAX_AUDIO_EVENTS + 1) return false;
    const timed = events.some(event => event?.kind === "clock" || event?.at !== undefined);
    if (!this.preparePCMChannels(events, reset) || !this.prepareSources(events, reset, timed)) return false;
    if (!timed) {
      if (reset) this.stopAll();
      return true;
    }
    const clock = events.at(-1);
    if (clock?.kind !== "clock" || !Number.isFinite(clock.at) || clock.at < 0) return false;
    let last = reset ? null : this.presentationLastTime;
    for (let index = 0; index < events.length - 1; index++) {
      const event = events[index];
      if (!event || event.kind === "clock" || !Number.isFinite(event.at) || event.at > clock.at ||
          (last !== null && event.at < last)) return false;
      last = event.at;
    }
    if (!reset && this.presentationFrontier !== null && clock.at < this.presentationFrontier) return false;

    const context = this.context || this.ensure();
    if (!context) return null;
    if (context.state !== "running") {
      this.stopAll();
      this.presentationInterrupted = true;
      return null;
    }
    if (this.presentationInterrupted && !reset) return false;
    const current = context.currentTime;
    const latency = Number.isFinite(context.baseLatency) ? context.baseLatency : 0;
    const lead = Math.min(0.25, Math.max(0.1, latency));
    const anchor = (!reset && this.presentationAnchor) || { audio: current + lead, presentation: clock.at };
    let refused = null;
    for (const event of events) {
      const at = anchor.audio + (event.at - anchor.presentation);
      if (!Number.isFinite(at) || at < 0) refused = "range";
      else if (at < current - 0.005) refused = "late";
      else if (at > current + 0.35) refused = "future";
      if (refused) break;
    }
    this.recordTiming(events, anchor, current, reset || !this.presentationAnchor, refused);
    if (refused) return false;
    // Validate a reset before cancelling the old timeline, then install its
    // new anchor after stopAll has cleared the previous presentation state.
    if (reset) this.stopAll();
    this.presentationAnchor = anchor;
    this.presentationFrontier = clock.at;
    this.presentationLastTime = last;
    this.presentationInterrupted = false;
    return true;
  }

  // Scope one presentation timestamp across every source and controller call
  // made by the dispatcher, without changing the legacy arrival clock.
  playTimed(at, callback) {
    if (!this.presentationAnchor || !Number.isFinite(at)) return;
    const previous = this.scheduledTime;
    this.scheduledTime = this.presentationAnchor.audio + (at - this.presentationAnchor.presentation);
    try {
      return callback();
    } finally {
      this.scheduledTime = previous;
    }
  }

  // Some Android outputs advance currentTime in large blocks (about 90 ms
  // on the emulator). Using that value for every event rounds distinct notes
  // onto the same boundary and can cancel an attack before it is rendered.
  // Preserve arrival spacing with the monotonic page clock, anchored one
  // output buffer ahead. A short floor covers small render/main-thread races;
  // a cap and reanchoring keep a stalled or resumed output from building lag.
  eventTime() {
    if (this.scheduledTime !== null) return this.scheduledTime;
    const current = this.context.currentTime;
    const wall = performance.now() / 1000;
    const lead = Math.min(0.25, Math.max(0.01, this.context.baseLatency || 0));
    let at = this.scheduleAnchor && this.scheduleAnchor.audio + wall - this.scheduleAnchor.wall;
    if (at === null || at < current || this.context.state !== "running") {
      at = current + lead;
      this.scheduleAnchor = { audio: at, wall };
    } else if (at > current + lead + 0.1) {
      // Trim only the excess drift. Jumping back a whole buffer here could
      // move a short note's release in front of its already scheduled attack.
      at = current + lead + 0.1;
      this.scheduleAnchor = { audio: at, wall };
    }
    return at;
  }

  // Restored voices continue their envelope instead of replaying its attack.
  noteResume(channel, note, velocity, ageMilliseconds, sound = 0) {
    if (!Number.isFinite(ageMilliseconds) || ageMilliseconds < 0) return;
    this.noteOn(channel, note, velocity, sound, ageMilliseconds / 1000);
  }

  noteOn(channel, note, velocity, sound = 0, age = 0) {
    if (!Number.isFinite(age) || age < 0) return;
    channel &= 15;
    if (velocity === 0) {
      this.noteOff(channel, note, 0, sound);
      return;
    }
    const drum = (channel & 15) === DRUM_CHANNEL;
    if (drum && age >= 0.2) return;
    if (this.sources.size >= MAX_AUDIO_SOURCES) return false;
    const context = this.ensure();
    if (!context) return;
    const now = this.eventTime();
    // onended follows the render clock. A coarse batch may already have
    // passed a percussion voice's end on the score clock before that callback.
    for (const [key, voice] of this.voices) {
      if ((voice.end ?? Infinity) <= now) this.voices.delete(key);
    }
    if (this.voices.size >= MAX_VOICES) {
      // Steal the oldest voice rather than refusing the note: a dropped note
      // is more noticeable than a shortened one.
      const oldest = this.voices.keys().next().value;
      this.stopVoice(oldest, 0.01, now);
    }

    const key = voiceKey(sound, channel, note);
    this.stopVoice(key, 0.005, now);

    let source, gain, filter, voice;
    try {
      gain = context.createGain();
      const peak = Math.max(0.0001, (velocity / 127) * 0.25);
      gain.connect(this.channelOutputFor(channel, sound).gain);
      if (drum) {
        // Percussion is a short filtered noise burst.
        source = context.createBufferSource();
        source.buffer = this.noiseBuffer(context);
        filter = context.createBiquadFilter();
        filter.type = "bandpass";
        // Higher drum keys are the smaller pieces, so they ring higher.
        filter.frequency.value = 200 + (note % 24) * 180;
        source.connect(filter);
        filter.connect(gain);
        gain.gain.setValueAtTime(drumLevel(peak, age), now);
        if (age < 0.18) gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.18 - age);
      } else {
        source = context.createOscillator();
        source.type = FAMILY_WAVES[(this.channelsFor(sound)[channel & 15].program >> 3) & 15];
        source.frequency.value = this.noteFrequency(channel, note, sound);
        source.connect(gain);
        // A short attack and decay keep notes distinct without clicking.
        gain.gain.setValueAtTime(melodyLevel(peak, age), now);
        if (age < 0.01) gain.gain.exponentialRampToValueAtTime(peak, now + 0.01 - age);
        if (age < 0.12) gain.gain.exponentialRampToValueAtTime(Math.max(0.0001, peak * 0.7), now + 0.12 - age);
      }
      // A resumed envelope predates its source. Keep both times so a clock
      // correction cannot release it before its first rendered sample.
      voice = { source, gain, filter, kind: "midi", drum, started: now - age, sourceStart: now, peak, sound, channel, note };
      if (drum) voice.end = now + 0.2 - age;
      this.voices.set(key, voice);
      this.sources.set(source, voice);
      source.onended = () => this.finishSource(source, voice);
      if (drum && age > 0) source.start(now, age);
      else source.start(now);
      if (drum) source.stop(voice.end);
    } catch (error) {
      try { source?.stop(); } catch { /* The source may not have started. */ }
      if (voice) this.finishSource(source, voice);
      else {
        source?.disconnect?.();
        gain?.disconnect?.();
        filter?.disconnect?.();
      }
      throw error;
    }
  }

  noteOff(channel, note, velocity = 0, sound = 0, at) {
    channel &= 15;
    // Rhythm notes are one-shots; ordinary note-off must not remove them
    // from the shared voice budget before their natural end.
    if (channel === DRUM_CHANNEL) return;
    const key = voiceKey(sound, channel, note);
    const voice = this.voices.get(key);
    if (!voice) return;
    if (this.channelsFor(sound)[channel].sustain >= 64) {
      voice.deferred = true;
      return;
    }
    this.stopVoice(key, 0.06, at);
  }

  releaseDeferred(channel, sound, at) {
    for (const [key, voice] of this.voices) {
      if (voice.sound === sound && voice.channel === channel && voice.deferred) this.stopVoice(key, 0.06, at);
    }
  }

  silenceChannel(channel, sound, at) {
    for (const [key, voice] of this.voices) {
      if (voice.sound === sound && voice.channel === channel) this.voices.delete(key);
    }
    for (const [source, voice] of this.sources) {
      if (voice.retired || voice.kind !== "midi" || voice.sound !== sound || voice.channel !== channel) continue;
      voice.end = Math.min(at, voice.end ?? Infinity);
      // Stopping the source preserves the envelope before the cut. Removing
      // a future ramp endpoint or disconnecting now would change that audio.
      try { source.stop(voice.end); } catch { /* Already ended. */ }
    }
  }

  // stopVoice releases a note over a short ramp. Cutting the gain outright
  // would click.
  stopVoice(key, release, at) {
    const voice = this.voices.get(key);
    if (!voice) return;
    this.voices.delete(key);
    const now = Math.max(voice.sourceStart, at ?? this.eventTime());
    if (now >= (voice.end ?? Infinity)) return;
    try {
      // AudioParam.value describes the render clock, which may not have
      // reached this note yet. Recreate the envelope's value at the scheduled
      // release, retaining a partial attack/decay ramp before that boundary.
      const elapsed = now - voice.started;
      const level = voice.drum ? drumLevel(voice.peak, elapsed) : melodyLevel(voice.peak, elapsed);
      voice.gain.gain.cancelScheduledValues(now);
      if (elapsed > 0) voice.gain.gain.exponentialRampToValueAtTime(level, now);
      else voice.gain.gain.setValueAtTime(level, now);
      voice.gain.gain.exponentialRampToValueAtTime(0.0001, Math.min(now + release, voice.end ?? Infinity));
      voice.end = Math.min(now + release + 0.01, voice.end ?? Infinity);
      voice.source.stop(voice.end);
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

  programChange(channel, program, sound = 0) {
    this.channelsFor(sound)[channel & 15].program = program & 127;
  }

  controlChange(channel, control, value, sound = 0) {
    channel &= 15;
    const state = this.channelsFor(sound)[channel];
    const now = this.context ? this.eventTime() : 0;
    let retune = false;
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
      case 64:
        state.sustain = value;
        if (value < 64) this.releaseDeferred(channel, sound, now);
        break;
      case 101:
        state.rpnMSB = value;
        state.parameterKind = "rpn";
        break;
      case 100:
        state.rpnLSB = value;
        state.parameterKind = "rpn";
        break;
      case 99:
        state.nrpnMSB = value;
        state.parameterKind = "nrpn";
        break;
      case 98:
        state.nrpnLSB = value;
        state.parameterKind = "nrpn";
        break;
      case 6:
      case 38:
      case 96:
      case 97:
        if (state.parameterKind !== "rpn" || state.rpnMSB !== 0 || state.rpnLSB !== 0) break;
        if (control === 6) {
          state.bendRange = value;
          state.bendRangeCents = 0;
        } else if (control === 38) {
          state.bendRangeCents = Math.min(99, value);
        } else {
          // RPN 0 data increment/decrement is one cent; its value byte is
          // ignored. Carry cents into semitones and clamp both endpoints.
          const cents = Math.max(0, Math.min(12799, state.bendRange * 100 + state.bendRangeCents + (control === 96 ? 1 : -1)));
          state.bendRange = Math.floor(cents / 100);
          state.bendRangeCents = cents % 100;
        }
        retune = true;
        break;
      case 120:
        this.silenceChannel(channel, sound, now);
        break;
      case 121:
        state.expression = 127;
        state.sustain = 0;
        state.bend = 8192;
        state.rpnMSB = state.rpnLSB = state.nrpnMSB = state.nrpnLSB = 127;
        state.parameterKind = null;
        this.releaseDeferred(channel, sound, now);
        retune = true;
        break;
      case 123:
        for (const [key, voice] of this.voices) {
          if (voice.sound !== sound || voice.channel !== channel) continue;
          // All Notes Off releases rhythm tails too, independently of the
          // damper pedal; melodic notes follow the ordinary note-off path.
          if (voice.drum) this.stopVoice(key, 0.06, now);
          else this.noteOff(channel, voice.note, 0, sound, now);
        }
        break;
      default:
        break;
    }
    if (retune) this.retuneChannel(channel, sound, now);
    const output = this.channelOutputs.get(sound)?.[channel & 15];
    if (!output) return;
    if (control === 7 || control === 11 || control === 121) {
      output.gainRamp = rampControl(output.gain.gain, output.gainRamp, this.channelGain(channel, sound), now, this.context.currentTime);
    } else if (control === 10 && output.panner) {
      output.panRamp = rampControl(output.panner.pan, output.panRamp, (state.pan - 64) / 64, now, this.context.currentTime);
    }
  }

  pitchBend(channel, value, sound = 0) {
    channel &= 15;
    const state = this.channelsFor(sound)[channel];
    state.bend = value;
    const now = this.context ? this.eventTime() : 0;
    this.retuneChannel(channel, sound, now);
  }

  retuneChannel(channel, sound, at) {
    for (const [source, voice] of this.sources) {
      if (voice.retired || voice.kind !== "midi" || voice.sound !== sound || voice.channel !== channel || voice.drum || at >= (voice.end ?? Infinity)) continue;
      try {
        source.frequency.cancelScheduledValues?.(at);
        source.frequency.setValueAtTime(this.noteFrequency(channel, voice.note, sound), at);
      } catch {
        // Ignore a voice that ended between the lookup and the write.
      }
    }
  }

  sysex() {
    // Device-specific setup with no device to configure.
  }

  clearWaveBuffers() {
    this.waveBufferKeys = new WeakMap();
    this.waveBuffers.clear();
    this.waveBufferBytes = 0;
    this.waveBufferContext = null;
  }

  waveBuffer(context, channels, frames, rate, samples, cacheable) {
    if (cacheable && this.waveBufferContext !== context) {
      this.clearWaveBuffers();
      this.waveBufferContext = context;
    }
    let key = cacheable ? this.waveBufferKeys.get(samples) : undefined;
    const previous = this.waveBuffers.get(key);
    if (previous && previous.channels === channels && previous.frames === frames && previous.rate === rate) {
      this.waveBuffers.delete(key);
      this.waveBuffers.set(key, previous);
      return previous.buffer;
    }
    const buffer = context.createBuffer(channels, frames, rate);
    for (let channel = 0; channel < buffer.numberOfChannels; channel += 1) {
      const target = buffer.getChannelData(channel);
      for (let frame = 0; frame < frames; frame += 1) {
        target[frame] = samples[frame * buffer.numberOfChannels + channel];
      }
    }
    const bytes = buffer.numberOfChannels * frames * Float32Array.BYTES_PER_ELEMENT;
    if (cacheable && bytes <= MAX_WAVE_BUFFER_BYTES) {
      if (previous) {
        this.waveBuffers.delete(key);
        this.waveBufferBytes -= previous.bytes;
      }
      while (this.waveBuffers.size >= MAX_WAVE_BUFFERS || this.waveBufferBytes + bytes > MAX_WAVE_BUFFER_BYTES) {
        const oldest = this.waveBuffers.keys().next().value;
        this.waveBufferBytes -= this.waveBuffers.get(oldest).bytes;
        this.waveBuffers.delete(oldest);
      }
      if (!key) {
        // The LRU owns only opaque keys and AudioBuffers. It must not keep
        // decoded sample arrays alive after the stream forgets a definition.
        key = {};
        this.waveBufferKeys.set(samples, key);
      }
      this.waveBuffers.set(key, { buffer, channels, frames, rate, bytes });
      this.waveBufferBytes += bytes;
    }
    return buffer;
  }

  // Only definition-backed, immutable samples opt into buffer reuse. Mutable
  // callers still get a fresh snapshot. Every play has its own source and gain.
  playWave(channels, samplingRate, samples, sound = 0, cacheable = false, pcmChannel = 0, framePhase = 0) {
    if (!validFramePhase(framePhase)) return false;
    if (pcmChannel !== 0 && (!validPCMChannel(pcmChannel) || !validPCMOwner(sound) || channels !== 1)) return;
    if (!samples || samples.length === 0) return;
    const bytes = wavePayloadBytes(channels, samples.length);
    if (bytes === null || !validWaveRate(samplingRate) || this.sources.size >= MAX_AUDIO_SOURCES ||
        this.liveWaveBytes + bytes > MAX_LIVE_WAVE_BYTES) return false;
    if (bytes === 0) return;
    const context = this.ensure();
    if (!context) return;
    const frameCount = Math.floor(samples.length / channels);
    if (pcmChannel && !this.pcmStateFor(pcmChannel, sound)) return;
    const buffer = this.waveBuffer(context, channels, frameCount, samplingRate, samples, cacheable);
    const pcm = pcmChannel ? this.pcmOutputFor(pcmChannel, sound) : null;
    let source, gain, voice;
    try {
      source = context.createBufferSource();
      source.buffer = buffer;
      gain = context.createGain();
      gain.gain.value = 0.8;
      source.connect(gain);
      if (pcm) {
        gain.connect(pcm.left);
        gain.connect(pcm.right);
      } else gain.connect(this.outputFor(sound, "wave"));
      const now = this.eventTime();
      // The trimmed buffer keeps its first frame; resume inside it without
      // moving the event's presentation time or changing its cached samples.
      const offset = framePhase / (1_000_000_000 * samplingRate);
      voice = { kind: "wave", sound, gain, pcm, waveBytes: bytes, sourceStart: now, end: now + frameCount / samplingRate - offset };
      this.sources.set(source, voice);
      this.liveWaveBytes += bytes;
      if (pcm) pcm.references++;
      source.onended = () => this.finishSource(source, voice);
      if (framePhase) source.start(now, offset);
      else source.start(now);
    } catch (error) {
      if (voice) this.finishSource(source, voice);
      else {
        source?.disconnect?.();
        gain?.disconnect?.();
        if (pcm && pcm.references === 0) this.disconnectPCMOutput(pcm);
      }
      throw error;
    }
  }

  finishSource(source, voice) {
    if (this.sources.get(source) !== voice) return;
    this.sources.delete(source);
    this.liveWaveBytes -= voice.waveBytes || 0;
    source.disconnect?.();
    voice.gain?.disconnect?.();
    voice.filter?.disconnect?.();
    if (voice.kind === "midi") {
      const key = voiceKey(voice.sound, voice.channel, voice.note);
      // A retrigger can replace the key before the old source ends.
      if (this.voices.get(key) === voice) this.voices.delete(key);
    }
    if (voice.pcm && !voice.retired && --voice.pcm.references === 0) this.disconnectPCMOutput(voice.pcm);
    if (voice.retired && --voice.retired.remaining === 0) this.disconnectRetiredOutput(voice.retired);
  }

  disconnectRetiredOutput(output) {
    if (!this.retiredOutputs.delete(output)) return;
    for (const channel of output.channels || []) {
      channel?.gain.disconnect?.();
      channel?.panner?.disconnect?.();
    }
    for (const pcm of output.pcm?.values() || []) this.disconnectPCMOutput(pcm);
    output.gains?.midi?.disconnect?.();
    output.gains?.wave?.disconnect?.();
  }

  // Cancel only this clip, including sources no longer in the note map.
  stopSound(sound = 0) {
    for (const [key, voice] of this.voices) {
      if (voice.sound === sound) this.voices.delete(key);
    }
    if (this.scheduledTime !== null) {
      const retiring = [...this.sources].filter(([, voice]) => voice.sound === sound && !voice.retired);
      const output = { channels: this.channelOutputs.get(sound), pcm: this.pcmOutputs.get(sound),
        gains: this.soundGains.get(sound), remaining: retiring.length };
      this.retiredOutputs.add(output);
      this.channelOutputs.delete(sound);
      this.soundGains.delete(sound);
      this.soundChannels.delete(sound);
      this.pcmOutputs.delete(sound);
      this.clearPCMChannels(sound);
      if (!sound) this.channels = Array.from({ length: 16 }, channelDefaults);
      for (const [source, voice] of retiring) {
        voice.retired = output;
        voice.end = Math.min(this.scheduledTime, voice.end ?? Infinity);
        try { source.stop(voice.end); } catch { /* Already ended. */ }
        if (voice.end <= this.context.currentTime) this.finishSource(source, voice);
      }
      if (output.remaining === 0) this.disconnectRetiredOutput(output);
      return;
    }
    for (const [source, state] of this.sources) {
      if (state.sound !== sound) continue;
      try { source.stop(); } catch { /* Already ended. */ }
      this.finishSource(source, state);
    }
    this.soundChannels.delete(sound);
    this.clearPCMChannels(sound);
    for (const output of this.pcmOutputs.get(sound)?.values() || []) this.disconnectPCMOutput(output);
    this.disconnectChannelOutputs(sound);
    const gains = this.soundGains.get(sound);
    gains?.midi?.disconnect?.();
    gains?.wave?.disconnect?.();
    this.soundGains.delete(sound);
    if (!sound) this.channels = Array.from({ length: 16 }, channelDefaults);
  }

  // End or replace a timeline, including PCM and already released notes.
  stopAll() {
    this.clearWaveBuffers();
    this.scheduleAnchor = null;
    this.presentationAnchor = null;
    this.presentationFrontier = null;
    this.presentationLastTime = null;
    this.presentationInterrupted = false;
    this.scheduledTime = null;
    this.voices.clear();
    for (const [source, voice] of this.sources) {
      try { source.stop(); } catch { /* The source may already have ended. */ }
      this.finishSource(source, voice);
    }
    this.sources.clear();
    this.liveWaveBytes = 0;
    for (const output of this.retiredOutputs) this.disconnectRetiredOutput(output);
    this.soundChannels.clear();
    this.pcmChannels.clear();
    this.pcmChannelCount = 0;
    for (const outputs of this.pcmOutputs.values()) {
      for (const output of outputs.values()) this.disconnectPCMOutput(output);
    }
    for (const sound of this.channelOutputs.keys()) this.disconnectChannelOutputs(sound);
    for (const gains of this.soundGains.values()) {
      gains.midi?.disconnect?.();
      gains.wave?.disconnect?.();
    }
    this.soundGains.clear();
    this.channels = Array.from({ length: 16 }, channelDefaults);
  }
}
