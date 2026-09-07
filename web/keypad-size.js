// How big the keypad is, and where that is remembered.
//
// The pad this page draws is one shape, and the hands that use it are not: a
// phone held in one hand wants the direction pad under the thumb that is
// holding it and hardly ever presses a number, and a phone held in two wants
// the two halves the same size. One default cannot be both, and the layout list
// beside this setting does not help — Type1/2/3 change *which* keys are drawn,
// never how much room they get.
//
// So the four bands of the keypad are measurements rather than constants. The
// stylesheet already drew the pad from custom properties on :root and computed
// the canvas above it from the same numbers (style.css, "The keypad's metrics
// live here"), so what this module adds is a person changing them: it writes
// the same properties the stylesheet declares, and every rule that reads them —
// the pad, the gaps, and the height the game screen is allowed — follows.
//
// Two consequences are worth stating because they are what makes the setting
// safe rather than a way to push the keypad off the bottom of the page:
//
//   - The keypad and the screen are one budget. `--keypad-key-pref` is not
//     "how big the keys are", it is how the room is split: the stylesheet
//     budgets the canvas against the smallest keypad there can be and hands
//     the pad what is left, so a bigger key here is a smaller screen above it.
//     That is the trade the person is making, and it is the honest one.
//   - Every number is clamped on the way in and on the way out. The ranges
//     below are the ones a slider offers, and a value from storage — an older
//     page, a hand-edited entry — is put through the same clamp rather than
//     trusted, because a `--keypad-key-pref` of 4000px is a page with no
//     screen and no way back to this panel.

import { local } from "./storage.js";

const KEYPAD_SIZE_KEY = "wfeature:keypadSize";

// The four controls, in the order the panel lists them, which is the order the
// bands sit on the keypad: the pad itself first because it is what a thumb is
// on, then the row above it and the row below.
//
// `value` is today's keypad. Changing one of these changes what a page with
// nothing stored draws, and `keypad-size.test.mjs` compares each one against
// the declaration in style.css, since a default that disagrees with the
// stylesheet is a keypad that jumps the first time the panel is opened.
export const metrics = [
  {
    name: "key",
    label: "키 크기",
    property: "--keypad-key-pref",
    unit: "px",
    min: 36,
    max: 68,
    step: 1,
    value: 48,
  },
  {
    // The middle two bands share one number: the pad is a row of two, so the
    // share the direction pad takes is the share the number pad does not. A
    // slider that moves left shrinks the direction pad's keys and gives the
    // room to the number pad, which is what a thumb on the left-hand side of a
    // phone wants and the exact opposite of what the other hand wants.
    name: "split",
    label: "가운데 좌우",
    property: "--keypad-split",
    unit: "",
    min: 0.3,
    max: 0.7,
    step: 0.01,
    value: 0.5,
  },
  {
    name: "band",
    label: "윗줄",
    property: "--keypad-band",
    unit: "px",
    min: 20,
    max: 56,
    step: 1,
    value: 32,
  },
  {
    // The last row is a multiplier rather than a height because it is the one
    // band whose keys are not the pad's: * 0 # are pressed rarely and are the
    // first thing somebody wanting a bigger pad gives up.
    name: "footer",
    label: "아랫줄",
    property: "--keypad-footer-scale",
    unit: "",
    min: 0.7,
    max: 1.4,
    step: 0.05,
    value: 1,
  },
];

const byName = new Map(metrics.map(metric => [metric.name, metric]));

export const defaults = () =>
  Object.fromEntries(metrics.map(metric => [metric.name, metric.value]));

