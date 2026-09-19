// The switch is a page control, never a handset key. Modes are intentionally
// transient: callers reset on input loss, editing, and session transitions.
export const RAPID_FIRE = "RAPID_FIRE";
export const createRapidFire = ({ send, changed = () => {}, schedule = setTimeout, cancel = clearTimeout }) => {
  let mode = "off";
  let timer = null;
  let autoKey = null;
  const eligible = name => name === "5" || name === "OK";
  const held = new Set();
  const down = new Set();
  const pulses = new Map();
  const emit = (name, pressed) => {
    if (down.has(name) === pressed) return;
    if (pressed) down.add(name);
    else down.delete(name);
    send(pressed ? "press" : "release", name);
  };
  const repeats = name => (mode === "auto" && name === autoKey) ||
    (mode === "manual" && held.has(name) && eligible(name));
  const arm = () => {
    if (timer !== null || pulses.size === 0) return;
    // Schedule from delivery, without catch-up bursts after a stalled page.
    timer = schedule(() => {
      timer = null;
      for (const [name, pressed] of pulses) {
        pulses.set(name, !pressed);
        emit(name, !pressed);
      }
      arm();
    }, 50);
  };
  const ordinaryHold = name => held.has(name) && !(mode === "auto" && eligible(name));
  const reconcile = () => {
    // Release the old auto target before pressing its replacement, regardless
    // of the order in which physical keys were held.
    for (const name of [...down]) {
      if (!repeats(name) && !ordinaryHold(name)) emit(name, false);
    }
    for (const name of [...pulses.keys()]) {
      if (!repeats(name)) pulses.delete(name);
    }
    if (pulses.size === 0 && timer !== null) {
      cancel(timer);
      timer = null;
    }
    for (const name of new Set([...held, ...down, ...pulses.keys(), ...(autoKey ? [autoKey] : [])])) {
      if (repeats(name)) {
        if (!pulses.has(name)) {
          pulses.set(name, true);
          emit(name, true);
        }
      } else {
        pulses.delete(name);
        emit(name, ordinaryHold(name));
      }
    }
    if (pulses.size === 0 && timer !== null) {
      cancel(timer);
      timer = null;
    }
    arm();
  };
  return {
    mode: () => mode,
    press: name => {
      if (held.has(name)) return;
      held.add(name);
      if (mode === "auto" && eligible(name)) autoKey = autoKey === name ? null : name;
      reconcile();
    },
    release: name => { held.delete(name); reconcile(); },
    cycle: () => {
      autoKey = null;
      mode = mode === "off" ? "manual" : mode === "manual" ? "auto" : "off";
      reconcile();
      changed(mode);
    },
    reset: () => {
      if (timer !== null) cancel(timer);
      timer = null;
      autoKey = null;
      pulses.clear();
      held.clear();
      for (const name of [...down]) emit(name, false);
      mode = "off";
      changed(mode);
    },
  };
};
