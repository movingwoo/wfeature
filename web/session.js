// The server session: the emulator runs on the server and this page draws what
// it sends. That is the whole reason this file exists — the browser build is
// about fifteen times slower on a phone than on a desktop, and the cost is the
// WebAssembly backend rather than the emulator, so the only way a phone plays
// at full speed is to stop emulating and start watching.
//
// What crosses the socket, in protocol 2: JSON text in both directions for
// everything small, and binary messages for pictures and sound. A picture is
// the game's own size, sent as an update to the one this page holds, and is
// decoded with createImageBitmap, off the main thread; magnifying it is this
// page's job (magnify.js). Sound is compact binary with each sample carried
// once (audio-stream.js). docs/session.md has the whole protocol.

import { AudioStream, isAudioMessage } from "./audio-stream.js";
import { FrameReceiver } from "./frame-stream.js";

// sessionURL is the page's own origin with the websocket scheme, so a session
// reaches the server the page came from without anything to configure. A page
// served over https gets wss, which is what a reverse proxy in front of this
// would need. A server that does not know protocol 2 answers in protocol 1,
// which this page also reads.
export const sessionURL = () => {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${location.host}/api/session?protocol=2`;
};

// available reports whether this browser can hold a session at all. Everything
// here is old enough to be everywhere, but a page that cannot open one should
// say so rather than hang.
export const sessionAvailable = () =>
  typeof WebSocket === "function" && typeof createImageBitmap === "function";

export class GameSession {
  // handlers: onFrame(canvas, { scale, dirty }), onAudio(events), onVibrate(request), onStarted(info),
  // onExited(reason), onError(message), onStats(stats), onClosed().
  constructor(handlers = {}) {
    this.handlers = handlers;
    this.socket = null;
    // pending maps a request id to the resolver waiting for its answer. Only
    // the messages that ask something use one; a key press does not.
    this.pending = new Map();
    this.nextId = 1;
    this.closed = false;
    // The sounds this connection's server has defined; a new connection
    // starts with none, and so does the server's record of them.
    this.audio = new AudioStream();
    this.frames = new FrameReceiver(
      (canvas, presentation) => { if (!this.closed) this.handlers.onFrame?.(canvas, presentation); },
      error => {
        console.warn("wfeature frame could not be decoded", error);
        // A missing patch invalidates subsequent pictures. Reconnect through
        // session-link so the retained game supplies a complete frame again.
        this.close();
      },
    );
    // The server's build profile, known from the moment it says it is ready.
    // The page hides the developer's half of its interface unless a debug
    // build answered, so a release is not a page with parts switched off — it
    // never builds them.
    this.profile = "";
  }

  // open connects and resolves once the server says it is ready to take a
  // game. It rejects if the socket fails before that.
  open(timeoutMillis = 10000) {
    return new Promise((resolve, reject) => {
      let socket;
      try {
        socket = new WebSocket(sessionURL());
      } catch (error) {
        reject(error);
        return;
      }
      // Binary messages are read as bytes: a picture's header and a sound's
      // operations are parsed here, and reading a blob would make that
      // asynchronous and let a sound overtake the picture before it.
      socket.binaryType = "arraybuffer";
      this.socket = socket;

      let ready = false;
      const timer = setTimeout(() => {
        reject(new Error("세션 서버가 응답하지 않습니다."));
        this.close();
      }, timeoutMillis);
      socket.addEventListener("message", event => {
        if (this.closed) return;
        if (typeof event.data !== "string") {
          if (isAudioMessage(event.data)) this.#receiveAudio(event.data);
          else this.frames.receive(event.data);
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
          clearTimeout(timer);
          this.profile = message.profile ?? "";
          resolve(this);
          return;
        }
        this.#receive(message);
      });
      socket.addEventListener("error", () => {
        if (!ready) { clearTimeout(timer); reject(new Error("세션 서버에 연결하지 못했습니다.")); this.close(); }
      });
      socket.addEventListener("close", () => {
        this.closed = true;
        this.frames.close();
        clearTimeout(timer);
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

  #receive(message) {
    // An answer to something that was asked goes to whoever asked it, and
    // nowhere else: a cheat command's reply is not a session-wide event.
    if (message.id && this.pending.has(message.id)) {
      const { resolve, reject } = this.pending.get(message.id);
      this.pending.delete(message.id);
      if (message.kind === "error") {
        const failure = new Error(message.message);
        // An ending is refused the same way a failure is — the request
        // produced no game either way — and carries the mark that says which
        // of the two it was, so whoever asked can say the right sentence.
        failure.exited = message.exited === true;
        reject(failure);
      } else resolve(message);
      return;
    }
    switch (message.kind) {
      case "detached":
        this.handlers.onDetached?.();
        break;
      case "started":
        this.handlers.onStarted?.(message.started);
        break;
      case "exited":
        // The reason travels with the ending: a game of this era quits itself
        // for ordinary reasons, and the run log is where that gets read.
        this.handlers.onExited?.(message.message ?? "");
        break;
      case "audio":
        this.handlers.onAudio?.(message.audio ?? []);
        break;
      case "stats":
        this.handlers.onStats?.(message.stats);
        break;
      case "vibrate":
        // One request of the handset's motor, sent when the guest makes one
        // rather than every tick. What to do with it — including nothing —
        // belongs to the page; see vibrate.js.
        this.handlers.onVibrate?.(message.vibrate ?? {});
        break;
      case "error":
        if (message.exited) this.handlers.onExited?.(message.message ?? "");
        this.handlers.onError?.(message.message ?? "세션 오류");
        break;
      default:
        break;
    }
  }

  #receiveAudio(buffer) {
    let events;
    try {
      events = this.audio.decode(buffer);
    } catch (error) {
      // One malformed batch is one lost moment of sound, not a lost session.
      console.warn("wfeature sound could not be decoded", error);
      return;
    }
    if (events.length) this.handlers.onAudio?.(events);
  }

  #send(message) {
    if (!this.socket || this.closed || this.socket.readyState !== 1) return false;
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
  start(gamePath, scale = 1, screen = null, token = "", confirmation = "") {
    const message = { kind: "start", game: gamePath, value: scale };
    if (token) message.token = token;
    if (confirmation) message.confirmation = confirmation;
    // The screen travels only when it is not the server's own default, so a
    // page that never opened the setting sends what it always sent.
    if (screen && (screen.width !== 240 || screen.height !== 320)) {
      message.width = screen.width;
      message.height = screen.height;
    }
    return this.ask(message, 300000);
  }

  // A connected owner is reported as occupied unless takeover was explicit.
  resume(token, takeover = false) {
    return this.ask({ kind: "resume", token, ...(takeover ? { takeover: true } : {}) });
  }

  openTextInput() { return this.ask({ kind: "text", action: "open" }); }
  commitTextInput(edit, text) { return this.ask({ kind: "text", action: "commit", edit, text }); }
  cancelTextInput(edit) { return this.ask({ kind: "text", action: "cancel", edit }); }

  ping() { return this.ask({ kind: "ping" }, 10000); }
  park() { return this.ask({ kind: "park" }); }
  stop() { return this.ask({ kind: "stop" }); }

  sendKey(action, code) {
    this.#send({ kind: "key", action, code });
  }

  // The coordinates are the game's own screen. Undoing the canvas geometry is
  // the page's job because the geometry is the page's; see web/touch.js.
  sendPointer(action, x, y) {
    this.#send({ kind: "pointer", action, x, y });
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
    this.frames.close();
    for (const { reject } of this.pending.values()) {
      reject(new Error("세션 연결이 끊어졌습니다."));
    }
    this.pending.clear();
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
      // The first protocol carries bytes as base64 text; the second has
      // already decoded them.
      case "sysex":
        if (event.data) audio.sysex(typeof event.data === "string" ? decodeBytes(event.data) : event.data);
        break;
      case "playWave":
        if (event.samples) {
          const samples = typeof event.samples === "string" ? decodeSamples(event.samples) : event.samples;
          audio.playWave(event.channels ?? 1, event.rate ?? 8000, samples);
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
