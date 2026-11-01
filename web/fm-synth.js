// FM synthesis for the page's MIDI half.
//
// The music these games play was written for Yamaha's MA chips, which are FM
// synthesizers, and almost every score carries its instruments as FM voices of
// its own. The server reads those into operator parameters (Go's smaf.Voice,
// in its canonical encoding); this file plays them, and plays its own small set
// of FM stand-ins for the programs and drum keys a file leaves to the chip's
// built-in bank.
//
// One synthesizer renders every note into one of FM_LANES mono outputs, one per
// sound channel the page has open. The page keeps routing each lane through
// that channel's volume, pan and sound gain, as it did for oscillators. The
// same class runs in an AudioWorklet where the page is a secure context, and in
// a ScriptProcessor where it is not (a plain-HTTP page on the network): the
// sound is the same either way, only the thread differs.
//
// The operator model is the FM family's: phase modulation, envelopes linear in
// decibels, rates that double every four steps, a total level of 0.75 dB per
// step and feedback from the average of an operator's last two outputs. The
// constants below say where a value is the family's convention and where it is
// a choice; nothing here has been compared against a handset by ear.

export const FM_LANES = 32;
// Notes the page sounds are bounded by its own voice budget. Release tails
// outlive that budget, so the synthesizer holds more and steals the quietest.
const MAX_SYNTH_VOICES = 64;
const SINE_SIZE = 4096;
const SINE_MASK = SINE_SIZE - 1;
const HALF = SINE_SIZE / 2;
const QUARTER = SINE_SIZE / 4;
const SINE = new Float32Array(SINE_SIZE);
for (let index = 0; index < SINE_SIZE; index++) SINE[index] = Math.sin(2 * Math.PI * index / SINE_SIZE);
// Wave 7, the family's "derived square": each half an exponential fall.
const LOG_SAW = new Float32Array(SINE_SIZE);
for (let index = 0; index < SINE_SIZE; index++) {
  const x = index / SINE_SIZE;
  LOG_SAW[index] = x < 0.5 ? 2 ** (-16 * x) : -(2 ** (-16 * (x - 0.5)));
}
// Attenuation to gain, in sixteenths of a decibel.
const GAIN_STEPS = 16;
const SILENT_DB = 96;
const GAIN = new Float32Array(SILENT_DB * GAIN_STEPS + 2);
for (let index = 0; index < GAIN.length; index++) GAIN[index] = index >= SILENT_DB * GAIN_STEPS ? 0 : 10 ** (-index / GAIN_STEPS / 20);
const MULTIPLES = [0.5, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];
const DETUNE_CENTS = [0, 1.5, 3, 4.5, 0, -1.5, -3, -4.5];
// Peak phase deviation, in cycles, of a full-level modulator and of each
// feedback setting: the FM family's 4π for feedback 7, halving per step.
const MODULATION_CYCLES = 4;
const FEEDBACK_CYCLES = [0, 1 / 32, 1 / 16, 1 / 8, 1 / 4, 1 / 2, 1, 2];
const KEY_SCALE_DB_PER_OCTAVE = [0, 3, 1.5, 6];
const LFO_HZ = [1.8, 4, 5.9, 7];
const VIBRATO_CENTS = [3.4, 6.7, 13.5, 26.8];
const TREMOLO_DB = [1.3, 2.8, 5.8, 11.8];
const BASIC_OCTAVE_SEMITONES = [12, 0, -12, -24];
// A channel's modulation controller adds this much vibrato at full depth.
const MODULATION_WHEEL_CENTS = 50;
const OUTPUT_LEVEL = 0.25;
// The stand-in drums use noise, which no file voice can name.
const WAVE_NOISE = 32;
const CARRIERS = [[1], [0, 1], [0, 1, 2, 3], [3], [3], [1, 3], [0, 3], [0, 2, 3]];
// Envelopes, LFOs and pitch modulation move this many samples at a time, the
// gain ramping linearly between; only phase and waveform run every sample.
const CONTROL_SAMPLES = 8;
// A voice whose every carrier is this far below full scale, envelope and
// total level together, is over: about -92 dBFS at the page's output level.
// Rendering a long release tail below it costs as much as an audible note.
const AUDIBLE_DB = 80;

