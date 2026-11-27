// Connection ownership and recovery, independent of the page's DOM.
// The browser token is a capability for one current game, not a save owner.
import { phrase } from "./text.js";

export const browserTokenKey = "wfeature.browser-token";
export const browserToken = (storage, legacy, random = globalThis.crypto) => {
  const valid = value => /^[0-9a-f]{32}$/i.test(value ?? "");
  const stored = storage.getItem(browserTokenKey);
  if (valid(stored)) return stored;
  const previous = legacy?.getItem("wfeature.resume-token");
  const token = valid(previous) ? previous : Array.from(
    random.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, "0"),
  ).join("");
  storage.setItem(browserTokenKey, token);
  legacy?.removeItem("wfeature.resume-token");
  return token;
};

export const createSessionLink = ({
  token, connect, shared = false, visible = () => true, onState = () => {},
  confirmStart = async () => false,
  onConnection = () => {}, onStarted = () => {}, onExited = () => {},
  schedule = (fn, ms) => setTimeout(fn, ms), cancel = id => clearTimeout(id),
}) => {
  let state = "offline";
  let socket = null;
  let task = null;
  let retry = null;
  let delay = 1000;
  let heartbeat = null;
  // A page can return before work from its previous visit settles. Only the
  // current visit can attach a socket, publish game state, or schedule retries.
  let generation = 0;
  let blocked = false;
  const later = (callback, ms) => {
    const turn = generation;
    return schedule(() => { if (turn === generation) callback(); }, ms);
  };
  const change = next => {
    if (heartbeat !== null) cancel(heartbeat);
    heartbeat = null;
    state = next;
    onState(next);
  };
  const watch = () => {
    if (heartbeat !== null || state !== "playing" || !visible()) return;
    heartbeat = later(() => { heartbeat = null; void wake(); }, 15000);
  };
  const clearRetry = () => { if (retry !== null) cancel(retry); retry = null; };
  const again = () => {
    if (retry !== null || blocked || !visible() || state !== "offline") return;
    retry = later(() => { retry = null; void wake(); }, delay);
    delay = Math.min(delay * 2, 15000);
  };
  const lost = current => {
    if (socket !== current) return;
    socket = null;
    onConnection(null);
    change(blocked ? "occupied" : "offline");
    again();
  };
  const discard = () => {
    const old = socket;
    socket = null;
    onConnection(null);
    old?.close();
  };
  const disconnected = turn => {
    if (turn !== generation) return;
    discard();
    change(blocked ? "occupied" : "offline");
  };
  const connection = async turn => {
    if (socket && !socket.closed) return socket;
    let current;
    current = await connect({
      onClosed: () => { if (turn === generation) lost(current); },
      onDetached: () => {
        if (turn !== generation || socket !== current) return;
        blocked = true;
        clearRetry();
        change("occupied");
      },
      onExited: reason => {
        if (turn !== generation || socket !== current) return;
        change("ready");
        onExited(reason);
      },
    });
    if (turn !== generation) {
      current?.close();
      throw new Error(phrase("session.disconnected"));
    }
    if (!current || current.closed) throw new Error("session connection unavailable");
    socket = current;
    onConnection(current);
    return current;
  };
  const checkConnection = (turn, current) => {
    if (turn !== generation || current !== socket || current.closed) throw new Error(phrase("session.disconnected"));
  };
  const answer = response => {
    // Any answer is a server that is back, so the next outage starts its
    // backoff from the beginning whether or not a game was waiting.
    delay = 1000;
    if (response.occupied) {
      blocked = true;
      change("occupied");
    } else if (response.started) {
      blocked = false;
      change("playing");
      onStarted(response.started);
    } else {
      blocked = false;
      change("ready");
    }
  };
  const run = operation => {
    if (task) return task;
    const turn = generation;
    task = Promise.resolve().then(() => {
      if (turn === generation) return operation(turn);
    }).finally(() => {
      if (turn !== generation) return;
      task = null;
      if (!visible() && state === "playing") void suspend();
      again();
      watch();
    });
    return task;
  };
  const wake = (takeover = false) => {
    if (!visible() || (blocked && !takeover)) return Promise.resolve();
    if (state === "starting") return task ?? Promise.resolve();
    if (state === "playing") return run(async turn => {
      try { await socket.ping(); }
      catch { disconnected(turn); }
    });
    clearRetry();
    return run(async turn => {
      change("connecting");
      try {
        const current = await connection(turn);
        checkConnection(turn, current);
        const response = await current.resume(token, takeover);
        checkConnection(turn, current);
        answer(response);
      } catch {
        // A failed explicit takeover may be retried by the player. Automatic
        // retries must never turn into an implicit takeover.
        disconnected(turn);
      }
    });
  };
  const suspend = () => {
    clearRetry();
    if (state !== "playing" || task) return task ?? Promise.resolve();
    const turn = generation;
    return run(async () => {
      change("parked");
      try { await socket.park(); }
      catch { disconnected(turn); }
      // A quick app switch can return before the parking answer arrives.
    }).then(() => { if (turn === generation && visible() && state === "parked") return wake(); });
  };
  return {
    state: () => state,
    wake, suspend,
    leave: () => {
      generation++;
      task = null;
      clearRetry();
      discard();
      change(blocked ? "occupied" : "offline");
    },
    start: (path, scale, screen, quickLoad = false) => {
      const canStart = (state === "ready" && socket) || (shared && state === "occupied");
      if (task || !canStart) return Promise.reject(new Error(phrase("session.wait-server")));
      return run(async turn => {
        change("starting");
        try {
          const current = await connection(turn);
          checkConnection(turn, current);
          let confirmation = "";
          for (;;) {
            const response = await current.start(path, scale, screen, token, confirmation, quickLoad);
            checkConnection(turn, current);
            if (!response.confirmation) { answer(response); return; }
            const accepted = await confirmStart(response.message);
            checkConnection(turn, current);
            if (!accepted) { change(blocked ? "occupied" : "ready"); return; }
            confirmation = response.confirmation;
          }
        }
        catch (error) {
          if (turn === generation) {
            const fallback = socket && !socket.closed ? "ready" : "offline";
            change(blocked ? "occupied" : fallback);
          }
          throw error;
        }
      });
    },
    stop: () => {
      if (task || !socket || state !== "playing") return Promise.reject(new Error(phrase("session.wait-game")));
      return run(async turn => {
        change("stopping");
        try {
          const current = socket;
          await current.stop();
          checkConnection(turn, current);
          change(blocked ? "occupied" : "ready");
        } catch (error) {
          disconnected(turn);
          throw error;
        }
      });
    },
  };
};
