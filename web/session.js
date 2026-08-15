// The server session: the emulator runs on the server and this page draws what
// it sends. That is the whole reason this file exists — the browser build is
// about fifteen times slower on a phone than on a desktop, and the cost is the
// WebAssembly backend rather than the emulator, so the only way a phone plays
// at full speed is to stop emulating and start watching.
//
// What crosses the socket: JSON text in both directions for everything small,
// and one binary message per frame carrying a PNG. Frames are decoded with
// createImageBitmap, which hands the work to the browser's own decoder off the
// main thread — the phone's job is now a bitmap blit twenty times a second.

// sessionURL is the page's own origin with the websocket scheme, so a session
// reaches the server the page came from without anything to configure. A page
// served over https gets wss, which is what a reverse proxy in front of this
// would need.
export const sessionURL = () => {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${location.host}/api/session`;
};

// available reports whether this browser can hold a session at all. Everything
// here is old enough to be everywhere, but a page that cannot open one should
// say so rather than hang.
export const sessionAvailable = () =>
  typeof WebSocket === "function" && typeof createImageBitmap === "function";

export class GameSession {
  // handlers: onFrame(bitmap), onAudio(events), onStarted(info), onExited(),
  // onError(message), onStats(stats), onClosed().
  constructor(handlers = {}) {
    this.handlers = handlers;
    this.socket = null;
    // pending maps a request id to the resolver waiting for its answer. Only
    // the messages that ask something use one; a key press does not.
    this.pending = new Map();
    this.nextId = 1;
    this.closed = false;
    // The server's build profile, known from the moment it says it is ready.
    // The page hides the developer's half of its interface unless a debug
    // build answered, so a release is not a page with parts switched off — it
    // never builds them.
    this.profile = "";
  }

  // open connects and resolves once the server says it is ready to take a
  // game. It rejects if the socket fails before that.
  open() {
    return new Promise((resolve, reject) => {
      let socket;
      try {
        socket = new WebSocket(sessionURL());
      } catch (error) {
        reject(error);
        return;
      }
      // Frames arrive as blobs so they can go straight to createImageBitmap.
      socket.binaryType = "blob";
      this.socket = socket;

      let ready = false;
      socket.addEventListener("message", event => {
        if (typeof event.data !== "string") {
          this.#receiveFrame(event.data);
          return;
        }
        let message;
        try {
          message = JSON.parse(event.data);
        } catch {
          return;
        }
        if (message.kind === "ready" && !ready) {
          ready = true;
          this.profile = message.profile ?? "";
          resolve(this);
          return;
        }
        this.#receive(message);
      });
      socket.addEventListener("error", () => {
        if (!ready) reject(new Error("세션 서버에 연결하지 못했습니다."));
      });
      socket.addEventListener("close", () => {
        this.closed = true;
        // Everything still waiting for an answer is never getting one.
        for (const { reject: rejectPending } of this.pending.values()) {
          rejectPending(new Error("세션 연결이 끊어졌습니다."));
        }
        this.pending.clear();
        if (!ready) reject(new Error("세션 서버에 연결하지 못했습니다."));
        this.handlers.onClosed?.();
      });
    });
  }

  async #receiveFrame(blob) {
    try {
      // createImageBitmap decodes off the main thread, which is what keeps a
      // phone's frame budget for drawing rather than decoding.
      const bitmap = await createImageBitmap(blob);
      this.handlers.onFrame?.(bitmap);
    } catch (error) {
      console.warn("wfeature frame could not be decoded", error);
    }
  }

  #receive(message) {
    // An answer to something that was asked goes to whoever asked it, and
    // nowhere else: a cheat command's reply is not a session-wide event.
    if (message.id && this.pending.has(message.id)) {
      const { resolve, reject } = this.pending.get(message.id);
      this.pending.delete(message.id);
      if (message.kind === "error") reject(new Error(message.message));
      else resolve(message);
      return;
    }
    switch (message.kind) {
      case "started":
        this.handlers.onStarted?.(message.started);
        break;
      case "exited":
        this.handlers.onExited?.();
        break;
      case "audio":
        this.handlers.onAudio?.(message.audio ?? []);
        break;
      case "stats":
        this.handlers.onStats?.(message.stats);
        break;
      case "error":
        this.handlers.onError?.(message.message ?? "세션 오류");
        break;
      default:
        break;
    }
  }

  #send(message) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return false;
    this.socket.send(JSON.stringify(message));
    return true;
  }

  // ask sends a message and waits for the answer carrying the same id.
  ask(message, timeoutMillis = 30000) {
    return new Promise((resolve, reject) => {
      const id = this.nextId++;
      // The timer is cleared by whichever of the two outcomes happens first.
      // Leaving it armed held the page awake for the whole timeout after every
      // answered request, which a game asking for cheat refreshes does often.
      let timer = 0;
      const settle = handler => value => {
        clearTimeout(timer);
        handler(value);
      };
      this.pending.set(id, { resolve: settle(resolve), reject: settle(reject) });
      if (!this.#send({ ...message, id })) {
        this.pending.delete(id);
        reject(new Error("세션이 연결되어 있지 않습니다."));
        return;
      }
      timer = setTimeout(() => {
        if (!this.pending.has(id)) return;
        this.pending.delete(id);
        reject(new Error("세션이 응답하지 않습니다."));
      }, timeoutMillis);
    });
  }

  // start loads a game on the server. A KTF title's start takes tens of
  // seconds inside the guest, so the wait is long by nature rather than by
  // fault, and the answer only arrives when the game is up.
  start(gamePath, scale = 1) {
    return this.ask({ kind: "start", game: gamePath, value: scale }, 300000);
  }

  // resume asks for the game the server parked when this page's last socket
  // closed. The answer is a "started" message when the game is still there and
  // a "resumed" one saying it is not when the window has run out — a phone that
  // was away too long has nothing to come back to, which is an answer rather
  // than a failure.
  resume(token) {
    return this.ask({ kind: "resume", token });
  }

  sendKey(action, code) {
    this.#send({ kind: "key", action, code });
  }

  setSpeed(multiplier) {
    this.#send({ kind: "speed", value: multiplier });
  }

  setScale(scale) {
    this.#send({ kind: "scale", value: scale });
  }

  cheat(command) {
    return this.ask({ kind: "cheat", command });
  }

  report(label) {
    return this.ask({ kind: "report", label }, 60000);
  }

  close() {
    this.closed = true;
    this.socket?.close();
  }
}

// decodeSamples turns the base64 signed 16-bit frames a sampled sound arrives
// as into the normalised floats an AudioBuffer holds. JSON numbers would have
// cost several times the bytes for the same sound.
export const decodeSamples = encoded => {
  const binary = atob(encoded);
  const samples = new Float32Array(binary.length / 2);
  for (let index = 0; index < samples.length; index++) {
    const low = binary.charCodeAt(index * 2);
    const high = binary.charCodeAt(index * 2 + 1);
    const value = (high << 8) | low;
    samples[index] = (value >= 0x8000 ? value - 0x10000 : value) / 32768;
  }
  return samples;
};

const decodeBytes = encoded => {
  const binary = atob(encoded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < bytes.length; index++) bytes[index] = binary.charCodeAt(index);
  return bytes;
};

// playAudioEvents replays one batch on the page's synthesiser. The event names
// are the sink's own method names, so the browser side of sound is unchanged by
// moving emulation to the server: the calls simply arrive over a socket.
export const playAudioEvents = (audio, events) => {
  if (!audio) return;
  for (const event of events) {
    switch (event.kind) {
      case "noteOn":
        audio.noteOn(event.channel ?? 0, event.note ?? 0, event.velocity ?? 0);
        break;
      case "noteOff":
        audio.noteOff(event.channel ?? 0, event.note ?? 0, event.velocity ?? 0);
        break;
      case "programChange":
        audio.programChange(event.channel ?? 0, event.program ?? 0);
        break;
      case "controlChange":
        audio.controlChange(event.channel ?? 0, event.control ?? 0, event.value ?? 0);
        break;
      case "pitchBend":
        audio.pitchBend(event.channel ?? 0, event.value ?? 0);
        break;
      case "sysex":
        if (event.data) audio.sysex(decodeBytes(event.data));
        break;
      case "playWave":
        if (event.samples) {
          audio.playWave(event.channels ?? 1, event.rate ?? 8000, decodeSamples(event.samples));
        }
        break;
      case "allOff":
        // The game ended; nothing is left to release the notes it was holding.
        audio.stopAll();
        break;
      default:
        break;
    }
  }
};