const ATTACK = 1, DECAY = 2, SUSTAIN = 3, RELEASE = 4, OFF = 0;

// Rates are 0..15 register values, 0 never moving. Key scaling adds up to a
// quarter of the key code, or all of it with the operator's flag set, giving
// an effective rate of 0..63 whose time halves every four steps.
const effectiveRate = (rate, keyScale, keyCode) => rate === 0 ? 0 : Math.min(63, rate * 4 + (keyScale ? keyCode : keyCode >> 2));
const decaySeconds = rate => 78.56 / 2 ** (rate / 4); // 96 dB
const attackSeconds = rate => 5.652 / 2 ** (rate / 4);
const keyCodeOf = pitch => Math.max(0, Math.min(15, Math.floor((pitch - 12) / 6)));

// waveAt answers one operator waveform at a phase in cycles, which may be
// negative or past one under modulation. Waves 0-7 are the FM family's sine
// derivatives. The MA-3 voices name 8-31 too, which the format's
// specification does not draw; they play their low three bits' shape.
const waveAt = (wave, phase) => {
  const index = ((phase * SINE_SIZE) | 0) & SINE_MASK;
  if (wave === 0) return SINE[index];
  if (wave === WAVE_NOISE) return Math.random() * 2 - 1;
  const sine = SINE[index];
  switch (wave & 7) {
    case 0: return sine;
    case 1: return sine > 0 ? sine : 0;
    case 2: return sine < 0 ? -sine : sine;
    case 3: return (index & QUARTER) === 0 ? (sine < 0 ? -sine : sine) : 0;
    case 4: return index < HALF ? SINE[(index << 1) & SINE_MASK] : 0;
    case 5: {
      if (index >= HALF) return 0;
      const doubled = SINE[(index << 1) & SINE_MASK];
      return doubled < 0 ? -doubled : doubled;
    }
    case 6: return index < HALF ? 1 : -1;
    default: return LOG_SAW[index];
  }
};

// decodeVoice reads the canonical encoding the server sends: a version byte,
// algorithm, LFO and basic octave, then seventeen bytes per operator. Anything
// out of range is refused, so a voice that would misbehave never plays.
export const decodeVoice = data => {
  if (!data || data.length < 4 || data[0] !== 1 || data[1] > 7 || data[2] > 3 || data[3] > 3) return null;
  const algorithm = data[1];
  const count = algorithm < 2 ? 2 : 4;
  if (data.length !== 4 + count * 17) return null;
  const limits = [15, 7, 63, 15, 15, 15, 15, 15, 3, 1, 31, 7, 1, 3, 1, 3, 1];
  const operators = [];
  for (let index = 0; index < count; index++) {
    const field = data.subarray ? data.subarray(4 + index * 17, 4 + (index + 1) * 17) : data.slice(4 + index * 17, 4 + (index + 1) * 17);
    for (let position = 0; position < 17; position++) if (field[position] > limits[position]) return null;
    operators.push({
      multiple: field[0], detune: field[1], totalLevel: field[2], attack: field[3], decay: field[4],
      sustainRate: field[5], release: field[6], sustainLevel: field[7], keyScaleLevel: field[8], keyScaleRate: field[9] === 1,
      wave: field[10], feedback: field[11], tremolo: field[12] === 1, tremoloDepth: field[13], vibrato: field[14] === 1,
      vibratoDepth: field[15], ignoreKeyOff: field[16] === 1,
    });
  }
  return { algorithm, lfo: data[2], basicOctave: data[3], operators };
};

// Stand-in voices for what a file leaves to the chip's own bank. Built here,
// in the same terms as a file's voices, and only ever an approximation: one
// voice per General MIDI family and one per drum group.
const op = (multiple, totalLevel, attack, decay, sustainLevel, sustainRate, release, extra = {}) => ({
  multiple, detune: 0, totalLevel, attack, decay, sustainRate, release, sustainLevel, keyScaleLevel: 0,
  keyScaleRate: false, wave: 0, feedback: 0, tremolo: false, tremoloDepth: 0, vibrato: false, vibratoDepth: 0,
  ignoreKeyOff: false, ...extra,
});
const voice = (algorithm, operators, extra = {}) => ({ algorithm, lfo: 2, basicOctave: 1, operators, ...extra });
const vibrato = { vibrato: true, vibratoDepth: 1 };

