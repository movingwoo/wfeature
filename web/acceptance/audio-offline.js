// Offline renders for the browser audio acceptance harnesses.
//
// The page side is the page's own sound: PageAudio driving the real FM
// synthesizer host (fm-host.js) on an OfflineAudioContext, in its AudioWorklet
// or, where a harness asks for it, its ScriptProcessor fallback. PageAudio
// reads a clock object instead of the context, as the harnesses always did, so
// a harness can hold the page's view of time where a scenario needs it.
//
// The reference side never calls PageAudio. It renders the synthesizer class
// itself (fm-synth.js) with the commands a scenario should produce, in the
// render calls the page's host made, and routes each lane through native nodes
// the harness schedules from the scenario's own levels.
import { PageAudio } from "../audio.js";
import { FMHost } from "../fm-host.js";
import { FMSynth, FM_LANES } from "../fm-synth.js";

export const RATE = 48000;
// Both engines render offline in quanta of this many frames, and the worklet
// renders the synthesizer one quantum at a time.
export const QUANTUM = 128;
// A channel's volume, expression and pan move linearly over this long.
export const RAMP = 0.005;
// Every sampled source plays through this gain before its sound's gain.
export const WAVE_GAIN = 0.8;

// The fallback renders up to two 2048-frame buffers ahead of the render clock
// (85.3 ms), so commands it is to play on time need a longer lead than the
// page's 10 ms minimum. modeTiming answers the clock latency that gives the
// page that lead, the lead itself, and how much later than in the worklet every
// scheduled sound then plays; a harness moves its windows, its duration and the
// steps that act at once (a clip stop) by that shift, and keeps the rest.
// References take their times from the page's scheduling clock, since an
// automation event on an exact sample boundary moves a frame with one ulp, so a
// harness checks each of those times against the render clock plus the lead.
export const modeTiming = worklet => worklet ? { baseLatency: 0, lead: 0.01, shift: 0 } : { baseLatency: 0.1, lead: 0.1, shift: 0.09 };

const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));

// wallClock stands in for performance.now, which the page reads to space
// events that arrive between render-clock updates.
export const wallClock = () => {
  let seconds = 0;
  const previous = Object.getOwnPropertyDescriptor(performance, "now");
  Object.defineProperty(performance, "now", { configurable: true, value: () => seconds * 1000 });
  return {
    set(value) { seconds = value; },
    get() { return seconds; },
    restore() {
      if (previous) Object.defineProperty(performance, "now", previous);
      else delete performance.now;
    },
  };
};

// pageAudio answers a PageAudio on the offline context with the real
// synthesizer host. clock is the page's view of the context (state,
// currentTime, baseLatency); by default it reads the context's own time and
// reports baseLatency.
// In a worklet the host's processor answers each batch (audio-worklet-ack.js)
// and sync() waits until every posted batch has reached the audio thread. In
// the fallback the host renders on this thread; its render calls are recorded
// for the reference, with every timed command that arrived for a time already
// rendered.
export const pageAudio = async (context, { worklet = true, clock = null, baseLatency = 0, master = false } = {}) => {
  if (worklet) {
    await context.audioWorklet.addModule(new URL("./audio-worklet-ack.js", import.meta.url).href);
  } else {
    // A page opened over plain HTTP on the network has no AudioWorklet.
    Object.defineProperty(context, "audioWorklet", { configurable: true, value: undefined });
  }
  let host = null;
  const reports = [];
  // PageAudio offers its host the clock object; the host gets the context
  // itself, which an AudioWorkletNode and a ScriptProcessor are built on.
  const audio = new PageAudio({
    createFM: (pageClock, options) => (host = new FMHost(context, options)),
    report: message => reports.push(message),
  });
  const view = clock ?? { state: "running", get currentTime() { return context.currentTime; }, baseLatency };
  view.sampleRate = context.sampleRate;
  // How many nodes and buffers the page asked its clock object for.
  const counts = {};
  for (const name of ["createGain", "createStereoPanner", "createChannelMerger", "createBuffer", "createBufferSource"]) {
    view[name] = (...args) => {
      counts[name] = (counts[name] ?? 0) + 1;
      return context[name](...args);
    };
  }
  audio.context = view;
  if (master) {
    audio.master = context.createGain();
    audio.master.connect(context.destination);
  }
  audio.midiGain = context.createGain();
  audio.waveGain = context.createGain();
  audio.midiGain.connect(audio.master ?? context.destination);
  audio.waveGain.connect(audio.master ?? context.destination);
  if (!audio.synthesizer()) throw new Error(`the page created no synthesizer host: ${reports.join("; ")}`);
  for (let waited = 0; host.mode === "pending"; waited += 10) {
    if (waited > 10000) throw new Error("the synthesizer host never attached");
    await pause(10);
  }
  const mode = worklet ? "worklet" : "script";
  if (host.mode !== mode) throw new Error(`the synthesizer runs in ${host.mode}, not ${mode}: ${reports.join("; ")}`);
  const calls = [], late = [];
  if (!worklet) {
    const render = host.synth.render.bind(host.synth);
    host.synth.render = (lanes, frames, start) => {
      calls.push({ start, frames });
      return render(lanes, frames, start);
    };
  }
  // Every command the page posts, in order, for counts and diagnostics. A
  // timed command is late when the synthesizer has already rendered its time:
  // the worklet renders from the render clock on once the batch arrives, the
  // fallback from the end of its last render call. A reference receives the
  // same command at the same point, so lateness is checked here instead. A cut
  // at the render clock is how an untimed clip stop ends a note, not lateness.
  const commands = [];
  const post = host.post.bind(host);
  host.post = command => {
    commands.push({ ...command });
    const last = calls.at(-1);
    const rendered = worklet ? context.currentTime : last ? last.start + last.frames / RATE : 0;
    if (command.at !== undefined && command.op !== "cut" && command.at < rendered - 1e-9) late.push({ ...command, rendered });
    post(command);
  };
  let posted = 0, answered = 0;
  if (worklet) {
    const port = host.node.port;
    const send = port.postMessage.bind(port);
    port.postMessage = message => {
      posted++;
      send(message);
    };
    port.addEventListener("message", event => {
      if (Array.isArray(event.data) && event.data.length === 0) answered++;
    });
  }
  const sync = async () => {
    // The host flushes what was posted in a microtask.
    await null;
    const deadline = Date.now() + 10000;
    while (answered < posted) {
      if (Date.now() > deadline) throw new Error(`the worklet answered ${answered} of ${posted} batches`);
      await pause(1);
    }
  };
  // A batch that changes nothing proves the processor answers.
  host.post({ op: "forget", sound: -1 });
  await sync();
  commands.length = 0;
  return { audio, host, sync, commands, reports, mode, calls, late,
    created: name => counts[name] ?? 0, bufferSources: () => counts.createBufferSource ?? 0 };
};

