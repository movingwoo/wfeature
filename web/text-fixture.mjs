// The page's text, for a test. In a browser the server reads text.properties
// and hands the page the table; a test has no server, so this reads the same
// file the same way and installs it, and a test that imports this module sees
// the sentences a player would. text.test.mjs holds this reader and the
// server's (internal/webhost/text.go) to one set of cases, so the two read the
// file alike.

import { readFileSync } from "node:fs";
import { useText } from "./text.js";

const keyPattern = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
// Spaces, tabs and a CRLF file's carriage return; see lineSpace in text.go.
const trim = text => text.replace(/^[ \t\r]+|[ \t\r]+$/g, "");

export const parseText = source => {
  const table = {};
  source.replace(/^\uFEFF/, "").split("\n").forEach((raw, index) => {
    const line = trim(raw);
    if (line === "" || line.startsWith("#")) return;
    const at = line.indexOf("=");
    if (at < 0) throw new Error(`text.properties:${index + 1}: ${JSON.stringify(line)} is neither a comment nor key = value`);
    const key = trim(line.slice(0, at));
    if (!keyPattern.test(key)) throw new Error(`text.properties:${index + 1}: ${JSON.stringify(key)} is not a key`);
    if (Object.hasOwn(table, key)) throw new Error(`text.properties:${index + 1}: ${key} is written twice`);
    table[key] = trim(line.slice(at + 1));
  });
  return table;
};

export const textSource = readFileSync(new URL("./text.properties", import.meta.url), "utf8");
export const pageText = parseText(textSource);

useText(pageText);
