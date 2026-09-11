import assert from "node:assert/strict";
import { test } from "node:test";
import { browserToken, browserTokenKey, createSessionLink } from "./session-link.js";

const memory = () => {
  const values = new Map();
  return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) };
};
const settled = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
const deferred = () => {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
};

// Two real controllers share one simulated server. Its retained game is an
// object so continuity, control ownership and explicit stop can be distinguished.
const fixture = () => {
  let game = null;
  let owner = null;
  let available = true;
  let startGate = null;
  let parkGate = null;
  let pingGate = null;
  const sockets = [];
  const connect = async handlers => {
    if (!available) throw new Error("offline");
    const socket = {
      closed: false,
      resume: async (token, takeover) => {
        if (!game) return {};
        if (owner && owner !== socket) {
          if (!takeover) return { occupied: true };
          owner.handlers.onDetached();
        }
        owner = socket;
        return { started: game };
      },
      start: async (path, scale, screen, token) => {
        if (game) return { occupied: true };
        if (startGate) await startGate.promise;
        game = { game: path, token, width: 240, height: 320 };
        owner = socket;
        return { started: game };
      },
      ping: async () => { if (pingGate) await pingGate.promise; if (!available) throw new Error("offline"); return {}; },
      park: async () => {
        if (parkGate) await parkGate.promise;
        if (owner === socket) owner = null;
        return {};
      },
      stop: async () => { if (owner === socket) { game = null; owner = null; } return {}; },
      close: () => {
        socket.closed = true;
        if (owner === socket) owner = null;
        handlers.onClosed();
      },
      handlers,
    };
    sockets.push(socket);
    return socket;
  };
  const page = () => {
    let visible = true;
    const timers = new Map();
    const starts = [];
    let sequence = 0;
    const link = createSessionLink({
      token: "shared-token", connect, visible: () => visible,
      onStarted: game => starts.push(game),
      schedule: callback => { timers.set(++sequence, callback); return sequence; },
      cancel: id => timers.delete(id),
    });
    return { link, timers, starts,
      hide: () => { visible = false; return link.suspend(); },
      show: () => { visible = true; return link.wake(); },
      retry: async () => {
        const [id, callback] = timers.entries().next().value;
        timers.delete(id);
        callback();
        await settled();
      },
    };
  };
  return { page, sockets, game: () => game, owner: () => owner,
    offline: value => { available = !value; },
    restart: () => { game = null; owner = null; },
    gateStart: () => (startGate = deferred()),
    gatePark: () => (parkGate = deferred()),
    gatePing: () => (pingGate = deferred()),
  };
};

test("browser identity survives tabs and adopts the previous tab's resume token", () => {
  const storage = memory();
  const legacy = memory();
  legacy.setItem("wfeature.resume-token", "0123456789abcdef0123456789abcdef");
  const token = browserToken(storage, legacy);
  assert.equal(token, "0123456789abcdef0123456789abcdef");
  assert.equal(storage.getItem(browserTokenKey), token);
  assert.equal(browserToken(storage, memory()), token);
  assert.equal(legacy.getItem("wfeature.resume-token"), null);
});

test("a second tab needs explicit takeover and the old tab never takes control back automatically", async () => {
  const server = fixture();
  const a = server.page();
  const b = server.page();
  await a.link.wake();
  await a.link.start("games/skt/fixture.zip", 1);
  const game = server.game();
  await b.link.wake();
  assert.equal(a.link.state(), "playing");
  assert.equal(b.link.state(), "occupied");
  await b.link.wake(true);
  assert.equal(a.link.state(), "occupied");
  assert.equal(b.link.state(), "playing");
  assert.equal(b.starts[0], game);
  await a.link.wake();
  assert.equal(server.owner(), server.sockets[1]);
  await b.hide();
  assert.equal(b.link.state(), "parked");
  await a.link.wake();
  assert.equal(server.owner(), null);
  await b.show();
  assert.equal(b.link.state(), "playing");
  assert.equal(server.game(), game);
  await b.link.stop();
  assert.equal(b.link.state(), "ready");
  assert.equal(server.game(), null);
  await b.link.start("games/skt/next.zip", 1);
  assert.equal(b.link.state(), "playing");
});