const FAMILY_VOICES = [
  voice(0, [op(1, 30, 15, 5, 6, 3, 6, { feedback: 3, keyScaleRate: true }), op(1, 0, 15, 3, 9, 2, 6, { keyScaleRate: true })]), // piano
  voice(0, [op(4, 32, 15, 7, 15, 5, 6), op(1, 0, 15, 4, 12, 3, 6)]), // chromatic percussion
  voice(1, [op(1, 2, 14, 0, 0, 0, 8, { feedback: 2 }), op(2, 10, 14, 0, 0, 0, 8)]), // organ
  voice(0, [op(1, 26, 15, 7, 15, 6, 7, { feedback: 4 }), op(1, 0, 15, 4, 10, 4, 7)]), // guitar
  voice(0, [op(1, 20, 15, 6, 15, 5, 7, { feedback: 5 }), op(1, 0, 15, 3, 6, 3, 8)]), // bass
  voice(0, [op(1, 28, 7, 0, 0, 0, 6, { feedback: 6 }), op(1, 0, 7, 0, 0, 0, 6, vibrato)]), // strings
  voice(0, [op(1, 30, 6, 0, 0, 0, 6, { feedback: 5 }), op(1, 0, 6, 0, 0, 0, 6, vibrato)]), // ensemble
  voice(0, [op(1, 20, 10, 2, 3, 0, 7, { feedback: 6 }), op(1, 0, 11, 0, 0, 0, 7, vibrato)]), // brass
  voice(0, [op(2, 24, 12, 2, 3, 0, 7, { feedback: 2 }), op(1, 0, 12, 0, 0, 0, 7, vibrato)]), // reed
  voice(0, [op(1, 40, 10, 2, 4, 0, 7), op(1, 0, 10, 0, 0, 0, 7, vibrato)]), // pipe
  voice(0, [op(1, 18, 15, 0, 0, 0, 8, { feedback: 7 }), op(1, 0, 15, 0, 0, 0, 8)]), // synth lead
  voice(0, [op(2, 34, 5, 0, 0, 0, 5, { tremolo: true, tremoloDepth: 1 }), op(1, 0, 5, 0, 0, 0, 5, vibrato)]), // synth pad
  voice(0, [op(3, 30, 8, 3, 6, 0, 6, { feedback: 3 }), op(1, 0, 8, 0, 0, 0, 6)]), // synth effects
  voice(0, [op(3, 28, 15, 6, 15, 5, 7, { feedback: 2 }), op(1, 0, 15, 4, 10, 4, 7)]), // ethnic
  voice(0, [op(5, 26, 15, 8, 15, 7, 7), op(1, 0, 15, 6, 15, 6, 7)]), // percussive
  voice(0, [op(1, 10, 12, 0, 0, 0, 6, { feedback: 7, wave: WAVE_NOISE }), op(1, 0, 12, 0, 0, 0, 6)]), // sound effects
];

const drum = (algorithm, operators, octave = 1) => voice(algorithm, operators, { basicOctave: octave, drum: true });
const DRUM_VOICES = {
  kick: drum(0, [op(0, 10, 15, 9, 15, 9, 9), op(1, 0, 15, 7, 15, 7, 8)]),
  snare: drum(1, [op(1, 6, 15, 10, 15, 9, 9, { wave: WAVE_NOISE }), op(2, 10, 15, 9, 15, 9, 9)]),
  clap: drum(1, [op(1, 4, 15, 9, 15, 8, 9, { wave: WAVE_NOISE }), op(1, 63, 15, 15, 15, 15, 15)]),
  closedHat: drum(1, [op(1, 10, 15, 12, 15, 12, 12, { wave: WAVE_NOISE }), op(1, 63, 15, 15, 15, 15, 15)]),
  openHat: drum(1, [op(1, 12, 15, 8, 15, 7, 9, { wave: WAVE_NOISE }), op(1, 63, 15, 15, 15, 15, 15)]),
  tom: drum(0, [op(1, 24, 15, 9, 15, 9, 9), op(2, 0, 15, 6, 15, 6, 8)]),
  cymbal: drum(1, [op(1, 14, 15, 5, 15, 5, 6, { wave: WAVE_NOISE }), op(7, 20, 15, 5, 15, 5, 6, { feedback: 7 })]),
  metal: drum(0, [op(7, 18, 15, 8, 15, 8, 8, { feedback: 5 }), op(3, 0, 15, 8, 15, 8, 8)]),
  wood: drum(0, [op(3, 26, 15, 11, 15, 11, 11), op(1, 0, 15, 10, 15, 10, 10)], 0),
};
const drumGroup = key => {
  if (key === 35 || key === 36) return "kick";
  if (key === 38 || key === 40) return "snare";
  if (key === 39) return "clap";
  if (key === 42 || key === 44) return "closedHat";
  if (key === 46) return "openHat";
  if ([41, 43, 45, 47, 48, 50].includes(key)) return "tom";
  if ([49, 51, 52, 53, 55, 57, 59].includes(key)) return "cymbal";
  if ([56, 67, 68, 80, 81].includes(key)) return "metal";
  return key < 35 ? "kick" : "wood";
};

