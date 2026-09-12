// The name is the shell's version, and it moves whenever the list below does:
// activate deletes every cache that is not this one, so a new name is what
// retires the entries an older shell left behind. The fetch handler is network
// first, so a stale entry is not what this prevents — an entry for a file the
// shell no longer has is.
const cacheName = "wfeature-shell-v24";

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

self.addEventListener("activate", event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(
    keys.filter(key => key !== cacheName).map(key => caches.delete(key)),
  )));
  self.clients.claim();
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
      void caches.open(cacheName).then(cache => cache.put(event.request, copy));
    }
    return response;
  }).catch(() => caches.match(event.request)));
});
