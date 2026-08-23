import assert from "node:assert/strict";
import { test } from "node:test";

import { capturing, recordEvent, saveReport, stopLogCapture, subscribeLog } from "./debug-log.js";

// The module wraps the console at load and keeps what the page writes, which
// is the half of a run the server cannot see. A release is the same page with
// none of that: what these check is that stopping is a real stop — the console
// is the page's own again, the buffer is empty, and nothing is posted anywhere
// afterwards — because "the release collects nothing" is otherwise a claim
// only the missing button makes.

test("what the page logs is retained while it is collecting", () => {
  const seen = [];
  const unsubscribe = subscribeLog(line => seen.push(line));
  recordEvent("server session opened");
  console.log("a line the page wrote");
  unsubscribe();

  assert.ok(capturing());
  assert.ok(seen.some(line => line?.includes("EVENT server session opened")));
  assert.ok(seen.some(line => line?.includes("LOG a line the page wrote")));
});

test("a release stops the capture, empties the buffer, and posts nothing", async () => {
  // Whoever the console belongs to after the module wrapped it, stopping has
  // to hand back the function that was there before.
  const wrapped = console.log;
  const replayed = [];

  stopLogCapture();
  assert.equal(capturing(), false);
  assert.notEqual(console.log, wrapped, "the console kept the capture's wrapper");

  recordEvent("this happened after the stop");
  console.log("");
  const unsubscribe = subscribeLog(line => replayed.push(line));
  unsubscribe();
  assert.deepEqual(replayed, [], "a subscriber replayed lines a release should not have");

  // No fetch is installed here, so a post would throw rather than quietly
  // reach a server: the null is the refusal, not a swallowed failure.
  assert.equal(await saveReport("some game"), null);
});
