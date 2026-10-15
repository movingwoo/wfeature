// A game controller is one more way to hold a phone key, and that is the whole
// of how it is wired: every press goes through the holds the keypad and the
// keyboard already share (key-holds.js), so it lights the same button, feeds
// the same rapid fire and is let go the same way when the window loses focus.
// The server is never told a controller exists — it is sent phone keys — which
// is why every platform takes it without a line of its own.
//
// What is left is three questions only a controller asks, and they are this
// file:
//
//   - which phone key each control sends. A controller has sixteen buttons and
//     two sticks for the twenty-one keys a handset had, so the defaults below
//     are a layout rather than a list, and the settings panel can change any
//     entry of it;
//   - when a control counts as held. The browser hands over a picture of the
//     controller rather than events — every button's level and every stick's
//     position at the moment it is asked — so a press is the difference
//     between two pictures, and a trigger or a stick resting near a threshold
//     needs two thresholds, one to arrive and one to leave, or it chatters;
//   - when a control must not act. A control held across a reset — the window
//     losing focus, a game starting — stays dead until it is let go, as a
//     keyboard key's auto-repeat is ignored, and nothing presses on its own
//     when input comes back.

import { keyOrder } from "./keybindings.js";
import { keyName, SETTINGS } from "./keypad-layout.js";
import { RAPID_FIRE } from "./rapid-fire.js";
import { QUICK_LOAD, QUICK_SAVE } from "./checkpoint.js";
import { FULLSCREEN } from "./fullscreen.js";

export const PAD_BINDINGS_KEY = "wfeature:padBindings";
export const RAW_PAD_BINDINGS_KEY = "wfeature:padBindingsRaw";

// The page's own controls a controller can reach, in the order the panel lists
// them. They act on the press and hold nothing, like their keypad cells.
export const padActions = [SETTINGS, RAPID_FIRE, FULLSCREEN, QUICK_SAVE, QUICK_LOAD];
const knownTarget = new Set([...keyOrder, ...padActions]);
export const isPadAction = name => padActions.includes(name);
export const targetLabel = name => (name ? keyName(name) : "없음");

// The two sticks of the standard layout, as sectors read clockwise from
// pointing right — the browser's y axis points down. The left stick has four,
// because the handset's direction key went one way at a time; the right one has
// eight, because it is the number block around 5.
const sticks = {
  ls: { x: 0, y: 1, sectors: ["ls-right", "ls-down", "ls-left", "ls-up"] },
  rs: {
    x: 2,
    y: 3,
    sectors: ["rs-right", "rs-downright", "rs-down", "rs-downleft", "rs-left", "rs-upleft", "rs-up", "rs-upright"],
  },
};
const stickOf = input => (input.startsWith("ls-") ? "ls" : input.startsWith("rs-") ? "rs" : "");

// Every control of the standard layout, in the order the panel lists them:
// the direction controls first, then the face, the shoulders and the middle.
// Button 16 is the controller's home button and is not here, because the
// operating system takes it before a page ever sees it.
export const standardInputs = [
  "b12", "b13", "b14", "b15",
  "ls-up", "ls-down", "ls-left", "ls-right",
  "rs-upleft", "rs-up", "rs-upright",
  "rs-left", "rs-right",
  "rs-downleft", "rs-down", "rs-downright",
  "b0", "b1", "b2", "b3",
  "b4", "b5", "b6", "b7",
  "b8", "b9", "b10", "b11",
];
const standardInput = new Set(standardInputs);

