// vid-build blocking editor island v2 (spec §7.4).
// The JSON document is authoritative; this island is a bounded, unsaved-edit
// helper. Undo/redo only covers local unsaved edits. Saving submits an ordinary
// authenticated form POST that creates a new version.
(function () {
  "use strict";

  var W = 1000;
  var HISTORY_LIMIT = 50;

  function el(tag, attrs, children) {
    var e = document.createElement(tag);
    if (attrs) {
      Object.keys(attrs).forEach(function (k) {
        if (k === "class") e.className = attrs[k];
        else if (k === "text") e.textContent = attrs[k];
        else e.setAttribute(k, attrs[k]);
      });
    }
    (children || []).forEach(function (c) { if (c) e.appendChild(c); });
    return e;
  }

  function num(v, d) {
    var n = parseFloat(v);
    return isNaN(n) ? d : n;
  }

  function clamp01(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function nextID(prefix, list) {
    var max = 0;
    (list || []).forEach(function (it) {
      var m = /_(\d+)$/.exec(it.element_id || "");
      if (m) max = Math.max(max, parseInt(m[1], 10));
    });
    return prefix + "_" + (max + 1);
  }

  var POSE_CATS = ["standing", "sitting", "walking", "running", "lying", "crouching", "kneeling", "reaching", "leaning", "other"];
  var FOCAL_CATS = ["", "wide", "normal", "telephoto", "macro", "fisheye"];
  var SHOT_SIZES = ["wide", "medium", "close", "extreme_close"];
  var ANGLES = ["eye", "low", "high", "dutch"];
  var FACINGS = ["camera", "left", "right", "away"];
  var EASINGS = ["", "linear", "ease_in", "ease_out", "fast_out", "slow_in", "hold"];
  var CAMERA_MOVES = ["", "static", "push_in", "pull_out", "pan_left", "pan_right", "tilt_up", "tilt_down", "orbit", "handheld", "crane"];

  function normalize(state) {
    state.canvas = state.canvas || { width: 1, height: 1 };
    state.camera = state.camera || {};
    var c = state.camera;
    if (c.shot_size == null) c.shot_size = "medium";
    if (c.angle == null) c.angle = "eye";
    if (c.height == null) c.height = 0.5;
    if (c.x == null) c.x = 0.05;
    if (c.y == null) c.y = 0.05;
    if (c.w == null) c.w = 0.9;
    if (c.h == null) c.h = 0.9;
    state.actors = state.actors || [];
    state.props = state.props || [];
    state.arrows = state.arrows || [];
    state.labels = state.labels || [];
    state.zones = state.zones || [];
    state.environment = state.environment || [];
    state.actors.forEach(function (a, i) {
      if (!a.element_id) a.element_id = "actor_" + (i + 1);
      if (!a.pose_category && a.pose) a.pose_category = a.pose;
      if (a.gaze_x == null) a.gaze_x = a.x;
      if (a.gaze_y == null) a.gaze_y = a.y;
    });
    state.props.forEach(function (p, i) { if (!p.element_id) p.element_id = "prop_" + (i + 1); });
    state.arrows.forEach(function (a, i) { if (!a.element_id) a.element_id = "arrow_" + (i + 1); });
    state.labels.forEach(function (l, i) { if (!l.element_id) l.element_id = "label_" + (i + 1); });
    return state;
  }

  function renderSVG(s, selected) {
    var h = s.canvas && s.canvas.width ? (W * s.canvas.height) / s.canvas.width : W;
    var out = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + W + " " + h + '" width="100%" style="touch-action:none">';
    out += '<defs><marker id="ah" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 z" fill="#d97706"/></marker></defs>';
    out += '<rect x="0" y="0" width="' + W + '" height="' + h + '" fill="#0b0f14" stroke="#334155" stroke-width="2"/>';
    (s.zones || []).forEach(function (z) {
      out += '<rect x="' + z.x * W + '" y="' + z.y * h + '" width="' + z.w * W + '" height="' + z.h * h + '" fill="#1e293b" fill-opacity="0.45" stroke="#334155" stroke-dasharray="4 4"/>';
      out += '<text x="' + (z.x * W + 8) + '" y="' + (z.y * h + 22) + '" fill="#64748b" font-size="16" font-family="sans-serif">' + esc(z.name) + "</text>";
    });
    var camSel = selected === "camera" ? ' stroke-width="5"' : "";
    out += '<rect data-el="camera" x="' + s.camera.x * W + '" y="' + s.camera.y * h + '" width="' + s.camera.w * W + '" height="' + s.camera.h * h + '" fill="none" stroke="#38bdf8" stroke-width="3" stroke-dasharray="10 6"' + camSel + ' style="cursor:move"/>';
    (s.arrows || []).forEach(function (a) {
      var c = a.kind === "camera" ? "#38bdf8" : "#d97706";
      var sel = selected === a.element_id ? ' stroke-width="5"' : ' stroke-width="3"';
      out += '<line data-el="' + esc(a.element_id) + '" x1="' + a.x1 * W + '" y1="' + a.y1 * h + '" x2="' + a.x2 * W + '" y2="' + a.y2 * h + '" stroke="' + c + '"' + sel + ' marker-end="url(#ah)" style="cursor:move"/>';
    });
    (s.props || []).forEach(function (p) {
      var sel = selected === p.element_id ? ' stroke-width="4" stroke="#f0abfc"' : "";
      out += '<rect data-el="' + esc(p.element_id) + '" x="' + (p.x * W - 18) + '" y="' + (p.y * h - 18) + '" width="36" height="36" fill="#a78bfa" fill-opacity="0.7" rx="4" style="cursor:move"' + sel + '/>';
      out += '<text x="' + p.x * W + '" y="' + (p.y * h + 48) + '" fill="#c4b5fd" font-size="14" text-anchor="middle" font-family="sans-serif">' + esc(p.name) + (p.state ? " (" + esc(p.state) + ")" : "") + "</text>";
    });
    (s.actors || []).forEach(function (a) {
      var cx = a.x * W, cy = a.y * h;
      var sel = selected === a.element_id ? ' stroke-width="5" stroke="#facc15"' : ' stroke-width="2"';
      out += '<circle data-el="' + esc(a.element_id) + '" cx="' + cx + '" cy="' + cy + '" r="26" fill="#f472b6" fill-opacity="0.85" stroke="#be185d"' + sel + ' style="cursor:move"/>';
      var rad = ((a.body_rotation || 0) - 90) * Math.PI / 180;
      out += '<line x1="' + cx + '" y1="' + cy + '" x2="' + (cx + Math.cos(rad) * 30) + '" y2="' + (cy + Math.sin(rad) * 30) + '" stroke="#fbcfe8" stroke-width="4"/>';
      out += '<line data-el="' + esc(a.element_id) + ':gaze" x1="' + cx + '" y1="' + cy + '" x2="' + a.gaze_x * W + '" y2="' + a.gaze_y * h + '" stroke="#fde68a" stroke-width="3" stroke-dasharray="6 4" marker-end="url(#ah)" style="cursor:move"/>';
      out += '<text x="' + cx + '" y="' + (cy + 52) + '" fill="#fbcfe8" font-size="16" text-anchor="middle" font-family="sans-serif">' + esc(a.name) + (a.locked ? " \uD83D\uDD12" : "") + "</text>";
    });
    (s.labels || []).forEach(function (l) {
      out += '<text data-el="' + esc(l.element_id) + '" x="' + l.x * W + '" y="' + l.y * h + '" fill="#e2e8f0" font-size="14" font-family="sans-serif" style="cursor:move">' + esc(l.text) + "</text>";
    });
    out += "</svg>";
    return out;
  }

  function field(label, input) {
    var l = el("label", { class: "flex items-center gap-2 text-xs" });
    l.appendChild(el("span", { class: "w-24 opacity-70", text: label }));
    l.appendChild(input);
    return l;
  }

  function textInput(value, on) {
    var i = el("input", { class: "input input-bordered input-xs flex-1", value: value == null ? "" : value });
    i.addEventListener("change", function () { on(i.value); });
    return i;
  }

  function numInput(value, step, on) {
    var i = el("input", { class: "input input-bordered input-xs w-20", type: "number", step: step, value: value == null ? 0 : value });
    i.addEventListener("change", function () { on(num(i.value, 0)); });
    return i;
  }

  function selectInput(value, options, on) {
    var s = el("select", { class: "select select-bordered select-xs" });
    options.forEach(function (o) {
      var op = el("option", { value: o, text: o === "" ? "(none)" : o });
      if (o === value) op.selected = true;
      s.appendChild(op);
    });
    s.addEventListener("change", function () { on(s.value); });
    return s;
  }

  function Editor(root) {
    var stateEl = root.querySelector(".blocking-state");
    var canvas = root.querySelector(".blocking-canvas");
    var controls = root.querySelector(".blocking-controls");
    var textarea = root.querySelector(".blocking-textarea");
    var state, selected = null, undoStack = [], redoStack = [];
    try {
      var raw = stateEl.value !== undefined && stateEl.value !== null ? stateEl.value : stateEl.textContent;
      state = normalize(JSON.parse(raw));
    } catch (e) {
      state = normalize({});
    }

    function serializable() {
      return JSON.stringify(state);
    }

    function pushHistory() {
      undoStack.push(serializable());
      if (undoStack.length > HISTORY_LIMIT) undoStack.shift();
      redoStack = [];
    }

    function undo() {
      if (!undoStack.length) return;
      redoStack.push(serializable());
      state = normalize(JSON.parse(undoStack.pop()));
      selected = null;
      buildControls(); sync();
    }

    function redo() {
      if (!redoStack.length) return;
      undoStack.push(serializable());
      state = normalize(JSON.parse(redoStack.pop()));
      selected = null;
      buildControls(); sync();
    }

    function sync() {
      var json = serializable();
      if (textarea) textarea.value = json;
      canvas.innerHTML = renderSVG(state, selected);
      attachDrag();
    }

    function findElement(id) {
      var kinds = ["actors", "props", "arrows", "labels"];
      for (var k = 0; k < kinds.length; k++) {
        var list = state[kinds[k]] || [];
        for (var i = 0; i < list.length; i++) {
          if (list[i].element_id === id) return { list: list, item: list[i], kind: kinds[k] };
        }
      }
      return null;
    }

    function attachDrag() {
      var svg = canvas.querySelector("svg");
      if (!svg) return;
      Array.prototype.forEach.call(svg.querySelectorAll("[data-el]"), function (node) {
        node.addEventListener("pointerdown", function (ev) {
          var raw = node.getAttribute("data-el");
          var isGaze = raw.indexOf(":gaze") >= 0;
          var id = isGaze ? raw.split(":gaze")[0] : raw;
          if (id !== "camera") {
            var found = findElement(id);
            if (found && found.item.locked) { selected = id; sync(); return; }
          }
          selected = id;
          var rect = svg.getBoundingClientRect();
          var scaleX = W / rect.width;
          var scaleY = (svg.viewBox.baseVal.height) / rect.height;
          node.setPointerCapture(ev.pointerId);
          pushHistory();
          function move(e) {
            var px = (e.clientX - rect.left) * scaleX / W;
            var py = (e.clientY - rect.top) * scaleY / svg.viewBox.baseVal.height;
            px = clamp01(px); py = clamp01(py);
            if (id === "camera") {
              state.camera.x = clamp01(px - state.camera.w / 2);
              state.camera.y = clamp01(py - state.camera.h / 2);
            } else {
              var found = findElement(id);
              if (!found) return;
              if (isGaze) { found.item.gaze_x = px; found.item.gaze_y = py; }
              else if (found.kind === "actors") { found.item.x = px; found.item.y = py; }
              else if (found.kind === "props" || found.kind === "labels") { found.item.x = px; found.item.y = py; }
              else if (found.kind === "arrows") { found.item.x1 = px; found.item.y1 = py; }
            }
            sync();
          }
          function up() {
            window.removeEventListener("pointermove", move);
            window.removeEventListener("pointerup", up);
            buildControls();
          }
          window.addEventListener("pointermove", move);
          window.addEventListener("pointerup", up);
        });
      });
    }

    function mirror() {
      pushHistory();
      state.actors.forEach(function (a) { a.x = clamp01(1 - a.x); a.gaze_x = clamp01(1 - a.gaze_x); });
      state.props.forEach(function (p) { p.x = clamp01(1 - p.x); });
      state.labels.forEach(function (l) { l.x = clamp01(1 - l.x); });
      state.arrows.forEach(function (ar) { ar.x1 = clamp01(1 - ar.x1); ar.x2 = clamp01(1 - ar.x2); });
      state.camera.x = clamp01(1 - (state.camera.x + state.camera.w));
      buildControls(); sync();
    }

    function align(mode) {
      var found = selected ? findElement(selected) : null;
      var list = found && found.kind === "actors" ? state.actors : state.actors;
      if (!list.length) return;
      pushHistory();
      if (mode === "left") list.forEach(function (a) { a.x = 0.1; });
      if (mode === "center") list.forEach(function (a) { a.x = 0.5; });
      if (mode === "right") list.forEach(function (a) { a.x = 0.9; });
      if (mode === "middle") list.forEach(function (a) { a.y = 0.5; });
      buildControls(); sync();
    }

    function distribute() {
      if (state.actors.length < 3) return;
      pushHistory();
      var sorted = state.actors.slice().sort(function (a, b) { return a.x - b.x; });
      sorted.forEach(function (a, i) { a.x = clamp01((i + 1) / (sorted.length + 1)); });
      buildControls(); sync();
    }

    function toggleLock() {
      var found = selected ? findElement(selected) : null;
      if (!found) return;
      pushHistory();
      found.item.locked = !found.item.locked;
      buildControls(); sync();
    }

    function removeSelected() {
      var found = selected ? findElement(selected) : null;
      if (!found) return;
      pushHistory();
      var idx = found.list.indexOf(found.item);
      if (idx >= 0) found.list.splice(idx, 1);
      selected = null;
      buildControls(); sync();
    }

    function toolbar() {
      var bar = el("div", { class: "flex flex-wrap gap-1 my-2" });
      function btn(label, fn) {
        var b = el("button", { class: "btn btn-xs", type: "button", text: label });
        b.addEventListener("click", fn);
        bar.appendChild(b);
      }
      btn("+ actor", function () {
        pushHistory();
        state.actors.push({ element_id: nextID("actor", state.actors), name: "Subject", x: 0.5, y: 0.55, body_rotation: 0, pose_category: "standing", pose: "standing", gaze_x: 0.5, gaze_y: 0.4, facing: "camera" });
        buildControls(); sync();
      });
      btn("+ prop", function () {
        pushHistory();
        state.props.push({ element_id: nextID("prop", state.props), name: "prop", x: 0.7, y: 0.6, state: "" });
        buildControls(); sync();
      });
      btn("+ arrow", function () {
        pushHistory();
        state.arrows.push({ element_id: nextID("arrow", state.arrows), kind: "movement", x1: 0.4, y1: 0.55, x2: 0.6, y2: 0.55 });
        buildControls(); sync();
      });
      btn("+ label", function () {
        pushHistory();
        state.labels.push({ element_id: nextID("label", state.labels), text: "note", x: 0.3, y: 0.3 });
        buildControls(); sync();
      });
      btn("Remove", removeSelected);
      btn("Mirror", mirror);
      btn("Align left", function () { align("left"); });
      btn("Center", function () { align("center"); });
      btn("Align right", function () { align("right"); });
      btn("Middle", function () { align("middle"); });
      btn("Distribute", distribute);
      btn("Lock", toggleLock);
      btn("Undo", undo);
      btn("Redo", redo);
      return bar;
    }

    function buildControls() {
      controls.innerHTML = "";
      controls.appendChild(toolbar());
      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mb-1", text: "Camera" }));
      var c = state.camera;
      controls.appendChild(field("shot size", selectInput(c.shot_size, SHOT_SIZES, function (v) { pushHistory(); c.shot_size = v; sync(); })));
      controls.appendChild(field("angle", selectInput(c.angle, ANGLES, function (v) { pushHistory(); c.angle = v; sync(); })));
      controls.appendChild(field("focal", selectInput(c.focal_category || "", FOCAL_CATS, function (v) { pushHistory(); c.focal_category = v; sync(); })));
      controls.appendChild(field("height", numInput(c.height, 0.05, function (v) { pushHistory(); c.height = clamp01(v); sync(); })));

      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mt-2 mb-1", text: "Actors" }));
      state.actors.forEach(function (a) {
        var box = el("div", { class: "border border-base-300 rounded-box p-2 mb-1 flex flex-col gap-1" });
        if (a.locked) box.appendChild(el("div", { class: "badge badge-warning badge-xs", text: "locked" }));
        box.appendChild(field("id", textInput(a.element_id, function (v) { pushHistory(); a.element_id = v; sync(); })));
        box.appendChild(field("name", textInput(a.name, function (v) { pushHistory(); a.name = v; sync(); })));
        box.appendChild(field("character", textInput(a.character_id || "", function (v) { pushHistory(); a.character_id = v; sync(); })));
        box.appendChild(field("x", numInput(a.x, 0.01, function (v) { pushHistory(); a.x = clamp01(v); sync(); })));
        box.appendChild(field("y", numInput(a.y, 0.01, function (v) { pushHistory(); a.y = clamp01(v); sync(); })));
        box.appendChild(field("pose", selectInput(a.pose_category || "other", POSE_CATS, function (v) { pushHistory(); a.pose_category = v; sync(); })));
        box.appendChild(field("pose text", textInput(a.pose, function (v) { pushHistory(); a.pose = v; sync(); })));
        box.appendChild(field("facing", selectInput(a.facing || "camera", FACINGS, function (v) { pushHistory(); a.facing = v; sync(); })));
        box.appendChild(field("gaze x", numInput(a.gaze_x, 0.01, function (v) { pushHistory(); a.gaze_x = clamp01(v); sync(); })));
        box.appendChild(field("gaze y", numInput(a.gaze_y, 0.01, function (v) { pushHistory(); a.gaze_y = clamp01(v); sync(); })));
        controls.appendChild(box);
      });

      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mt-2 mb-1", text: "Props" }));
      state.props.forEach(function (p) {
        var box = el("div", { class: "border border-base-300 rounded-box p-2 mb-1 flex flex-col gap-1" });
        box.appendChild(field("id", textInput(p.element_id, function (v) { pushHistory(); p.element_id = v; sync(); })));
        box.appendChild(field("name", textInput(p.name, function (v) { pushHistory(); p.name = v; sync(); })));
        box.appendChild(field("state", textInput(p.state || "", function (v) { pushHistory(); p.state = v; sync(); })));
        box.appendChild(field("x", numInput(p.x, 0.01, function (v) { pushHistory(); p.x = clamp01(v); sync(); })));
        box.appendChild(field("y", numInput(p.y, 0.01, function (v) { pushHistory(); p.y = clamp01(v); sync(); })));
        controls.appendChild(box);
      });

      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mt-2 mb-1", text: "Arrows" }));
      state.arrows.forEach(function (ar) {
        var box = el("div", { class: "border border-base-300 rounded-box p-2 mb-1 flex flex-col gap-1" });
        box.appendChild(field("kind", selectInput(ar.kind || "movement", ["movement", "camera"], function (v) { pushHistory(); ar.kind = v; sync(); })));
        ["x1", "y1", "x2", "y2"].forEach(function (k) {
          box.appendChild(field(k, numInput(ar[k], 0.01, function (v) { pushHistory(); ar[k] = clamp01(v); sync(); })));
        });
        controls.appendChild(box);
      });

      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mt-2 mb-1", text: "Environment cues" }));
      var env = textInput((state.environment || []).join("; "), function (v) {
        pushHistory();
        state.environment = v.split(";").map(function (s) { return s.trim(); }).filter(Boolean);
        sync();
      });
      controls.appendChild(env);
    }

    if (textarea) {
      textarea.addEventListener("input", function () {
        try { state = normalize(JSON.parse(textarea.value)); selected = null; buildControls(); canvas.innerHTML = renderSVG(state, selected); attachDrag(); } catch (e) {}
      });
    }
    buildControls();
    sync();
  }

  function init() {
    Array.prototype.forEach.call(document.querySelectorAll(".blocking-editor"), function (r) {
      try { Editor(r); } catch (e) { console.error("blocking editor", e); }
    });
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
