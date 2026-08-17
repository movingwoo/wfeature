import assert from "node:assert/strict";
import { test } from "node:test";

import { GameSession, decodeSamples, playAudioEvents } from "./session.js";

// The audio path is where the session protocol has to be exactly right and
// cannot be checked by looking at the screen: a wrong sample decode is not
// silence, it is noise, and a dropped note-off is a note that never stops.

test("decodes sampled sound back to the floats an AudioBuffer holds", () => {
  const samples = Int16Array.from([0, 16384, -16384, 32767, -32768]);
  const encoded = Buffer.from(samples.buffer).toString("base64");
  const decoded = decodeSamples(encoded);

  assert.equal(decoded.length, samples.length);
  assert.equal(decoded[0], 0);
  assert.equal(decoded[1], 0.5);
  assert.equal(decoded[2], -0.5);
  // The extremes are where a sign error shows up: the top of the range must
  // stay just under one and the bottom must be exactly minus one.
  assert.ok(decoded[3] > 0.999 && decoded[3] <= 1);
  assert.equal(decoded[4], -1);
});

// recordingSynth stands in for the page's synthesiser, which needs a real
// AudioContext.
const recordingSynth = () => {
  const calls = [];
  return {
    calls,
    noteOn: (...args) => calls.push(["noteOn", ...args]),
    noteOff: (...args) => calls.push(["noteOff", ...args]),
    programChange: (...args) => calls.push(["programChange", ...args]),
    controlChange: (...args) => calls.push(["controlChange", ...args]),
    pitchBend: (...args) => calls.push(["pitchBend", ...args]),
    sysex: data => calls.push(["sysex", [...data]]),
    playWave: (channels, rate, samples) => calls.push(["playWave", channels, rate, samples.length]),
    stopAll: () => calls.push(["stopAll"]),
  };
};

test("replays a batch of events onto the synthesiser in order", () => {
  const synth = recordingSynth();
  playAudioEvents(synth, [
    { kind: "programChange", channel: 1, program: 42 },
    { kind: "noteOn", channel: 1, note: 60, velocity: 100 },
    { kind: "controlChange", channel: 1, control: 7, value: 90 },
    { kind: "pitchBend", channel: 1, value: 9000 },
    { kind: "noteOff", channel: 1, note: 60, velocity: 0 },
    { kind: "sysex", data: Buffer.from([0xf0, 0x7e, 0xf7]).toString("base64") },
    { kind: "playWave", channels: 1, rate: 8000, samples: Buffer.from(new Int16Array(8).buffer).toString("base64") },
    { kind: "allOff" },
  ]);

  assert.deepEqual(synth.calls, [
    ["programChange", 1, 42],
    ["noteOn", 1, 60, 100],
    ["controlChange", 1, 7, 90],
    ["pitchBend", 1, 9000],
    ["noteOff", 1, 60, 0],
    ["sysex", [0xf0, 0x7e, 0xf7]],
    ["playWave", 1, 8000, 8],
    ["stopAll"],
  ]);
});

test("omitted fields default rather than arriving as undefined", () => {
  // The server leaves a zero out of the JSON, so channel 0 and note 0 reach
  // the page as absent fields. A synthesiser handed undefined would produce
  // nothing at all.
  const synth = recordingSynth();
  playAudioEvents(synth, [{ kind: "noteOn" }, { kind: "noteOff" }]);
  assert.deepEqual(synth.calls, [
    ["noteOn", 0, 0, 0],
    ["noteOff", 0, 0, 0],
  ]);
});

test("an unknown event is ignored rather than thrown on", () => {
  const synth = recordingSynth();
  playAudioEvents(synth, [{ kind: "hum" }, { kind: "noteOn", channel: 2, note: 64, velocity: 80 }]);
  assert.deepEqual(synth.calls, [["noteOn", 2, 64, 80]]);
});

test("a page with no audio graph drops the batch instead of failing", () => {
  playAudioEvents(null, [{ kind: "noteOn", channel: 0, note: 60, velocity: 100 }]);
});

// The resume path is the other place the protocol has to be exactly right and
// cannot be seen on screen: a page that asks wrongly gets its game back as a
// silent "no", and a page that reads the answer wrongly throws away a game the
// server is still holding.