// The layout a controller starts with. The browser's standard mapping numbers
// the buttons by where they sit rather than by what is printed on them, so this
// is the same layout on every make: the bottom face button is the bottom face
// button whether it says A, ✕ or B.
//
//   - the direction pad and the left stick are the direction key;
//   - the right stick is the number block, each way the key that lies that way
//     from 5 on a handset, so up-left is 1 and down is 8;
//   - the bottom face button is the centre key, the one a help screen calls
//     확인키 or OK키, and the right one is 취소 — titles of this era draw
//     BACK:CLR on every screen that can be left, so it is the key pressed
//     second most;
//   - the left and right face buttons are 5 and 0, the two number keys the
//     stick does not reach;
//   - the shoulders are the two soft keys, which the help screens name by the
//     corner they sat in: 좌측상단키 and 우측상단키;
//   - the triggers are * and #, the outer keys of the handset's bottom row;
//   - the left middle button is the send key, and the right one opens the
//     settings, which is where a controller's start button leads anywhere;
//   - pressing the left stick switches rapid fire, the one page control a
//     player changes while playing, and pressing the right one switches
//     전체화면, which puts away the keypad a controller has made useless.
//
// Quick save and quick load are left off on purpose. Either is one press of a
// button that was reached for by mistake, and a load puts the game back to the
// last quick save; the panel offers both for anyone who wants them.
export const defaultPadBindings = {
  b12: "UP",
  b13: "DOWN",
  b14: "LEFT",
  b15: "RIGHT",
  "ls-up": "UP",
  "ls-down": "DOWN",
  "ls-left": "LEFT",
  "ls-right": "RIGHT",
  "rs-upleft": "1",
  "rs-up": "2",
  "rs-upright": "3",
  "rs-left": "4",
  "rs-right": "6",
  "rs-downleft": "7",
  "rs-down": "8",
  "rs-downright": "9",
  b0: "OK",
  b1: "CLR",
  b2: "5",
  b3: "0",
  b4: "MENU",
  b5: "SOFT2",
  b6: "*",
  b7: "#",
  b8: "CALL",
  b9: SETTINGS,
  b10: RAPID_FIRE,
  b11: FULLSCREEN,
};

// A controller the browser does not recognise is read as what it is: numbered
// buttons and axes, each axis two controls, one for either way. Nothing is
// known about what sits where, so it starts with nothing bound. The bounds are
// the browser's numbers rather than any controller's, and they keep a table
// made of them small.
const MAX_BUTTONS = 32;
const MAX_AXES = 16;
const rawInput = /^(b([0-9]|[12][0-9]|3[01])|a([0-9]|1[0-5])[+-])$/;

export const rawInputs = ({ buttons = 0, axes = 0 } = {}) => [
  ...Array.from({ length: Math.min(buttons, MAX_BUTTONS) }, (_, n) => `b${n}`),
  ...Array.from({ length: Math.min(axes, MAX_AXES) }, (_, n) => [`a${n}+`, `a${n}-`]).flat(),
];

// Which printed names a controller's buttons carry, guessed from the name the
// browser gives it. The guess only picks labels — the layout is positional —
// so a wrong one costs a name, which is why the panel also says where each
// face button sits.
export const padFamily = id => {
  const name = String(id ?? "").toLowerCase();
  if (/054c|playstation|dualsense|dualshock/.test(name)) return "playstation";
  if (/057e|nintendo|pro controller|joy-con/.test(name)) return "nintendo";
  return "xbox";
};

const faceNames = {
  xbox: ["A", "B", "X", "Y"],
  playstation: ["✕", "○", "□", "△"],
  nintendo: ["B", "A", "Y", "X"],
};
const faceSides = ["아래", "오른쪽", "왼쪽", "위"];
const upperNames = {
  xbox: ["LB", "RB", "LT", "RT", "View", "Menu"],
  playstation: ["L1", "R1", "L2", "R2", "Share", "Options"],
  nintendo: ["L", "R", "ZL", "ZR", "−", "+"],
};
const arrows = {
  up: "↑",
  down: "↓",
  left: "←",
  right: "→",
  upleft: "↖",
  upright: "↗",
  downleft: "↙",
  downright: "↘",
};

// What a control is called on screen.
export const inputLabel = (input, { standard = true, family = "xbox" } = {}) => {
  const stick = /^(ls|rs)-(\w+)$/.exec(input);
  if (stick) return `${stick[1] === "ls" ? "왼스틱" : "오른스틱"} ${arrows[stick[2]] ?? stick[2]}`;
  const axis = /^a(\d+)([+-])$/.exec(input);
  if (axis) return `축 ${Number(axis[1]) + 1} ${axis[2] === "+" ? "+" : "−"}`;
  const button = /^b(\d+)$/.exec(input);
  if (!button) return input;
  const n = Number(button[1]);
  if (!standard || n > 15) return `버튼 ${n + 1}`;
  if (n < 4) return `${(faceNames[family] ?? faceNames.xbox)[n]} (${faceSides[n]})`;
  if (n < 10) return (upperNames[family] ?? upperNames.xbox)[n - 4];
  if (n === 10) return "L3 (왼스틱 누르기)";
  if (n === 11) return "R3 (오른스틱 누르기)";
  return `십자 ${["↑", "↓", "←", "→"][n - 12]}`;
};