// suspensionFrame answers the frame a suspension asked for at `time` lands on:
// both engines round it up to the next quantum boundary.
const suspensionFrame = time => Math.ceil(time * RATE / QUANTUM - 1e-9) * QUANTUM;

// renderSteps applies steps[0] before rendering and every later step while the
// context is suspended at its time, waiting for the synthesizer to receive a
// step's commands before rendering on. apply(step, index) may be async. It
// answers the rendered buffer and, for each step, a mark of where it reached
// the synthesizer: the frame, and the render calls the fallback had made. It
// fails, naming the engine, when a suspension lands on another frame.
export const renderSteps = async (context, page, steps, apply) => {
  const marks = [];
  const frames = new Set();
  for (const step of steps.slice(1)) {
    const frame = suspensionFrame(step.time);
    if (frames.has(frame)) throw new Error(`two steps share the quantum at ${step.time}`);
    frames.add(frame);
  }
  const pauses = steps.slice(1).map(step => context.suspend(step.time));
  marks.push({ frame: 0, calls: page.calls.length });
  await apply(steps[0], 0);
  await page.sync();
  const rendering = context.startRendering();
  for (let index = 1; index < steps.length; index++) {
    await pauses[index - 1];
    const frame = Math.round(context.currentTime * RATE), requested = suspensionFrame(steps[index].time);
    // Everything after is timed from the render clock, so a suspension that
    // lands elsewhere would go unseen by the comparisons; it is the engine's
    // fault, not the page's, and is named here.
    if (frame !== requested) {
      throw new Error(`the engine resolved suspend(${steps[index].time}) at frame ${frame} (${(frame / RATE).toFixed(6)} s), ` +
        `not at frame ${requested} (${(requested / RATE).toFixed(6)} s), the quantum it rounds up to`);
    }
    marks.push({ frame, calls: page.calls.length });
    await apply(steps[index], index);
    await page.sync();
    await context.resume();
  }
  return { buffer: await rendering, marks };
};

export const channelsOf = buffer => Array.from({ length: buffer.numberOfChannels }, (_, channel) => buffer.getChannelData(channel));

