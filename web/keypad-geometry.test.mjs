import assert from "node:assert/strict";
import { test } from "node:test";
import { groupGeometry, containsPoint } from "./keypad-geometry.js";

test("a rectangle fills internal seams and centers its label", () => {
  const geometry = groupGeometry(["r2c2", "r2c3", "r3c2", "r3c3"], 40, 28, 4);
  assert.equal(geometry.width, 84);
  assert.equal(geometry.height, 60);
  assert.equal(geometry.left, 44);
  assert.equal(geometry.top, 32);
  for (const [x, y] of [
    [41, 10],
    [10, 30],
    [41, 30],
    [83, 59],
  ])
    assert.ok(containsPoint(geometry, x, y));
  assert.deepEqual(geometry.label, { x: 0, y: 0, width: 84, height: 60 });
  assert.ok(!containsPoint(geometry, 85, 10));
});

test("an L shape excludes its missing corner and retains every joined seam", () => {
  const geometry = groupGeometry(["r1c1", "r2c1", "r2c2"], 40, 28, 4);
  assert.ok(containsPoint(geometry, 20, 30));
  assert.ok(containsPoint(geometry, 42, 45));
  assert.ok(!containsPoint(geometry, 60, 14));
  assert.ok(
    containsPoint(
      geometry,
      geometry.label.x + geometry.label.width / 2,
      geometry.label.y + geometry.label.height / 2,
    ),
  );
});

test("a ring leaves its hole available to another button", () => {
  const cells = [
    "r1c1",
    "r1c2",
    "r1c3",
    "r2c1",
    "r2c3",
    "r3c1",
    "r3c2",
    "r3c3",
  ];
  const geometry = groupGeometry(cells, 40, 28, 4);
  assert.ok(!containsPoint(geometry, 64, 46));
  for (const [x, y] of [
    [20, 14],
    [64, 14],
    [108, 46],
    [64, 78],
  ])
    assert.ok(containsPoint(geometry, x, y));
  assert.ok(
    containsPoint(
      geometry,
      geometry.label.x + geometry.label.width / 2,
      geometry.label.y + geometry.label.height / 2,
    ),
  );
  assert.ok(geometry.outline.length > 0);
});

test("fractions and short-window cells keep shape membership", () => {
  const geometry = groupGeometry(["r4c4", "r4c5", "r5c4"], 37.71, 19.5, 4);
  assert.equal(geometry.height, 43);
  assert.ok(containsPoint(geometry, 10, 21));
  assert.ok(!containsPoint(geometry, 60, 35));
});
