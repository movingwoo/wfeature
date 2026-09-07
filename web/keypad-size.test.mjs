import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import {
  clampMetric,
  clampMetrics,
  createKeypadSize,
  cssText,
  defaults,
  label,
  metrics,
} from "./keypad-size.js";

const css = readFileSync(new URL("./style.css", import.meta.url), "utf8");
const page = readFileSync(new URL("./index.html", import.meta.url), "utf8");
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");

// The declared value of a custom property, as written in the stylesheet.
const declared = property => {
  const match = css.match(new RegExp(`\\n\\s*${property}:\\s*([^;]+);`));
  return match?.[1].trim() ?? "";
};

// A storage the tests can look inside. It is the shape storage.js presents —
// setItem answers whether the value reached the browser — rather than the
// browser's own, because that is what the module is written against.
const fakeStorage = () => {
  const entries = new Map();
  return {
    entries,
    getItem: key => entries.get(key) ?? null,
    setItem: (key, value) => {
      entries.set(key, value);
      return true;
    },
    removeItem: key => entries.delete(key),
  };
};

test("the defaults are the keypad the stylesheet ships", () => {
  // The stylesheet declares the keypad and this module replaces it. A default
  // here that disagrees with the declaration there is a keypad that changes
  // shape the first time somebody opens the panel — without touching anything,
  // because the panel applies what it is holding as soon as it is built.
  for (const metric of metrics) {
    assert.equal(
      declared(metric.property),
      cssText(metric.name, metric.value),
      `${metric.property} does not match this module's default`,
    );
  }
});

test("the canvas budget moves with every metric that changes the keypad's height", () => {
  // The screen is budgeted against the smallest keypad there can be and the pad
  // spends what is left (style.css, --keypad-floor). A control that makes the
  // pad taller without appearing in that budget is a pad pushed off the bottom
  // of the page, with no way back to the panel that did it.
  const floor = declared("--keypad-floor");
  const slack = declared("--keypad-slack");
  for (const source of [floor, slack]) {
    assert.match(source, /--keypad-band/, "the top row is outside the height budget");
    assert.match(source, /--keypad-rows/, "the rows of keys are outside the height budget");
  }
  // The footer is a multiplier, so it reaches the budget as a row count.
  assert.match(declared("--keypad-rows"), /--keypad-footer-scale/);
  // And the floor is what the budget is taken against, so the smallest key has
  // to follow the chosen size rather than sit at a constant: on a short window
  // the pad is at its minimum, and a constant one would have left the keypad
  // the same size at every setting while taking the room off the screen.
  assert.match(declared("--keypad-key-min"), /--keypad-key-pref/);
});