const record = value => (value !== null && typeof value === "object" && !Array.isArray(value) ? value : {});

// loadPadBindings folds a stored table into the layout this build declares. A
// control the user set keeps what they set, including nothing; a control this
// build has just gained takes its default; and a stored entry naming a control
// or a key this build does not have is dropped by never being asked for. More
// than one control may send the same key — the direction pad and the left
// stick both do — so unlike the keyboard there is nothing to take away.
export const loadPadBindings = stored => {
  const saved = record(stored);
  return Object.fromEntries(standardInputs.map(input => {
    if (!Object.hasOwn(saved, input)) return [input, defaultPadBindings[input] ?? ""];
    const name = saved[input];
    return [input, knownTarget.has(name) ? name : ""];
  }));
};

// How many unrecognised controllers keep a table. Each is stored under the
// name the browser gives it, which is all there is to tell two apart, and a
// limit keeps a page that has met many from carrying all of them forever.
const RAW_TABLE_LIMIT = 16;

export const loadRawPadBindings = stored => {
  const tables = {};
  const entries = Object.entries(record(stored)).filter(([id]) => id !== "" && id.length <= 256);
  for (const [id, table] of entries.slice(-RAW_TABLE_LIMIT)) {
    const kept = Object.entries(record(table)).filter(([input, name]) => rawInput.test(input) && knownTarget.has(name));
    if (kept.length > 0) tables[id] = Object.fromEntries(kept);
  }
  return tables;
};

// assignPad gives a control a key, an action or — with the empty string —
// nothing. A control or a key this build does not know leaves the table as it
// was.
export const assignPad = (bindings, input, name) => {
  if (!standardInput.has(input) && !rawInput.test(input)) return bindings;
  if (name !== "" && !knownTarget.has(name)) return bindings;
  return { ...bindings, [input]: name };
};

// storeRawTable puts one unrecognised controller's table back among the
// others. Writing it moves it to the end, so the controller dropped at the
// limit is the one set up longest ago; a table with nothing bound is removed.
export const storeRawTable = (tables, id, table) => {
  const next = { ...tables };
  delete next[id];
  const kept = Object.entries(table).filter(([input, name]) => rawInput.test(input) && knownTarget.has(name));
  if (kept.length > 0) next[id] = Object.fromEntries(kept);
  const ids = Object.keys(next);
  for (const old of ids.slice(0, Math.max(0, ids.length - RAW_TABLE_LIMIT))) delete next[old];
  return next;
};

// The two thresholds of each kind of control. A digital button reads 0 or 1
// and crosses both at once; a trigger is a lever, and one threshold would let a
// finger resting on it send a stream of presses. A stick also has a margin on
// the edges between its sectors, so a thumb held on a diagonal does not flicker
// between the two keys either side of it.
const BUTTON_PRESS = 0.5;
const BUTTON_RELEASE = 0.3;
const STICK_PRESS = 0.5;
const STICK_RELEASE = 0.35;
const STICK_MARGIN = 10;
const AXIS_PRESS = 0.5;
const AXIS_RELEASE = 0.35;

// How far a button is down. A digital button may report `pressed` with no
// value at all, and a trigger reports `pressed` from the first touch, which is
// why the value is preferred whenever there is one.
const buttonLevel = button => {
  if (typeof button === "number") return button;
  const value = Number(button?.value);
  if (Number.isFinite(value) && value > 0) return value;
  return button?.pressed ? 1 : 0;
};

// The sector a stick points into, or null when it is near the middle. A stick
// already in a sector keeps it until the thumb is a margin past the edge.
const sectorOf = (stick, axes, previous) => {
  const x = Number(axes?.[stick.x]) || 0;
  const y = Number(axes?.[stick.y]) || 0;
  if (Math.hypot(x, y) < (previous ? STICK_RELEASE : STICK_PRESS)) return null;
  const width = 360 / stick.sectors.length;
  const angle = ((Math.atan2(y, x) * 180) / Math.PI + 360) % 360;
  if (previous) {
    const centre = stick.sectors.indexOf(previous) * width;
    const away = Math.abs(((((angle - centre) % 360) + 540) % 360) - 180);
    if (away <= width / 2 + STICK_MARGIN) return previous;
  }
  return stick.sectors[Math.round(angle / width) % stick.sectors.length];
};

