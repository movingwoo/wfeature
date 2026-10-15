import assert from "node:assert/strict";
import { test } from "node:test";

import {
  MAX_VIBRATION_MS,
  VIBRATION_KEY,
  createVibration,
  initVibrationSetting,
  vibrationLength,
} from "./vibrate.js";

const navigatorOf = () => {
  const calls = [];
  return { calls, vibrate: pattern => calls.push(pattern) };
};

const storageOf = (entries = {}) => {
  const map = new Map(Object.entries(entries));
  return {
    map,
    getItem: key => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => {
      map.set(key, String(value));
      return true;
    },
  };
};

// A duration of zero at a level above zero is the guest asking for a vibration
// that runs until it stops it. Reading it as "no time at all" is how a
// continuous buzz becomes silence, which is the trap in the guest's contract.
test("zero milliseconds at a level above zero means until stopped", () => {
  assert.equal(vibrationLength({ level: 100, ms: 0 }), MAX_VIBRATION_MS);
  assert.equal(vibrationLength({ level: 1, ms: 0 }), MAX_VIBRATION_MS);
});

test("a level of zero is the stop, whatever the duration says", () => {
  assert.equal(vibrationLength({ level: 0, ms: 500 }), 0);
  assert.equal(vibrationLength({ level: 0, ms: 0 }), 0);
  assert.equal(vibrationLength(), 0);
});

test("a request is capped rather than trusted", () => {
  assert.equal(vibrationLength({ level: 50, ms: 200 }), 200);
  assert.equal(vibrationLength({ level: 50, ms: 60 * 60 * 1000 }), MAX_VIBRATION_MS);
});

// The level is not a length. A weak buzz is not a short one, and scaling the
// duration by the strength would turn a long gentle rumble into a tap.
test("the strength does not shorten the vibration", () => {
  assert.equal(vibrationLength({ level: 1, ms: 400 }), vibrationLength({ level: 100, ms: 400 }));
});

test("a browser with no vibrator is not asked and not nagged", () => {
  const vibration = createVibration({ navigator: {}, storage: storageOf() });
  assert.equal(vibration.supported, false);
  assert.equal(vibration.request({ level: 100, ms: 100 }), false);
  const doc = { getElementById: () => null };
  initVibrationSetting({ document: doc, vibration });
});

test("a vibration the browser refuses is not an error on screen", () => {
  const vibration = createVibration({
    navigator: {
      vibrate: () => {
        throw new Error("not allowed before a gesture");
      },
    },
    storage: storageOf(),
  });
  assert.equal(vibration.request({ level: 100, ms: 100 }), false);
});

test("it is on where it works, and the switch is remembered", () => {
  const nav = navigatorOf();
  const storage = storageOf();
  const vibration = createVibration({ navigator: nav, storage });
  assert.equal(vibration.enabled, true);
  vibration.request({ level: 60, ms: 120 });
  assert.deepEqual(nav.calls, [120]);

  vibration.set(false);
  assert.equal(storage.getItem(VIBRATION_KEY), "off");
  // Switching it off stops what is running now rather than waiting for the
  // next request, which is what pressing it during a rumble means.
  assert.deepEqual(nav.calls, [120, 0]);
  vibration.request({ level: 60, ms: 120 });
  assert.deepEqual(nav.calls, [120, 0], "a request arrived while it was off");

  // A page opened later reads the choice back.
  assert.equal(createVibration({ navigator: navigatorOf(), storage }).enabled, false);
});

// A stop has to reach the browser: an indefinite buzz the guest started is
// still running until something says otherwise.
test("the guest's own stop is passed on", () => {
  const nav = navigatorOf();
  const vibration = createVibration({ navigator: nav, storage: storageOf() });
  vibration.request({ level: 100, ms: 0 });
  vibration.request({ level: 0, ms: 0 });
  assert.deepEqual(nav.calls, [MAX_VIBRATION_MS, 0]);
});

