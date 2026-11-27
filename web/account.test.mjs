import assert from "node:assert/strict";
import { test } from "node:test";
import { accountOwnerKey, initAccounts } from "./account.js";
import { browserTokenKey } from "./session-link.js";
import { createSafeStorage } from "./storage.js";
import { phrase } from "./text.js";
import "./text-fixture.mjs";

const playerA = { id: "id-a", login: "first" };
const playerB = { id: "id-b", login: "second" };
const json = user => Response.json({ user });
const later = () => {
  let finish;
  const response = new Promise(resolve => { finish = resolve; });
  return { response, finish };
};
const target = () => {
  const listeners = {};
  return { value: "", textContent: "",
    focus() { this.focused = true; },
    addEventListener: (type, callback) => { listeners[type] = callback; },
    fire: (type, event = {}) => listeners[type]({ preventDefault() {}, ...event }),
  };
};
const fixture = (protocol = "https:") => {
  const nodes = Object.fromEntries(["form", "id", "password", "name", "message", "logout"]
    .map(name => ["account-" + name, target()]));
  const document = { ...target(), documentElement: { dataset: {} }, getElementById: id => nodes[id] };
  nodes["account-form"].elements = [nodes["account-id"], nodes["account-password"], target(), target()];
  const localStore = createSafeStorage(() => null), sessionStore = createSafeStorage(() => null);
  let reloads = 0, locks = 0;
  const window = { ...target(), location: { protocol, reload: () => { reloads++; } } };
  const requests = [];
  let respond = () => json(null);
  const accounts = initAccounts({ document, window, localStore, sessionStore,
    fetch: (url, options) => {
      const path = new URL(url, "https://example.test/").pathname;
      assert.equal(options.cache, "no-store");
      requests.push({ path, ...options });
      return respond(path);
    },
    onLocked: () => { locks++; },
  });
  return { ...accounts, document, window, localStore, sessionStore, requests, nodes,
    respond: answer => { respond = answer; }, reloads: () => reloads, locks: () => locks,
    submit: value => nodes["account-form"].fire("submit", { submitter: { value } }),
    auth: () => document.documentElement.dataset.auth,
  };
};
const seed = page => {
  page.localStore.setItem("wfeature:lastGame", "old-choice");
  page.localStore.setItem(browserTokenKey, "old-token");
  page.sessionStore.setItem("wfeature.resume-token", "old-tab-token");
};
const forgotten = page => {
  assert.equal(page.localStore.getItem("wfeature:lastGame"), null);
  assert.equal(page.localStore.getItem(browserTokenKey), null);
  assert.equal(page.sessionStore.getItem("wfeature.resume-token"), null);
};

test("empty account fields show the catalog message and focus the field before sending", async () => {
  for (const action of ["login", "register"]) {
    const page = fixture();
    page.nodes["account-id"].value = "  ";
    page.nodes["account-password"].value = "keep this password";
    await page.submit(action);
    assert.equal(page.nodes["account-message"].textContent, phrase("account.id-required"));
    assert.ok(page.nodes["account-id"].focused);
    assert.equal(page.nodes["account-password"].value, "keep this password");
    page.nodes["account-id"].value = "someone";
    page.nodes["account-password"].value = "";
    await page.submit(action);
    assert.equal(page.nodes["account-message"].textContent, phrase("account.password-required"));
    assert.ok(page.nodes["account-password"].focused);
    assert.equal(page.requests.length, 0);
  }
});

test("only a verified owner unlocks the picker, retaining state across checks by the same player", async () => {
  const page = fixture();
  seed(page);
  page.respond(() => json(playerA));
  assert.equal(await page.check(), true);
  assert.equal(page.auth(), "player");
  assert.equal(page.nodes["account-name"].textContent, "first");
  assert.equal(page.localStore.getItem(accountOwnerKey), playerA.id);
  forgotten(page);
  seed(page);
  assert.equal(await page.check(), true);
  assert.equal(page.localStore.getItem(browserTokenKey), "old-token");
  assert.equal(page.reloads(), 0);

  page.respond(() => json(playerB));
  assert.equal(await page.check(), false);
  assert.notEqual(page.auth(), "player");
  assert.equal(page.nodes["account-name"].textContent, "");
  assert.equal(page.localStore.getItem(accountOwnerKey), playerB.id);
  forgotten(page);
  assert.equal(page.reloads(), 1);
});

