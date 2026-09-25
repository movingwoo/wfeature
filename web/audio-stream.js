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
    const need = count => {
      if (at + count > bytes.length) throw new Error("sound message ends inside an operation");
    };
    while (at < bytes.length) {
      const operation = bytes[at++];
      switch (operation) {
        case 0x01:
          need(3);
          events.push({ kind: "noteOn", channel: bytes[at], note: bytes[at + 1], velocity: bytes[at + 2] });
          at += 3;
          break;
        case 0x02:
          need(3);
          events.push({ kind: "noteOff", channel: bytes[at], note: bytes[at + 1], velocity: bytes[at + 2] });
          at += 3;
          break;
        case 0x03:
          need(2);
          events.push({ kind: "programChange", channel: bytes[at], program: bytes[at + 1] });
          at += 2;
          break;
        case 0x04:
          need(3);
          events.push({ kind: "controlChange", channel: bytes[at], control: bytes[at + 1], value: bytes[at + 2] });
          at += 3;
          break;
        case 0x05:
          need(3);
          events.push({ kind: "pitchBend", channel: bytes[at], value: view.getUint16(at + 1) });
          at += 3;
          break;
        case 0x06:
          events.push({ kind: "allOff" });
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
        case 0x11: {
          need(9);
          const definition = this.definitions.get(view.getUint32(at));
          const channels = bytes[at + 4], rate = view.getUint32(at + 5);
          at += 9;
          // A sound whose definition never arrived is skipped, not guessed.
          if (definition) {
            definition.samples ??= toSamples(definition.bytes);
            events.push({ kind: "playWave", channels, rate, samples: definition.samples });
          }
          break;
        }
        case 0x12: {
          need(4);
          const definition = this.definitions.get(view.getUint32(at));
          at += 4;
          if (definition) events.push({ kind: "sysex", data: definition.bytes });
          break;
        }
        case 0x13:
          this.definitions.clear();
          break;
        default:
          throw new Error(`unknown sound operation ${operation}`);
      }
    }
    return events;
  }
}
