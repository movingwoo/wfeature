// Sound from a server speaking protocol 2. A message is binary: "WFA2" and the
// tick's operations, with each sampled sound and SysEx message defined once
// and named by an id afterwards. internal/webhost/audio_stream.go is the other
// end and lays the format out; decode answers events in the shape the first
// protocol's JSON has, so the synthesiser is driven the same way by both.

const audioMagic = 0x57464132; // "WFA2"

export const isAudioMessage = buffer =>
  buffer.byteLength >= 4 && new DataView(buffer).getUint32(0) === audioMagic;

// Signed 16-bit little-endian frames to the floats an AudioBuffer holds.
const toSamples = bytes => {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const samples = new Float32Array(bytes.byteLength >> 1);
  for (let index = 0; index < samples.length; index++) samples[index] = view.getInt16(index * 2, true) / 32768;
  return samples;
};

export class AudioStream {
  constructor() {
    // What the server has defined on this connection, by id. The server
    // bounds it and says when to start over.
    this.definitions = new Map();
  }

  // decode answers one message's events. A message that ends inside an
  // operation or names an operation this page does not know is refused whole.
  decode(buffer) {
    const bytes = new Uint8Array(buffer);
    const view = new DataView(buffer);
    const events = [];
    let at = 4;
    // Selections start over for every message; a dropped batch cannot leave
    // later events attached to a different sound or time.
    let sound = 0;
    let presentationTime = null;
    const emit = event => {
      if (sound !== 0) event.sound = sound;
      if (presentationTime !== null) event.at = presentationTime;
      events.push(event);
    };
    const need = count => {
      if (at + count > bytes.length) throw new Error("sound message ends inside an operation");
    };
    const readTime = () => {
      need(8);
      const seconds = view.getFloat64(at);
      at += 8;
      if (!Number.isFinite(seconds)) throw new Error("sound message has a nonfinite time");
      return seconds;
    };
    while (at < bytes.length) {
      const operation = bytes[at++];
      switch (operation) {
        case 0x01:
          need(3);
          emit({ kind: "noteOn", channel: bytes[at], note: bytes[at + 1], velocity: bytes[at + 2] });
          at += 3;
          break;
        case 0x02:
          need(3);
          emit({ kind: "noteOff", channel: bytes[at], note: bytes[at + 1], velocity: bytes[at + 2] });
          at += 3;
          break;
        case 0x03:
          need(2);
          emit({ kind: "programChange", channel: bytes[at], program: bytes[at + 1] });
          at += 2;
          break;
        case 0x04:
          need(3);
          emit({ kind: "controlChange", channel: bytes[at], control: bytes[at + 1], value: bytes[at + 2] });
          at += 3;
          break;
        case 0x05:
          need(3);
          emit({ kind: "pitchBend", channel: bytes[at], value: view.getUint16(at + 1) });
          at += 3;
          break;
        case 0x06:
          events.push({ kind: "allOff" });
          presentationTime = null;
          break;
        case 0x07:
          need(4);
          sound = view.getUint32(at);
          at += 4;
          break;
        case 0x08:
          emit({ kind: "stopSound" });
          break;
        case 0x09:
          need(2);
          emit({ kind: "soundGain", value: view.getUint16(at) });
          at += 2;
          break;
        case 0x0a:
          need(7);
          emit({ kind: "noteResume", channel: bytes[at], note: bytes[at + 1], velocity: bytes[at + 2], age: view.getUint32(at + 3) });
          at += 7;
          break;
        case 0x0b: {
          need(1);
          const present = bytes[at++];
          if (present === 0) presentationTime = null;
          else if (present === 1) presentationTime = readTime();
          else throw new Error("sound message has an invalid time selection");
          break;
        }
        case 0x0c:
          events.push({ kind: "clock", at: readTime() });
          break;
        case 0x10: {
          need(8);
          const id = view.getUint32(at), length = view.getUint32(at + 4);
          at += 8;
          need(length);
          this.definitions.set(id, { bytes: bytes.slice(at, at + length), samples: null });
          at += length;
          break;
        }
        case 0x11:
        case 0x14:
        case 0x16: {
          const routed = operation === 0x14;
          const resumed = operation === 0x16;
          need(resumed ? 15 : routed ? 11 : 9);
          const definition = this.definitions.get(view.getUint32(at));
          const channels = bytes[at + 4], rate = view.getUint32(at + 5);
          const pcmChannel = routed || resumed ? view.getUint16(at + 9) : 0;
          const framePhase = resumed ? view.getUint32(at + 11) : 0;
          at += resumed ? 15 : routed ? 11 : 9;
          if (routed && (pcmChannel === 0 || channels !== 1)) throw new Error("sound message has an invalid PCM route");
          if (resumed && (framePhase >= 1e9 || pcmChannel !== 0 && channels !== 1)) throw new Error("sound message has an invalid PCM phase or route");
          // A sound whose definition never arrived is skipped, not guessed.
          if (definition) {
            definition.samples ??= toSamples(definition.bytes);
            // Definition samples stay immutable across triggers. A new
            // definition or forget creates a new identity even if an ID repeats.
            const event = { kind: "playWave", channels, rate, samples: definition.samples, cacheable: true };
            if (routed || pcmChannel !== 0) event.pcmChannel = pcmChannel;
            if (resumed) event.framePhase = framePhase;
            emit(event);
          }
          break;
        }
        case 0x12: {
          need(4);
          const definition = this.definitions.get(view.getUint32(at));
          at += 4;
          if (definition) emit({ kind: "sysex", data: definition.bytes });
          break;
        }
        case 0x13:
          this.definitions.clear();
          break;
        case 0x15: {
          need(4);
          const pcmChannel = view.getUint16(at), control = bytes[at + 2], value = bytes[at + 3];
          at += 4;
          if (pcmChannel === 0 || ![7, 10, 11].includes(control) || value > 127) throw new Error("sound message has an invalid PCM control");
          emit({ kind: "pcmControl", pcmChannel, control, value });
          break;
        }
        default:
          throw new Error(`unknown sound operation ${operation}`);
      }
    }
    return events;
  }
}