// The controls of one controller that are held in this picture.
const readControls = (gamepad, state) => {
  const held = new Set();
  const buttons = gamepad.buttons ?? [];
  const buttonCount = Math.min(buttons.length, state.standard ? 16 : MAX_BUTTONS);
  for (let n = 0; n < buttonCount; n++) {
    const input = `b${n}`;
    const level = buttonLevel(buttons[n]);
    if (state.held.has(input) ? level > BUTTON_RELEASE : level >= BUTTON_PRESS) held.add(input);
  }
  const axes = gamepad.axes ?? [];
  if (state.standard) {
    for (const [name, stick] of Object.entries(sticks)) {
      state.sectors[name] = sectorOf(stick, axes, state.sectors[name]);
      if (state.sectors[name]) held.add(state.sectors[name]);
    }
    return held;
  }
  const axisCount = Math.min(axes.length, MAX_AXES);
  for (let n = 0; n < axisCount; n++) {
    const value = Number(axes[n]) || 0;
    for (const [input, toward] of [[`a${n}+`, value], [`a${n}-`, -value]]) {
      if (state.held.has(input) ? toward > AXIS_RELEASE : toward >= AXIS_PRESS) held.add(input);
    }
  }
  return held;
};

const sameSet = (a, b) => a.size === b.size && [...a].every(item => b.has(item));

// createPadInput turns pictures of the controllers into presses and releases
// on the page's holds. Everything it needs from the page is handed in:
//
//   - latch, lift and moveTo are key-holds' own: a button holds a key like a
//     keyboard key does, and a stick is a finger sliding over the number block,
//     which lets go of the key it leaves before it presses the next;
//   - act runs a page control;
//   - bindingsFor answers the table a controller reads;
//   - game says whether keys may reach the game at all, and allows whether a
//     page control may run — both are the page's state, not a controller's;
//   - activity hears which controls of a controller are lit, for the panel.
export const createPadInput = ({
  latch,
  lift,
  moveTo,
  act = () => {},
  bindingsFor = () => ({}),
  game = () => true,
  allows = () => true,
  activity = () => {},
}) => {
  const pads = new Map();

  const describe = (state, extra) => ({
    index: state.index,
    id: state.id,
    standard: state.standard,
    family: state.family,
    buttons: state.buttons,
    axes: state.axes,
    ...extra,
  });

  // Lets go of everything one controller holds. Lifting a pointer the holds
  // have already forgotten does nothing, so this is safe after holds.clear().
  const letGo = state => {
    for (const id of state.keys) lift(id);
    state.keys.clear();
  };

  const drop = index => {
    const state = pads.get(index);
    if (!state) return;
    letGo(state);
    pads.delete(index);
    activity(describe(state, { lit: new Set(), rose: false, gone: true }));
  };

  const step = (gamepad, state) => {
    const held = readControls(gamepad, state);
    // A control held when the controller is first seen is dead if it is an
    // axis: some controllers rest a trigger at one end of its travel, and that
    // is not somebody pressing it. After a reset everything held is dead.
    if (state.settle) {
      for (const input of held) if (state.settle === "all" || input.startsWith("a")) state.dead.add(input);
      state.settle = "";
    }
    for (const input of state.dead) if (!held.has(input)) state.dead.delete(input);
    state.held = held;
    const lit = new Set([...held].filter(input => !state.dead.has(input)));
    const table = bindingsFor(describe(state)) ?? {};
    const flowing = game();
    if (!flowing) letGo(state);

    // Releases before presses, so a key moved from one control to another is
    // let go of before it is pressed again — the order a game reads them in.
    for (const input of state.lit) {
      if (lit.has(input) || stickOf(input)) continue;
      const id = `pad:${state.index}:${input}`;
      if (state.keys.delete(id)) lift(id);
    }
    if (state.standard) {
      for (const name of Object.keys(sticks)) {
        const was = [...state.lit].find(input => stickOf(input) === name) ?? null;
        const now = [...lit].find(input => stickOf(input) === name) ?? null;
        if (was === now) continue;
        const id = `pad:${state.index}:${name}`;
        const bound = now ? table[now] ?? "" : "";
        if (bound && !isPadAction(bound) && flowing) {
          state.keys.add(id);
          moveTo(id, bound);
          continue;
        }
        if (state.keys.delete(id)) lift(id);
        if (isPadAction(bound) && allows(bound)) act(bound);
      }
    }
    for (const input of lit) {
      if (state.lit.has(input) || stickOf(input)) continue;
      const bound = table[input] ?? "";
      if (!bound) continue;
      if (isPadAction(bound)) {
        if (allows(bound)) act(bound);
        continue;
      }
      if (!flowing) continue;
      const id = `pad:${state.index}:${input}`;
      state.keys.add(id);
      latch(id, bound);
    }

    const rose = [...lit].some(input => !state.lit.has(input));
    const changed = !sameSet(lit, state.lit);
    state.lit = lit;
    if (changed || state.unseen) activity(describe(state, { lit, rose: rose || state.unseen, gone: false }));
    state.unseen = false;
  };

  return {
    // poll reads every controller the browser lists and answers how many there
    // are, which is what decides whether anything needs reading again.
    poll(list) {
      const seen = new Set();
      for (const gamepad of Array.from(list ?? [])) {
        if (!gamepad || gamepad.connected === false) continue;
        const index = Number(gamepad.index);
        if (!Number.isInteger(index) || seen.has(index)) continue;
        seen.add(index);
        const standard = gamepad.mapping === "standard";
        const id = String(gamepad.id ?? "");
        let state = pads.get(index);
        if (state && (state.id !== id || state.standard !== standard)) {
          drop(index);
          state = undefined;
        }
        if (!state) {
          state = {
            index,
            id,
            standard,
            family: padFamily(id),
            buttons: gamepad.buttons?.length ?? 0,
            axes: gamepad.axes?.length ?? 0,
            held: new Set(),
            dead: new Set(),
            lit: new Set(),
            keys: new Set(),
            sectors: { ls: null, rs: null },
            settle: "axes",
            unseen: true,
          };
          pads.set(index, state);
        }
        step(gamepad, state);
      }
      for (const index of [...pads.keys()]) if (!seen.has(index)) drop(index);
      return pads.size;
    },
    // reset is the page letting go of everything — the window lost focus, a
    // game started or stopped. Whatever is still held when the controllers are
    // next read is dead until it is let go.
    reset() {
      for (const state of pads.values()) {
        letGo(state);
        state.settle = "all";
        if (state.lit.size > 0) activity(describe(state, { lit: new Set(), rose: false, gone: false }));
        state.lit = new Set();
      }
    },
  };
};

