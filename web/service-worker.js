// The name is the shell's version, and it moves whenever the list below does:
// activate deletes older shell caches, so a new name is what
// retires the entries an older shell left behind. The fetch handler is network
// first, so a stale entry is not what this prevents — an entry for a file the
// shell no longer has is.
const cacheName = "wfeature-shell-v28";

// The shell is what the page needs to come up, which is now only the page: a
// game runs on the server and this page draws what it sends.
const shell = [
  "./",
  "./index.html",
  "./style.css",
  "./app.js",
  "./keybindings.js",
  "./session.js",
  "./session-link.js",
  "./debug-log.js",
  "./audio.js",
  "./audio-settings.js",
  "./key-holds.js",
  "./game-speed.js",
  "./keypad-size.js",
  "./keypad-layout.js",
  "./storage.js",
  "./touch.js",
  "./add-game.js",
  "./confirm.js",
  "./text-input.js",
  "./save-backup.js",
  "./vibrate.js",
  "./manifest.webmanifest",
  "./icon-32.png",
  "./icon-192.png",
  "./icon-512.png",
  "./apple-touch-icon.png",
];

// One missing entry must not fail the whole installation.
self.addEventListener("install", event => {
  event.waitUntil(caches.open(cacheName).then(cache => Promise.all(
    shell.map(url => cache.add(url).catch(() => undefined)),
  )));
  self.skipWaiting();
});

const retireOldShells = () => caches.keys().then(keys => Promise.all(
  keys.filter(key => key.startsWith("wfeature-shell-") && key !== cacheName)
    .map(key => caches.delete(key)),
));

self.addEventListener("activate", event => {
  event.waitUntil(self.clients.claim().then(retireOldShells));
});

self.addEventListener("fetch", event => {
  const url = new URL(event.request.url);
  // Save API responses must always reflect the server's current state, and the
  // game archives are far too large to keep a copy of in the shell cache.
  if (event.request.method !== "GET" || url.origin !== self.location.origin) return;
  if (
    url.pathname.startsWith("/api/") ||
    url.pathname.startsWith("/games/") ||
    // The same, for the archives that came in through the page: a game that
    // was deleted must not still be served out of a cache.
    url.pathname.startsWith("/ext/")
  ) {
    return;
  }
  event.respondWith(fetch(event.request).then(response => {
    if (response.ok) {
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