// referenceLanes renders the synthesizer for a reference and answers each
// lane's samples. entries are { mark, commands }: the commands a step should
// have produced, handed to the synthesizer where the page's step reached its
// own (a mark from renderSteps).
//
// In the worklet the synthesizer renders one quantum at a time from frame zero,
// at the context's own time, and so does the reference. The fallback renders
// in the calls recorded from the page's host: its buffer clock adds up buffer
// lengths in floating point, and an LFO table lookup that lands exactly on an
// entry boundary, as the stand-in voices' do every 1875 frames, can round to
// the neighbouring entry at a time one ulp away. Its offline callback also runs
// inside the last quantum before its buffers swap, so the playbackTime its
// clock starts from is one quantum early: in both engines every buffer sounds
// one quantum after the time it was rendered for, and is placed there.
export const referenceLanes = (page, frames, entries) => {
  const synth = new FMSynth(RATE);
  const calls = page.mode === "script" ? page.calls.map(call => ({ ...call, at: Math.round(call.start * RATE) + QUANTUM }))
    : Array.from({ length: Math.ceil(frames / QUANTUM) }, (_, index) => ({ start: index * QUANTUM / RATE, frames: QUANTUM, at: index * QUANTUM }));
  const lanes = Array.from({ length: FM_LANES }, () => new Float32Array(frames + 2 * QUANTUM + 4096));
  const pending = entries.map((entry, order) => ({ ...entry, order })).sort((left, right) =>
    (page.mode === "script" ? left.mark.calls - right.mark.calls : left.mark.frame - right.mark.frame) || left.order - right.order);
  const due = (entry, index, call) => page.mode === "script" ? entry.mark.calls <= index : entry.mark.frame <= call.at;
  let next = 0;
  for (const [index, call] of calls.entries()) {
    if (call.at >= frames) break;
    while (next < pending.length && due(pending[next], index, call)) {
      for (const command of pending[next].commands) synth.handle(command);
      next++;
    }
    const block = Array.from({ length: FM_LANES }, () => new Float32Array(call.frames));
    synth.render(block, call.frames, call.start);
    for (let lane = 0; lane < FM_LANES; lane++) lanes[lane].set(block[lane], call.at);
  }
  return lanes.map(lane => lane.subarray(0, frames));
};

// transition moves an AudioParam to level over RAMP seconds from the value it
// holds at `at`, continuing an unfinished move rather than restarting it.
// state is { from, to, start, end } and the answer is the next state.
export const steady = value => ({ from: value, to: value, start: -Infinity, end: -Infinity });
export const transition = (parameter, state, level, at) => {
  const inside = at > state.start && at < state.end;
  const from = at >= state.end ? state.to : at <= state.start ? state.from
    : state.from + (state.to - state.from) * (at - state.start) / (state.end - state.start);
  parameter.cancelScheduledValues(at);
  if (inside) parameter.linearRampToValueAtTime(from, at);
  else parameter.setValueAtTime(from, at);
  parameter.linearRampToValueAtTime(level, at + RAMP);
  return { from, to: level, start: at, end: at + RAMP };
};

// referenceMixer routes synthesizer lanes and sampled sources through native
// nodes on a new offline context: lane -> channel gain -> panner -> its sound's
// MIDI gain -> MIDI gain -> master -> destination, and a sample -> 0.8 -> its
// sound's wave gain -> wave gain -> master.
export const referenceMixer = (frames, channelCount = 2) => {
  const context = new OfflineAudioContext(channelCount, frames, RATE);
  const master = context.createGain();
  master.connect(context.destination);
  const midi = context.createGain(), wave = context.createGain();
  midi.connect(master);
  wave.connect(master);
  const owners = new Map();
  const owner = sound => {
    if (!owners.has(sound)) {
      const nodes = { midi: context.createGain(), wave: context.createGain() };
      nodes.midi.connect(midi);
      nodes.wave.connect(wave);
      owners.set(sound, nodes);
    }
    return owners.get(sound);
  };
  const buffer = (samples, channels) => {
    const result = context.createBuffer(channels, samples.length / channels, RATE);
    for (let channel = 0; channel < channels; channel++) {
      const target = result.getChannelData(channel);
      for (let frame = 0; frame < target.length; frame++) target[frame] = samples[frame * channels + channel];
    }
    return result;
  };
  return {
    context, master, midi, wave, owner,
    // lane plays one synthesizer lane through a channel gain and panner of its
    // own, answering them for automation; stop(at) silences it from at.
    lane(samples, sound, level, pan) {
      const source = context.createBufferSource(), gain = context.createGain(), panner = context.createStereoPanner();
      source.buffer = buffer(samples, 1);
      gain.gain.value = level;
      panner.pan.value = pan;
      source.connect(gain);
      gain.connect(panner);
      panner.connect(owner(sound).midi);
      source.start(0);
      return { source, gain, panner, stop: at => source.stop(at) };
    },
    // sample plays interleaved samples from `at`, from `offset` seconds in,
    // until stopAt when given.
    sample(samples, channels, sound, at, { offset = 0, stopAt = null, level = WAVE_GAIN } = {}) {
      const source = context.createBufferSource(), gain = context.createGain();
      source.buffer = buffer(samples, channels);
      gain.gain.value = level;
      source.connect(gain);
      gain.connect(owner(sound).wave);
      source.start(at, offset);
      if (stopAt !== null) source.stop(stopAt);
      return source;
    },
    render: async () => channelsOf(await context.startRendering()),
  };
};