export const fallbackVoice = (program, drumKey) =>
  drumKey === undefined ? FAMILY_VOICES[(program >> 3) & 15] : DRUM_VOICES[drumGroup(drumKey)];

// One operator's running state.
class Operator {
  constructor() {
    this.phase = 0;
    this.increment = 0;
    this.attenuation = SILENT_DB;
    this.stage = OFF;
  }
}

class Voice {
  constructor() {
    this.operators = [new Operator(), new Operator(), new Operator(), new Operator()];
    // The last two outputs of each operator, for feedback.
    this.out1 = new Float64Array(4);
    this.out2 = new Float64Array(4);
    this.active = false;
  }
}

// FMSynth is the renderer. Commands carry a context time in seconds and take
// effect at that sample; render answers each block's lanes.
export class FMSynth {
  constructor(sampleRate) {
    this.sampleRate = sampleRate;
    this.voices = Array.from({ length: MAX_SYNTH_VOICES }, () => new Voice());
    this.patches = new Map(); // `${sound}:${slot}` -> decoded voice
    this.lanes = Array.from({ length: FM_LANES }, () => ({ ratio: 1, depth: 0 }));
    this.queue = [];
    this.pending = new Map(); // id -> queued note-on command
    this.cancelled = new Set();
    this.onEnded = () => {};
    this.time = 0;
    // Working buffers for one control period of each operator.
    this.scratch = Array.from({ length: 4 }, () => new Float64Array(CONTROL_SAMPLES));
    this.modulation = new Float64Array(CONTROL_SAMPLES);
  }

  // handle applies an immediate command or queues a timed one. A note-on
  // resolves its voice now, in the order the page issued its commands, so a
  // later forget cannot change what an earlier note plays.
  handle(command) {
    switch (command.op) {
      case "define": {
        const decoded = decodeVoice(command.data);
        if (decoded) this.patches.set(`${command.sound}:${command.slot}`, decoded);
        return;
      }
      case "forget":
        for (const key of [...this.patches.keys()]) if (key.startsWith(`${command.sound}:`)) this.patches.delete(key);
        return;
      case "lane":
        this.dropLane(command.lane);
        this.lanes[command.lane] = { ratio: command.ratio ?? 1, depth: command.depth ?? 0 };
        return;
      case "release":
        this.dropLane(command.lane);
        return;
      case "reset":
        for (const voice of this.voices) if (voice.active) this.finish(voice);
        for (const id of this.pending.keys()) this.onEnded(id);
        this.pending.clear();
        this.queue = [];
        this.cancelled.clear();
        this.patches.clear();
        return;
      case "on": {
        const patch = (command.slot && this.patches.get(`${command.sound}:${command.slot}`)) || fallbackVoice(command.program ?? 0, command.drumKey);
        const queued = { ...command, patch };
        this.pending.set(command.id, queued);
        this.enqueue(queued);
        return;
      }
      case "cut": {
        const queued = this.pending.get(command.id);
        if (queued && command.at <= queued.at) {
          this.pending.delete(command.id);
          this.queue = this.queue.filter(entry => entry !== queued);
          this.onEnded(command.id);
          return;
        }
        this.enqueue(command);
        return;
      }
      default:
        this.enqueue(command);
    }
  }

