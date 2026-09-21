// vid-build animatic player (spec §11). Plays the video as timed blocking with
// captions and optional placeholder speech. Blocking playback does not predict
// final model motion.
(function () {
  "use strict";

  var W = 1000;

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function lerp(a, b, t) { return a + (b - a) * t; }

  function renderSVG(s) {
    var h = s.canvas && s.canvas.width ? (W * s.canvas.height) / s.canvas.width : W;
    var out = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + W + " " + h + '" width="100%">';
    out += '<defs><marker id="ah" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 z" fill="#d97706"/></marker></defs>';
    out += '<rect x="0" y="0" width="' + W + '" height="' + h + '" fill="#0b0f14" stroke="#334155" stroke-width="2"/>';
    (s.zones || []).forEach(function (z) {
      out += '<rect x="' + z.x * W + '" y="' + z.y * h + '" width="' + z.w * W + '" height="' + z.h * h + '" fill="#1e293b" fill-opacity="0.45" stroke="#334155" stroke-dasharray="4 4"/>';
    });
    if (s.camera) {
      out += '<rect x="' + s.camera.x * W + '" y="' + s.camera.y * h + '" width="' + s.camera.w * W + '" height="' + s.camera.h * h + '" fill="none" stroke="#38bdf8" stroke-width="3" stroke-dasharray="10 6"/>';
    }
    (s.arrows || []).forEach(function (a) {
      var c = a.kind === "camera" ? "#38bdf8" : "#d97706";
      out += '<line x1="' + a.x1 * W + '" y1="' + a.y1 * h + '" x2="' + a.x2 * W + '" y2="' + a.y2 * h + '" stroke="' + c + '" stroke-width="3" marker-end="url(#ah)"/>';
    });
    (s.props || []).forEach(function (p) {
      out += '<rect x="' + (p.x * W - 18) + '" y="' + (p.y * h - 18) + '" width="36" height="36" fill="#a78bfa" fill-opacity="0.7" rx="4"/>';
      out += '<text x="' + p.x * W + '" y="' + (p.y * h + 48) + '" fill="#c4b5fd" font-size="14" text-anchor="middle" font-family="sans-serif">' + esc(p.name) + "</text>";
    });
    (s.actors || []).forEach(function (a) {
      var cx = a.x * W, cy = a.y * h;
      out += '<circle cx="' + cx + '" cy="' + cy + '" r="26" fill="#f472b6" fill-opacity="0.85" stroke="#be185d" stroke-width="2"/>';
      var rad = ((a.body_rotation || 0) - 90) * Math.PI / 180;
      out += '<line x1="' + cx + '" y1="' + cy + '" x2="' + (cx + Math.cos(rad) * 30) + '" y2="' + (cy + Math.sin(rad) * 30) + '" stroke="#fbcfe8" stroke-width="4"/>';
      out += '<line x1="' + cx + '" y1="' + cy + '" x2="' + a.gaze_x * W + '" y2="' + a.gaze_y * h + '" stroke="#fde68a" stroke-width="3" stroke-dasharray="6 4" marker-end="url(#ah)"/>';
      out += '<text x="' + cx + '" y="' + (cy + 52) + '" fill="#fbcfe8" font-size="16" text-anchor="middle" font-family="sans-serif">' + esc(a.name) + "</text>";
    });
    out += "</svg>";
    return out;
  }

  function interpolate(start, end, t) {
    var out = JSON.parse(JSON.stringify(start));
    out.camera = out.camera || {};
    var ec = end.camera || {};
    out.camera.x = lerp(start.camera.x, ec.x, t);
    out.camera.y = lerp(start.camera.y, ec.y, t);
    out.camera.w = lerp(start.camera.w, ec.w, t);
    out.camera.h = lerp(start.camera.h, ec.h, t);
    var endActors = {};
    (end.actors || []).forEach(function (a) { endActors[a.element_id || a.name] = a; });
    (out.actors || []).forEach(function (a) {
      var b = endActors[a.element_id || a.name];
      if (!b) return;
      a.x = lerp(a.x, b.x, t); a.y = lerp(a.y, b.y, t);
      a.gaze_x = lerp(a.gaze_x, b.gaze_x, t); a.gaze_y = lerp(a.gaze_y, b.gaze_y, t);
    });
    return out;
  }

  function Player(root) {
    var src = root.getAttribute("data-src");
    var frame = root.querySelector(".animatic-frame");
    var captions = root.querySelector(".animatic-captions");
    var overlay = root.querySelector(".animatic-overlay");
    var scrub = root.querySelector(".animatic-scrub");
    var timeEl = root.querySelector(".animatic-time");
    var playBtn = root.querySelector(".animatic-play");
    var speedSel = root.querySelector(".animatic-speed");
    var loopSel = root.querySelector(".animatic-loop");
    var captionsToggle = root.querySelector(".animatic-captions-toggle");
    var voiceToggle = root.querySelector(".animatic-voice-toggle");
    var overlayToggle = root.querySelector(".animatic-overlay-toggle");
    var doc = null, starts = [], total = 0, current = 0, playing = false, last = 0, spoken = {};
    var speed = 1, loopMode = "none";

    function shotIndexAt(ms) {
      for (var i = 0; i < starts.length; i++) {
        var end = starts[i] + doc.shots[i].duration_ms;
        if (ms < end) return i;
      }
      return doc.shots.length - 1;
    }

    function render() {
      if (!doc || !doc.shots.length) return;
      var idx = shotIndexAt(current);
      var shot = doc.shots[idx];
      var local = Math.max(0, current - starts[idx]);
      var t = shot.duration_ms > 0 ? Math.min(1, local / shot.duration_ms) : 0;
      frame.innerHTML = renderSVG(interpolate(shot.start_blocking, shot.end_blocking, t));
      var captionLines = [];
      (shot.cues || []).forEach(function (cue) {
        var cueEnd = cue.start_ms + cue.duration_ms;
        if (local >= cue.start_ms && local <= cueEnd) {
          captionLines.push(cue.content);
          if (voiceToggle.checked && (cue.cue_type === "dialogue" || cue.cue_type === "voiceover") && !spoken[cue.id]) {
            spoken[cue.id] = true;
            if (window.speechSynthesis) {
              var u = new SpeechSynthesisUtterance(cue.content);
              window.speechSynthesis.speak(u);
            }
          }
        }
      });
      captions.textContent = captionsToggle.checked ? captionLines.join(" / ") : "";
      captions.className = "animatic-captions absolute bottom-2 left-2 right-2 text-center text-lg font-semibold drop-shadow";
      if (overlayToggle.checked) {
        var m = shot.metrics || {};
        overlay.textContent = shot.title + " — " + shot.purpose +
          " · " + (shot.duration_ms / 1000).toFixed(1) + "s" +
          (Object.keys(m).length ? " · " + JSON.stringify(m) : "");
      } else {
        overlay.textContent = "";
      }
      scrub.value = total > 0 ? String(Math.round((current / total) * 1000)) : "0";
      timeEl.textContent = (current / 1000).toFixed(1) + "s / " + (total / 1000).toFixed(1) + "s";
    }

    function tick(now) {
      if (!playing) return;
      if (!last) last = now;
      var dt = (now - last) * speed;
      last = now;
      current += dt;
      if (current >= total) {
        if (loopMode === "none") {
          current = total;
          playing = false;
          playBtn.textContent = "Play";
          render();
          return;
        }
        var idx = shotIndexAt(total - 1);
        var loopStart = 0;
        if (loopMode === "shot") loopStart = starts[idx];
        else if (loopMode === "beat") {
          var beat = doc.shots[idx].beat_id;
          for (var i = idx; i >= 0; i--) { if (doc.shots[i].beat_id === beat) loopStart = starts[i]; else break; }
        } else if (loopMode === "scene") {
          var scene = doc.shots[idx].scene_id;
          for (var j = idx; j >= 0; j--) { if (doc.shots[j].scene_id === scene) loopStart = starts[j]; else break; }
        }
        current = loopStart;
        spoken = {};
      }
      render();
      requestAnimationFrame(tick);
    }

    function play() {
      if (playing) return;
      if (current >= total) current = 0;
      playing = true; last = 0;
      playBtn.textContent = "Pause";
      requestAnimationFrame(tick);
    }
    function pause() { playing = false; playBtn.textContent = "Play"; }

    function bind() {
      playBtn.addEventListener("click", function () { playing ? pause() : play(); });
      root.querySelector(".animatic-restart").addEventListener("click", function () { current = 0; spoken = {}; render(); });
      root.querySelector(".animatic-prev").addEventListener("click", function () {
        var idx = Math.max(0, shotIndexAt(current) - 1);
        current = starts[idx]; render();
      });
      root.querySelector(".animatic-next").addEventListener("click", function () {
        var idx = Math.min(doc.shots.length - 1, shotIndexAt(current) + 1);
        current = starts[idx]; render();
      });
      scrub.addEventListener("input", function () {
        current = (parseFloat(scrub.value) / 1000) * total;
        render();
      });
      speedSel.addEventListener("change", function () { speed = parseFloat(speedSel.value) || 1; });
      loopSel.addEventListener("change", function () { loopMode = loopSel.value; });
      captionsToggle.addEventListener("change", render);
      overlayToggle.addEventListener("change", render);
      root.querySelector(".animatic-fullscreen").addEventListener("click", function () {
        if (root.requestFullscreen) root.requestFullscreen();
      });
      document.addEventListener("keydown", function (e) {
        if (e.target && /input|textarea|select/i.test(e.target.tagName)) return;
        if (e.code === "Space") { e.preventDefault(); playing ? pause() : play(); }
      });
    }

    fetch(src, { credentials: "same-origin" })
      .then(function (r) { return r.json(); })
      .then(function (data) {
        doc = data;
        starts = [];
        total = 0;
        (doc.shots || []).forEach(function (s) { starts.push(total); total += s.duration_ms; });
        if (doc.settings) {
          speed = doc.settings.speed || 1;
          speedSel.value = String(speed);
          loopMode = doc.settings.loop_mode || "none";
          loopSel.value = loopMode;
          captionsToggle.checked = !!doc.settings.captions;
          voiceToggle.checked = !!doc.settings.voice;
          overlayToggle.checked = !!doc.settings.metrics_overlay;
        }
        bind();
        render();
      })
      .catch(function (e) { console.error("animatic", e); });
  }

  function init() {
    var root = document.getElementById("animatic-player");
    if (root) {
      try { Player(root); } catch (e) { console.error("animatic player", e); }
    }
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