test("a game that ends does not leave a phone buzzing", () => {
  const nav = navigatorOf();
  createVibration({ navigator: nav, storage: storageOf() }).stop();
  assert.deepEqual(nav.calls, [0]);
});

const checkboxPage = () => {
  const listeners = new Map();
  const input = {
    checked: false,
    addEventListener: (kind, handler) => listeners.set(kind, handler),
  };
  const row = { hidden: false };
  return {
    input,
    row,
    fire: kind => listeners.get(kind)?.(),
    document: {
      getElementById: id => (id === "vibrate-toggle" ? input : id === "vibrate-row" ? row : null),
    },
  };
};

test("the switch is hidden where there is nothing to switch", () => {
  const page = checkboxPage();
  initVibrationSetting({
    document: page.document,
    vibration: createVibration({ navigator: {}, storage: storageOf() }),
  });
  assert.equal(page.row.hidden, true);
});

test("the switch shows the setting and changes it", () => {
  const page = checkboxPage();
  const storage = storageOf();
  const vibration = createVibration({ navigator: navigatorOf(), storage });
  initVibrationSetting({ document: page.document, vibration });
  assert.equal(page.input.checked, true);
  assert.equal(page.row.hidden, false);

  page.input.checked = false;
  page.fire("change");
  assert.equal(vibration.enabled, false);
  assert.equal(storage.getItem(VIBRATION_KEY), "off");
});

// A connected controller's motor, as gamepad.js hands it over.
const rumbleOf = ({ available = true } = {}) => {
  const calls = [];
  return {
    calls,
    available: () => available,
    play: (level, length) => calls.push(["play", level, length]),
    stop: () => calls.push(["stop"]),
  };
};

test("a controller's motor gets the same requests under the same switch", () => {
  const nav = navigatorOf();
  const rumble = rumbleOf();
  const vibration = createVibration({ navigator: nav, storage: storageOf(), rumble });
  vibration.request({ level: 70, ms: 0 });
  vibration.request({ level: 0, ms: 200 });
  assert.deepEqual(rumble.calls, [["play", 70, MAX_VIBRATION_MS], ["play", 0, 0]]);
  assert.deepEqual(nav.calls, [MAX_VIBRATION_MS, 0], "the phone's motor is asked as before");
  rumble.calls.length = 0;
  vibration.set(false);
  assert.deepEqual(rumble.calls, [["stop"]], "switching off stops a rumble already running");
  vibration.request({ level: 70, ms: 100 });
  assert.deepEqual(rumble.calls, [["stop"]], "and nothing more is played");
  vibration.set(true);
  rumble.calls.length = 0;
  vibration.stop();
  assert.deepEqual(rumble.calls, [["stop"]]);
});

test("a controller that cannot be reached never costs the phone its vibration", () => {
  const nav = navigatorOf();
  const rumble = {
    available: () => { throw new Error("gone"); },
    play: () => { throw new Error("gone"); },
    stop: () => { throw new Error("gone"); },
  };
  const vibration = createVibration({ navigator: nav, storage: storageOf(), rumble });
  assert.equal(vibration.request({ level: 50, ms: 100 }), true);
  assert.equal(vibration.stop(), true);
  assert.deepEqual(nav.calls, [100, 0]);
  assert.equal(createVibration({ navigator: {}, storage: storageOf(), rumble }).available, false);
});

test("the switch appears when a controller that can rumble does", () => {
  const page = checkboxPage();
  const rumble = rumbleOf({ available: false });
  const vibration = createVibration({ navigator: {}, storage: storageOf(), rumble });
  const setting = initVibrationSetting({ document: page.document, vibration });
  assert.equal(page.row.hidden, true, "nothing here can vibrate yet");
  assert.equal(page.input.checked, true);
  rumble.available = () => true;
  setting.refresh();
  assert.equal(page.row.hidden, false);
  page.input.checked = false;
  page.fire("change");
  assert.equal(vibration.enabled, false, "the switch was wired while it was hidden");
});
