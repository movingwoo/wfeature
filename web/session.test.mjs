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

// An ending arrives with the platform's own account of it. The page's run log
// is where an ending gets read afterwards, and a bare "the game exited" is
// what left a sweep unable to tell a title's first-run restart notice — these
// games quit themselves and ask to be launched again — from a title that broke.
test("an ending hands over the reason the server sent with it", async () => {
  const endings = [];
  const { socket } = await openFakeSession({ onExited: reason => endings.push(reason) });

  socket.deliver({
    kind: "exited",
    message: "run LGT handleCletEvent at 0x1219: MC_knlExit from 0x1959: LGT guest exited",
  });
  assert.equal(endings.length, 1);
  assert.match(endings[0], /0x1959/);

  // A server with nothing to say still ends the game, and the handler must be
  // able to tell that apart from a reason rather than printing "undefined".
  socket.deliver({ kind: "exited" });
  assert.equal(endings.length, 2);
  assert.equal(endings[1], "");
});

// A game that closes itself inside its own start refuses the request, because
// there is no game to hand back — and it has to say which of the two it was.
// The picker comes back either way; only the sentence the player reads differs,
// and a title whose first run installs itself reaches this on purpose.
test("a start the guest ended itself is refused as an ending, not a failure", async () => {
  const { session, socket } = await openFakeSession();

  const asking = session.start("ktf/game.zip");
  const sent = socket.sent.at(-1);
  socket.deliver({
    kind: "error",
    id: sent.id,
    exited: true,
    message: "start KTF main class Clet: KTF guest requested exit",
  });

  await assert.rejects(asking, error => {
    assert.equal(error.exited, true);
    assert.match(error.message, /guest requested exit/);
    return true;
  });
});

// An ordinary refusal must not start looking like an ending, or a real failure
// would be reported to the player as a game that finished.
test("a start that failed is refused without the ending mark", async () => {
  const { session, socket } = await openFakeSession();

  const asking = session.start("ktf/game.zip");
  const sent = socket.sent.at(-1);
  socket.deliver({ kind: "error", id: sent.id, message: "open KTF archive: not a zip" });

  await assert.rejects(asking, error => {
    assert.equal(error.exited, false);
    return true;
  });
});


test("takeover is explicit and detachment is a control event, not a game ending", async () => {
  let detached = 0;
  let ended = 0;
  const { session, socket } = await openFakeSession({ onDetached: () => detached++, onExited: () => ended++ });
  const asking = session.resume("browser-token", true);
  const sent = socket.sent.at(-1);
  assert.equal(sent.takeover, true);
  socket.deliver({ kind: "resumed", id: sent.id, occupied: true });
  assert.equal((await asking).occupied, true);
  socket.deliver({ kind: "detached" });
  assert.equal(detached, 1);
  assert.equal(ended, 0);
});

test("park, stop and liveness wait for their own acknowledgements", async () => {
  const { session, socket } = await openFakeSession();
  for (const kind of ["park", "stop", "ping"]) {
    const asking = session[kind]();
    const sent = socket.sent.at(-1);
    assert.equal(sent.kind, kind);
    socket.deliver({ kind: "result", id: sent.id });
    await asking;
    assert.equal(session.pending.size, 0);
  }
});


test("closing the transport rejects pending requests without waiting for a socket close event", async () => {
  const { session } = await openFakeSession();
  const asking = session.park();
  session.close();
  await assert.rejects(asking, /세션 연결이 끊어졌습니다/);
  assert.equal(session.pending.size, 0);
});


test("authentication is automatic and its result survives the protocol", async () => {
  const { session, socket } = await openFakeSession();
  for (const status of ["unsupported", "ktf-certificate-23"]) {
    const asking = session.start("ktf/game.zip", 1, null, "browser", "");
    const sent = socket.sent.at(-1);
    assert.equal(sent.authentication, undefined);
    socket.deliver({ kind: "started", id: sent.id, started: { authentication: status } });
    assert.equal((await asking).started.authentication, status);
  }
});

test("native text requests retain the edit capability and complete Unicode value", async () => {
  const { session, socket } = await openFakeSession();
  const opening = session.openTextInput();
  const request = socket.sent.at(-1);
  assert.equal(request.kind, "text");
  assert.equal(request.action, "open");
  socket.deliver({ kind: "result", id: request.id, textInput: { edit: 17, text: "이름" } });
  assert.equal((await opening).textInput.edit, 17);
  const committing = session.commitTextInput(17, "한글 이름 😀");
  const commit = socket.sent.at(-1);
  assert.equal(commit.action, "commit");
  assert.equal(commit.edit, 17);
  assert.equal(commit.text, "한글 이름 😀");
  socket.deliver({ kind: "error", id: commit.id, message: "the active text field changed; open text input again" });
  await assert.rejects(committing, /active text field changed/);
  const cancelling = session.cancelTextInput(17);
  const cancel = socket.sent.at(-1);
  assert.equal(cancel.action, "cancel");
  assert.equal(cancel.edit, 17);
  socket.deliver({ kind: "result", id: cancel.id });
  await cancelling;
  assert.equal(session.pending.size, 0);
});

test("a guest exit during text commit settles the request and ends the session", async () => {
  const exits = [];
  const { session, socket } = await openFakeSession({ onExited: reason => exits.push(reason) });
  const committing = session.commitTextInput(3, "complete text");
  const request = socket.sent.at(-1);

  socket.deliver({ kind: "exited", message: "text listener requested exit" });
  assert.deepEqual(exits, ["text listener requested exit"]);
  socket.deliver({ kind: "error", id: request.id, exited: true, message: "the game exited" });
  await assert.rejects(committing, error => {
    assert.equal(error.exited, true);
    return true;
  });
  assert.equal(session.pending.size, 0);
});


test("native text request events are delivered independently of request replies", async () => {
  let requests = 0;
  const { socket } = await openFakeSession({ onTextInput: () => requests++ });
  socket.deliver({ kind: "textInput" });
  assert.equal(requests, 1);
});