test("the split is spent on the two pads and not on the space between them", () => {
  // The middle band is one row of two pads. What one is not given the other is,
  // which is the whole of the setting: a split that moved the gap instead would
  // shrink both halves and hand the room to neither.
  const columns = css.slice(css.indexOf(".keypad-main {"), css.indexOf("}", css.indexOf(".keypad-main {")));
  assert.match(columns, /--keypad-split/, "the columns do not follow the split");
  assert.ok(
    !/grid-template-columns:[^;]*repeat\(2/.test(columns),
    "the middle band is back on two equal columns",
  );
  // Both pads read the same number, from the two ends.
  assert.match(css, /\.direction-pad \{\s*--keypad-pad-share: var\(--keypad-split\);/);
  assert.match(css, /\.number-pad \{\s*--keypad-pad-share: calc\(1 - var\(--keypad-split\)\);/);
});

test("a value out of range is the end of the range rather than what was asked", () => {
  for (const metric of metrics) {
    assert.equal(clampMetric(metric.name, metric.max * 100), metric.max);
    assert.equal(clampMetric(metric.name, -1), metric.min);
    // A setting from an older page, a hand-edited entry, or a slider a browser
    // answered with an empty string.
    assert.equal(clampMetric(metric.name, "그렇게"), metric.value);
    assert.equal(clampMetric(metric.name, null), metric.value);
  }
});

test("a clamped value is a value the slider can show", () => {
  // Snapped to the step, and rounded: the fractional steps land on 0.30000004
  // as readily as on 0.3, and both the panel and the stylesheet print these.
  assert.equal(clampMetric("split", 0.333), 0.33);
  assert.equal(clampMetric("split", 0.3), 0.3);
  assert.equal(clampMetric("footer", 1.02), 1);
  assert.equal(clampMetric("key", 47.6), 48);
  for (const metric of metrics) {
    const value = clampMetric(metric.name, (metric.min + metric.max) / 2);
    assert.ok(String(value).length <= 6, `${metric.name} clamps to an unprintable ${value}`);
  }
});

test("a name the module does not know is dropped rather than kept", () => {
  // A control this page no longer has must not come back the day a later one
  // reuses its name.
  const values = clampMetrics({ key: 40, gone: 999 });
  assert.deepEqual(Object.keys(values).sort(), metrics.map(metric => metric.name).sort());
  assert.equal(values.key, 40);
  assert.equal(clampMetric("gone", 1), null);
});

test("a length keeps its unit and a multiplier does not", () => {
  // `calc()` multiplies by a number and not by a length, and the stylesheet
  // does both: --keypad-key-pref is a height and --keypad-split is a share.
  assert.equal(cssText("key", 48), "48px");
  assert.equal(cssText("split", 0.5), "0.5");
  assert.equal(label("key", 48), "48px");
  assert.equal(label("split", 0.45), "45%");
  assert.equal(label("footer", 1.4), "140%");
});

test("a size is remembered, and comes back clamped", () => {
  const storage = fakeStorage();
  const size = createKeypadSize(storage);
  assert.equal(size.set("key", 60), 60);
  assert.deepEqual(createKeypadSize(storage).values(), { ...defaults(), key: 60 });

  // Not through this page: an entry somebody wrote by hand, or one left by a
  // page whose ranges were different. A --keypad-key-pref of 4000px is a page
  // with no screen on it and no way back to the panel that did it.
  storage.entries.set("wfeature:keypadSize", JSON.stringify({ key: 4000, split: 9 }));
  const stored = createKeypadSize(storage).values();
  assert.equal(stored.key, 68);
  assert.equal(stored.split, 0.7);
});

test("storage that is not JSON, or not there, is the keypad this page ships", () => {
  const storage = fakeStorage();
  storage.entries.set("wfeature:keypadSize", "48px");
  assert.deepEqual(createKeypadSize(storage).values(), defaults());
  assert.deepEqual(createKeypadSize(undefined).values(), defaults());

  // A storage that throws on every call is a browser told to block site data.
  // The keypad is the default and the setting lasts as long as the page.
  const throwing = {
    getItem: () => { throw new Error("blocked"); },
    setItem: () => { throw new Error("blocked"); },
    removeItem: () => { throw new Error("blocked"); },
  };
  const size = createKeypadSize(throwing);
  assert.deepEqual(size.values(), defaults());
  assert.equal(size.set("key", 52), 52);
  assert.deepEqual(size.reset(), defaults());
});

test("reset forgets the entry rather than storing today's defaults", () => {
  // A stored default would go on meaning "this keypad" after the shipped one
  // changed, which is the bug game-speed.js records at its own key.
  const storage = fakeStorage();
  const size = createKeypadSize(storage);
  size.set("band", 20);
  assert.ok(storage.entries.has("wfeature:keypadSize"));
  assert.deepEqual(size.reset(), defaults());
  assert.equal(storage.entries.has("wfeature:keypadSize"), false);
});

test("applying writes the properties the stylesheet reads, and nothing else", () => {
  const written = new Map();
  const root = { style: { setProperty: (name, value) => written.set(name, value) } };
  const size = createKeypadSize(fakeStorage());
  size.set("split", 0.35);
  size.apply(root);
  assert.deepEqual(
    [...written.keys()].sort(),
    metrics.map(metric => metric.property).sort(),
  );
  assert.equal(written.get("--keypad-split"), "0.35");
  // A root without a style — a document that has not parsed, a test harness —
  // is not a page to throw on.
  assert.doesNotThrow(() => size.apply(null));
});

test("the panel is a screen over the canvas, built from this module's list", () => {
  // Ids the script reaches for: a missing one is a control that silently never
  // appears, which looks exactly like the page having no setting.
  for (const id of [
    "keypad-size",
    "keypad-size-list",
    "keypad-size-open",
    "keypad-size-close",
    "keypad-size-reset",
  ]) {
    assert.ok(page.includes(`id="${id}"`), `the page has no ${id}`);
    assert.ok(app.includes(`"${id}"`), `app.js never looks up ${id}`);
  }
  // Over the game screen rather than in the settings panel, which is a modal on
  // a phone and covers the keypad these sliders move.
  const wrapper = page.slice(
    page.indexOf('class="canvas-wrapper"'),
    page.indexOf('id="status-message"'),
  );
  assert.ok(wrapper.includes('id="keypad-size"'), "the size panel is outside the screen's hole");
  // The rows are built in script, so the panel cannot come to disagree with the
  // list about what there is to set.
  assert.ok(
    !page.includes('type="range" id="keypad-size'),
    "the size panel has sliders of its own in the markup",
  );
  assert.ok(app.includes("keypadSizeMetrics"), "app.js does not build the rows from the list");
});
