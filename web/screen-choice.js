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
// test — what an earlier page's value means now, and what a value the menu
// does not offer becomes — and because storage is the one browser API that
// throws in normal use.

import { local } from "./storage.js";

const SCREEN_KEY_PREFIX = "wfeature:screen-choice:";

// Where an earlier page kept the choice, while 240x320 was the default and was
// never sent. It is read for a game with nothing under the key above. That
// page wrote it only when a player changed the menu, so whatever it holds was
// somebody's choice and stays one — 240x320 included: a player who tried
// another size and went back picked it, and reading it as automatic would move
// a title that declares a smaller phone off the screen they settled on.
const EARLIER_SCREEN_KEY_PREFIX = "wfeature:screen:";

export const AUTO_SCREEN = "auto";

// The handsets the screen menu offers, in its order. A stored value that is
// not one of them reads as automatic, both in the menu and in what a start
// carries, so the menu never says one thing while a start sends another: such
// a value can only come from an older page, a size later dropped from the
// menu, or somebody editing storage. screen.test.mjs holds the markup to this
// list and every size in it to the range the server accepts.
export const SCREEN_SIZES = ["128x160", "176x220", "240x320", "240x400", "320x480"];

export const screenKey = path => `${SCREEN_KEY_PREFIX}${path}`;
export const earlierScreenKey = path => `${EARLIER_SCREEN_KEY_PREFIX}${path}`;

const offered = value => value === AUTO_SCREEN || SCREEN_SIZES.includes(value);

// sizeOf answers the screen a menu value names, or null for the automatic
// choice and for anything the menu does not offer.
export const sizeOf = value => {
  if (!SCREEN_SIZES.includes(value)) return null;
  const [width, height] = value.split("x").map(Number);
  return { width, height };
};

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

  // choice answers what the menu says for a game: "auto" or one of the sizes
  // it offers.
  const choice = path => {
    if (!path) return AUTO_SCREEN;
    const stored = read(screenKey(path));
    if (stored !== null) return offered(stored) ? stored : AUTO_SCREEN;
    const earlier = read(earlierScreenKey(path));
    return earlier !== null && offered(earlier) ? earlier : AUTO_SCREEN;
  };

  return {
    choice,
    // screen answers what a start carries: null when the choice is automatic,
    // and otherwise the size the menu shows.
    screen: path => sizeOf(choice(path)),
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