  enqueue(command) {
    let index = this.queue.length;
    while (index > 0 && this.queue[index - 1].at > command.at) index--;
    this.queue.splice(index, 0, command);
  }

  // dropLane silences a lane at once and forgets what was queued for it: the
  // page reuses a released lane for another channel.
  dropLane(lane) {
    for (const voice of this.voices) if (voice.active && voice.lane === lane) this.finish(voice);
    this.queue = this.queue.filter(command => {
      if (command.lane !== lane) return true;
      if (command.op === "on") {
        this.pending.delete(command.id);
        this.onEnded(command.id);
      }
      return false;
    });
    this.lanes[lane] = { ratio: 1, depth: 0 };
  }

  finish(voice) {
    voice.active = false;
    this.onEnded(voice.id);
  }

  findVoice(id) {
    return this.voices.find(voice => voice.active && voice.id === id);
  }

  apply(command, offsetSeconds) {
    switch (command.op) {
      case "on":
        this.pending.delete(command.id);
        this.start(command, offsetSeconds);
        return;
      case "off": {
        const voice = this.findVoice(command.id);
        if (voice) this.keyOff(voice);
        return;
      }
      case "cut": {
        const voice = this.findVoice(command.id);
        if (voice) this.cut(voice, command.fade ?? 0.005);
        return;
      }
      case "bend": {
        this.lanes[command.lane].ratio = command.ratio;
        for (const voice of this.voices) if (voice.active && voice.lane === command.lane && !voice.drum) this.tune(voice);
        return;
      }
      case "depth":
        this.lanes[command.lane].depth = command.depth;
        for (const voice of this.voices) if (voice.active && voice.lane === command.lane) voice.wheel = command.depth * MODULATION_WHEEL_CENTS;
        return;
    }
  }

  // start sounds a note. A resumed note's age advances its envelopes without
  // rendering them, so a reconnect continues a held note rather than
  // replaying its attack.
  start(command, lateSeconds) {
    let voice = this.voices.find(candidate => !candidate.active);
    if (!voice) {
      // Steal the quietest voice, preferring one already released.
      let best = null, bestScore = -Infinity;
      for (const candidate of this.voices) {
        const score = (candidate.keyOn ? 0 : 1000) + candidate.operators[CARRIERS[candidate.algorithm][0]].attenuation;
        if (score > bestScore) [best, bestScore] = [candidate, score];
      }
      this.finish(best);
      voice = best;
    }
    const patch = command.patch;
    const pitch = Math.max(0, Math.min(127, (command.pitch ?? 60) + BASIC_OCTAVE_SEMITONES[patch.basicOctave ?? 1]));
    voice.active = true;
    voice.id = command.id;
    voice.lane = command.lane;
    voice.algorithm = patch.algorithm;
    voice.count = patch.algorithm < 2 ? 2 : 4;
    voice.patch = patch;
    voice.drum = command.drumKey !== undefined;
    voice.keyOn = true;
    voice.level = OUTPUT_LEVEL * Math.max(0, Math.min(1, (command.velocity ?? 100) / 127));
    voice.frequency = 440 * 2 ** ((pitch - 69) / 12);
    voice.fade = 1;
    voice.fadeStep = 0;
    voice.lfo = patch.lfo ?? 2;
    voice.wheel = this.lanes[voice.lane].depth * MODULATION_WHEEL_CENTS;
    voice.out1.fill(0);
    voice.out2.fill(0);
    voice.control = 0;
    const keyCode = keyCodeOf(pitch);
    const rate = this.sampleRate;
    for (let index = 0; index < voice.count; index++) {
      const source = patch.operators[index];
      const operator = voice.operators[index];
      operator.multiple = MULTIPLES[source.multiple] * 2 ** (DETUNE_CENTS[source.detune] / 1200);
      operator.wave = source.wave;
      operator.feedback = FEEDBACK_CYCLES[source.feedback];
      operator.ignoreKeyOff = source.ignoreKeyOff;
      operator.staticDb = source.totalLevel * 0.75 + KEY_SCALE_DB_PER_OCTAVE[source.keyScaleLevel] * Math.max(0, (pitch - 48) / 12);
      operator.vibratoCents = source.vibrato ? VIBRATO_CENTS[source.vibratoDepth] : 0;
      operator.tremoloDb = source.tremolo ? TREMOLO_DB[source.tremoloDepth] : 0;
      const attackRate = effectiveRate(source.attack, source.keyScaleRate, keyCode);
      operator.attackFactor = attackRate >= 60 ? 0 : attackRate === 0 ? 1 : Math.exp(Math.log(0.1 / SILENT_DB) / (attackSeconds(attackRate) * rate));
      operator.attackControl = operator.attackFactor ** CONTROL_SAMPLES;
      const step = value => {
        const effective = effectiveRate(value, source.keyScaleRate, keyCode);
        return effective === 0 ? 0 : SILENT_DB / (decaySeconds(effective) * rate);
      };
      operator.decayStep = step(source.decay);
      operator.sustainStep = step(source.sustainRate);
      // A release that never ends would hold a voice forever; the slowest
      // real rate stands in for zero.
      operator.releaseStep = step(Math.max(1, source.release));
      operator.sustainDb = source.sustainLevel === 15 ? 93 : source.sustainLevel * 3;
      operator.phase = 0;
      operator.gain = 0;
      operator.gainStep = 0;
      operator.step = 0;
      operator.attenuation = SILENT_DB;
      operator.stage = ATTACK;
      if (operator.attackFactor === 0) {
        operator.attenuation = 0;
        operator.stage = DECAY;
      }
    }
    this.tune(voice);
    const age = (command.age ?? 0) + Math.max(0, lateSeconds);
    if (age > 0) for (let index = 0; index < voice.count; index++) this.advance(voice.operators[index], age * rate);
  }

