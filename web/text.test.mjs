import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { pageText, parseText } from "./text-fixture.mjs";
import { phrase, readPageText, useText } from "./text.js";

// Everything a player reads is in text.properties, and these are the checks
// that keep it so: the file is read here the way the server reads it, every
// key the page or the server asks for is written in it, everything written in
// it is asked for, and no sentence is written anywhere else.

const here = new URL(".", import.meta.url);
const read = name => readFileSync(new URL(name, here), "utf8");
const cases = JSON.parse(read("testdata/text-format.json"));

test("the file is read and filled the way the server reads and fills it", () => {
  for (const { source, table } of cases.parsed) {
    assert.deepEqual(parseText(source), table, JSON.stringify(source));
  }
  for (const source of cases.refused) {
    assert.throws(() => parseText(source), undefined, JSON.stringify(source));
  }
  try {
    for (const { sentence, values, want } of cases.phrased) {
      useText({ sentence });
      assert.equal(phrase("sentence", values), want, JSON.stringify({ sentence, values }));
    }
  } finally {
    useText(pageText);
  }
});

test("a refused line is named by its number", () => {
  assert.throws(() => parseText("# a comment\na = 1\nno separator here\n"), /text\.properties:3:/);
});

test("a missing sentence shows as its key and a page without a table as keys", () => {
  assert.equal(phrase("absent.key"), "absent.key");
  assert.equal(phrase("absent.key", { name: "x" }), "absent.key");
  const page = text => ({ getElementById: id => (id === "page-text" ? { textContent: text } : null) });
  assert.deepEqual(readPageText(page('{"a":"1"}')), { a: "1" });
  assert.deepEqual(readPageText(page("not json")), {});
  assert.deepEqual(readPageText(page("[1]")), {});
  assert.deepEqual(readPageText({ getElementById: () => null }), {});
  assert.deepEqual(readPageText(undefined), {});
});

// The scripts the page loads, which is every .js beside it; tests and the
// acceptance routes are not the page.
const scripts = readdirSync(here).filter(name => name.endsWith(".js")).map(name => [name, read(name)]);
const markup = read("index.html");
const withoutHTMLComments = markup.replace(/<!--[\s\S]*?-->/g, "");

// The string literals of a script, with its comments left out. A small lexer
// rather than a parser: strings, template literals, comments and regular
// expression literals are all it has to tell apart for this. The parts of a
// template between its ${} are strings of their own; a regular expression's
// text is counted with them, since a sentence could hide in one too.
const scriptStrings = source => {
  const strings = [];
  const templates = [];
  let depth = 0;
  let last = "";
  let word = "";
  const regexAfter = new Set([..."(,=:[!&|?{};+-*%<>~^"]);
  const regexAfterWord = new Set(["return", "typeof", "case", "do", "else", "in", "of", "new", "delete", "void", "throw", "yield", "await"]);
  const quoted = (from, ends) => {
    let at = from;
    let value = "";
    while (at < source.length && !ends(at)) {
      if (source[at] === "\\") {
        value += source.slice(at, at + 2);
        at += 2;
      } else {
        value += source[at++];
      }
    }
    return [value, at];
  };
  let at = 0;
  while (at < source.length) {
    const c = source[at];
    const next = source[at + 1];
    if (c === "/" && next === "/") {
      const end = source.indexOf("\n", at);
      at = end < 0 ? source.length : end;
      continue;
    }
    if (c === "/" && next === "*") {
      at = source.indexOf("*/", at + 2) + 2;
      continue;
    }
    if (c === '"' || c === "'") {
      const [value, end] = quoted(at + 1, position => source[position] === c);
      strings.push(value);
      at = end + 1;
      last = c;
      word = "";
      continue;
    }
    if (c === "`" || (c === "}" && templates.length > 0 && templates.at(-1) === depth)) {
      if (c === "}") templates.pop();
      const [value, end] = quoted(at + 1, position => source[position] === "`" || (source[position] === "$" && source[position + 1] === "{"));
      strings.push(value);
      if (source[end] === "`") {
        at = end + 1;
        last = "`";
      } else {
        templates.push(depth);
        at = end + 2;
        last = "{";
      }
      word = "";
      continue;
    }
    if (c === "/" && (last === "" || regexAfter.has(last) || regexAfterWord.has(word))) {
      let end = at + 1;
      let inClass = false;
      while (end < source.length && source[end] !== "\n") {
        if (source[end] === "\\") {
          end += 2;
          continue;
        }
        if (source[end] === "[") inClass = true;
        else if (source[end] === "]") inClass = false;
        else if (source[end] === "/" && !inClass) break;
        end++;
      }
      strings.push(source.slice(at + 1, end));
      at = end + 1;
      while (/[a-z]/.test(source[at] ?? "")) at++;
      last = "/";
      word = "";
      continue;
    }
    if (c === "{") depth++;
    else if (c === "}") depth--;
    if (/\s/.test(c)) {
      at++;
      continue;
    }
    if (/[\w$]/.test(c)) {
      let end = at;
      while (end < source.length && /[\w$]/.test(source[end])) end++;
      word = source.slice(at, end);
      last = "a";
      at = end;
      continue;
    }
    last = c;
    word = "";
    at++;
  }
  return strings;
};

