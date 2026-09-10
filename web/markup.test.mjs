import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// The page is one hand-written file and nothing in the build reads it as
// markup: the server serves the bytes, the browser forgives almost anything,
// and the tests that came before this one all read it as *text*. So a stray
// `</div>` is invisible to every check there is — and it is not a harmless
// stray. It closes whatever is open early, and the elements after it move: one
// extra closed the page's column, which put the rail that holds the settings
// panel outside it, and the panel disappeared while every test still passed.
//
// This walks the tags instead. It is a few lines because it does not need to
// be a parser — void elements and a stack are the whole of it — and what it
// buys is that the shape of the page is checked at all.

const page = readFileSync(new URL("./index.html", import.meta.url), "utf8");

const voidElements = new Set([
  "area", "base", "br", "col", "embed", "hr", "img",
  "input", "link", "meta", "source", "track", "wbr",
]);

// The tags, in order, with comments and the contents of <script>/<style> taken
// out first: both may hold anything that looks like a tag and neither nests.
const scan = () => {
  const source = page
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, "")
    .replace(/<style\b[^>]*>[\s\S]*?<\/style>/gi, "");
  const tags = [];
  for (const match of source.matchAll(/<(\/?)([a-zA-Z][a-zA-Z0-9-]*)\b([^>]*)>/g)) {
    const [, slash, name, attributes] = match;
    if (voidElements.has(name.toLowerCase())) continue;
    // A self-closing tag opens and closes in one, which the page uses for its
    // file inputs.
    if (!slash && attributes.trim().endsWith("/")) continue;
    tags.push({
      closing: slash === "/",
      name: name.toLowerCase(),
      line: source.slice(0, match.index).split("\n").length,
      id: /\bid="([^"]+)"/.exec(attributes)?.[1] ?? "",
      className: /\bclass="([^"]+)"/.exec(attributes)?.[1] ?? "",
    });
  }
  return tags;
};

// The stack of ancestors an element sits under, which is what a rule like "the
// settings panel is in the rail" is about — an element can be spelled right and
// still be in the wrong place, and that is what a stray close does.
const ancestry = () => {
  const stack = [];
  const paths = new Map();
  const faults = [];
  for (const tag of scan()) {
    if (!tag.closing) {
      const label = tag.id ? `${tag.name}#${tag.id}` : tag.className
        ? `${tag.name}.${tag.className.split(/\s+/)[0]}`
        : tag.name;
      stack.push({ name: tag.name, label });
      if (tag.id) paths.set(tag.id, stack.map(entry => entry.label).join(" > "));
      continue;
    }
    if (stack.length === 0) {
      faults.push(`line ${tag.line}: </${tag.name}> with nothing open`);
      continue;
    }
    if (stack[stack.length - 1].name !== tag.name) {
      faults.push(`line ${tag.line}: </${tag.name}> closes <${stack[stack.length - 1].name}>`);
    }
    stack.pop();
  }
  return { paths, faults, open: stack.map(entry => entry.label) };
};

test("every tag on the page is closed by the tag that opened it", () => {
  const { faults, open } = ancestry();
  assert.deepEqual(faults, [], "the page's tags do not nest");
  assert.deepEqual(open, [], "the page ends with elements still open");
});

test("the panels are where the stylesheet and the script expect them", () => {
  // Three placements the page depends on, and each of them is what a stray
  // close moves. The settings panel is a modal drawn from the rail; the keypad
  // screen is absolutely positioned over the game screen, so it has to be
  // inside the wrapper that is its containing block; and the keypad's own
  // buttons are in the container `initInput` reads regions out of.
  const { paths } = ancestry();
  const under = (id, ancestor) => {
    const path = paths.get(id);
    assert.ok(path, `the page has no #${id}`);
    assert.ok(path.includes(ancestor), `#${id} is not inside ${ancestor}: ${path}`);
  };
  under("settings-panel", "aside.rail");
  under("keypad-arrange", "div.canvas-wrapper");
  under("canvas", "div.canvas-wrapper");
  // Opts is the band's first column rather than a corner positioned over the
  // container's padding, which is what makes the band a row of cells at all.
  under("settings-toggle", "div.keypad-band");
  // The keypad screen holds all three of its halves, which is the whole of the
  // reason it is one screen.
  for (const id of ["keypad-shape-panel", "keypad-size-list", "keypad-arrange-keys", "keypad-arrange-reset"]) {
    under(id, "div#keypad-arrange");
  }
  // And the way in is in the settings panel rather than on the pad, where the
  // spare button belongs to the handset's menu key — as is the second shape
  // list, which is the one control of the three worth reaching without opening
  // anything.
  under("keypad-arrange-open", "div#settings-panel");
  under("keypad-shape-settings", "div#settings-panel");
});

test("no id is used twice", () => {
  // `getElementById` answers the first, so a duplicate is a control the script
  // reaches for and never finds again.
  const seen = new Map();
  for (const tag of scan()) {
    if (tag.closing || !tag.id) continue;
    assert.ok(!seen.has(tag.id), `#${tag.id} is on line ${seen.get(tag.id)} and line ${tag.line}`);
    seen.set(tag.id, tag.line);
  }
});
