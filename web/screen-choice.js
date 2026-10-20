// Which handset screen a game starts on, and where that is remembered.
//
// The setting is a fact about one title — which handset its archive was
// packaged for — so it is kept per game. Its default is automatic: the start
// carries no size and the server asks the archive. A KTF descriptor names the
// handset its title was packaged for, and a 176x220 package started on a
// 240x320 screen keeps its pictures in a corner while its background and text
// spread over the rest, so the archive's answer is the right one for most
// titles without anybody opening the menu. A size is a player's choice and
// always travels, 240x320 included: without one the server would ask the
// archive, which may name a smaller phone.
//
// The bookkeeping is here rather than in app.js because it has rules worth a
// test — what an earlier page's value means now, and what a value the server
// would refuse becomes — and because storage is the one browser API that
// throws in normal use.

import { local } from "./storage.js";

const SCREEN_KEY_PREFIX = "wfeature:screen-choice:";

// Where an earlier page kept the choice, while 240x320 was the default and was
// never sent. It is read for a game with nothing under the key above, and its
// 240x320 reads as automatic: that was the option labelled 기본, and what it
// meant was "whatever the server starts it on", which is what automatic is
// now. Any other size there was a choice, and stays one.
const EARLIER_SCREEN_KEY_PREFIX = "wfeature:screen:";

export const AUTO_SCREEN = "auto";
export const DEFAULT_SCREEN = AUTO_SCREEN;

// The range the server accepts, mirrored so a stored value outside it is
// treated as absent here rather than sent on and refused there. See
// `validScreen` in internal/webhost/session.go; screen.test.mjs compares them.
export const MIN_SCREEN = 32;
export const MAX_SCREEN = 1024;

export const screenKey = path => `${SCREEN_KEY_PREFIX}${path}`;
export const earlierScreenKey = path => `${EARLIER_SCREEN_KEY_PREFIX}${path}`;

const inRange = value => Number.isInteger(value) && value >= MIN_SCREEN && value <= MAX_SCREEN;

// createScreenChoice answers what app.js asks, over whatever storage it is
// given; a test hands it a map. The default is the page's own fail-safe store,
// for the reason game-speed.js gives.
export const createScreenChoice = (storage = local) => {
  const read = key => {
    try {
      return storage?.getItem(key) ?? null;
    } catch {
      return null;
    }
  };

  // choice answers what the menu says for a game: "auto" or a "WxH" size.
  const choice = path => {
    if (!path) return DEFAULT_SCREEN;
    const stored = read(screenKey(path));
    if (stored !== null) return stored;
    const earlier = read(earlierScreenKey(path));
    if (earlier === null || earlier === "240x320") return DEFAULT_SCREEN;
    return earlier;
  };

  return {
    choice,
    // screen answers what a start carries: null when the choice is automatic,
    // and otherwise the size. A stored value that is not two numbers in range
    // is treated as automatic rather than sent on: it can only come from an
    // older page or from somebody editing storage.
    screen: path => {
      const chosen = choice(path);
      if (chosen === AUTO_SCREEN) return null;
      const [width, height] = String(chosen).split("x").map(Number);
      if (!inRange(width) || !inRange(height)) return null;
      return { width, height };
    },
    // remember stores a choice against the game it was made for. A call with
    // no game is a no-op rather than a write to a key nothing would read back.
    remember: (path, value) => {
      if (!path || !storage) return false;
      try {
        return storage.setItem(screenKey(path), String(value)) !== false;
      } catch {
        return false;
      }
    },
  };
};
