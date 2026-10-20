import assert from "node:assert/strict";
import { test } from "node:test";

import { AUTO_SCREEN, DEFAULT_SCREEN, createScreenChoice, earlierScreenKey, screenKey } from "./screen-choice.js";

// A map that answers like localStorage, so the rules below are tested against
// what the page actually calls rather than against a copy of them.
const storageOf = (entries = {}) => {
  const map = new Map(Object.entries(entries));
  return {
    map,
    getItem: key => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => map.set(key, String(value)),
  };
};

const GAME = "ktf/some-game.zip";
const OTHER = "ktf/another-game.zip";

test("a game nobody chose a screen for starts automatically and sends none", () => {
  const screens = createScreenChoice(storageOf());
  assert.equal(DEFAULT_SCREEN, AUTO_SCREEN);
  assert.equal(screens.choice(GAME), AUTO_SCREEN);
  assert.equal(screens.screen(GAME), null);
});

// The default size is a choice like any other: leaving it out would let the
// archive decide, and a KTF descriptor may name a smaller phone.
test("a chosen size travels, 240x320 included", () => {
  const storage = storageOf();
  const screens = createScreenChoice(storage);
  screens.remember(GAME, "240x320");
  screens.remember(OTHER, "176x220");
  assert.deepEqual(screens.screen(GAME), { width: 240, height: 320 });
  assert.deepEqual(screens.screen(OTHER), { width: 176, height: 220 });
  assert.equal(storage.map.get(screenKey(GAME)), "240x320");
  screens.remember(GAME, AUTO_SCREEN);
  assert.equal(screens.screen(GAME), null);
});

// An earlier page never sent 240x320: it was the option labelled 기본, which
// is what automatic means now. Any other size it kept was somebody's choice.
test("an earlier page's default reads as automatic and its other sizes stay chosen", () => {
  const screens = createScreenChoice(storageOf({
    [earlierScreenKey(GAME)]: "240x320",
    [earlierScreenKey(OTHER)]: "128x160",
  }));
  assert.equal(screens.choice(GAME), AUTO_SCREEN);
  assert.equal(screens.screen(GAME), null);
  assert.equal(screens.choice(OTHER), "128x160");
  assert.deepEqual(screens.screen(OTHER), { width: 128, height: 160 });
});

test("a choice made now wins over what an earlier page kept", () => {
  const storage = storageOf({ [earlierScreenKey(GAME)]: "176x220" });
  const screens = createScreenChoice(storage);
  screens.remember(GAME, "240x320");
  assert.deepEqual(screens.screen(GAME), { width: 240, height: 320 });
  screens.remember(GAME, AUTO_SCREEN);
  assert.equal(screens.screen(GAME), null);
});

// Only an older page or an edited store can hold these, and the server would
// refuse every one; sending nothing lets the game start.
test("a stored size the server would refuse is sent as no size at all", () => {
  for (const stored of ["wide", "240", "20000x220", "176x16", "1.5x2", ""]) {
    const screens = createScreenChoice(storageOf({ [screenKey(GAME)]: stored }));
    assert.equal(screens.screen(GAME), null, JSON.stringify(stored));
  }
});

test("a storage that throws leaves every game automatic and a write unsaved", () => {
  const broken = {
    getItem() { throw new Error("denied"); },
    setItem() { throw new Error("denied"); },
  };
  const screens = createScreenChoice(broken);
  assert.equal(screens.choice(GAME), AUTO_SCREEN);
  assert.equal(screens.remember(GAME, "176x220"), false);
  assert.equal(screens.remember("", "176x220"), false);
});
