// The keyboard's half of the keypad. The page ships one physical key per phone
// key and lets the user move any of them, which makes two rules worth holding
// in one place rather than in the panel that draws them: a physical key belongs
// to exactly one phone key, and a stored table has to survive this file
// changing under it.
//
// The table is written phone key to physical key, the direction the settings
// panel reads it in and the direction the uniqueness rule is stated in. The
// keydown handler needs the other direction and builds it with codeLookup.

// The keys the panel lists, in the order they sit on the keypad. This is a
// list of its own rather than the table's own order because a table keyed by
// "1" and "0" is not read back in the order it was written: JavaScript hands
// out integer-like keys first and in numeric order, which would put 0 at the
// top of the panel.
export const keyOrder = [
  "1", "2", "3",
  "4", "5", "6",
  "7", "8", "9",
  "*", "0", "#",
  "CLR", "CALL", "MENU",
  "UP", "LEFT", "RIGHT", "DOWN", "OK",
];

// Every name here is a name the code table in app.js knows, and
// keypad.test.mjs is what says so.
export const defaultBindings = {
  1: "Digit1",
  2: "Digit2",
  3: "Digit3",
  4: "KeyQ",
  5: "KeyW",
  6: "KeyE",
  7: "KeyA",
  8: "KeyS",
  9: "KeyD",
  "*": "KeyZ",
  0: "KeyX",
  "#": "KeyC",
  CLR: "Backspace",
  CALL: "Backslash",
  MENU: "KeyM",
  UP: "ArrowUp",
  LEFT: "ArrowLeft",
  RIGHT: "ArrowRight",
  DOWN: "ArrowDown",
  OK: "Space",
};

// What each phone key is called on screen. The direction pad is drawn with
// arrows on the keypad itself, so it is named with arrows here too.
const keyNames = {
  CLR: "CLR",
  CALL: "통화",
  MENU: "메뉴",
  UP: "↑",
  DOWN: "↓",
  LEFT: "←",
  RIGHT: "→",
  OK: "확인",
};

export const keyLabel = name => keyNames[name] ?? name;

// Keys that the browser or the page needs more than the game does. Binding one
// would take it away for the rest of the run: the handler answers a bound key
// by preventing its default, so Tab would stop moving focus and Escape would
// stop closing the panel the user is standing in.
const reservedCodes = new Set(["Tab", "Escape", "Enter", "NumpadEnter", "F5", "F11", "F12"]);

const modifierCodes = /^(Shift|Control|Alt|Meta|OS)(Left|Right)?$|^(CapsLock|NumLock|ScrollLock|ContextMenu)$/;

export const bindable = code =>
  typeof code === "string" && code !== "" && !reservedCodes.has(code) && !modifierCodes.test(code);

const codeNames = {
  Space: "Space",
  Backspace: "Backspace",
  Backslash: "\\",
  Slash: "/",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Minus: "-",
  Equal: "=",
  BracketLeft: "[",
  BracketRight: "]",
  Backquote: "`",
  ArrowUp: "↑",
  ArrowDown: "↓",
  ArrowLeft: "←",
  ArrowRight: "→",
  Insert: "Ins",
  Delete: "Del",
  Home: "Home",
  End: "End",
  PageUp: "PgUp",
  PageDown: "PgDn",
};

// What a physical key is called on screen. `event.code` names a position on the
// keyboard rather than the letter printed there, so this is a guess for anyone
// not typing on a US layout — but it is the same guess every emulator makes,
// and the row shows what was actually pressed, which settles it either way.
export const codeLabel = code => {
  if (!code) return "";
  if (codeNames[code]) return codeNames[code];
  const letter = /^Key([A-Z])$/.exec(code);
  if (letter) return letter[1];
  const digit = /^Digit([0-9])$/.exec(code);
  if (digit) return digit[1];
  const numpad = /^Numpad(.+)$/.exec(code);
  if (numpad) return `숫자패드 ${codeNames[numpad[1]] ?? numpad[1]}`;
  return code;
};

// loadBindings folds whatever was stored into the table this build declares.
// The stored half is the explicit one and is laid down first: a phone key this
// build has just gained takes its default only if the user has not already
// spent that physical key elsewhere. An entry naming a key this build no longer
// has is dropped by never being asked for.
export const loadBindings = stored => {
  const record = stored !== null && typeof stored === "object" ? stored : {};
  const bound = {};
  const taken = new Map();
  const set = (name, code) => {
    bound[name] = code;
    if (code) taken.set(code, name);
  };

  for (const name of keyOrder) {
    if (!Object.hasOwn(record, name)) continue;
    const code = record[name];
    set(name, bindable(code) && !taken.has(code) ? code : "");
  }
  for (const name of keyOrder) {
    if (Object.hasOwn(bound, name)) continue;
    const code = defaultBindings[name];
    set(name, taken.has(code) ? "" : code);
  }

  return Object.fromEntries(keyOrder.map(name => [name, bound[name] ?? ""]));
};

// assign gives a phone key a physical key, and takes that physical key off
// whatever else was holding it. Nothing else can be done with a collision: two
// phone keys on one physical key means one press sending two keys to the game,
// and asking the user to clear the old one first is the same two steps with a
// refusal in the middle.
export const assign = (bindings, name, code) => {
  if (!Object.hasOwn(bindings, name) || !bindable(code)) return bindings;
  const next = {};
  for (const [other, held] of Object.entries(bindings)) next[other] = held === code ? "" : held;
  next[name] = code;
  return next;
};

export const clear = (bindings, name) =>
  Object.hasOwn(bindings, name) ? { ...bindings, [name]: "" } : bindings;

// The direction the keydown handler asks in. Unbound keys are absent rather
// than empty, so a lookup answers undefined for a key nothing is on.
export const codeLookup = bindings =>
  Object.fromEntries(
    Object.entries(bindings)
      .filter(([, code]) => code)
      .map(([name, code]) => [code, name]),
  );

// Which phone key, if any, would lose its binding to this one.
export const heldBy = (bindings, code, name) =>
  Object.entries(bindings).find(([other, held]) => held === code && other !== name)?.[0] ?? "";
