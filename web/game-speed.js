// Which speed a game runs at, and where that is remembered.
//
// The setting corrects something that is a fact about one game rather than
// about this page: how much of its frame period was its own drawing on the
// handset. A title that issued a couple of hundred platform draw calls a frame
// and asked to be woken 10ms later gets almost all of that period back here,
// because the drawing is native code now — so it needs a quarter speed to look
// right. The title beside it, which rasterised in its own guest code, needs
// none. One shared setting therefore meant changing it by hand on every switch,
// and forgetting to is a game running at four times its own pace with nothing
// on screen to say why.
//
// The bookkeeping is here rather than in app.js because it has a rule worth a
// test — what a game with no stored speed inherits — and because storage is the
// one browser API that throws in normal use: private browsing denies it, and a
// setting nobody can save is not a page that may not run.

import { local } from "./storage.js";

const SPEED_KEY_PREFIX = "wfeature:speed:";

// An earlier page kept one speed for every game, under "wfeature:speed". That
// key is **not** read any more. Inheriting it looked like the conservative
// direction — somebody who slowed a game down before this change would find it
// still slow — but the setting it carried was chosen for one title, and once
// speeds became per-game it silently opened every other game at that title's
// speed. A quarter-speed setting made for one title is not a sensible default
// for the next one, and the page gives no reason on screen for why the game is
// slow. A game nobody has chosen a speed for now runs at the speed it was
// written for, and the setting is one change away for anyone who wants it back.

export const DEFAULT_SPEED = 1;

export const speedKey = path => `${SPEED_KEY_PREFIX}${path}`;

// A stored value that is not a positive number is treated as absent rather than
// used: it can only come from an older page or from somebody editing storage,
// and the server would refuse it anyway.
const positive = value => {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : 0;
};

// createGameSpeed answers the pair app.js uses, over whatever storage it is
// given. The storage is a parameter so a test can hand it a map; the default
// is the page's own fail-safe store, which never throws and keeps a value it
// could not save for the life of the page.
//
// The default is `local` rather than `globalThis.localStorage` for a reason
// that has nothing to do with tidiness: a default argument is evaluated at the
// call, and in a browser told to block site data *reading the property* throws
// — so the old default raised before this function's body, and its `catch`
// could not have caught it.
export const createGameSpeed = (storage = local) => {
  const read = key => {
    try {
      return storage?.getItem(key) ?? null;
    } catch {
      // A storage handed in by a caller may still throw; the page's own does
      // not. Every game is then the default and whatever is chosen lasts one
      // session.
      return null;
    }
  };

  return {
    // stored answers the multiplier for a game: its own, else the speed the
    // game was written for.
    stored: path => {
      const own = path ? positive(read(speedKey(path))) : 0;
      return own || DEFAULT_SPEED;
    },
    // remember stores a speed against the game it was chosen for. A call with
    // no game is a no-op rather than a write to a key nothing would read back.
    remember: (path, value) => {
      if (!path || !positive(value) || !storage) return false;
      try {
        // The page's own store answers whether the value reached the browser
        // and keeps what it could not save; a plain storage object answers
        // nothing, and a call that did not throw is a write that happened.
        return storage.setItem(speedKey(path), String(value)) !== false;
      } catch {
        return false;
      }
    },
  };
};
