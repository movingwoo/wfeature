import assert from "node:assert/strict";
import { test } from "node:test";

import { DEFAULT_SPEED, createGameSpeed, speedKey } from "./game-speed.js";

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

const LEGACY = "wfeature:speed";
const GAME = "ktf/some-game.zip";
const OTHER = "ktf/another-game.zip";

test("a game nothing was ever chosen for runs at its own pace", () => {
  const speed = createGameSpeed(storageOf());
  assert.equal(speed.stored(GAME), DEFAULT_SPEED);
});

test("each game keeps its own speed", () => {
  const storage = storageOf();
  const speed = createGameSpeed(storage);
  speed.remember(GAME, 0.25);
  speed.remember(OTHER, 2);
  assert.equal(speed.stored(GAME), 0.25);
  assert.equal(speed.stored(OTHER), 2);
  // The point of the whole change: one game's choice does not reach the next.
  assert.equal(speed.stored("ktf/untouched.zip"), DEFAULT_SPEED);
});

test("the speed an older page shared is not inherited by anything", () => {
  const speed = createGameSpeed(storageOf({ [LEGACY]: "0.25" }));
  // A speed chosen for one title, back when there was one setting for all of
  // them, is not a default for the next title. A game nobody has chosen for
  // opens at the speed it was written for.
  assert.equal(speed.stored(GAME), DEFAULT_SPEED);
  assert.equal(speed.stored(OTHER), DEFAULT_SPEED);
});

test("a game's own speed is used whatever the older page left behind", () => {
  const storage = storageOf({ [LEGACY]: "0.25" });
  const speed = createGameSpeed(storage);
  speed.remember(GAME, 2);
  assert.equal(speed.stored(GAME), 2);
  assert.equal(speed.stored(OTHER), DEFAULT_SPEED);
});

test("choosing a speed never writes the shared key back", () => {
  const storage = storageOf({ [LEGACY]: "0.25" });
  const speed = createGameSpeed(storage);
  speed.remember(GAME, 4);
  // The key is neither read nor written now; a page that still has one is left
  // holding it rather than having storage rewritten underneath it.
  assert.equal(storage.map.get(LEGACY), "0.25");
  assert.equal(storage.map.get(speedKey(GAME)), "4");
});

test("a speed chosen with no game in hand is not written anywhere", () => {
  const storage = storageOf();
  const speed = createGameSpeed(storage);
  assert.equal(speed.remember("", 0.5), false);
  assert.equal(storage.map.size, 0);
});

test("a stored value that is not a positive number is treated as absent", () => {
  for (const stored of ["0", "-1", "", "fast", "NaN"]) {
    const speed = createGameSpeed(storageOf({ [speedKey(GAME)]: stored }));
    assert.equal(speed.stored(GAME), DEFAULT_SPEED, `stored ${JSON.stringify(stored)}`);
  }
});

test("storage that refuses every call is the default rather than a broken page", () => {
  const denied = {
    getItem: () => {
      throw new DOMException("denied");
    },
    setItem: () => {
      throw new DOMException("denied");
    },
  };
  const speed = createGameSpeed(denied);
  assert.equal(speed.stored(GAME), DEFAULT_SPEED);
  assert.equal(speed.remember(GAME, 0.5), false);
});

test("a page with no storage at all still answers", () => {
  const speed = createGameSpeed(undefined);
  assert.equal(speed.stored(GAME), DEFAULT_SPEED);
  assert.equal(speed.remember(GAME, 0.5), false);
});