test("network failure keeps retrying, sleeps when hidden, and resumes the same game", async () => {
  const server = fixture();
  const page = server.page();
  await page.link.wake();
  await page.link.start("fixture", 1);
  const game = server.game();
  server.offline(true);
  server.sockets[0].close();
  for (let i = 0; i < 130; i++) await page.retry();
  assert.equal(page.link.state(), "offline");
  assert.equal(page.timers.size, 1);
  await page.hide();
  assert.equal(page.timers.size, 0);
  server.offline(false);
  await page.show();
  assert.equal(page.link.state(), "playing");
  assert.equal(page.starts.at(-1), game);
});

test("server restart returns to the picker without reloading or automatically restarting a game", async () => {
  const server = fixture();
  const page = server.page();
  await page.link.wake();
  await page.link.start("fixture", 1);
  server.sockets[0].close();
  server.restart();
  await page.link.wake();
  assert.equal(page.link.state(), "ready");
  assert.equal(server.game(), null);
  await page.link.start("fixture", 1);
  assert.equal(page.link.state(), "playing");
});

test("returning while a park is still in flight resumes after its answer", async () => {
  const server = fixture();
  const page = server.page();
  await page.link.wake();
  await page.link.start("fixture", 1);
  const gate = server.gatePark();
  const hiding = page.hide();
  await settled();
  const showing = page.show();
  gate.resolve();
  await Promise.all([hiding, showing]);
  assert.equal(page.link.state(), "playing");
});

test("a game finishing its start while hidden is immediately parked", async () => {
  const server = fixture();
  const page = server.page();
  await page.link.wake();
  const gate = server.gateStart();
  const starting = page.link.start("fixture", 1);
  await settled();
  const hiding = page.hide();
  gate.resolve();
  await Promise.all([starting, hiding]);
  await settled();
  assert.equal(page.link.state(), "parked");
  assert.equal(server.owner(), null);
});


test("a half-open connection is detected by liveness without a close event", async () => {
  const server = fixture();
  const page = server.page();
  await page.link.wake();
  await page.link.start("fixture", 1);
  server.offline(true);
  await page.link.wake();
  assert.equal(page.link.state(), "offline");
  assert.equal(server.sockets[0].closed, true);
  server.offline(false);
  await page.link.wake();
  assert.equal(page.link.state(), "playing");
});


test("a late liveness failure cannot undo a detachment", async () => {
  const server = fixture();
  const a = server.page();
  const b = server.page();
  await a.link.wake();
  await a.link.start("fixture", 1);
  const gate = server.gatePing();
  const pinging = a.link.wake();
  await settled();
  await b.link.wake();
  await b.link.wake(true);
  assert.equal(a.link.state(), "occupied");
  server.offline(true);
  gate.resolve();
  await pinging;
  assert.equal(a.link.state(), "occupied");
  assert.equal(a.timers.size, 0);
});

test("fresh starts require approval, cancellation preserves the picker", async () => {
  for (const accepted of [false, true]) {
    const calls = [];
    const socket = {
      closed: false, resume: async () => ({}), close() { this.closed = true; },
      start: async (...args) => {
        calls.push(args);
        return args[4] ? { started: { game: "fixture" } } : { confirmation: "one-use", message: "Discard retained progress?" };
      },
    };
    let asked = 0;
    const link = createSessionLink({ token: "browser", connect: async () => socket,
      confirmStart: async message => { asked++; assert.equal(message, "Discard retained progress?"); return accepted; },
      schedule: () => 1, cancel: () => {},
    });
    await link.wake();
    await link.start("fixture", 1, null);
    assert.equal(asked, 1);
    for (const call of calls) assert.equal(call.length, 5, "browser must not send an authentication switch");
    assert.equal(calls.length, accepted ? 2 : 1);
    assert.equal(link.state(), accepted ? "playing" : "ready");
    if (accepted) assert.equal(calls[1][4], "one-use");
    link.leave();
  }
});

test("changed victims ask again and a disconnected dialog never restarts", async () => {
  const gate = deferred();
  let calls = 0;
  const socket = { closed: false, resume: async () => ({}), close() { this.closed = true; },
    start: async () => ({ confirmation: `victim-${++calls}`, message: "Confirm" }) };
  let asked = 0;
  const link = createSessionLink({ token: "browser", connect: async () => socket,
    confirmStart: async () => ++asked === 1 ? true : gate.promise,
    schedule: () => 1, cancel: () => {},
  });
  await link.wake();
  const start = link.start("fixture", 1, null);
  await settled();
  assert.equal(asked, 2);
  link.leave();
  gate.resolve(true);
  await assert.rejects(start, /연결/);
  assert.equal(calls, 2);
});
