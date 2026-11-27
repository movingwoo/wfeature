// Increment the version when the shell changes. Legacy workers delete every
// other wfeature-shell-* cache on a late navigation, even after replacement.
// A separate prefix protects this shell from that already-installed code.
const cacheName = "wfeature-pwa-v78";
const cacheVersion = Number(cacheName.match(/-v(\d+)$/)[1]);

// The shell is what the page needs to come up, which is now only the page: a
// game runs on the server and this page draws what it sends.
const shell = [
  "./",
  "./index.html",
  "./style.css",
  "./app.js",
  "./account.js",
  "./keybindings.js",
  "./gamepad.js",
  "./fullscreen.js",
  "./session.js",
  "./frame-stream.js",
  "./audio-stream.js",
  "./magnify.js",
  "./hqx.js",
  "./hqx-blend.js",
  "./hqx-patterns.js",
  "./session-link.js",
  "./checkpoint.js",
  "./debug-log.js",
  "./audio.js",
  "./fm-host.js",
  "./fm-synth.js",
  "./audio-settings.js",
  "./av-sync.js",
  "./key-holds.js",
  "./rapid-fire.js",
  "./game-speed.js",
  "./screen-choice.js",
  "./keypad-geometry.js",
  "./keypad-editor.js",
  "./keypad-layout.js",
  "./storage.js",
  "./touch.js",
  "./add-game.js",
  "./game-browser.js",
  "./confirm.js",
  "./text-input.js",
  "./save-backup.js",
  "./settings-file.js",
  "./reach.js",
  "./vibrate.js",
  "./text.js",
  "./manifest.webmanifest",
  "./icon-32.png",
  "./icon-192.png",
  "./icon-512.png",
  "./apple-touch-icon.png",
];

const shellURLs = new Set(shell.map(path => new URL(path, self.registration.scope).href));
const fetchShell = (request, url) => fetch(request, {
  // The same listener can restart in another mode between navigations.
  cache: url.endsWith("/") || url.endsWith("/index.html") ? "no-store" : "no-cache",
});
const cacheable = response => response.status === 200 && !response.redirected
  && !/\b(?:no-store|private)\b/i.test(response.headers.get("Cache-Control") ?? "")
  && !/\b(?:Cookie|Authorization)\b/i.test(response.headers.get("Vary") ?? "");

// One missing entry must not fail the whole installation.
self.addEventListener("install", event => {
  event.waitUntil(caches.open(cacheName).then(cache => Promise.all(
    Array.from(shellURLs, url => fetchShell(url, url).then(response => {
      if (cacheable(response)) return cache.put(url, response);
    }).catch(() => undefined)),
  )));
  self.skipWaiting();
});

const retireOldShells = () => caches.keys().then(keys => Promise.all(
  keys.filter(key => {
    if (key.startsWith("wfeature-shell-")) return true;
    const version = /^wfeature-pwa-v(\d+)$/.exec(key);
    // This worker may finish a response after its replacement activates.
    // Only retire predecessors; never delete a future worker's shell.
    return version && Number(version[1]) < cacheVersion;
  })
    .map(key => caches.delete(key)),
));

self.addEventListener("activate", event => {
  event.waitUntil(self.clients.claim().then(retireOldShells));
});

self.addEventListener("fetch", event => {
  // Save API responses must always reflect the server's current state, and the
  // game archives are far too large to keep a copy of in the shell cache.
  // The same, for the archives that came in through the page: a game that
  // was deleted must not still be served out of a cache.
  // An explicit shell list also keeps game listings and account data online-only.
  const url = event.request.url;
  if (event.request.method !== "GET" || !shellURLs.has(url)) return;
  event.respondWith(fetchShell(event.request, url).then(response => {
    if (cacheable(response)) {
      const copy = response.clone();
      event.waitUntil(caches.open(cacheName).then(async cache => {
        await cache.put(event.request, copy);
        // An older worker can finish a fetch after activation and recreate
        // its retired cache. Sweep again on a controlled navigation.
        if (event.request.mode === "navigate") await retireOldShells();
      }));
    }
    return response;
  }).catch(async () => {
    const cache = await caches.open(cacheName);
    return cache.match(event.request);
  }));
});