  tune(voice) {
    const lane = this.lanes[voice.lane];
    const ratio = voice.drum ? 1 : lane.ratio;
    for (let index = 0; index < voice.count; index++) {
      const operator = voice.operators[index];
      operator.increment = voice.frequency * operator.multiple * ratio / this.sampleRate;
    }
  }

  // advance moves an operator's envelope forward by a number of samples in
  // closed form, stage by stage.
  advance(operator, samples) {
    let left = samples;
    while (left > 0 && operator.stage !== OFF) {
      if (operator.stage === ATTACK) {
        if (operator.attackFactor === 1) return;
        const needed = Math.log(0.1 / Math.max(0.1, operator.attenuation)) / Math.log(operator.attackFactor);
        if (needed > left) {
          operator.attenuation *= operator.attackFactor ** left;
          return;
        }
        left -= needed;
        operator.attenuation = 0;
        operator.stage = DECAY;
      } else {
        const step = operator.stage === DECAY ? operator.decayStep : operator.stage === SUSTAIN ? operator.sustainStep : operator.releaseStep;
        const target = operator.stage === DECAY ? operator.sustainDb : SILENT_DB;
        if (step === 0) return;
        const needed = (target - operator.attenuation) / step;
        if (needed > left) {
          operator.attenuation += step * left;
          return;
        }
        left -= Math.max(0, needed);
        operator.attenuation = target;
        if (operator.stage === DECAY) operator.stage = SUSTAIN;
        else operator.stage = OFF;
      }
    }
  }

  keyOff(voice) {
    voice.keyOn = false;
    for (let index = 0; index < voice.count; index++) {
      const operator = voice.operators[index];
      if (!operator.ignoreKeyOff && operator.stage !== OFF) operator.stage = RELEASE;
    }
  }

  cut(voice, fadeSeconds) {
    const samples = Math.max(1, fadeSeconds * this.sampleRate);
    voice.fadeStep = Math.max(voice.fadeStep, voice.fade / samples);
  }

  // render writes frames samples into each lane, starting at the context
  // time startTime, applying every command due before the block ends.
  render(lanes, frames, startTime) {
    for (const lane of lanes) lane.fill(0, 0, frames);
    const rate = this.sampleRate;
    let position = 0;
    while (position < frames) {
      let until = frames;
      while (this.queue.length) {
        const command = this.queue[0];
        const offset = Math.round((command.at - startTime) * rate);
        if (offset > position) {
          until = Math.min(frames, offset);
          break;
        }
        this.queue.shift();
        this.apply(command, (startTime + position / rate) - command.at);
      }
      if (until > position) this.renderSpan(lanes, position, until, startTime);
      position = until;
    }
    this.time = startTime + frames / rate;
  }

