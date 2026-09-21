// Guided tours: a scripted driver that performs real operations in the
// interface while a caption explains each step. State survives full page loads
// via sessionStorage, so a tour can span navigation.
(function () {
  "use strict";

  var STORE_KEY = "vidbuild.tutorial";
  var scriptCache = {};

  function loadState() {
    try {
      return JSON.parse(sessionStorage.getItem(STORE_KEY) || "null");
    } catch (e) {
      return null;
    }
  }
  function saveState(s) {
    sessionStorage.setItem(STORE_KEY, JSON.stringify(s));
  }
  function clearState() {
    sessionStorage.removeItem(STORE_KEY);
  }

  function injectStyles() {
    if (document.getElementById("tutorial-styles")) return;
    var css =
      ".tutorial-spotlight{outline:3px solid #f59e0b !important;outline-offset:2px;box-shadow:0 0 0 5px rgba(245,158,11,.35) !important;border-radius:6px;position:relative;z-index:9997}" +
      "#tutorial-overlay{position:fixed;left:50%;bottom:20px;transform:translateX(-50%);z-index:10000;width:min(560px,92vw)}" +
      "#tutorial-overlay .tutorial-card{background:#1d232a;color:#e5e7eb;border:1px solid #f59e0b;border-radius:12px;box-shadow:0 10px 30px rgba(0,0,0,.45);padding:14px 16px}" +
      "#tutorial-overlay .tutorial-caption{font-size:.95rem;line-height:1.4;margin-bottom:10px}" +
      "#tutorial-overlay .tutorial-row{display:flex;align-items:center;gap:8px}" +
      "#tutorial-overlay button{cursor:pointer;border:1px solid #4b5563;background:#374151;color:#e5e7eb;border-radius:8px;padding:5px 10px;font-size:.8rem}" +
      "#tutorial-overlay button.primary{background:#f59e0b;border-color:#f59e0b;color:#1f2937;font-weight:600}" +
      "#tutorial-overlay .tutorial-count{margin-left:auto;font-size:.75rem;opacity:.75}";
    var el = document.createElement("style");
    el.id = "tutorial-styles";
    el.textContent = css;
    document.head.appendChild(el);
  }

  function overlay() {
    var el = document.getElementById("tutorial-overlay");
    if (!el) {
      el = document.createElement("div");
      el.id = "tutorial-overlay";
      document.body.appendChild(el);
    }
    return el;
  }

  function clearSpotlight() {
    var cur = document.querySelector(".tutorial-spotlight");
    if (cur) cur.classList.remove("tutorial-spotlight");
  }

  function render(state, script, note) {
    injectStyles();
    var step = script.steps[state.index];
    var el = overlay();
    var caption = note || (step ? step.caption : "Tour complete.");
    var isLast = state.index >= script.steps.length - 1;
    el.innerHTML =
      '<div class="tutorial-card" role="dialog" aria-label="Guided tour">' +
      '<div class="tutorial-caption"></div>' +
      '<div class="tutorial-row">' +
      '<button data-tut="back">Back</button>' +
      '<button class="primary" data-tut="next">' + (isLast ? "Finish" : "Next") + "</button>" +
      '<button data-tut="play">' + (state.playing ? "Pause" : "Play") + "</button>" +
      '<button data-tut="exit">Exit</button>' +
      '<span class="tutorial-count">' + script.title + " · " + (state.index + 1) + "/" + script.steps.length + "</span>" +
      "</div></div>";
    el.querySelector(".tutorial-caption").textContent = caption;
    el.querySelector('[data-tut="back"]').onclick = function () {
      state.playing = false;
      saveState(state);
      runStep(Math.max(0, state.index - 1));
    };
    el.querySelector('[data-tut="next"]').onclick = function () {
      state.playing = false;
      saveState(state);
      if (isLast) {
        finish(state);
      } else {
        runStep(state.index + 1);
      }
    };
    el.querySelector('[data-tut="play"]').onclick = function () {
      state.playing = !state.playing;
      saveState(state);
      if (state.playing) {
        scheduleNext(state, script);
      } else {
        render(state, script);
      }
    };
    el.querySelector('[data-tut="exit"]').onclick = function () {
      exit();
    };
  }

  function exit() {
    clearSpotlight();
    clearState();
    var el = document.getElementById("tutorial-overlay");
    if (el) el.remove();
  }

  function finish(state) {
    var el = overlay();
    el.innerHTML =
      '<div class="tutorial-card" role="dialog" aria-label="Guided tour">' +
      '<div class="tutorial-caption">Tour complete. You can replay it any time from Guided tours.</div>' +
      '<div class="tutorial-row"><button class="primary" data-tut="exit">Close</button></div></div>';
    el.querySelector('[data-tut="exit"]').onclick = exit;
    clearSpotlight();
    clearState();
  }

  function scheduleNext(state, script) {
    if (!state.playing) return;
    window.setTimeout(function () {
      if (!state.playing) return;
      if (state.index >= script.steps.length - 1) {
        finish(state);
        return;
      }
      runStep(state.index + 1);
    }, 2200);
  }

  function targetOf(step) {
    if (!step.target) return null;
    try {
      return document.querySelector(step.target);
    } catch (e) {
      return null;
    }
  }

  function fill(el, value) {
    el.focus();
    if (el.tagName === "SELECT") {
      el.value = value;
      el.dispatchEvent(new Event("change", { bubbles: true }));
      return;
    }
    el.value = value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function runStep(index) {
    var state = loadState();
    if (!state) return;
    state.index = index;
    saveState(state);
    loadScript(state.slug, function (script) {
      if (!script) {
        exit();
        return;
      }
      if (index >= script.steps.length) {
        finish(state);
        return;
      }
      var step = script.steps[index];
      clearSpotlight();

      // Optional route: navigate before acting.
      if (step.route && location.pathname.indexOf(step.route) === -1) {
        saveState(state);
        location.assign(step.route);
        return;
      }

      if (step.action === "caption" || step.action === "done") {
        render(state, script);
        scheduleNext(state, script);
        return;
      }

      var el = targetOf(step);
      if (!el) {
        render(state, script, step.caption + "  —  (this control is not on the current page; open the right screen, then press Next)");
        return;
      }

      if (step.action === "fill") {
        fill(el, step.value || "");
        el.classList.add("tutorial-spotlight");
        render(state, script);
        scheduleNext(state, script);
        return;
      }

      if (step.action === "click") {
        el.scrollIntoView({ block: "center" });
        if (step.navigates) {
          state.index = index + 1;
          saveState(state);
        }
        el.click();
        render(state, script);
        if (!step.navigates) scheduleNext(state, script);
        return;
      }

      // highlight
      el.scrollIntoView({ block: "center", behavior: "smooth" });
      el.classList.add("tutorial-spotlight");
      render(state, script);
      scheduleNext(state, script);
    });
  }

  function loadScript(slug, cb) {
    if (scriptCache[slug]) {
      cb(scriptCache[slug]);
      return;
    }
    fetch("/tutorials/" + encodeURIComponent(slug) + "/script.json", {
      headers: { Accept: "application/json" },
    })
      .then(function (r) {
        if (!r.ok) throw new Error("script " + r.status);
        return r.json();
      })
      .then(function (script) {
        scriptCache[slug] = script;
        cb(script);
      })
      .catch(function () {
        cb(null);
      });
  }

  function boot() {
    var params = new URLSearchParams(location.search);
    var tour = params.get("tour");
    if (tour) {
      var step = parseInt(params.get("step") || "0", 10) || 0;
      saveState({ slug: tour, index: step, playing: false });
      params.delete("tour");
      params.delete("step");
      var q = params.toString();
      history.replaceState(null, "", location.pathname + (q ? "?" + q : "") + location.hash);
    }
    var state = loadState();
    if (!state) return;
    runStep(state.index);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
