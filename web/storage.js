// Where the page remembers things, and what happens when it may not.
//
// Browser storage is the one API this page calls that throws in ordinary use.
// It is not only private browsing: a browser told to block site data throws on
// the *property* — reading `globalThis.localStorage` is itself the exception,
// before any key is named — and a browser that allows storage still throws
// `QuotaExceededError` on a write once the origin is full. Neither is a reason
// for a page not to run: every one of these values is a convenience, and a
// game that will not start because a setting could not be saved is a worse
// page than one that forgets the setting.
//
// It lives in its own module because the alternative is what this replaced: a
// `try` around each of eighteen call sites, three of which did not have one,
// and one of those three sat inside the block that starts a game — so a
// browser that denied storage answered the start button with an error message
// instead of a game.
//
// A value that could not be stored is kept in memory for the life of the page
// rather than dropped. The setting then lasts as long as the tab, which is the
// difference between a control that appears not to work and one that works
// until the page is closed.

export const createSafeStorage = open => {
  // Keys whose write did not reach the browser. A read prefers this: if the
  // write failed, whatever storage holds for that key is stale or absent, and
  // the value the page last chose is the true answer.
  const shadow = new Map();

  // The backing is resolved per call rather than once, because the throw can
  // come from the property itself and a page that failed to resolve it at load
  // would keep failing after the user allowed storage.
  const backing = () => {
    try {
      return open() ?? null;
    } catch {
      return null;
    }
  };

  return {
    getItem: key => {
      if (shadow.has(key)) return shadow.get(key);
      try {
        return backing()?.getItem(key) ?? null;
      } catch {
        return null;
      }
    },
    // setItem answers whether the value reached the browser, so a caller that
    // wants to say "this will not survive a reload" can. Nothing has to check:
    // the value is readable either way.
    setItem: (key, value) => {
      const text = String(value);
      try {
        const store = backing();
        if (!store) throw new Error("no storage");
        store.setItem(key, text);
        shadow.delete(key);
        return true;
      } catch {
        shadow.set(key, text);
        return false;
      }
    },
    // removeItem answers the same question setItem does — whether the browser
    // was reached — so a key dropped only from the shadow is not reported as
    // removed from a store that never held it.
    removeItem: key => {
      shadow.delete(key);
      try {
        const store = backing();
        if (!store) return false;
        store.removeItem(key);
        return true;
      } catch {
        return false;
      }
    },
    // available reports whether anything written here survives the page. The
    // page uses it to say so rather than to decide whether to run.
    available: () => {
      try {
        const store = backing();
        if (!store) return false;
        const probe = "wfeature:storage-probe";
        store.setItem(probe, "1");
        store.removeItem(probe);
        return true;
      } catch {
        return false;
      }
    },
  };
};

// The two stores the page uses. `local` outlives the tab and holds settings;
// `session` belongs to one tab and holds the resume token, which is what makes
// a phone discarding and reloading the page keep its game.
export const local = createSafeStorage(() => globalThis.localStorage);
export const session = createSafeStorage(() => globalThis.sessionStorage);