  // renderSpan renders one voice at a time across the span, one control
  // period at a time, each operator through the whole period before the next:
  // the same arithmetic in the same order per sample, without a call per
  // operator per sample.
  renderSpan(lanes, from, to, startTime) {
    for (const voice of this.voices) {
      if (!voice.active) continue;
      const lane = lanes[voice.lane];
      let sample = from, ended = false;
      while (sample < to) {
        if (voice.control === 0 && this.control(voice, startTime + sample / this.sampleRate)) {
          ended = true;
          break;
        }
        const count = Math.min(voice.control, to - sample);
        ended = this.renderChunk(voice, lane, sample, count);
        voice.control -= count;
        sample += count;
        if (ended) break;
      }
      if (ended) this.finish(voice);
    }
  }

  // control moves a voice's envelopes, LFOs and pitch modulation on by one
  // control period and sets each operator's gain ramp toward where it will
  // be. It answers true when the voice has fallen silent.
  control(voice, time) {
    const lfo = SINE[((time * LFO_HZ[voice.lfo]) % 1 * SINE_SIZE) | 0];
    const wheel = voice.wheel ? SINE[((time * LFO_HZ[2]) % 1 * SINE_SIZE) | 0] : 0;
    for (let index = 0; index < voice.count; index++) {
      const operator = voice.operators[index];
      switch (operator.stage) {
        case ATTACK:
          operator.attenuation *= operator.attackControl;
          if (operator.attenuation < 0.1) {
            operator.attenuation = 0;
            operator.stage = DECAY;
          }
          break;
        case DECAY:
          operator.attenuation += operator.decayStep * CONTROL_SAMPLES;
          if (operator.attenuation >= operator.sustainDb) {
            operator.attenuation = operator.sustainDb;
            operator.stage = SUSTAIN;
          }
          break;
        case SUSTAIN:
          operator.attenuation += operator.sustainStep * CONTROL_SAMPLES;
          break;
        case RELEASE:
          operator.attenuation += operator.releaseStep * CONTROL_SAMPLES;
          break;
      }
      if (operator.stage !== OFF && operator.attenuation >= SILENT_DB) {
        operator.attenuation = SILENT_DB;
        operator.stage = OFF;
      }
      let target = 0;
      if (operator.stage !== OFF) {
        let db = operator.attenuation + operator.staticDb;
        if (operator.tremoloDb !== 0) db += operator.tremoloDb * (0.5 + 0.5 * lfo);
        const position = (db * GAIN_STEPS) | 0;
        target = position < GAIN.length ? GAIN[position] : 0;
      }
      operator.gainStep = (target - operator.gain) / CONTROL_SAMPLES;
      const cents = operator.vibratoCents * lfo + voice.wheel * wheel;
      operator.step = cents === 0 ? operator.increment : operator.increment * (1 + cents * 0.000577623); // ln(2)/1200
    }
    voice.control = CONTROL_SAMPLES;
    const carriers = CARRIERS[voice.algorithm];
    for (let index = 0; index < carriers.length; index++) {
      const operator = voice.operators[carriers[index]];
      if (operator.stage === ATTACK || operator.stage !== OFF && operator.attenuation + operator.staticDb < AUDIBLE_DB) return false;
    }
    return true;
  }

  // runOperator renders count samples of one operator into output, phase
  // modulated by modulation (cycles, or null for none). Only the first and
  // third operators feed back into themselves.
  runOperator(voice, index, count, modulation, output) {
    const operator = voice.operators[index];
    const wave = operator.wave, step = operator.step, gainStep = operator.gainStep;
    const feedback = index === 0 || index === 2 ? operator.feedback * 0.5 : 0;
    let phase = operator.phase, gain = operator.gain;
    let previous = voice.out1[index], older = voice.out2[index];
    for (let sample = 0; sample < count; sample++) {
      phase += step;
      if (phase >= 1) phase -= 1; // a step is always below one cycle
      let input = modulation === null ? 0 : modulation[sample];
      if (feedback !== 0) input += (previous + older) * feedback;
      let value = 0;
      if (gain > 0) {
        value = (wave === 0 ? SINE[(((phase + input) * SINE_SIZE) | 0) & SINE_MASK] : waveAt(wave, phase + input)) * gain;
      }
      gain += gainStep;
      older = previous;
      previous = value;
      output[sample] = value;
    }
    operator.phase = phase;
    operator.gain = gain;
    voice.out1[index] = previous;
    voice.out2[index] = older;
  }

