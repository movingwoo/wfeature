const cacheName = "wfeature-shell-v7";

// The shell is what the page needs to come up, which is now only the page: a
// game runs on the server and this page draws what it sends.
const shell = [
  "./",
  "./index.html",
  "./style.css",
  "./app.js",
  "./keybindings.js",
  "./session.js",
  "./debug-log.js",
  "./audio.js",
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
  if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/games/")) return;
  event.respondWith(fetch(event.request).then(response => {
    if (response.ok) {
      const copy = response.clone();
      void caches.open(cacheName).then(cache => cache.put(event.request, copy));
    }
    return response;
  }).catch(() => caches.match(event.request)));
});
