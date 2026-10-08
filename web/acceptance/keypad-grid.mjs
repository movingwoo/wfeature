// Browser acceptance for grid geometry, editing, migration and the real input path.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import {
  mkdirSync,
  copyFileSync,
  writeFileSync,
  rmSync,
  readFileSync,
} from "node:fs";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import net from "node:net";

const [binary, engineName = "chromium"] = process.argv.slice(2);
if (!binary || !process.env.PLAYWRIGHT_MODULE)
  throw new Error(
    "Usage: PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node web/acceptance/keypad-grid.mjs SERVER [chromium|webkit]",
  );
const { [engineName]: engine } = await import(
  pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE))
);
assert.ok(engine && ["chromium", "webkit"].includes(engineName));
const run = `keypad-grid-${engineName}-${Date.now()}`;
const output = resolve("var/acceptance", run),
  games = resolve("var/games", `.${run}`),
  ext = resolve("var/ext", run),
  saves = resolve("var/savedata", run);
for (const path of [output, join(games, "library"), ext, saves])
  mkdirSync(path, { recursive: true });
copyFileSync(
  "internal/platform/skt/testdata/canvas-skt.zip",
  join(games, "library", "canvas.zip"),
);
const reservation = net.createServer();
await new Promise((done) => reservation.listen(0, "127.0.0.1", done));
const port = reservation.address().port;
await new Promise((done) => reservation.close(done));
const origin = `http://127.0.0.1:${port}`;
const server = spawn(resolve(binary), [
  "-addr",
  `127.0.0.1:${port}`,
  "-games",
  games,
  "-ext",
  ext,
  "-saves",
  join(saves, "ktf"),
  "-logs",
  join(output, "logs"),
  "-web",
  join(output, "no-external-web"),
  "-open=false",
]);
let logs = "",
  browser;
