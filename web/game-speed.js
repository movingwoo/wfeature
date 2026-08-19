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

const SPEED_KEY_PREFIX = "wfeature:speed:";

// LEGACY_SPEED_KEY is the key an earlier page used for the one speed every game
// shared. It is still read, for a game that has no speed of its own, so that
// somebody who slowed a game down before this change finds it still slow
// afterwards. **Nothing writes it any more**: the first change made to any game
// writes that game's own value, so the shared one only ever fades out. Reading
// it is the conservative direction — inheriting a slow setting is a game that
// looks as it did yesterday, where ignoring it is one that silently speeds up.
const LEGACY_SPEED_KEY = "wfeature:speed";

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
// given. The storage is a parameter so a test can hand it a map, and so a
// browser that throws on every access is one `catch` rather than one per site.
export const createGameSpeed = (storage = globalThis.localStorage) => {
  const read = key => {
    try {
      return storage?.getItem(key) ?? null;
    } catch {
      // Private browsing denies storage; every game is then the default and
      // whatever is chosen lasts one session.
      return null;
    }
  };

  return {
    // stored answers the multiplier for a game: its own, else the shared one an
    // older page left behind, else 1.
    stored: path => {
      const own = path ? positive(read(speedKey(path))) : 0;
      if (own) return own;
      return positive(read(LEGACY_SPEED_KEY)) || DEFAULT_SPEED;
    },
    // remember stores a speed against the game it was chosen for. A call with
    // no game is a no-op rather than a write to a key nothing would read back.
    remember: (path, value) => {
      if (!path || !positive(value) || !storage) return false;
      try {
        storage.setItem(speedKey(path), String(value));
        return true;
      } catch {
        return false;
      }
    },
  };
};