// clampMetric answers a number inside the slider's own range, and the default
// for anything that is not a number at all. It also snaps to the step, so a
// value that arrived from storage cannot sit between two positions of the
// slider that is supposed to be showing it.
export const clampMetric = (name, value) => {
  const metric = byName.get(name);
  if (!metric) return null;
  // A number, or the string a range input answers with. Everything else is
  // absent rather than zero, which is what `Number` would make of null, of an
  // empty string and of a slider a browser handed back blank — and zero is a
  // long way from any of these ranges, so an absent value would have come out
  // as the smallest keypad there is rather than as the shipped one.
  const asked =
    typeof value === "number" || (typeof value === "string" && value.trim() !== "")
      ? Number(value)
      : Number.NaN;
  if (!Number.isFinite(asked)) return metric.value;
  const stepped = Math.round((asked - metric.min) / metric.step) * metric.step + metric.min;
  const bounded = Math.min(metric.max, Math.max(metric.min, stepped));
  // The steps are fractions here, so the arithmetic above lands on 0.30000004
  // as readily as on 0.3; the panel prints these and the stylesheet reads them.
  return Math.round(bounded * 1000) / 1000;
};

// clampMetrics answers a full set: every name the module knows, taking what the
// input offers and the default for what it does not. Names it does not know are
// dropped rather than kept, so a value left behind by a control this page no
// longer has cannot come back when a later one reuses the name.
export const clampMetrics = values => {
  const source = values && typeof values === "object" ? values : {};
  return Object.fromEntries(
    metrics.map(metric => [metric.name, clampMetric(metric.name, source[metric.name])]),
  );
};

// cssText is what the stylesheet is given: a length keeps its unit and a
// multiplier is a bare number, because `calc()` multiplies by one and not by
// the other.
export const cssText = (name, value) => {
  const metric = byName.get(name);
  return metric ? `${value}${metric.unit}` : "";
};

// label is what the panel prints beside a slider. A share and a multiplier are
// both percentages there — "45%" is a thing a person can picture and "0.45" is
// not — and a length is the pixels it is.
export const label = (name, value) => {
  const metric = byName.get(name);
  if (!metric) return "";
  return metric.unit === "px" ? `${value}px` : `${Math.round(value * 100)}%`;
};

// createKeypadSize answers the object app.js drives, over whatever storage it
// is given. The storage is a parameter so a test can hand it a map; the default
// is the page's own fail-safe store, which never throws and keeps a value it
// could not save for the life of the page.
//
// The default is `local` rather than `globalThis.localStorage` for the reason
// game-speed.js gives at its own default: a browser told to block site data
// throws on the *property*, and a default argument is evaluated at the call, so
// naming the browser's store here would raise before this function's body.
export const createKeypadSize = (storage = local) => {
  const read = () => {
    try {
      return JSON.parse(storage?.getItem(KEYPAD_SIZE_KEY) ?? "null");
    } catch {
      // Both halves of that line can fail: a storage handed in by a caller may
      // throw where the page's own does not, and a stored string that is not
      // JSON is what an older page or a hand edit leaves behind. Either way the
      // keypad is the one this page ships.
      return null;
    }
  };

  let values = clampMetrics(read());

  const write = () => {
    try {
      return storage?.setItem(KEYPAD_SIZE_KEY, JSON.stringify(values)) !== false;
    } catch {
      return false;
    }
  };

  return {
    values: () => ({ ...values }),

    // apply writes the properties on the element the stylesheet reads them
    // from, which is the document's root. Nothing else has to be told: the
    // pad's rules, the gaps, and the canvas's height budget are all `calc()`
    // over these four, so one write moves the whole page and a rotation or a
    // resize needs no second one.
    apply: (root = document.documentElement) => {
      if (!root?.style) return;
      for (const metric of metrics) {
        root.style.setProperty(metric.property, cssText(metric.name, values[metric.name]));
      }
    },

    // set stores one control's new value and answers what it became, which is
    // not always what was asked for — the panel prints the answer rather than
    // the request, so a slider dragged past the end shows the end.
    set: (name, value) => {
      if (!byName.has(name)) return null;
      values = { ...values, [name]: clampMetric(name, value) };
      write();
      return values[name];
    },

    // reset takes the setting back to the keypad this page ships, and removes
    // the entry rather than storing the defaults: a stored default would go on
    // meaning "this keypad" after the shipped one changed.
    reset: () => {
      values = defaults();
      try {
        storage?.removeItem?.(KEYPAD_SIZE_KEY);
      } catch {
        // A store that will not forget is one this page cannot help; the
        // values above are already the defaults and the page is correct.
      }
      return { ...values };
    },
  };
};
