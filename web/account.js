import { reach } from "./reach.js";
import { browserTokenKey } from "./session-link.js";
import { phrase } from "./text.js";

// This marker coordinates browser state across tabs. The server's HttpOnly
// cookie identifies the login for every account check and protected request.
export const accountOwnerKey = "wfeature.account-owner";

export const initAccounts = ({ document, window, localStore, sessionStore, fetch, onLocked }) => {
  const root = document.documentElement;
  const form = document.getElementById("account-form");
  const loginID = document.getElementById("account-id");
  const password = document.getElementById("account-password");
  const name = document.getElementById("account-name");
  const message = document.getElementById("account-message");
  const secure = window.location.protocol === "https:";
  let player = null;
  let checked = false;
  let checking = null;
  let posting = false;
  let reloading = false;
  let generation = 0;

  const lock = (reason = "") => {
    root.dataset.auth = "locked";
    name.textContent = "";
    message.textContent = reason;
    onLocked();
  };
  const invalidate = () => {
    checking = null;
    return ++generation;
  };
  const own = user => {
    const id = user?.id ?? null;
    if (!id || localStore.getItem(accountOwnerKey) !== id) {
      localStore.removeItem("wfeature:lastGame");
      localStore.removeItem(browserTokenKey);
      if (id) localStore.setItem(accountOwnerKey, id);
      else localStore.removeItem(accountOwnerKey);
    }
    sessionStore.removeItem("wfeature.resume-token");
  };
  const reload = () => {
    reloading = true;
    window.location.reload();
  };
  const request = async (action, options) => {
    const response = await reach(fetch, "api/" + action, {
      cache: "no-store", credentials: "same-origin", ...options,
    });
    const fallback = phrase("account.failed", { status: response.status });
    if (!response.ok) {
      const text = response.headers.get("Content-Type")?.startsWith("text/plain") ? await response.text() : "";
      throw new Error(text.trim() || fallback);
    }
    const body = await response.json().catch(() => null);
    if (body?.user === null) return null;
    const user = body?.user;
    if (typeof user?.id !== "string" || !user.id || typeof user.login !== "string" || !user.login) {
      throw new Error(fallback);
    }
    return user;
  };
  const check = () => {
    if (!secure) {
      lock(phrase("account.https"));
      return Promise.resolve(false);
    }
    if (posting || reloading) return Promise.resolve(false);
    if (checking) return checking;
    const turn = generation;
    checking = (async () => {
      try {
        const user = await request("account");
        if (turn !== generation) return false;
        own(user);
        if (!user) {
          lock(player ? phrase("account.invalid-login") : "");
          player = null;
          return false;
        }
        if (checked && (player !== user.id || root.dataset.auth !== "player")) {
          // A signed-out or offline entry never initialized the emulator. An
          // initialized entry belongs to the player for whom it was opened.
          lock();
          reload();
          return false;
        }
        player = user.id;
        name.textContent = user.login;
        message.textContent = "";
        root.dataset.auth = "player";
        return true;
      } catch (error) {
        if (turn === generation) lock(error.message);
        return false;
      } finally {
        if (turn === generation) {
          checked = true;
          checking = null;
        }
      }
    })();
    return checking;
  };
  const submit = async action => {
    if (!secure || posting || reloading) return;
    if (action !== "logout") {
      if (!loginID.value.trim()) {
        message.textContent = phrase("account.id-required");
        loginID.focus();
        return;
      }
      if (!password.value) {
        message.textContent = phrase("account.password-required");
        password.focus();
        return;
      }
    }
    posting = true;
    const turn = invalidate();
    const options = { method: "POST" };
    if (action !== "logout") {
      options.headers = { "Content-Type": "application/json" };
      options.body = JSON.stringify({ id: loginID.value, password: password.value });
    }
    if (action !== "register") password.value = "";
    lock();
    try {
      const user = await request(action, options);
      if (turn !== generation) return;
      if ((action === "logout") !== (user === null)) throw new Error(phrase("account.unavailable"));
      password.value = "";
      own(user);
      reload();
    } catch (error) {
      if (turn === generation) lock(error.message);
    } finally {
      posting = false;
      if (turn !== generation && window.navigator?.onLine !== false) void check();
    }
  };
  form.addEventListener("submit", event => {
    event.preventDefault();
    return submit(event.submitter?.value === "register" ? "register" : "login");
  });
  for (const control of form.elements) control.disabled = !secure;
  document.getElementById("account-logout").addEventListener("click", () => submit("logout"));
  window.addEventListener("storage", event => {
    if (event.key !== accountOwnerKey && event.key !== null) return;
    invalidate();
    checked = true;
    password.value = "";
    sessionStore.removeItem("wfeature.resume-token");
    lock();
    return check();
  });
  window.addEventListener("offline", () => {
    invalidate();
    checked = true;
    lock(phrase("network.unreachable"));
  });
  window.addEventListener("online", check);
  return { check };
};
