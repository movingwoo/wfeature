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
  sizeRow,
} from "./keypad-size.js";

const css = readFileSync(new URL("./style.css", import.meta.url), "utf8");
const page = readFileSync(new URL("./index.html", import.meta.url), "utf8");
const app = readFileSync(new URL("./app.js", import.meta.url), "utf8");

// The body of one rule, found by its selector on a line of its own.
//
// `indexOf` alone is not enough and neither is a naive regex. `.keypad-pad {`
// is also the second line of `.keypad-band,\n.keypad-pad {`, and that rule
// belongs to the group rather than to either selector on its own — so a match
// whose previous line ends in a comma is skipped. Pass the whole grouped
// selector to read the group.
const ruleBody = selector => {
  const needle = `\n${selector} {`;
  let at = -1;
  for (let from = 0; ; ) {
    const found = css.indexOf(needle, from);
    if (found === -1) break;
    const previousLine = css.slice(css.lastIndexOf("\n", found - 1) + 1, found);
    if (!previousLine.trimEnd().endsWith(",")) {
      at = found;
      break;
    }
    from = found + 1;
  }
  assert.notEqual(at, -1, `the stylesheet has no rule for ${selector} on its own`);
  const opened = css.indexOf("{", at);
  return css.slice(opened + 1, css.indexOf("}", opened));
};

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
  // The pad is one grid and every row of it is a row of keys, so the budget
  // counts them rather than adding a band's own scale to three.
  assert.match(declared("--keypad-rows"), /^\d+$/, "the row count is not a count");
  // And the floor is what the budget is taken against, so the smallest key has
  // to follow the chosen size rather than sit at a constant: on a short window
  // the pad is at its minimum, and a constant one would have left the keypad
  // the same size at every setting while taking the room off the screen.
  assert.match(declared("--keypad-key-min"), /--keypad-key-pref/);
});

