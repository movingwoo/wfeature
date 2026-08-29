// Debug run logs, page side.
//
// The emulator runs on the server and writes its own report, so what is left
// here is the half the server cannot see: what this page did. Console output
// and uncaught errors are retained as they happen, because closing the tab
// loses them otherwise, and saveReport writes them where the session's report
// already goes.

const BROWSER_LOG_LINES = 2000;

// Only the most recent lines are kept, because a run that misbehaves for
// minutes would otherwise grow the buffer without bound.
const browserLog = [];
let browserLogWritten = 0;

// Watchers of the live stream. The page shows the log on screen when there is
// room for it, and the buffer is where those lines come from.
const listeners = new Set();
// A listener that logged would append while appending. The flag drops such a
// line from the watchers rather than letting it recurse; the buffer, which is
// what a saved report reads back, still has it.
let notifying = false;

const notify = line => {
  if (notifying) return;
  notifying = true;
  try {
    for (const listener of listeners) {
      try {
        listener(line);
      } catch {
        // A broken watcher must not cost the buffer a line.
      }
    }
  } finally {
    notifying = false;
  }
};

// Whether anything is being collected at all. A release build turns this off
// the moment the server says which profile answered: see stopLogCapture.
let collecting = true;

const append = line => {
  if (!collecting) return;
  browserLogWritten += 1;
  browserLog.push(line);
  if (browserLog.length > BROWSER_LOG_LINES) browserLog.shift();
  notify(line);
};

// subscribeLog replays what has already been recorded and then follows the
// stream, so a watcher registered after startup still sees how the page got
// here. It answers the function that stops the subscription.
export const subscribeLog = listener => {
  for (const line of browserLog) listener(line);
  listeners.add(listener);
  return () => listeners.delete(listener);
};

// clearLog empties both the buffer and what the watchers are showing. The
// report written after a clear covers the run from that point on, which is the
// point of clearing before reproducing something.
export const clearLog = () => {
  browserLog.length = 0;
  browserLogWritten = 0;
  for (const listener of listeners) listener(null);
};

const formatArgument = value => {
  if (typeof value === "string") return value;
  // **An error's message is the part worth keeping, and a stack is not
  // guaranteed to carry it.** V8 begins `stack` with "Name: message" and this
  // used to log the stack alone on that basis; WebKit begins it with the first
  // frame, so a server refusal reported through console.error reached the run
  // log as two lines of addresses and nothing about what was refused. One
  // session's whole record of why a game would not start was
  // "#receive@…/session.js:113:53".
  if (value instanceof Error) {
    const described = `${value.name}: ${value.message}`;
    if (!value.stack) return described;
    return value.stack.startsWith(value.name) ? value.stack : `${described}\n${value.stack}`;
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
};

const stamp = () => new Date().toISOString();

// What installConsoleCapture put in place, kept so it can be taken back out.
// The console methods are the page's own by then, so restoring means putting
// back exactly the functions that were there rather than deleting a property.
let restore = null;

// installConsoleCapture wraps the console so the page's own output is retained
// as well as displayed. The original methods still run, so devtools behaves
// exactly as before.
const installConsoleCapture = () => {
  const undo = [];
  for (const level of ["log", "info", "warn", "error", "debug"]) {
    const original = console[level].bind(console);
    const wrapped = (...args) => {
      append(`${stamp()} ${level.toUpperCase()} ${args.map(formatArgument).join(" ")}`);
      original(...args);
    };
    console[level] = wrapped;
    undo.push(() => {
      // Only this wrapper is taken back off. Something else that wrapped the
      // console after this did so over the wrapper, and putting the original
      // back over it would drop that instead.
      if (console[level] === wrapped) console[level] = original;
    });
  }

  const onError = event => {
    const where = event.filename ? ` (${event.filename}:${event.lineno}:${event.colno})` : "";
    append(`${stamp()} UNCAUGHT ${formatArgument(event.error ?? event.message)}${where}`);
  };
  const onRejection = event => {
    append(`${stamp()} REJECTED ${formatArgument(event.reason)}`);
  };
  // The module is loaded by the tests as well, where there is no window and
  // nothing throws at one.
  if (typeof window !== "undefined") {
    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);
    undo.push(() => {
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
    });
  }
  restore = () => {
    for (const step of undo) step();
    restore = null;
  };
};

// stopLogCapture takes the capture back off and drops what it has collected.
//
// Capture starts at load because the lines worth having are the ones from
// before anything knows what this is — a module that failed to import, a
// socket that never opened. Which build is running is the server's answer,
// and it arrives later, so a release collects for the moment it takes to ask
// and then stops: the console is the page's own again, the buffer is emptied,
// and nothing further is retained. It is the same page either way; only the
// binary serving it knows which profile this is.
export const stopLogCapture = () => {
  collecting = false;
  if (restore) restore();
  browserLog.length = 0;
  browserLogWritten = 0;
  for (const listener of listeners) listener(null);
  listeners.clear();
};

// capturing reports whether the page is still collecting, which is what a test
// reads to tell a stopped capture from a quiet one.
export const capturing = () => collecting;

// recordEvent notes a page-side milestone in the same stream, so the report
// shows what the page was doing around a failure.
export const recordEvent = message => append(`${stamp()} EVENT ${message}`);

// saveReport writes the page log through the same API the session's own report
// lands in, so the two halves of a run end up side by side under var/logs. The
// server report says what the guest did; this says what the page did with it,
// which is the only place a dropped socket or a draw failure shows up.
export const saveReport = async label => {
  // A release has no report to write and no route to write it to; the button
  // that calls this is not in its page either. Refusing here as well keeps the
  // page from posting to an address the server it is talking to does not serve.
  if (!collecting) return null;
  const report = [
    "wfeature page log",
    `generated: ${stamp()}`,
    `page: ${location.href}`,
    `agent: ${navigator.userAgent}`,
    `lines: ${browserLog.length} of ${browserLogWritten} written`,
    "",
    ...browserLog,
    "",
  ].join("\n");
  const response = await fetch(`api/debug-log?label=${encodeURIComponent(label ?? "")}-page`, {
    method: "POST",
    headers: { "Content-Type": "text/plain; charset=utf-8" },
    body: report,
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const { name } = await response.json();
  return name;
};

installConsoleCapture();

