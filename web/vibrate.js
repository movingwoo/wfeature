// The handset's motor, on this end of the socket.
//
// The guest asks for a vibration, the server records what it asked for and
// sends it on, and this decides what actually happens — which is the whole
// shape of the boundary: the emulator core does not know whether there is a
// motor here, whether the browser will use it, or whether the person wants it.
// It only reports the request.
//
// What arrives is the guest's own request: a strength from 0 to 100 and a time
// in milliseconds, where a time of zero with a strength above zero means "until
// I say stop" and a strength of zero *is* the stop.
//
// `navigator.vibrate` has no strength — it takes a length and nothing else — so
// the level is used for the one decision it can make here, which is whether to
// vibrate at all. Scaling the duration by it instead would be an invention: a
// weak buzz is not a short one, and a title asking for a long gentle rumble
// would get a tap.
//
// A connected game controller is a second motor, and it is given the same
// requests under the same switch (gamepad.js has its half). Its motor does have
// a strength, so there the level is the strength.
//
// Three things make this do nothing at all, and none of them is an error worth
// telling anyone about: a browser without the API (every desktop Safari and
// Firefox) and no controller that can rumble, a platform whose core does not
// report vibration (the server sends nothing, so nothing arrives), and the
// person having switched it off.

// The upper bound on one request. An indefinite request has to become a finite
// call because the API takes a length, and a page that asked for an hour would
// leave a phone buzzing long after the game that asked stopped caring. This is
// long enough that a continuous rumble reads as continuous and short enough
// that a lost stop is a nuisance rather than a fault.
export const MAX_VIBRATION_MS = 5000;

export const VIBRATION_KEY = "wfeature:vibrate";

// vibrationLength turns one request into the number of milliseconds to ask the
// browser for, or 0 for "stop".
export const vibrationLength = ({ level = 0, ms = 0 } = {}) => {
  if (!(level > 0)) return 0;
  // Zero milliseconds at a level above zero is the guest asking for a
  // vibration that runs until it stops it. Reading it as "no time at all" is
  // how a continuous buzz becomes silence.
  if (!(ms > 0)) return MAX_VIBRATION_MS;
  return Math.min(Math.round(ms), MAX_VIBRATION_MS);
};

// createVibration wires the setting and the outgoing calls. `navigator`,
// `storage` and the controllers' `rumble` are handed in so the rules above are
// testable without a browser.
export const createVibration = ({ navigator: nav, storage, rumble } = {}) => {
  // A browser that has no vibrator is not a browser to nag: the setting is
  // simply not offered, and the calls below are dropped.
  const supported = typeof nav?.vibrate === "function";
  // On by default where it works. A handset vibrated when a game asked it to,
  // and this page is trying to be one; somebody who does not want it has the
  // switch.
  let enabled = storage?.getItem(VIBRATION_KEY) !== "off";

  const call = pattern => {
    if (!supported) return false;
    try {
      nav.vibrate(pattern);
      return true;
    } catch {
      // Some browsers throw where the page is not in the foreground, or where
      // the user has not interacted with it yet. A vibration nobody feels is
      // not worth an error on a screen.
      return false;
    }
  };

  // The controllers' motors. Each call is the controller module's to make
  // safely; this only decides that one is made.
  const pads = (level, length) => {
    try {
      rumble?.play(level, length);
    } catch {
      // A controller is never a reason for the phone's own motor to miss out.
    }
  };
  const stopPads = () => {
    try {
      rumble?.stop();
    } catch {
      // As above.
    }
  };

  return {
    supported,
    // available answers whether anything on this end can vibrate now: the
    // phone's own motor, or a controller's. The second changes when a
    // controller is connected, which is why it is asked rather than stored.
    get available() {
      if (supported) return true;
      try {
        return Boolean(rumble?.available());
      } catch {
        return false;
      }
    },
    get enabled() {
      return enabled;
    },
    // set answers whether the choice reached storage, the way the page's other
    // settings do. Switching it off stops whatever is running now rather than
    // waiting for the next request, which is what somebody pressing it during
    // a rumble means by it.
    set(on) {
      enabled = Boolean(on);
      if (!enabled) {
        call(0);
        stopPads();
      }
      return storage?.setItem(VIBRATION_KEY, enabled ? "on" : "off") ?? false;
    },
    // request handles one message from the server. What it answers is the
    // phone's motor; a controller's is not asked to report.
    request(message) {
      if (!enabled) return false;
      const length = vibrationLength(message);
      // A zero-length request is the guest turning the motor off, and it is
      // passed on rather than dropped: an indefinite buzz already started is
      // still running until something says otherwise.
      pads(message?.level, length);
      return call(length);
    },
    // stop is for the page rather than the guest: a game that ended or a
    // session that went away must not leave a phone buzzing.
    stop() {
      stopPads();
      return call(0);
    },
  };
};

// initVibrationSetting wires the checkbox. The row is hidden where nothing can
// vibrate, because a switch that cannot do anything is worse than no switch: it
// reads as a thing that is broken. A controller that can rumble may arrive
// later, so the switch is wired either way and `refresh` asks again.
export const initVibrationSetting = ({ document: doc, vibration } = {}) => {
  const input = doc?.getElementById("vibrate-toggle");
  const row = doc?.getElementById("vibrate-row");
  const refresh = () => {
    if (row) row.hidden = !vibration?.available;
  };
  if (!input) return { refresh };
  refresh();
  input.checked = Boolean(vibration?.enabled);
  input.addEventListener("change", () => vibration.set(input.checked));
  return { refresh };
};