// Whether a controller's motor can play the one effect a handset's vibrator
// becomes. A browser that lists its effects says so in the list; an older one
// names its single effect as `type`.
const rumbles = actuator =>
  typeof actuator?.playEffect === "function" &&
  (Array.isArray(actuator.effects) ? actuator.effects.includes("dual-rumble") : actuator.type === "dual-rumble");

// createPadRumble is the controllers' half of the handset's vibrator; vibrate.js
// decides whether a request is made at all. Unlike `navigator.vibrate` it has a
// strength, so the guest's level is the strength rather than a yes or no.
// Every call can be refused — a page in the background, a controller unplugged
// between the listing and the call — and none of that is worth an error.
export const createPadRumble = ({ gamepads = () => [] } = {}) => {
  const actuators = () => {
    try {
      return Array.from(gamepads() ?? []).map(pad => pad?.vibrationActuator).filter(rumbles);
    } catch {
      return [];
    }
  };
  const quietly = run => {
    try {
      run()?.catch?.(() => {});
    } catch {
      // Refused outright rather than through the promise.
    }
  };
  const still = actuator =>
    quietly(() =>
      typeof actuator.reset === "function"
        ? actuator.reset()
        : actuator.playEffect("dual-rumble", { duration: 0, strongMagnitude: 0, weakMagnitude: 0 }));
  return {
    available: () => actuators().length > 0,
    // play takes the guest's level, 0 to 100, and the length vibrate.js has
    // already turned the request into; nothing of either is a stop.
    play(level, length) {
      const strength = Math.min(Math.max(Number(level) / 100 || 0, 0), 1);
      for (const actuator of actuators()) {
        if (!(length > 0) || !(strength > 0)) {
          still(actuator);
          continue;
        }
        quietly(() => actuator.playEffect("dual-rumble", {
          duration: length,
          startDelay: 0,
          strongMagnitude: strength,
          weakMagnitude: strength,
        }));
      }
    },
    stop() {
      for (const actuator of actuators()) still(actuator);
    },
  };
};
