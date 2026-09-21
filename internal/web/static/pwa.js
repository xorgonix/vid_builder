// vid-build PWA registration and connection indicator (spec §12.2).
(function () {
  "use strict";
  if ("serviceWorker" in navigator) {
    window.addEventListener("load", function () {
      navigator.serviceWorker.register("/sw.js").catch(function (e) {
        console.warn("service worker registration failed", e);
      });
    });
  }
  function indicator() {
    var bar = document.getElementById("net-indicator");
    if (!bar) return;
    if (navigator.onLine) {
      bar.classList.add("hidden");
    } else {
      bar.classList.remove("hidden");
    }
  }
  window.addEventListener("online", indicator);
  window.addEventListener("offline", indicator);
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", indicator);
  else indicator();
})();
