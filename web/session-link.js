// Connection ownership and recovery, independent of the page's DOM.
// The browser token is a capability for one current game, not a save owner.
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
  token, connect, visible = () => true, onState = () => {},
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
  let generation = 0;
  let blocked = false;
  const change = next => {
    if (heartbeat !== null) cancel(heartbeat);
    heartbeat = null;
    state = next;
    onState(next);
  };
  const watch = () => {
    if (heartbeat !== null || state !== "playing" || !visible()) return;
    heartbeat = schedule(() => { heartbeat = null; void wake(); }, 15000);
  };
  const clearRetry = () => { if (retry !== null) cancel(retry); retry = null; };
  const again = () => {
    if (retry !== null || blocked || !visible() || state !== "offline") return;
    retry = schedule(() => { retry = null; void wake(); }, delay);
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
  const answer = response => {
    if (response.occupied) {
      blocked = true;
      change("occupied");
    } else if (response.started) {
      blocked = false;
      delay = 1000;
      change("playing");
      onStarted(response.started);
    } else {
      blocked = false;
      change("ready");
    }
  };
  const run = operation => {
    if (task) return task;
    task = Promise.resolve().then(operation).finally(() => {
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
    if (state === "playing") return run(async () => {
      try { await socket.ping(); }
      catch { discard(); change(blocked ? "occupied" : "offline"); }
    });
    clearRetry();
    return run(async () => {
      change("connecting");
      try {
        if (!socket || socket.closed) {
          let current;
          const openingGeneration = generation;
          current = await connect({
            onClosed: () => lost(current),
            onDetached: () => {
              if (socket !== current) return;
              blocked = true;
              clearRetry();
              change("occupied");
            },
            onExited: reason => {
              if (socket !== current) return;
              change("ready");
              onExited(reason);
            },
          });
          if (openingGeneration !== generation) { current?.close(); return; }
          if (!current || current.closed) throw new Error("session connection unavailable");
          socket = current;
          onConnection(current);
        }
        answer(await socket.resume(token, takeover));
      } catch {
        discard();
        // A failed explicit takeover may be retried by the player. Automatic
        // retries must never turn into an implicit takeover.
        change(blocked ? "occupied" : "offline");
      }
    });
  };
  const suspend = () => {
    clearRetry();
    if (state !== "playing" || task) return task ?? Promise.resolve();
    return run(async () => {
      change("parked");
      try { await socket.park(); }
      catch { discard(); change(blocked ? "occupied" : "offline"); }
      // A quick app switch can return before the parking answer arrives.
    }).then(() => { if (visible() && state === "parked") return wake(); });
  };
  return {
    state: () => state,
    wake, suspend,
    leave: () => {
      generation++;
      clearRetry();
      discard();
      change(blocked ? "occupied" : "offline");
    },
    start: (path, scale, screen) => {
      if (task || !socket || state !== "ready") return Promise.reject(new Error("서버 연결을 기다려 주세요."));
      return run(async () => {
        change("starting");
        try {
          const current = socket;
          let response = await current.start(path, scale, screen, token, "");
          while (response.confirmation) {
            const accepted = await confirmStart(response.message);
            if (socket !== current || current.closed) throw new Error("세션 연결이 끊어졌습니다.");
            if (!accepted) { change("ready"); return; }
            response = await current.start(path, scale, screen, token, response.confirmation);
          }
          answer(response);
        }
        catch (error) {
          change(socket && !socket.closed ? "ready" : "offline");
          throw error;
        }
      });
    },
    stop: () => {
      if (task || !socket || state !== "playing") return Promise.reject(new Error("게임 연결을 기다려 주세요."));
      return run(async () => {
        change("stopping");
        try { await socket.stop(); change(blocked ? "occupied" : "ready"); }
        catch (error) { discard(); change("offline"); throw error; }
      });
    },
  };
};