server.stdout.on("data", (data) => {
  logs += data;
});
server.stderr.on("data", (data) => {
  logs += data;
});
const pause = (ms) => new Promise((done) => setTimeout(done, ms));
const result = { engine: engineName, checks: [], errors: [], geometry: [] };
const check = (name) => {
  result.checks.push(name);
  console.log(`PASS ${engineName}: ${name}`);
};
try {
  for (let attempt = 0; ; attempt++) {
    try {
      if ((await fetch(`${origin}/api/status`)).ok) break;
    } catch {}
    if (attempt === 200 || server.exitCode !== null)
      throw new Error("server did not start");
    await pause(50);
  }
  browser = await engine.launch();
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
  });
  await context.addInitScript(() => {
    window.keypadMessages = [];
    const send = WebSocket.prototype.send;
    WebSocket.prototype.send = function (data) {
      try {
        window.keypadMessages.push(JSON.parse(data));
      } catch {}
      return send.call(this, data);
    };
    document.addEventListener(
      "pointerdown",
      (event) => {
        window.lastKeypadPointer = event.pointerId;
      },
      true,
    );
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on("pageerror", (error) => result.errors.push(error.message));
  const legacy = JSON.stringify({
    type4: {
      "band-c1": "SETTINGS",
      "band-c2": "QUICK_SAVE",
      "pad-r1c1": "5",
      "pad-r1c2": "5",
      "pad-r2c6": "QUICK_LOAD",
      "pad-r4c7": "SOFT2",
    },
  });
  await page.goto(origin);
  await page.evaluate((legacy) => {
    localStorage.setItem("wfeature:keypadLayout", "type4");
    localStorage.setItem("wfeature:keypadKeys", legacy);
    localStorage.setItem(
      "wfeature:keypadSize",
      JSON.stringify({ key: 68, split: 0.3, band: 56 }),
    );
  }, legacy);
  await page.reload();
  const record = () =>
    page.evaluate(() => localStorage.getItem("wfeature:keypadGridV2"));
  await page.locator('[data-cell="r2c1"][data-key="5"]').waitFor();
  assert.equal(
    await page.locator('[data-cell="r1c3"]').getAttribute("data-key"),
    "QUICK_SAVE",
  );
  assert.equal(
    await page.locator('[data-cell="r4c11"]').getAttribute("data-key"),
    "QUICK_LOAD",
  );
  assert.equal(
    await page.evaluate(() => localStorage.getItem("wfeature:keypadKeys")),
    legacy,
  );
  const migrated = await record();
  await page.reload();
  assert.equal(await record(), migrated);
  check("legacy assignments migrate once and old records remain intact");

  const coarse = JSON.stringify({
    version: 1,
    layouts: {
      type4: {
        columns: 7,
        rows: 9,
        groups: Array.from({ length: 63 }, (_, index) => {
          const id = `r${Math.floor(index / 7) + 1}c${(index % 7) + 1}`;
          return {
            id,
            cells: [id],
            key: index === 0 ? "SETTINGS" : index === 62 ? "QUICK_LOAD" : "",
            activation: index === 0 || index === 62 ? "press" : "slide",
          };
        }),
      },
    },
  });
  await page.evaluate((coarse) => {
    localStorage.removeItem("wfeature:keypadGridV2");
    localStorage.setItem("wfeature:keypadGrid", coarse);
  }, coarse);
  await page.reload();
  await page.locator('[data-cell="r9c13"][data-key="QUICK_LOAD"]').waitFor();
  const refined = JSON.parse(await record());
  assert.equal(refined.version, 2);
  assert.deepEqual(
    refined.layouts.type4.groups.find((group) => group.id === "r9c13").cells,
    ["r9c13", "r9c14"],
  );
  assert.equal(
    await page.evaluate(() => localStorage.getItem("wfeature:keypadGrid")),
    coarse,
  );
  check(
    "coarse grid buttons keep paired columns and their old storage remains intact",
  );

  const openEditor = async () => {
    if (
      !(await page
        .locator("#settings-panel")
        .evaluate((e) => e.classList.contains("visible")))
    )
      await page.locator('button[data-key="SETTINGS"]').click();
    await page.locator("#keypad-arrange-open").click();
  };
  const multiple = async (on) => {
    const button = page.locator("#keypad-arrange-multiple");
    if (((await button.getAttribute("aria-pressed")) === "true") !== on)
      await button.click();
  };
  const point = async (id) =>
    page.evaluate((id) => {
      const [, row, column] = /^r(\d+)c(\d+)$/.exec(id);
      const board = document.querySelector("#keypad-grid");
      const r = board.getBoundingClientRect();
      const gap = Number.parseFloat(getComputedStyle(board).columnGap);
      const columns = Number(board.dataset.columns),
        rows = Number(board.dataset.rows);
      const width = (r.width - gap * (columns - 1)) / columns,
        height = (r.height - gap * (rows - 1)) / rows;
      return {
        x: r.x + (+column - 1) * (width + gap) + width / 2,
        y: r.y + (+row - 1) * (height + gap) + height / 2,
      };
    }, id);
  const tap = async (id) => {
    const p = await point(id);
    await page.mouse.click(p.x, p.y);
  };
  const assign = async (id, key) => {
    await multiple(false);
    await tap(id);
    await page
      .locator(`#keypad-arrange-keys button[data-assign="${key}"]`)
      .click();
  };
  const merge = async (cells, key) => {
    await multiple(true);
    await page.locator("#keypad-arrange-unselect").click();
    for (const id of cells) await tap(id);
    if (key)
      await page
        .locator(`#keypad-arrange-keys button[data-assign="${key}"]`)
        .click();
    await page.locator("#keypad-arrange-merge").click();
  };
  await openEditor();
  await page.locator("#keypad-arrange-reset").click();
  assert.equal(await page.locator("#keypad-grid button[data-key]").count(), 1);
  assert.equal(
    await page.locator("#keypad-grid button[data-cell]").count(),
    125,
  );
  await merge(["r3c3", "r3c2", "r2c2"]);
  assert.equal(
    await page.locator("#keypad-arrange-multiple").getAttribute("aria-pressed"),
    "false",
  );
  assert.equal(
    await page.locator('[data-cell="r2c2"]').getAttribute("aria-pressed"),
    "true",
  );
  await page.locator('#keypad-arrange-keys button[data-assign="5"]').click();
  assert.equal(
    await page.locator('[data-cell="r2c2"]').getAttribute("data-key"),
    "5",
  );
  check("merging selects the new button and allows immediate key assignment");
  await merge(["r2c5", "r2c6", "r2c7", "r3c6"], "6");
  await merge(
    ["r5c4", "r5c5", "r5c6", "r6c4", "r6c6", "r7c4", "r7c5", "r7c6"],
    "8",
  );
  await assign("r6c5", "9");
  await multiple(true);
  await page.locator("#keypad-arrange-unselect").click();
  const dragStart = await point("r5c1"),
    dragEnd = await point("r6c2");
  await page.mouse.move(dragStart.x, dragStart.y);
  await page.mouse.down();
  await page.mouse.move(dragEnd.x, dragStart.y, { steps: 5 });
  await page.mouse.move(dragEnd.x, dragEnd.y, { steps: 5 });
  await page.mouse.move(dragStart.x, dragEnd.y, { steps: 5 });
  await page.mouse.up();
  await page.locator('#keypad-arrange-keys button[data-assign="5"]').click();
  await page.locator("#keypad-arrange-merge").click();
  assert.equal(
    await page.locator('[data-cell="r5c1"]').getAttribute("data-key"),
    "5",
  );
  check("tap and drag selection build a rectangle, L, T and occupied ring");

  await assign("r9c1", "1");
  await assign("r9c2", "2");
  await multiple(true);
  await tap("r9c1");
  await tap("r9c2");
  const beforeConflict = await record();
  await page.locator("#keypad-arrange-merge").click();
  assert.equal(await record(), beforeConflict);
  assert.equal(
    await page.locator("#keypad-arrange-multiple").getAttribute("aria-pressed"),
    "true",
  );
  assert.match(
    await page.locator("#keypad-arrange-hint").textContent(),
    /서로 다른 키/,
  );
  await page.locator('#keypad-arrange-keys button[data-assign="3"]').click();
  await page.locator("#keypad-arrange-merge").click();
  await multiple(false);
  await tap("r9c1");
  await page.locator("#keypad-arrange-split").click();
  assert.equal(
    await page.locator('[data-cell="r9c2"]').getAttribute("data-key"),
    null,
  );
  await page.locator("#keypad-arrange-undo").click();
  assert.equal(await page.locator('[data-cell="r9c2"]').count(), 0);
  await tap("r9c1");
  await page.locator("#keypad-arrange-clear").click();
  assert.equal(
    await page.locator('[data-cell="r9c1"]').getAttribute("data-key"),
    null,
  );
  await page.locator("#keypad-arrange-undo").click();
  await merge(["r1c1", "r1c3"]);
  await multiple(false);
  await tap("r1c1");
  assert.ok(await page.locator("#keypad-arrange-clear").isDisabled());
  assert.ok(
    await page
      .locator('#keypad-arrange-keys button[data-assign="5"]')
      .isDisabled(),
  );
  await page.locator("#keypad-arrange-split").click();
  await assign("r9c14", "7");
  await assign("r1c4", "MENU");
  await assign("r1c5", "CLR");
  await assign("r1c6", "QUICK_SAVE");
  await assign("r1c8", "RAPID_FIRE");
  check(
    "conflicting keys require a choice; split, clear, undo and settings protection work",
  );

  const clearMessages = () =>
    page.evaluate(() => {
      window.keypadMessages = [];
    });
  const messages = () =>
    page.evaluate(() =>
      window.keypadMessages.filter((m) =>
        ["key", "pointer", "quickSave", "quickLoad"].includes(m.kind),
      ),
    );
  await clearMessages();
  await tap("r2c2");
  await page.keyboard.press("w");
  assert.deepEqual(await messages(), []);
  await page.locator("#keypad-arrange-close").click();
  const saved = await record();
  await page.reload();
  assert.equal(await record(), saved);
  assert.equal(await page.locator('[data-cell="r3c2"]').count(), 0);
  check("editing sends no input and shapes survive reload");

  // Sample actual hit testing over cells and their seams, including concave
  // corners and the other key inside a ring. The DOM's bounding box is larger.
  const hit = async (id) => {
    const p = await point(id);
    return page.evaluate(
      (p) =>
        document.elementFromPoint(p.x, p.y)?.closest("button[data-key]")
          ?.dataset.key ?? null,
      p,
    );
  };
  assert.equal(await hit("r2c3"), null);
  assert.equal(await hit("r6c5"), "9");
  assert.equal(await hit("r6c4"), "8");
  const labelsFit = async () => {
    const labels = await page.evaluate(() =>
      ["r1c1", "r1c4", "r1c5", "r1c6", "r1c8"].map((id) => {
        const button = document.querySelector(`[data-cell="${id}"]`);
        const label = button.querySelector(".keypad-label");
        const range = document.createRange();
        range.selectNodeContents(label);
        return {
          id,
          text: label.textContent,
          width: label.getBoundingClientRect().width,
          textWidth: range.getBoundingClientRect().width,
          whiteSpace: getComputedStyle(label).whiteSpace,
        };
      }),
    );
    for (const label of labels) {
      assert.equal(label.whiteSpace, "nowrap");
      assert.ok(label.textWidth <= label.width + 0.5, JSON.stringify(label));
    }
  };
  await labelsFit();
  const settled = () =>
    page.waitForFunction(() => {
      const element = document.querySelector("#keypad-grid");
      const board = element.getBoundingClientRect();
      const gap = Number.parseFloat(getComputedStyle(element).columnGap);
      const cell = document
        .querySelector('[data-cell="r6c5"]')
        .getBoundingClientRect();
      return (
        Math.abs(cell.x - (board.x + (4 * (board.width + gap)) / 14)) < 0.1 &&
        Math.abs(cell.y - (board.y + (5 * (board.height + gap)) / 9)) < 0.1
      );
    });
  for (const [width, height] of [
    [240, 320],
    [320, 480],
    [320, 568],
    [390, 844],
    [390, 700],
    [768, 1024],
    [844, 390],
    [1280, 900],
    [1280, 390],
  ]) {
    await page.setViewportSize({ width, height });
    await settled();
    const geometry = await page.evaluate(() => {
      const box = (selector) => {
        const r = document.querySelector(selector).getBoundingClientRect();
        return {
          x: r.x,
          y: r.y,
          width: r.width,
          height: r.height,
          bottom: r.bottom,
        };
      };
      return {
        canvas: box(".canvas-wrapper"),
        grid: box("#keypad-grid"),
        settings: box('button[data-key="SETTINGS"]'),
        edge: box('[data-cell="r9c14"]'),
        columns: Number(document.querySelector("#keypad-grid").dataset.columns),
        scroll: document.documentElement.scrollHeight,
        scrollWidth: document.documentElement.scrollWidth,
      };
    });
    result.geometry.push({ width, height, ...geometry });
    assert.ok(
      Math.abs(geometry.grid.bottom - height) < 1,
      JSON.stringify({ width, height, geometry }),
    );
    assert.ok(Math.abs(geometry.grid.y - geometry.canvas.bottom) < 1);
    assert.ok(Math.abs(geometry.grid.width - Math.min(width, 480)) < 1);
    assert.ok(geometry.grid.width >= geometry.canvas.width);
    assert.ok(
      Math.abs(
        geometry.edge.x +
          geometry.edge.width -
          geometry.grid.x -
          geometry.grid.width,
      ) < 1,
    );
    assert.ok(Math.abs(geometry.edge.bottom - geometry.grid.bottom) < 1);
    assert.ok(
      geometry.scroll <= height + 1 && geometry.scrollWidth <= width + 1,
      JSON.stringify({ width, height, geometry }),
    );
    assert.equal(geometry.columns, 14);
    assert.ok(geometry.settings.width > 0 && geometry.settings.height > 0);
    assert.equal(await hit("r2c3"), null);
    assert.equal(await hit("r6c5"), "9");
    assert.equal(await hit("r9c14"), "7");
    await labelsFit();
    assert.equal(await record(), saved);
    if (width === 240) {
      for (const id of ["game-select", "save-import"]) {
        const control = page.locator(`#${id}`);
        await control.scrollIntoViewIfNeeded();
        const bounds = await control.boundingBox();
        assert.ok(bounds.y >= geometry.canvas.y);
        assert.ok(bounds.y + bounds.height <= geometry.canvas.bottom + 1);
      }
      check(
        "short-screen menus scroll to their first and last controls inside the game area",
      );
    }
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => {
    document.documentElement.style.setProperty("--safe-top", "24px");
    document.documentElement.style.setProperty("--safe-bottom", "34px");
    document.documentElement.style.setProperty("--safe-left", "25px");
    document.documentElement.style.setProperty("--safe-right", "11px");
  });
  await settled();
  const safe = await page.evaluate(() => ({
    top: document.querySelector(".canvas-wrapper").getBoundingClientRect().top,
    left: document.querySelector("#keypad-grid").getBoundingClientRect().left,
    right: document.querySelector("#keypad-grid").getBoundingClientRect().right,
    bottom: document.querySelector("#keypad-grid").getBoundingClientRect()
      .bottom,
  }));
  assert.ok(Math.abs(safe.top - 24) < 1);
  assert.ok(Math.abs(safe.left - 25) < 1);
  assert.ok(Math.abs(safe.right - (390 - 11)) < 1);
  assert.ok(Math.abs(safe.bottom - (844 - 34)) < 1);
  assert.equal(await hit("r9c14"), "7");
  await labelsFit();
  assert.equal(await record(), saved);
  await page.evaluate(() => {
    for (const edge of ["top", "bottom", "left", "right"])
      document.documentElement.style.removeProperty(`--safe-${edge}`);
  });
  await settled();
  check(
    "rotations, short windows and safe-area budgets preserve coordinates and hit areas",
  );

  await page.locator("#game-start").click();
  await page.waitForFunction(
    () => !document.querySelector("#restart").classList.contains("hidden"),
  );
  await pause(300);
  // Rapid-fire mode changes are available only while a game is running.
  for (const mode of ["manual", "auto", "off"]) {
    await page.locator('[data-cell="r1c8"]').click();
    assert.equal(
      await page.locator('[data-cell="r1c8"] .keypad-label').textContent(),
      `연사 ${mode}`,
    );
    await labelsFit();
  }
  check(
    "two-character, long and changing single-cell labels fit without wrapping",
  );
  await clearMessages();
  const p1 = await point("r2c2"),
    p2 = await point("r3c2"),
    p3 = await point("r3c3"),
    missing = await point("r2c3"),
    other = await point("r2c5");
  await page.mouse.move(p1.x, p1.y);
  await page.mouse.down();
  await page.mouse.move(p2.x, p2.y, { steps: 5 });
  await page.mouse.move(p3.x, p3.y, { steps: 5 });
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [["press", 53]],
  );
  await page.mouse.move(missing.x, missing.y, { steps: 5 });
  await page.mouse.move(other.x, other.y, { steps: 5 });
  await page.mouse.up();
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [
      ["press", 53],
      ["release", 53],
      ["press", 54],
      ["release", 54],
    ],
  );
  await clearMessages();
  const hole = await point("r6c5");
  await page.touchscreen.tap(hole.x, hole.y);
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [
      ["press", 57],
      ["release", 57],
    ],
  );
  check(
    "merged seams keep one press; concave gaps release and a ring hole presses its own key",
  );

  await clearMessages();
  await page.keyboard.down("w");
  await page.mouse.move(p1.x, p1.y);
  await page.mouse.down();
  await page.keyboard.up("w");
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [["press", 53]],
  );
  assert.equal(
    await page.locator('#keypad-grid button[data-key="5"].pressed').count(),
    2,
  );
  await page.mouse.up();
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [
      ["press", 53],
      ["release", 53],
    ],
  );
  await clearMessages();
  await page.locator('[data-cell="r2c2"]').focus();
  await page.keyboard.press("Enter");
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [
      ["press", 53],
      ["release", 53],
    ],
  );
  await clearMessages();
  await page.mouse.move(p1.x, p1.y);
  await page.mouse.down();
  await page.evaluate(() =>
    window.dispatchEvent(
      new PointerEvent("pointercancel", {
        pointerId: window.lastKeypadPointer,
      }),
    ),
  );
  await page.mouse.up();
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [
      ["press", 53],
      ["release", 53],
    ],
  );
  await clearMessages();
  await page.mouse.down();
  await page.evaluate(() => window.dispatchEvent(new Event("blur")));
  await page.mouse.up();
  assert.deepEqual(
    (await messages()).map((m) => [m.action, m.code]),
    [
      ["press", 53],
      ["release", 53],
    ],
  );
  check(
    "keyboard/pointer ownership, duplicate feedback, focused activation and cancellation release correctly",
  );

  if (engineName === "chromium") {
    const devtools = await context.newCDPSession(page),
      second = await point("r5c1");
    await clearMessages();
    await devtools.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [
        { ...p1, id: 1 },
        { ...second, id: 2 },
      ],
    });
    await devtools.send("Input.dispatchTouchEvent", {
      type: "touchMove",
      touchPoints: [
        { ...p2, id: 1 },
        { ...second, id: 2 },
      ],
    });
    await devtools.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [{ ...second, id: 2 }],
    });
    assert.deepEqual(
      (await messages()).map((m) => [m.action, m.code]),
      [["press", 53]],
    );
    await devtools.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
    assert.deepEqual(
      (await messages()).map((m) => [m.action, m.code]),
      [
        ["press", 53],
        ["release", 53],
      ],
    );
    check(
      "two browser touch contacts share one guest hold across merged buttons",
    );
    await devtools.detach();
  }
  await openEditor();
  await assign("r8c6", "QUICK_SAVE");
  await assign("r8c7", "QUICK_LOAD");
  await page.locator("#keypad-arrange-close").click();
  await clearMessages();
  await page.mouse.move(p1.x, p1.y);
  await page.mouse.down();
  const quick = await point("r8c6");
  await page.mouse.move(quick.x, quick.y, { steps: 15 });
  await page.mouse.up();
  assert.ok(
    !(await messages()).some(
      (m) => m.kind === "quickSave" || m.kind === "quickLoad",
    ),
  );
  check("sliding across local checkpoint controls never activates them");
  await page.screenshot({ path: join(output, "playing.png") });
  await openEditor();
  await page.screenshot({ path: join(output, "editor.png") });

  const broken = await browser.newContext({
    viewport: { width: 390, height: 844 },
  });
  await broken.addInitScript(() => {
    Storage.prototype.setItem = function () {
      throw new DOMException("full", "QuotaExceededError");
    };
  });
  const blocked = await broken.newPage();
  blocked.on("pageerror", (error) => result.errors.push(error.message));
  await blocked.goto(origin);
  await blocked.locator('button[data-key="SETTINGS"]').click();
  await blocked.locator("#keypad-arrange-open").click();
  await blocked.locator('[data-cell="r1c3"]').click();
  await blocked.locator('#keypad-arrange-keys button[data-assign="5"]').click();
  assert.equal(
    await blocked.locator('[data-cell="r1c3"]').getAttribute("data-key"),
    "5",
  );
  assert.ok(await blocked.locator("#keypad-arrange-storage").isVisible());
  await broken.close();
  check(
    "failed browser writes retain the current edit and report its lifetime",
  );
  // A new page must receive an entirely cached module graph when disconnected.
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
  const workerSource = await (
    await fetch(`${origin}/service-worker.js`)
  ).text();
  const cacheName = workerSource.match(/const cacheName = "([^"]+)"/)[1];
  const shell = await page.evaluate(async (name) => {
    if (!(await caches.keys()).includes(name))
      throw new Error("Current shell cache is missing");
    return {
      name,
      urls: (await (await caches.open(name)).keys()).map(
        (request) => new URL(request.url).pathname,
      ),
    };
  }, cacheName);
  for (const file of [
    "/keypad-editor.js",
    "/keypad-layout.js",
    "/keypad-geometry.js",
  ])
    assert.ok(shell.urls.includes(file));
  assert.ok(!shell.urls.includes("/keypad-size.js"));
  await new Promise((done) => {
    server.once("exit", done);
    server.kill("SIGTERM");
  });
  await page.reload();
  await page.locator('#keypad-grid button[data-key="SETTINGS"]').waitFor();
  assert.equal(await page.locator('[data-cell="r3c2"]').count(), 0);
  check("the new shell and persisted merged shapes load offline");

  assert.deepEqual(result.errors, []);
  result.ok = true;
} catch (error) {
  result.failure = error.stack;
  throw error;
} finally {
  await browser?.close();
  if (server.exitCode === null)
    await new Promise((done) => {
      server.once("exit", done);
      server.kill("SIGTERM");
    });
  writeFileSync(join(output, "server.log"), logs);
  writeFileSync(join(output, "result.json"), JSON.stringify(result, null, 2));
  rmSync(games, { recursive: true, force: true });
  rmSync(ext, { recursive: true, force: true });
  console.log(`Report: ${join(output, "result.json")}`);
}