// fakeSocket stands in for the browser's WebSocket. It records what the page
// sent and lets a test deliver what the server would have answered.
const fakeSocket = () => {
  const listeners = new Map();
  const socket = {
    sent: [],
    readyState: 1,
    binaryType: "",
    addEventListener: (kind, handler) => {
      listeners.set(kind, [...(listeners.get(kind) ?? []), handler]);
    },
    send: text => socket.sent.push(JSON.parse(text)),
    close: () => {},
    deliver: message => {
      for (const handler of listeners.get("message") ?? []) {
        handler({ data: JSON.stringify(message) });
      }
    },
  };
  return socket;
};

// openFakeSession installs the fake for one test and hands back an opened
// session with the socket behind it.
const openFakeSession = async (handlers = {}) => {
  const socket = fakeSocket();
  const previous = globalThis.WebSocket;
  const previousBitmap = globalThis.createImageBitmap;
  // sessionURL reads the page's own origin, which node has no notion of.
  const previousLocation = globalThis.location;
  globalThis.location = { protocol: "http:", host: "localhost:11541" };
  globalThis.WebSocket = function () { return socket; };
  globalThis.WebSocket.OPEN = 1;
  globalThis.createImageBitmap = async () => ({});
  const session = new GameSession(handlers);
  const opening = session.open();
  socket.deliver({ kind: "ready", profile: "debug" });
  await opening;
  globalThis.WebSocket = previous;
  globalThis.createImageBitmap = previousBitmap;
  globalThis.location = previousLocation;
  return { session, socket };
};

test("a resume asks by token and takes the game back from the answer", async () => {
  const { session, socket } = await openFakeSession();

  const asking = session.resume("cafe1234");
  const sent = socket.sent.at(-1);
  assert.equal(sent.kind, "resume");
  assert.equal(sent.token, "cafe1234");
  // Without the id the answer would be read as a fresh game starting, and the
  // page would have no way to tell a resumed session from a new one.
  assert.ok(sent.id > 0);

  socket.deliver({
    kind: "started",
    id: sent.id,
    started: { platform: "skt", width: 240, height: 320, token: "cafe1234" },
  });
  const answer = await asking;
  assert.equal(answer.started.platform, "skt");
  assert.equal(answer.started.token, "cafe1234");
});

test("a token the server no longer holds resolves rather than throwing", async () => {
  const { session, socket } = await openFakeSession();

  const asking = session.resume("expired");
  const sent = socket.sent.at(-1);
  socket.deliver({ kind: "resumed", id: sent.id, resumed: false, message: "이어서 진행할 게임이 없습니다." });

  // The page decides what to do about it: forget the token and offer the game
  // list. A rejection here would turn the ordinary end of a long absence into
  // an error the page has to catch.
  const answer = await asking;
  assert.equal(answer.resumed, false);
  assert.ok(!answer.started);
});

// The screen a game runs on travels with the start, and only when it is not
// the one the server would have chosen: a page that never opened the setting
// has to send exactly what it always sent.
test("a start carries a screen only when it is not the default", async () => {
  const { session, socket } = await openFakeSession();
  // Each start is answered, because an unanswered ask holds a five-minute
  // timer open and the test would wait for it.
  const started = async (path, scale, screen) => {
    const asking = session.start(path, scale, screen);
    const sent = socket.sent.at(-1);
    socket.deliver({ kind: "started", id: sent.id, started: { platform: "skt", width: 240, height: 320 } });
    await asking;
    return sent;
  };

  const asDefault = await started("skt/game.zip", 1, { width: 240, height: 320 });
  assert.equal(asDefault.kind, "start");
  assert.equal(asDefault.width, undefined);
  assert.equal(asDefault.height, undefined);

  const asChosen = await started("skt/small.zip", 2, { width: 176, height: 220 });
  assert.equal(asChosen.game, "skt/small.zip");
  assert.equal(asChosen.value, 2);
  assert.equal(asChosen.width, 176);
  assert.equal(asChosen.height, 220);

  // A start with no screen at all is the same message as before this existed.
  const asPlain = await started("ktf/game.zip");
  assert.equal(asPlain.width, undefined);
  assert.equal(asPlain.height, undefined);
});