// maximumError answers the largest sample difference over [from, to) seconds
// of every channel.
export const maximumError = (actual, expected, from = 0, to = Infinity) => {
  let error = 0;
  for (let channel = 0; channel < expected.length; channel++) {
    const last = Math.min(expected[channel].length, Math.floor(to * RATE));
    for (let frame = Math.max(0, Math.ceil(from * RATE)); frame < last; frame++) {
      error = Math.max(error, Math.abs(actual[channel][frame] - expected[channel][frame]));
    }
  }
  return error;
};

// describeMismatch explains a sample comparison that failed, for the failure
// message: the first and the worst frames that differ by more than tolerance,
// the time shift of the reference that best matches the stretch from the
// first bad frame on, and every step's mark and scheduled time (steps are
// { label, mark, at }, mark as renderSteps answers it). It answers undefined
// when no frame differs, so a passing result carries nothing new.
export const describeMismatch = (actual, expected, { tolerance = 1e-6, steps = [], range = 4096, span = 4096 } = {}) => {
  let first = null, last = null, worst = null;
  for (let channel = 0; channel < expected.length; channel++) {
    for (let frame = 0; frame < expected[channel].length; frame++) {
      // A sample that is not a number differs more than any other.
      const difference = Math.abs(actual[channel][frame] - expected[channel][frame]);
      const error = Number.isNaN(difference) ? Infinity : difference;
      if (error <= tolerance) continue;
      if (first === null || frame < first.frame) first = { frame, channel };
      if (last === null || frame > last) last = frame;
      if (worst === null || error > worst.error) worst = { frame, channel, error };
    }
  }
  if (first === null) return undefined;
  const at = frame => `${frame} (${(frame / RATE).toFixed(6)} s)`;
  const value = number => Number.isFinite(number) ? number.toPrecision(4) : String(number);
  const pair = ({ frame, channel }) =>
    `channel ${channel}: page ${value(actual[channel][frame])}, reference ${value(expected[channel][frame])}`;
  // A positive shift means the page sounds that many frames after the reference.
  const samples = actual[first.channel], reference = expected[first.channel];
  const end = Math.min(samples.length, first.frame + span);
  const shiftedError = (shift, bound = Infinity) => {
    let error = 0;
    for (let frame = first.frame; frame < end && error < bound; frame++) {
      error = Math.max(error, Math.abs(samples[frame] - (reference[frame - shift] ?? 0)));
    }
    return error;
  };
  const unshifted = shiftedError(0);
  let best = { shift: 0, error: unshifted };
  for (let shift = -range; shift <= range; shift++) {
    if (shift === 0) continue;
    const error = shiftedError(shift, best.error);
    if (error < best.error) best = { shift, error };
  }
  const stretch = `frames ${first.frame}-${end - 1} of channel ${first.channel}`;
  const frames = Math.abs(best.shift);
  const alignment = best.shift === 0
    ? `over ${stretch}, no shift within ${range} frames matches better than none (error ${value(unshifted)})`
    : `over ${stretch}, the page matches the reference ${frames} frames (${Number((frames / QUANTUM).toFixed(2))} quanta, ` +
      `${(frames / RATE * 1000).toFixed(2)} ms) ${best.shift > 0 ? "later" : "earlier"}, leaving error ${value(best.error)} ` +
      `(unshifted ${value(unshifted)})`;
  const lines = [
    `first bad frame ${at(first.frame)}, ${pair(first)}`,
    `worst frame ${at(worst.frame)}, ${pair(worst)}, error ${value(worst.error)}`,
    `last bad frame ${at(last)}`,
    alignment,
    ...steps.map(({ label, mark, at: scheduled }, index) => `step ${index}${label ? ` (${label})` : ""}: ` +
      (mark ? `reached the synthesizer at frame ${at(mark.frame)}` +
        `${mark.calls ? ` after ${mark.calls} render call${mark.calls === 1 ? "" : "s"}` : ""}, ` : "") +
      `scheduled ${Number.isFinite(scheduled) ? `${scheduled.toFixed(6)} s` : String(scheduled)}`),
  ];
  return `\n  ${lines.join("\n  ")}`;
};

// energy answers the mean square of one channel over [start, end) seconds.
export const energy = (samples, start, end) => {
  const first = Math.round(start * RATE), last = Math.round(end * RATE);
  let sum = 0;
  for (let frame = first; frame < last; frame++) sum += samples[frame] ** 2;
  return sum / (last - first);
};