test("login and registration send only credentials and start a fresh page after success", async () => {
  for (const action of ["login", "register"]) {
    const page = fixture();
    assert.equal(await page.check(), false);
    page.nodes["account-id"].value = "second";
    page.nodes["account-password"].value = "password for fixture";
    page.respond(() => new Response("Server declined the credentials", { status: 400,
      headers: { "Content-Type": "text/plain" } }));
    await page.submit(action);
    assert.equal(page.nodes["account-message"].textContent, "Server declined the credentials");
    assert.equal(page.reloads(), 0);
    assert.equal(page.nodes["account-password"].value, action === "register" ? "password for fixture" : "");

    seed(page);
    page.nodes["account-password"].value = "new password";
    page.respond(() => json(playerB));
    await page.submit(action);
    const request = page.requests.at(-1);
    assert.equal(request.path, "/api/" + action);
    assert.equal(request.method, "POST");
    assert.deepEqual(JSON.parse(request.body), { id: "second", password: "new password" });
    assert.equal(page.nodes["account-password"].value, "");
    assert.equal(page.localStore.getItem(accountOwnerKey), playerB.id);
    forgotten(page);
    assert.equal(page.reloads(), 1);
  }
});

test("a lost session asks the previous player to sign in again", async () => {
  const page = fixture();
  page.respond(() => json(playerA));
  assert.equal(await page.check(), true);
  page.respond(() => json(null));
  assert.equal(await page.check(), false);
  assert.equal(page.nodes["account-message"].textContent, phrase("account.invalid-login"));
});

test("an HTTP entry explains HTTPS and cannot send credentials", async () => {
  const page = fixture("http:");
  assert.equal(await page.check(), false);
  assert.match(page.nodes["account-message"].textContent, /HTTPS/);
  assert.ok(page.nodes["account-form"].elements.every(control => control.disabled));
  await page.submit("login");
  await page.submit("register");
  assert.equal(page.requests.length, 0);
});

test("logout locks immediately and an earlier account check cannot restore the old game", async () => {
  const page = fixture();
  page.respond(() => json(playerA));
  await page.check();
  seed(page);
  const lookup = later(), revoke = later();
  page.respond(path => path === "/api/logout" ? revoke.response : lookup.response);
  const checking = page.check();
  const signingOut = page.nodes["account-logout"].fire("click");
  assert.notEqual(page.auth(), "player");
  assert.equal(page.nodes["account-name"].textContent, "");
  assert.ok(page.locks() > 0);
  assert.equal(page.requests.at(-1).path, "/api/logout");
  assert.equal(page.requests.at(-1).method, "POST");
  revoke.finish(json(null));
  await signingOut;
  lookup.finish(json(playerA));
  assert.equal(await checking, false);
  assert.notEqual(page.auth(), "player");
  assert.equal(page.localStore.getItem(accountOwnerKey), null);
  forgotten(page);
  assert.equal(page.reloads(), 1);
});

test("another tab's account change hides the old picker even while offline", async () => {
  const page = fixture();
  page.respond(() => json(playerA));
  await page.check();
  seed(page);
  const lookup = later();
  page.respond(() => lookup.response);
  const previous = page.check();
  page.localStore.setItem(accountOwnerKey, playerB.id);
  page.respond(() => { throw new Error("offline"); });
  const observed = page.window.fire("storage", { key: accountOwnerKey, newValue: playerB.id });
  assert.notEqual(page.auth(), "player");
  assert.equal(page.nodes["account-name"].textContent, "");
  await observed;
  lookup.finish(json(playerA));
  assert.equal(await previous, false);
  assert.notEqual(page.auth(), "player");
  assert.equal(page.localStore.getItem(accountOwnerKey), playerB.id);
  assert.equal(page.reloads(), 0);
  page.respond(() => json(playerB));
  await page.window.fire("online");
  assert.equal(page.reloads(), 1);
});

test("an offline startup waits for a fresh login check on reconnection", async () => {
  const page = fixture();
  page.respond(() => { throw new Error("offline"); });
  assert.equal(await page.check(), false);
  assert.notEqual(page.auth(), "player");
  assert.equal(page.reloads(), 0);
  page.respond(() => json(playerA));
  await page.window.fire("online");
  assert.equal(page.reloads(), 1);
});