test("the split is spent on the pad's columns and not on the space between them", () => {
  // The pad is one grid of seven columns, and the setting is what the two sides
  // of it divide: what one is not given the other is. A split that moved a gap
  // instead would shrink both sides and hand the room to neither — which is
  // what the wide gap between the two former pads did, and it is a key column
  // now.
  const pad = ruleBody(".keypad-pad");
  assert.match(pad, /--keypad-split/, "the columns do not follow the split");
  assert.ok(
    !/grid-template-columns:\s*repeat\(7/.test(pad),
    "the pad is back on seven columns the split cannot reach",
  );
  // Both sides read the same number, from the two ends.
  assert.match(pad, /--keypad-share-left: var\(--keypad-split\);/);
  assert.match(pad, /--keypad-share-right: calc\(1 - var\(--keypad-split\)\);/);
  assert.match(pad, /repeat\(3, calc\(var\(--keypad-column-unit\) \* var\(--keypad-share-left\)\)\)/);
  assert.match(pad, /repeat\(3, calc\(var\(--keypad-column-unit\) \* var\(--keypad-share-right\)\)\)/);
});

test("the band's columns are equal, and deliberately not the split's", () => {
  // This is a decision that looks like a defect, which is why it is pinned
  // rather than left to read as one: an audit of this branch called it a bug
  // and made the band follow the split, and it was taken back out.
  //
  // The left/right share is about which thumb gets the bigger keys. The keys in
  // the band have no hand — 메뉴, 통화 and CLR are aimed at one at a time from
  // wherever the thumb is — so tilting them buys nothing and costs the row its
  // even spacing. The price is that the band stops lining up with the pad once
  // the setting leaves 0.5, and that is accepted.
  const band = ruleBody(".keypad-band");
  assert.match(band, /grid-template-columns: repeat\(7, minmax\(0, 1fr\)\);/);
  assert.ok(!band.includes("--keypad-split"), "the band follows the split again");
  assert.ok(!band.includes("--keypad-column-unit"), "the band sizes its columns by the share again");
  // The pad is the one that does, and it is the only one.
  assert.match(ruleBody(".keypad-pad"), /--keypad-split/);
  // What they *do* share is the shape of the grid, so a cell of one sits over a
  // cell of the other at the default: seven columns and the same gap.
  const shared = ruleBody(".keypad-band,\n.keypad-pad");
  assert.match(shared, /column-gap: var\(--keypad-cell-gap\);/);
  assert.ok(!shared.includes("grid-template-columns"), "the columns are shared again");
});

test("the middle column goes with the wider side, and the shares fill the track", () => {
  // A split that left the middle column at a seventh would put an odd column
  // between the two sides belonging to neither. Taking the wider side's width
  // makes the pad read as four wide columns and three narrow ones, which is
  // what "make that side bigger" means on one grid.
  const pad = ruleBody(".keypad-pad");
  assert.match(pad, /--keypad-share-mid: max\(var\(--keypad-share-left\), var\(--keypad-share-right\)\);/);
  // And the shares are normalised by their own total, or seven columns of a
  // moved split would not add up to the row: three of each side plus the one
  // the middle took.
  assert.match(pad, /--keypad-share-total: calc\(3 \+ var\(--keypad-share-mid\)\);/);
  assert.match(pad, /--keypad-column-unit: calc\(var\(--keypad-track\) \/ var\(--keypad-share-total\)\);/);
  // The last row has no size of its own left to set.
  assert.ok(!css.includes("--keypad-footer-scale"), "the last row is scaled again");
  assert.ok(!page.includes("keypad-last-row"), "the markup still marks the last row");
  assert.ok(
    !metrics.some(metric => metric.name === "footer"),
    "the panel offers a setting the stylesheet no longer reads",
  );
});

test("every row of the pad is a row of keys, all the same height", () => {
  assert.match(ruleBody(".keypad-pad"), /grid-template-rows: repeat\(4, var\(--keypad-key-height\)\);/);
  assert.equal(declared("--keypad-rows"), "4");
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
  // A share is the other kind, and the panel prints it as one: "45%" is a thing
  // a person can picture and "0.45" is not.
  assert.equal(cssText("split", 0.45), "0.45");
});

test("a size is one shape's, remembered and clamped", () => {
  const storage = fakeStorage();
  const size = createKeypadSize(storage);
  assert.equal(size.set("type1", "key", 60), 60);
  assert.deepEqual(createKeypadSize(storage).values("type1"), { ...defaults(), key: 60 });
  // The shape it was not set on keeps the keypad this page ships.
  assert.deepEqual(size.values("type2"), defaults());

  // Not through this page: an entry somebody wrote by hand, or one left by a
  // page whose ranges were different. A --keypad-key-pref of 4000px is a page
  // with no screen on it and no way back to the panel that did it.
  storage.entries.set("wfeature:keypadSize", JSON.stringify({ type1: { key: 4000, split: 9 } }));
  const stored = createKeypadSize(storage).values("type1");
  assert.equal(stored.key, 68);
  assert.equal(stored.split, 0.7);
});

test("each shape keeps its own size, and one does not move another", () => {
  // A size belongs to a shape because the shapes differ in what is on them:
  // one has the arrows as digits on the left, one has a whole number pad, one
  // is empty until somebody fills it, and how much room each half wants differs
  // with them.
  const storage = fakeStorage();
  const size = createKeypadSize(storage);
  size.set("type1", "split", 0.65);
  size.set("type2", "split", 0.35);
  assert.equal(size.values("type1").split, 0.65);
  assert.equal(size.values("type2").split, 0.35);
  assert.equal(size.values("type4").split, defaults().split);

  // Resetting one is one shape's business.
  assert.deepEqual(size.reset("type1"), defaults());
  assert.equal(size.values("type1").split, defaults().split);
  assert.equal(size.values("type2").split, 0.35);
  assert.deepEqual(Object.keys(JSON.parse(storage.getItem("wfeature:keypadSize"))), ["type2"]);
});

test("the size a 0.4 page stored is every shape's, until one of them moves", () => {
  // That entry is a single set of numbers with no shape attached, because there
  // were no shapes to attach it to: switching keypads then did not change the
  // keys' size. Giving it to one shape only would move the pad the first time
  // somebody switched.
  const storage = fakeStorage();
  storage.entries.set("wfeature:keypadSize", JSON.stringify({ key: 60, split: 0.4, band: 40 }));
  const size = createKeypadSize(storage);
  for (const shape of ["type1", "type2", "type3", "type4"]) {
    assert.deepEqual(size.values(shape), { key: 60, split: 0.4, band: 40 }, `${shape} lost it`);
  }
  // The first per-shape write spreads it before it changes one, so the others
  // go on drawing the keypad they were drawing.
  size.set("type2", "key", 36);
  assert.equal(size.values("type2").key, 36);
  assert.equal(size.values("type1").key, 60);
  assert.equal(size.values("type3").key, 60);
  // And a shape reset after the spread is the shipped default rather than the
  // number that page had.
  assert.deepEqual(size.reset("type3"), defaults());
  assert.equal(size.values("type3").key, defaults().key);
  assert.equal(size.values("type1").key, 60);
});

test("storage that is not JSON, or not there, is the keypad this page ships", () => {
  const storage = fakeStorage();
  storage.entries.set("wfeature:keypadSize", "48px");
  assert.deepEqual(createKeypadSize(storage).values("type1"), defaults());
  assert.deepEqual(createKeypadSize(undefined).values("type1"), defaults());

  // A storage that throws on every call is a browser told to block site data.
  // The keypad is the default and the setting lasts as long as the page.
  const throwing = {
    getItem: () => { throw new Error("blocked"); },
    setItem: () => { throw new Error("blocked"); },
    removeItem: () => { throw new Error("blocked"); },
  };
  const size = createKeypadSize(throwing);
  assert.deepEqual(size.values("type1"), defaults());
  assert.equal(size.set("type1", "key", 52), 52);
  assert.deepEqual(size.reset("type1"), defaults());
});

test("reset forgets the entry rather than storing today's defaults", () => {
  // A stored default would go on meaning "this keypad" after the shipped one
  // changed, which is the bug game-speed.js records at its own key.
  const storage = fakeStorage();
  const size = createKeypadSize(storage);
  size.set("type1", "band", 20);
  assert.ok(storage.entries.has("wfeature:keypadSize"));
  assert.deepEqual(size.reset("type1"), defaults());
  assert.equal(storage.entries.has("wfeature:keypadSize"), false);
});

test("applying writes the properties the stylesheet reads, and nothing else", () => {
  const written = new Map();
  const root = { style: { setProperty: (name, value) => written.set(name, value) } };
  const size = createKeypadSize(fakeStorage());
  size.set("type1", "split", 0.35);
  size.apply("type1", root);
  assert.deepEqual(
    [...written.keys()].sort(),
    metrics.map(metric => metric.property).sort(),
  );
  assert.equal(written.get("--keypad-split"), "0.35");
  // The shape decides which numbers are written, which is what makes changing
  // the shape bring its own size back with it.
  written.clear();
  size.apply("type2", root);
  assert.equal(written.get("--keypad-split"), String(defaults().split));
  // A root without a style — a document that has not parsed, a test harness —
  // is not a page to throw on.
  assert.doesNotThrow(() => size.apply("type1", null));
});

// Enough of a document for a row: the element, its attributes, its children.
// A stub rather than a browser because there is no browser here, and the thing
// worth checking does not need one — whether a row is born showing its number.
const stubDocument = () => ({
  createElement: tag => ({
    tag,
    children: [],
    attributes: {},
    className: "",
    textContent: "",
    append(...nodes) {
      this.children.push(...nodes);
    },
    addEventListener() {},
  }),
});

test("a row is born showing its value, and does not wait to be told", () => {
  // This is the defect it was written for. The rows used to be built after the
  // only draw, so nothing set a thumb or a readout: every slider sat at
  // whatever a range with no value renders at — its own midpoint, not the
  // stored number — and every readout was blank until the first drag, which is
  // the one thing that did call `show`. A value the row cannot be created
  // without is what makes that unwritable.
  const doc = stubDocument();
  const split = metrics.find(metric => metric.name === "split");
  const { row, slider } = sizeRow(split, 0.65, doc);
  assert.equal(slider.value, "0.65", "the thumb was not put where the value is");
  const [name, thumb, readout] = row.children;
  assert.equal(name.textContent, split.label);
  assert.equal(thumb, slider);
  assert.equal(readout.textContent, "65%", "the readout is blank until something moves");
  // The range the slider offers is the metric's own, or a stored value could
  // sit somewhere the control cannot show.
  assert.equal(slider.min, String(split.min));
  assert.equal(slider.max, String(split.max));
  assert.equal(slider.step, String(split.step));
  assert.equal(slider.type, "range");
  // The classes the stylesheet lays the row out with.
  assert.deepEqual(
    [row.className, name.className, readout.className],
    ["keypad-size-row", "keypad-size-name", "keypad-size-value"],
  );
});

test("a row shows what the value became, not what was asked for", () => {
  // A number out of range, or between two steps, is clamped on the way in — and
  // a control showing the request instead of the answer disagrees with the
  // keypad beside it.
  const doc = stubDocument();
  const key = metrics.find(metric => metric.name === "key");
  assert.equal(sizeRow(key, 4000, doc).slider.value, String(key.max));
  assert.equal(sizeRow(key, undefined, doc).slider.value, String(key.value));
  assert.equal(sizeRow(metrics.find(m => m.name === "split"), 0.333, doc).slider.value, "0.33");
});

test("every metric can be a row", () => {
  const doc = stubDocument();
  for (const metric of metrics) {
    const { row, slider } = sizeRow(metric, metric.value, doc);
    assert.equal(slider.value, String(metric.value), `${metric.name} does not show its default`);
    assert.equal(row.children.length, 3, `${metric.name} is not a name, a slider and a number`);
  }
});

test("the sliders are rows of the one keypad screen, built from this module's list", () => {
  // There were two panels, one for the size and one for the cells. They
  // answered halves of the same question and each shape wants its own answer to
  // both, so they are one screen — and the ids the script reaches for are that
  // screen's. A missing one is a control that silently never appears, which
  // looks exactly like the page having no setting.
  for (const id of [
    "keypad-arrange",
    "keypad-size-list",
    "keypad-arrange-open",
    "keypad-arrange-close",
    "keypad-arrange-reset",
  ]) {
    assert.ok(page.includes(`id="${id}"`), `the page has no ${id}`);
    assert.ok(app.includes(`"${id}"`), `app.js never looks up ${id}`);
  }
  // The size panel is gone rather than hidden: two screens for one keypad is
  // what this replaced.
  for (const gone of ["keypad-size-open", "keypad-size-close", "keypad-size-reset", 'id="keypad-size"']) {
    assert.ok(!page.includes(gone), `the size panel is still in the page as ${gone}`);
  }
  // Over the game screen rather than in the settings panel, which is a modal on
  // a phone and covers the keypad these sliders move.
  const wrapper = page.slice(
    page.indexOf('class="canvas-wrapper"'),
    page.indexOf('id="status-message"'),
  );
  assert.ok(wrapper.includes('id="keypad-arrange"'), "the keypad screen is outside the screen's hole");
  // The rows are built in script, so the panel cannot come to disagree with the
  // list about what there is to set.
  assert.ok(
    !page.includes('type="range" id="keypad-size'),
    "the keypad screen has sliders of its own in the markup",
  );
  assert.ok(app.includes("keypadSizeMetrics"), "app.js does not build the rows from the list");
  // And it builds them through the row this module owns, which is what carries
  // the value into the control rather than leaving that to a later call.
  assert.match(app, /keypadSizeRow\(metric, size\.values\(layout\.shape\(\)\)\[metric\.name\]\)/);
});