const hangul = /\p{Script=Hangul}/u;
const sections = new Set(Object.keys(pageText).map(key => key.split(".")[0]));
const keyShaped = /^([a-z][a-z-]*)\.[A-Za-z0-9._-]+$/;
const placeholders = [...markup.matchAll(/\{\{([^{}]*)\}\}/g)].map(match => match[1]);

test("every sentence the page asks for is written", () => {
  for (const name of placeholders) {
    if (name === "@table") continue;
    assert.ok(Object.hasOwn(pageText, name), `index.html names {{${name}}}, which text.properties does not have`);
  }
  assert.equal(placeholders.filter(name => name === "@table").length, 1, "index.html carries the table once");
  assert.equal((markup.match(/\{\{/g) ?? []).length, placeholders.length, "every {{ in index.html is a placeholder");
  let asked = 0;
  for (const [name, source] of scripts) {
    for (const literal of scriptStrings(source)) {
      const match = keyShaped.exec(literal);
      if (!match || !sections.has(match[1])) continue;
      asked++;
      assert.ok(Object.hasOwn(pageText, literal), `${name} asks for ${literal}, which text.properties does not have`);
    }
  }
  assert.ok(asked > 100, `found only ${asked} keys in the page's scripts; the scan is not reading them`);
});

// The server's own answers are in the file too; its sources are read as text
// here only to see which keys they name, and the Go tests hold those to the
// file the other way round.
const serverRoot = new URL("../internal/webhost/", import.meta.url);
const server = readdirSync(serverRoot)
  .filter(name => name.endsWith(".go") && !name.endsWith("_test.go"))
  .map(name => readFileSync(new URL(name, serverRoot), "utf8").split("\n").filter(line => !line.trim().startsWith("//")).join("\n"))
  .join("\n");

test("everything written in the file is asked for", () => {
  const used = new Set(placeholders);
  for (const [, source] of scripts) for (const literal of scriptStrings(source)) used.add(literal);
  for (const match of server.matchAll(/"([^"\\\n]*)"/g)) used.add(match[1]);
  const unused = Object.keys(pageText).filter(key => !used.has(key));
  assert.deepEqual(unused, [], "text.properties has sentences nothing shows");
});

test("no sentence is written anywhere but the file", () => {
  for (const [name, source] of scripts) {
    for (const literal of scriptStrings(source)) {
      assert.doesNotMatch(literal, hangul, `${name} writes ${JSON.stringify(literal)}; it belongs in text.properties`);
    }
  }
  for (const line of withoutHTMLComments.split("\n")) {
    assert.doesNotMatch(line, hangul, `index.html writes ${JSON.stringify(line.trim())}; it belongs in text.properties`);
  }
  const style = read("style.css").replace(/\/\*[\s\S]*?\*\//g, "");
  assert.doesNotMatch(style, hangul, "style.css writes a sentence; it belongs in text.properties");
});

// The lexer is what the two checks above stand on, so it is held to the
// shapes the page's scripts use.
test("the scan reads strings and skips comments and regular expressions", () => {
  const source = [
    'const a = "one"; // "not this"',
    "/* 'nor this' */ const b = 'two';",
    "const c = `three ${d ? \"four\" : `five ${\"six\"}`} seven`;",
    'const e = /["\'`]/.test(f) ? 1 / 2 : g / h; const i = "eight";',
    "if (x) return /[/]/.exec(y);",
  ].join("\n");
  assert.deepEqual(scriptStrings(source), ["one", "two", "three ", "four", "five ", "six", "", " seven", "[\"'`]", "eight", "[/]"]);
});
