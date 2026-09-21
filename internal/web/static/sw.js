// vid-build service worker (spec §12.2). It caches only the app shell and safe
// static assets. It never caches mutations, generated media or playback data.
var CACHE = "vidbuild-shell-v1";
var SHELL = [
  "/offline",
  "/static/mobile.css",
  "/static/pwa.js",
  "/static/drafts.js",
  "/static/icons/icon-192.svg"
];

self.addEventListener("install", function (e) {
  e.waitUntil(caches.open(CACHE).then(function (c) { return c.addAll(SHELL); }).then(function () { return self.skipWaiting(); }));
});

self.addEventListener("activate", function (e) {
  e.waitUntil(caches.keys().then(function (keys) {
    return Promise.all(keys.filter(function (k) { return k !== CACHE; }).map(function (k) { return caches.delete(k); }));
  }).then(function () { return self.clients.claim(); }));
});

self.addEventListener("fetch", function (e) {
  var req = e.request;
  if (req.method !== "GET") return;
  var url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  // Never cache generated media, playback documents or job polling.
  if (url.pathname.indexOf("/assets/") === 0) return;
  if (url.pathname === "/animatic/data") return;
  if (url.pathname.indexOf("/jobs/") === 0) return;

  if (req.mode === "navigate") {
    e.respondWith(fetch(req).catch(function () { return caches.match("/offline"); }));
    return;
  }
  if (url.pathname.indexOf("/static/") === 0) {
    e.respondWith(caches.match(req).then(function (hit) {
      if (hit) return hit;
      return fetch(req).then(function (res) {
        var copy = res.clone();
        caches.open(CACHE).then(function (c) { c.put(req, copy); });
        return res;
      });
    }));
  }
});