  // modulate scales count operator outputs (summed when there are two) into
  // the phase deviation, in cycles, they drive a carrier by.
  modulate(count, first, second = null) {
    const modulation = this.modulation;
    for (let sample = 0; sample < count; sample++) {
      modulation[sample] = (second === null ? first[sample] : first[sample] + second[sample]) * MODULATION_CYCLES;
    }
    return modulation;
  }

  // renderChunk renders count samples of a voice into its lane through the
  // algorithm's connections and the voice's level and fade. It answers true
  // when a fade has finished the voice.
  renderChunk(voice, lane, offset, count) {
    const [a, b, c, d] = this.scratch;
    let first = null, second = null, third = null, fourth = null;
    this.runOperator(voice, 0, count, null, a);
    switch (voice.algorithm) {
      case 0:
        this.runOperator(voice, 1, count, this.modulate(count, a), b);
        first = b;
        break;
      case 1:
        this.runOperator(voice, 1, count, null, b);
        first = a; second = b;
        break;
      case 2:
        this.runOperator(voice, 1, count, null, b);
        this.runOperator(voice, 2, count, null, c);
        this.runOperator(voice, 3, count, null, d);
        first = a; second = b; third = c; fourth = d;
        break;
      case 3:
        this.runOperator(voice, 1, count, null, b);
        this.runOperator(voice, 2, count, this.modulate(count, b), c);
        this.runOperator(voice, 3, count, this.modulate(count, a, c), d);
        first = d;
        break;
      case 4:
        this.runOperator(voice, 1, count, this.modulate(count, a), b);
        this.runOperator(voice, 2, count, this.modulate(count, b), c);
        this.runOperator(voice, 3, count, this.modulate(count, c), d);
        first = d;
        break;
      case 5:
        this.runOperator(voice, 1, count, this.modulate(count, a), b);
        this.runOperator(voice, 2, count, null, c);
        this.runOperator(voice, 3, count, this.modulate(count, c), d);
        first = b; second = d;
        break;
      case 6:
        this.runOperator(voice, 1, count, null, b);
        this.runOperator(voice, 2, count, this.modulate(count, b), c);
        this.runOperator(voice, 3, count, this.modulate(count, c), d);
        first = a; second = d;
        break;
      default:
        this.runOperator(voice, 1, count, null, b);
        this.runOperator(voice, 2, count, this.modulate(count, b), c);
        this.runOperator(voice, 3, count, null, d);
        first = a; second = c; third = d;
    }
    const level = voice.level;
    for (let sample = 0; sample < count; sample++) {
      let value = first[sample];
      if (second !== null) value += second[sample];
      if (third !== null) value += third[sample];
      if (fourth !== null) value += fourth[sample];
      if (voice.fadeStep !== 0) {
        voice.fade -= voice.fadeStep;
        if (voice.fade <= 0) return true;
      }
      if (lane) lane[offset + sample] += value * level * voice.fade;
    }
    return false;
  }
}

// The AudioWorklet side. In a window this block does nothing; the page then
// imports FMSynth for its ScriptProcessor.
if (typeof registerProcessor === "function") {
  registerProcessor("wfeature-fm", class extends AudioWorkletProcessor {
    constructor() {
      super();
      this.ended = [];
      this.synth = new FMSynth(sampleRate);
      this.synth.onEnded = id => this.ended.push(id);
      this.port.onmessage = event => {
        for (const command of event.data) this.synth.handle(command);
        this.report();
      };
    }

    report() {
      if (this.ended.length) {
        this.port.postMessage(this.ended);
        this.ended = [];
      }
    }

    process(inputs, outputs) {
      const lanes = outputs[0];
      if (lanes && lanes.length) this.synth.render(lanes, lanes[0].length, currentTime);
      this.report();
      return true;
    }
  });
}
