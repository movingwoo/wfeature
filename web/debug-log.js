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

const append = line => {
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
  if (value instanceof Error) return value.stack ?? `${value.name}: ${value.message}`;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
};

const stamp = () => new Date().toISOString();

// installConsoleCapture wraps the console so the page's own output is retained
// as well as displayed. The original methods still run, so devtools behaves
// exactly as before.
const installConsoleCapture = () => {
  for (const level of ["log", "info", "warn", "error", "debug"]) {
    const original = console[level].bind(console);
    console[level] = (...args) => {
      append(`${stamp()} ${level.toUpperCase()} ${args.map(formatArgument).join(" ")}`);
      original(...args);
    };
  }

  window.addEventListener("error", event => {
    const where = event.filename ? ` (${event.filename}:${event.lineno}:${event.colno})` : "";
    append(`${stamp()} UNCAUGHT ${formatArgument(event.error ?? event.message)}${where}`);
  });
  window.addEventListener("unhandledrejection", event => {
    append(`${stamp()} REJECTED ${formatArgument(event.reason)}`);
  });
};

// recordEvent notes a page-side milestone in the same stream, so the report
// shows what the page was doing around a failure.
export const recordEvent = message => append(`${stamp()} EVENT ${message}`);

// saveReport writes the page log through the same API the session's own report
// lands in, so the two halves of a run end up side by side under var/logs. The
// server report says what the guest did; this says what the page did with it,
// which is the only place a dropped socket or a draw failure shows up.
export const saveReport = async label => {
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

