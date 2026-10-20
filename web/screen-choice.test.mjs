import assert from "node:assert/strict";
import { test } from "node:test";

import { AUTO_SCREEN, SCREEN_SIZES, createScreenChoice, earlierScreenKey, screenKey, sizeOf } from "./screen-choice.js";

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

// An earlier page wrote its key only when a player changed the menu, so its
// 240x320 is a player who tried another size and went back: reading it as
// automatic would move a title that declares a smaller phone off the screen
// they settled on.
test("every size an earlier page kept stays chosen, 240x320 included", () => {
  const screens = createScreenChoice(storageOf({
    [earlierScreenKey(GAME)]: "240x320",
    [earlierScreenKey(OTHER)]: "128x160",
  }));
  assert.equal(screens.choice(GAME), "240x320");
  assert.deepEqual(screens.screen(GAME), { width: 240, height: 320 });
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

// Only an older page, a size later dropped from the menu or an edited store can
// hold these. The menu cannot show one, so it says 자동, and the start has to
// agree with it rather than send a screen nobody can see chosen.
test("a stored value the menu does not offer is automatic in the menu and in the start", () => {
  for (const stored of ["200x300", "480x800", "wide", "240", "20000x220", "176x16", "1.5x2", ""]) {
    for (const key of [screenKey, earlierScreenKey]) {
      const screens = createScreenChoice(storageOf({ [key(GAME)]: stored }));
      assert.equal(screens.choice(GAME), AUTO_SCREEN, `${key(GAME)}=${JSON.stringify(stored)}`);
      assert.equal(screens.screen(GAME), null, `${key(GAME)}=${JSON.stringify(stored)}`);
    }
  }
});

// What decides whether a change needs a restart is the screen a value names,
// so every size the menu offers has to name one and the automatic choice none.
test("each size the menu offers names its screen, and the automatic choice names none", () => {
  for (const size of SCREEN_SIZES) {
    const [width, height] = size.split("x").map(Number);
    assert.deepEqual(sizeOf(size), { width, height });
  }
  assert.equal(sizeOf(AUTO_SCREEN), null);
  assert.equal(sizeOf("200x300"), null);
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
