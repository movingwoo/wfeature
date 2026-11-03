// Sound for the page.
//
// The engine hands us the two things a SMAF file decodes into: MIDI messages
// and sampled waveforms. Waveforms are easy — Web Audio plays a buffer. MIDI
// is not, because MIDI is a score, and playing a score needs instruments.
//
// The music was written for Yamaha's MA chips, which are FM synthesizers, and
// almost every score carries its own instruments as FM voices. The page plays
// them with an FM synthesizer of its own (fm-synth.js), which also stands in
// for what a file leaves to the chip's built-in bank. There is no sound bank
// to download: a soundfont would be megabytes on every phone, and an iOS tab
// that heavy can lose sound entirely. This file keeps the score's bookkeeping
// — channels, voice budget, timing, ownership — and routes each channel's lane
// of the synthesizer through its volume, pan and sound gain.

import { FMHost } from "./fm-host.js";

// Held notes that sound together; the server's output budget matches it.
export const MAX_VOICES = 32;
// A full reconstruction's worst case is about 134,000 events: every owner's
// channel controls and FM voice definitions, its notes and samples. The
// server's replay ceiling is the same (maxReplayAudio).
const MAX_AUDIO_EVENTS = 1 << 18;
const MAX_PCM_CHANNELS = 2048;
// Retained native sources include scheduled starts and release/retired tails.
// A full output reconstruction needs at most 256 PCM sources and 32 notes.
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
// How late a batch carrying events may arrive and still be played by moving
// the anchor rather than by rebuilding the whole output. A slow page delivers
// batches 15–100 ms late; a rebuild cuts every sounding voice to replay it.
const MAX_REANCHOR_LATE = 0.2;
// The anchor is set by one batch, and a late batch moves it later together
// with every sound after it. Between those moves it is steered so that the
// typical batch of the last ANCHOR_WINDOW seconds lands within ANCHOR_SLACK of
// the page's own lead: later at once when batches land closer than that,
// earlier once no batch has been late for the hold, and never so far that the
// most delayed batch of the last ANCHOR_MEMORY seconds would land closer than
// ANCHOR_SLACK. A move is at most ANCHOR_RATE of the render time since the
// batch before, except in silence, where nothing is heard hurrying or waiting.
// The hold is ANCHOR_HOLD seconds and doubles, up to ANCHOR_HOLD_LIMIT, each
// time a batch is late again within ANCHOR_RELAPSE seconds of the one before,
// so a page whose batches keep arriving late keeps the lead that absorbs them.
const ANCHOR_WINDOW = 2;
const ANCHOR_SLACK = 0.02;
const ANCHOR_RATE = 0.1;
const ANCHOR_MEMORY = 10;
const ANCHOR_HOLD = 2;
const ANCHOR_HOLD_LIMIT = 16;
const ANCHOR_RELAPSE = 30;
const voiceKey = (sound, channel, note) => sound ? `${sound}:${channel}:${note}` : `${channel}:${note}`;
// A stolen or retriggered note fades this fast rather than clicking; a
// released one follows its own envelope.
const RETRIGGER_FADE = 0.005;
const STEAL_FADE = 0.01;
const CUT_FADE = 0.005;
// Percussion holds a place in the voice budget this long, as the server's
// reconstruction assumes. Its sound rings on as long as its envelope does.
const DRUM_SLOT = 0.2;
const channelDefaults = () => ({
  program: 0, volume: 100, expression: 127, pan: 64, bend: 8192, sustain: 0, modulation: 0,
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
const validWaveVelocity = velocity => Number.isInteger(velocity) && velocity >= 0 && velocity <= 127;
// Every sampled source plays through this gain before the effect slider.
const WAVE_GAIN = 0.8;
// A stream wave's level follows its note's velocity on the chip's square-root
// curve, 6 dB above a note at the default channel volume of 100, and never
// past full: every velocity from 52 up plays at full level.
const waveLevel = velocity => velocity === 0 ? 1 : Math.min(1, Math.sqrt(velocity / 127) * (100 / 127) * 2);
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
const timingCounts = () => ({
  batches: 0, clock_only_batches: 0, admitted: 0, reanchored: 0, refused: { late: 0, future: 0, range: 0 },
  events: 0, not_late: 0, late_le_5ms: 0, late_le_20ms: 0,
  late_le_100ms: 0, late_gt_100ms: 0, invalid_events: 0,
  max_event_late_ms: 0, max_frontier_late_ms: 0, anchor_earlier_ms: 0, anchor_later_ms: 0,
});

export class PageAudio {
  constructor({ report = () => {}, diagnostics = () => false, createFM = (context, options) => new FMHost(context, options) } = {}) {
    this.report = report;
    this.diagnostics = diagnostics;
    this.createFM = createFM;
    // The FM synthesizer, its voices by id, and the drum notes playing a
    // file's own voice, which a note-off releases.
    this.fm = null;
    this.fmSources = new Map();
    this.drumKeys = new Map();
    this.nextVoiceId = 0;
    this.timing = null;
    this.activationCheck = null;
    this.recovering = false;
    this.context = null;
    this.scheduleAnchor = null;
    this.presentationAnchor = null;
    this.presentationFrontier = null;
    this.presentationLastTime = null;
    this.presentationLastAudio = null;
    this.anchoredAt = null;
    this.steeredAt = null;
    this.leadHistory = [];
    // Late batches describe the connection, not one timeline: a rebuild
    // keeps them.
    this.anchorHold = ANCHOR_HOLD;
    this.lateAt = null;
    this.presentationInterrupted = false;
    this.scheduledTime = null;
    // Seconds every sound is held back beyond the lead, for a picture that
    // reaches the eye later than the sound reaches the ear (av-sync.js). A
    // change waits for a rebuilt timeline rather than moving sounds already
    // scheduled, which could put a release in front of its attack.
    this.extraDelay = 0;
    this.extraDelayChanged = false;
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
        this.synthesizer();
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

  // synthesizer answers the FM synthesizer for the current context, creating
  // it on first use. Without one, sampled sound still plays and the score is
  // silent.
  synthesizer() {
    if (!this.fm && this.context && !this.fmFailed) {
      try {
        this.fm = this.createFM(this.context, { onEnded: ids => this.fmEnded(ids), report: this.report });
      } catch (error) {
        this.fmFailed = true;
        this.report(`audio synthesizer unavailable: ${error.message}`);
      }
    }
    return this.fm;
  }

  reportState(reason) {
    this.report(`audio ${reason}: state ${this.context.state}, time ${this.context.currentTime}`);
  }

  // Sample the render deadline at batch admission, before source creation or
  // JSON PCM decoding. This does not measure network or audible output delay.
  // Only fixed counters survive the call; no event/sample payload is retained.
  recordTiming(events, anchor, current, fresh, refused, reanchored = false) {
    if (!this.diagnostics()) {
      this.timing = null;
      return;
    }
    this.timing ??= { anchored: timingCounts(), continuing: timingCounts() };
    const counts = fresh ? this.timing.anchored : this.timing.continuing;
    counts.batches++;
    if (events.length === 1) counts.clock_only_batches++;
    if (reanchored) counts.reanchored++;
    else if (refused) counts.refused[refused]++;
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
      // The synthesizer lane is claimed by the first note and given back
      // when another channel needs it and this one has nothing sounding.
      outputs[channel] = { gain, panner, lane: null, voices: 0, lastUsed: 0,
        gainRamp: controlRamp(level), panRamp: controlRamp(pan) };
    }
    return outputs[channel];
  }

  // laneFor answers the synthesizer lane a channel's notes sound in. With
  // every lane held, it takes back the one idle longest: a clip that finished
  // on its own keeps its channels for its next play, not their lanes.
  laneFor(output, channel, sound) {
    if (output.lane !== null) return output.lane;
    const fm = this.synthesizer();
    if (!fm) return null;
    const state = this.channelsFor(sound)[channel];
    const claim = () => fm.lane(output.gain, channel === DRUM_CHANNEL ? 1 : this.bendRatio(state), state.modulation / 127);
    let lane = claim();
    if (lane === null) {
      let idle = null;
      for (const outputs of this.channelOutputs.values()) {
        for (const other of outputs) {
          if (other && other.lane !== null && other.voices === 0 && (!idle || other.lastUsed < idle.lastUsed)) idle = other;
        }
      }
      if (idle) {
        fm.release(idle.lane);
        idle.lane = null;
        lane = claim();
      }
    }
    output.lane = lane;
    return lane;
  }

  releaseChannelOutput(output) {
    if (!output) return;
    if (output.lane !== null && output.lane !== undefined) this.fm?.release(output.lane);
    output.lane = null;
    output.gain.disconnect?.();
    output.panner?.disconnect?.();
  }

  disconnectChannelOutputs(sound) {
    for (const output of this.channelOutputs.get(sound) || []) this.releaseChannelOutput(output);
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
        if (!validWaveVelocity(event.velocity === undefined ? 0 : event.velocity)) return false;
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

  bendRatio(state) {
    const bendSemitones = ((state.bend - 8192) / 8192) * (state.bendRange + state.bendRangeCents / 100);
    return Math.pow(2, bendSemitones / 12);
  }

  // setExtraDelay holds every sound back this many seconds more, up to one.
  setExtraDelay(seconds) {
    const delay = Number.isFinite(seconds) ? Math.min(1, Math.max(0, seconds)) : 0;
    if (delay === this.extraDelay) return;
    this.extraDelay = delay;
    this.extraDelayChanged = true;
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
    // A changed delay asks for the current output, as an out-of-window batch
    // does: the rebuilt timeline is anchored with the new lead, and nothing
    // already scheduled is moved.
    if (this.extraDelayChanged && !reset && this.presentationAnchor !== null) return false;
    this.extraDelayChanged = false;
    const current = context.currentTime;
    const latency = Number.isFinite(context.baseLatency) ? context.baseLatency : 0;
    const lead = Math.min(0.25, Math.max(0.1, latency)) + this.extraDelay;
    const horizon = current + 0.35 + this.extraDelay;
    let anchor = (!reset && this.presentationAnchor) || { audio: current + lead, presentation: clock.at };
    let refused = null;
    let lateBy = 0;
    for (const event of events) {
      const at = anchor.audio + (event.at - anchor.presentation);
      if (!Number.isFinite(at) || at < 0) refused = "range";
      else if (at < current - 0.005) {
        refused = "late";
        lateBy = current - at;
      } else if (at > horizon) refused = "future";
      if (refused) break;
    }
    // A late batch means the server or this page fell behind the render
    // clock and has caught up. Asking for the current output instead cuts
    // every sounding voice to replay it, so the anchor moves a lead ahead:
    // to the frontier for a batch that holds nothing else, which loses
    // nothing, and to the first event for a batch a little late, whose
    // events then keep their spacing and play that much later. Events are in
    // order, so the late one is the first, and everything already scheduled
    // is in the past by then: the move cannot reorder it. A batch later than
    // MAX_REANCHOR_LATE, or one that would not fit the window after the move,
    // still asks for the current output, which is then closer to the truth.
    let moved = null;
    if (refused === "late" && !reset && this.presentationAnchor !== null) {
      if (events.length === 1) {
        moved = { audio: current + lead, presentation: clock.at };
      } else if (lateBy <= MAX_REANCHOR_LATE) {
        const candidate = { audio: current + lead, presentation: events[0].at };
        if (events.every(event => candidate.audio + (event.at - candidate.presentation) <= horizon)) moved = candidate;
      }
    }
    const fresh = reset || !this.presentationAnchor;
    this.recordTiming(events, anchor, current, fresh, refused, moved !== null);
    if (refused === "late") this.noteLate(current);
    if (moved && events.length === 1) {
      this.presentationAnchor = moved;
      this.presentationFrontier = clock.at;
      this.anchoredAt = current;
      this.leadHistory = [];
      return true;
    }
    if (moved) {
      anchor = moved;
      refused = null;
    }
    if (refused) return false;
    // Validate a reset before cancelling the old timeline, then install its
    // new anchor after stopAll has cleared the previous presentation state.
    if (reset) this.stopAll();
    if (fresh || moved) {
      this.anchoredAt = current;
      this.leadHistory = [];
    } else {
      anchor = this.steerAnchor(anchor, events, current, lead);
    }
    this.presentationAnchor = anchor;
    this.presentationFrontier = clock.at;
    this.presentationLastTime = last;
    if (events.length > 1) this.presentationLastAudio = anchor.audio + (events.at(-2).at - anchor.presentation);
    this.presentationInterrupted = false;
    return true;
  }

  // noteLate records a batch that arrived late, moved or refused: the hold
  // before the anchor comes back doubles when it happens again soon.
  noteLate(current) {
    const again = this.lateAt !== null && current - this.lateAt < ANCHOR_RELAPSE;
    this.anchorHold = again ? Math.min(ANCHOR_HOLD_LIMIT, this.anchorHold * 2) : ANCHOR_HOLD;
    this.lateAt = current;
  }

  // steerAnchor answers the anchor a continuing batch is placed with: moved
  // so that a typical batch lands one lead ahead again. It never places this
  // batch's first event before the last one already scheduled or before the
  // render clock, so nothing is reordered; while anything sounds, a run of
  // notes plays at most ANCHOR_RATE quicker or slower as the anchor moves.
  steerAnchor(anchor, events, current, lead) {
    const elapsed = Math.max(0, current - (this.steeredAt ?? current));
    this.steeredAt = current;
    const frontier = anchor.audio + (events.at(-1).at - anchor.presentation);
    const history = this.leadHistory ??= [];
    history.push({ at: current, lead: frontier - current });
    while (history.length && history[0].at < current - ANCHOR_MEMORY) history.shift();
    if (current - (this.anchoredAt ?? current) < ANCHOR_WINDOW) return anchor;
    // The page's lead is where a typical batch lands, as the batch that set
    // the anchor did; the most delayed one shows how much of the lead recent
    // delays needed.
    const recent = [];
    let worst = Infinity;
    for (const entry of history) {
      if (entry.at >= current - ANCHOR_WINDOW) recent.push(entry.lead);
      worst = Math.min(worst, entry.lead);
    }
    recent.sort((a, b) => a - b);
    const off = recent[recent.length >> 1] - lead;
    const limit = this.sources.size === 0 ? Infinity : ANCHOR_RATE * elapsed;
    let step = 0;
    if (off < -ANCHOR_SLACK) {
      step = -Math.min(-off, limit);
    } else if (off > ANCHOR_SLACK && current - (this.lateAt ?? -Infinity) >= this.anchorHold) {
      const first = anchor.audio + (events[0].at - anchor.presentation);
      const room = Math.min(first - current, first - (this.presentationLastAudio ?? -Infinity));
      step = Math.max(0, Math.min(off, worst - ANCHOR_SLACK, limit, room));
    }
    if (!step) return anchor;
    for (const entry of history) entry.lead -= step;
    if (this.timing) {
      if (step > 0) this.timing.continuing.anchor_earlier_ms += step * 1000;
      else this.timing.continuing.anchor_later_ms -= step * 1000;
    }
    return { ...anchor, audio: anchor.audio - step };
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
    const lead = Math.min(0.25, Math.max(0.01, this.context.baseLatency || 0)) + this.extraDelay;
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
  noteResume(channel, note, velocity, ageMilliseconds, sound = 0, voice = 0, pitch = 0) {
    if (!Number.isFinite(ageMilliseconds) || ageMilliseconds < 0) return;
    this.noteOn(channel, note, velocity, sound, ageMilliseconds / 1000, voice, pitch);
  }

  // defineVoice holds one of a sound's own FM voices until the sound stops.
  defineVoice(slot, data, sound = 0) {
    if (!Number.isInteger(slot) || slot < 1 || slot > 255 || !data) return;
    if (!this.ensure()) return;
    this.synthesizer()?.post({ op: "define", sound, slot, data });
  }

  // noteOn sounds a note. voice names a slot the sound defined, and pitch the
  // note that voice sounds; without one the synthesizer's stand-in for the
  // channel's program, or for the drum key, plays the note itself.
  noteOn(channel, note, velocity, sound = 0, age = 0, voice = 0, pitch = 0) {
    if (!Number.isFinite(age) || age < 0) return;
    channel &= 15;
    if (velocity === 0) {
      this.noteOff(channel, note, 0, sound);
      return;
    }
    const drum = (channel & 15) === DRUM_CHANNEL;
    if (drum && age >= DRUM_SLOT) return;
    if (this.sources.size >= MAX_AUDIO_SOURCES) return false;
    const context = this.ensure();
    if (!context) return;
    // A note with nowhere to sound must not cut another one on its way out.
    const output = this.channelOutputFor(channel, sound);
    if (this.laneFor(output, channel, sound) === null) return;
    const now = this.eventTime();
    // A percussion note gives its place in the budget back on the score
    // clock, whatever the synthesizer is still rendering.
    for (const [key, held] of this.voices) {
      if ((held.slotEnd ?? held.end ?? Infinity) <= now) this.voices.delete(key);
    }
    if (this.voices.size >= MAX_VOICES) {
      // Steal the oldest voice rather than refusing the note: a dropped note
      // is more noticeable than a shortened one.
      const oldest = this.voices.keys().next().value;
      this.stopVoice(oldest, STEAL_FADE, now);
    }

    const key = voiceKey(sound, channel, note);
    this.stopVoice(key, RETRIGGER_FADE, now);
    const id = ++this.nextVoiceId;
    const source = {
      id,
      stop: at => this.fm?.post({ op: "cut", id, at: at ?? this.context.currentTime, fade: CUT_FADE }),
    };
    const own = Number.isInteger(voice) && voice > 0 && voice < 256;
    // A resumed envelope predates its source. Keep both times so a clock
    // correction cannot release it before its first rendered sample.
    const held = { source, kind: "midi", drum, fm: id, output, started: now - age, sourceStart: now, sound, channel, note, end: Infinity };
    output.voices++;
    output.lastUsed = now;
    if (drum) held.slotEnd = now + DRUM_SLOT - age;
    this.voices.set(key, held);
    this.sources.set(source, held);
    this.fmSources.set(id, source);
    if (drum && own) {
      // A drum key struck again before its last note's note-off releases that
      // note, as a channel's new key-on would.
      const previous = this.drumKeys.get(key);
      if (previous) this.fm.post({ op: "off", id: previous.id, at: now });
      this.drumKeys.set(key, source);
    }
    this.fm.post({
      op: "on", id, at: now, age, lane: output.lane, sound, slot: own ? voice : 0,
      pitch: own && Number.isInteger(pitch) && pitch >= 0 && pitch < 128 ? pitch : note,
      velocity, program: this.channelsFor(sound)[channel].program, drumKey: drum ? note : undefined,
    });
  }

  noteOff(channel, note, velocity = 0, sound = 0, at) {
    channel &= 15;
    const key = voiceKey(sound, channel, note);
    if (channel === DRUM_CHANNEL) {
      // A stand-in drum is a one-shot. A file's own drum voice is keyed off
      // like any FM voice, without giving up its place in the budget early.
      const source = this.drumKeys.get(key);
      if (source) {
        this.drumKeys.delete(key);
        const held = this.sources.get(source);
        this.fm?.post({ op: "off", id: source.id, at: Math.max(held?.sourceStart ?? 0, at ?? this.eventTime()) });
      }
      return;
    }
    const voice = this.voices.get(key);
    if (!voice) return;
    if (this.channelsFor(sound)[channel].sustain >= 64) {
      voice.deferred = true;
      return;
    }
    this.stopVoice(key, 0, at, true);
  }

  releaseDeferred(channel, sound, at) {
    for (const [key, voice] of this.voices) {
      if (voice.sound === sound && voice.channel === channel && voice.deferred) this.stopVoice(key, 0, at, true);
    }
  }

  silenceChannel(channel, sound, at) {
    for (const [key, voice] of this.voices) {
      if (voice.sound === sound && voice.channel === channel) this.voices.delete(key);
    }
    for (const [source, voice] of this.sources) {
      if (voice.retired || voice.kind !== "midi" || voice.sound !== sound || voice.channel !== channel) continue;
      voice.end = Math.min(at, voice.end ?? Infinity);
      try { source.stop(voice.end); } catch { /* Already ended. */ }
    }
  }

  // stopVoice ends a note: keyed off, it follows its own release; otherwise
  // it fades over release seconds, because cutting a voice outright would
  // click.
  stopVoice(key, release, at, keyOff = false) {
    const voice = this.voices.get(key);
    if (!voice) return;
    this.voices.delete(key);
    const now = Math.max(voice.sourceStart, at ?? this.eventTime());
    if (now >= (voice.end ?? Infinity)) return;
    if (keyOff) {
      this.fm?.post({ op: "off", id: voice.fm, at: now });
      return;
    }
    voice.end = now + release + 0.01;
    this.fm?.post({ op: "cut", id: voice.fm, at: now, fade: release });
  }

  // fmEnded learns which voices the synthesizer has finished rendering.
  fmEnded(ids) {
    for (const id of ids) {
      const source = this.fmSources.get(id);
      const voice = source && this.sources.get(source);
      if (voice) this.finishSource(source, voice);
      else this.fmSources.delete(id);
    }
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
      case 1:
        state.modulation = value;
        break;
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
        state.modulation = 0;
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
          if (voice.drum) this.stopVoice(key, 0, now, true);
          else this.noteOff(channel, voice.note, 0, sound, now);
        }
        break;
      default:
        break;
    }
    if (retune) this.retuneChannel(channel, sound, now);
    const output = this.channelOutputs.get(sound)?.[channel & 15];
    if (!output) return;
    if ((control === 1 || control === 121) && output.lane !== null && output.lane !== undefined) {
      this.fm?.post({ op: "depth", lane: output.lane, at: now, depth: state.modulation / 127 });
    }
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

  // retuneChannel bends every note of a channel at once: the channel's lane
  // carries one pitch ratio. Percussion is not bent.
  retuneChannel(channel, sound, at) {
    if (channel === DRUM_CHANNEL) return;
    const output = this.channelOutputs.get(sound)?.[channel];
    if (!output || output.lane === null || output.lane === undefined) return;
    this.fm?.post({ op: "bend", lane: output.lane, at, ratio: this.bendRatio(this.channelsFor(sound)[channel]) });
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
  // velocity is the stream note's that triggered the wave; zero, a PCM
  // track's or an older server's, plays it at full level.
  playWave(channels, samplingRate, samples, sound = 0, cacheable = false, pcmChannel = 0, framePhase = 0, velocity = 0) {
    if (!validFramePhase(framePhase) || !validWaveVelocity(velocity)) return false;
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
      gain.gain.value = WAVE_GAIN * waveLevel(velocity);
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
    if (voice.fm) {
      this.fmSources.delete(voice.fm);
      if (voice.output) voice.output.voices--;
      const key = voiceKey(voice.sound, voice.channel, voice.note);
      if (this.drumKeys.get(key) === source) this.drumKeys.delete(key);
    }
    this.liveWaveBytes -= voice.waveBytes || 0;
    source.disconnect?.();
    voice.gain?.disconnect?.();
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
    for (const channel of output.channels || []) this.releaseChannelOutput(channel);
    for (const pcm of output.pcm?.values() || []) this.disconnectPCMOutput(pcm);
    output.gains?.midi?.disconnect?.();
    output.gains?.wave?.disconnect?.();
  }

  // Cancel only this clip, including sources no longer in the note map.
  stopSound(sound = 0) {
    for (const [key, voice] of this.voices) {
      if (voice.sound === sound) this.voices.delete(key);
    }
    // Notes already scheduled keep the voices they resolved; later notes of
    // this owner name voices it defines again.
    this.fm?.post({ op: "forget", sound });
    for (const [key, source] of this.drumKeys) {
      if (this.sources.get(source)?.sound === sound) this.drumKeys.delete(key);
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
    this.presentationLastAudio = null;
    this.anchoredAt = null;
    this.steeredAt = null;
    this.leadHistory = [];
    this.presentationInterrupted = false;
    this.scheduledTime = null;
    this.voices.clear();
    this.drumKeys.clear();
    this.fm?.post({ op: "reset" });
    for (const [source, voice] of this.sources) {
      try { source.stop(); } catch { /* The source may already have ended. */ }
      this.finishSource(source, voice);
    }
    this.fmSources.clear();
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
