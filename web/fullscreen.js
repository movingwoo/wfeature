// 전체화면: the keypad put away and the game screen as large as the window
// allows, for somebody playing on a game controller or a keyboard, to whom the
// keypad drawn under the screen is only something that keeps the screen small.
//
// It is the page's own layout rather than the browser's fullscreen, for two
// reasons. The browser grants fullscreen only to a click or a key, and a
// controller's button is neither, so a switch that answered one way from the
// settings and another from the controller would be two switches; and an
// iPhone does not grant it to a page at all. The browser's own (F11, or a
// phone app that already fills the screen) is still there on top of this one.
//
// What it changes is one class on the document: the stylesheet draws the
// layout and app.js decides what else follows. This file is the state, and
// where it is remembered — somebody who plays on a controller wants to find
// the page the way they left it.

export const FULLSCREEN = "FULLSCREEN";
export const FULLSCREEN_KEY = "wfeature:fullscreen";

// createFullscreen restores the stored choice onto `root` at once, so the page
// does not come up in one layout and jump to the other.
export const createFullscreen = ({ root, storage, changed = () => {} } = {}) => {
  let on = false;
  try {
    on = storage?.getItem(FULLSCREEN_KEY) === "on";
  } catch {
    // A storage that throws is a page that starts in the ordinary layout.
  }
  const draw = () => root?.classList.toggle("fullscreen", on);
  draw();

  const set = value => {
    const next = Boolean(value);
    if (next === on) return false;
    on = next;
    draw();
    try {
      storage?.setItem(FULLSCREEN_KEY, on ? "on" : "off");
    } catch {
      // Kept for this page only.
    }
    changed(on);
    return true;
  };

  return {
    get on() {
      return on;
    },
    // set answers whether anything changed, which is what a caller that
    // follows the layout needs to know.
    set,
    toggle: () => set(!on),
  };
};
