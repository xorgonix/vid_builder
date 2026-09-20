// vid-build blocking editor island (spec §4.3).
// State is the authoritative JSON; this island only helps the creator edit it.
// Saving submits ordinary form data through the normal authenticated route.
(function () {
  "use strict";

  var W = 1000;

  function el(tag, attrs, children) {
    var e = document.createElement(tag);
    if (attrs) {
      Object.keys(attrs).forEach(function (k) {
        if (k === "class") e.className = attrs[k];
        else if (k === "text") e.textContent = attrs[k];
        else e.setAttribute(k, attrs[k]);
      });
    }
    (children || []).forEach(function (c) {
      if (c) e.appendChild(c);
    });
    return e;
  }

  function num(v, d) {
    var n = parseFloat(v);
    return isNaN(n) ? d : n;
  }

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function renderSVG(s) {
    var h = s.canvas && s.canvas.width ? (W * s.canvas.height) / s.canvas.width : W;
    var out = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + W + " " + h + '" width="100%">';
    out += '<defs><marker id="ah" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 z" fill="#d97706"/></marker></defs>';
    out += '<rect x="0" y="0" width="' + W + '" height="' + h + '" fill="#0b0f14" stroke="#334155" stroke-width="2"/>';
    (s.zones || []).forEach(function (z) {
      out += '<rect x="' + z.x * W + '" y="' + z.y * h + '" width="' + z.w * W + '" height="' + z.h * h + '" fill="#1e293b" fill-opacity="0.45" stroke="#334155" stroke-dasharray="4 4"/>';
      out += '<text x="' + (z.x * W + 8) + '" y="' + (z.y * h + 22) + '" fill="#64748b" font-size="16" font-family="sans-serif">' + esc(z.name) + "</text>";
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
      var cx = a.x * W,
        cy = a.y * h;
      out += '<circle cx="' + cx + '" cy="' + cy + '" r="26" fill="#f472b6" fill-opacity="0.85" stroke="#be185d" stroke-width="2"/>';
      var rad = ((a.body_rotation || 0) - 90) * Math.PI / 180;
      out += '<line x1="' + cx + '" y1="' + cy + '" x2="' + (cx + Math.cos(rad) * 30) + '" y2="' + (cy + Math.sin(rad) * 30) + '" stroke="#fbcfe8" stroke-width="4"/>';
      out += '<line x1="' + cx + '" y1="' + cy + '" x2="' + a.gaze_x * W + '" y2="' + a.gaze_y * h + '" stroke="#fde68a" stroke-width="3" stroke-dasharray="6 4" marker-end="url(#ah)"/>';
      out += '<text x="' + cx + '" y="' + (cy + 52) + '" fill="#fbcfe8" font-size="16" text-anchor="middle" font-family="sans-serif">' + esc(a.name) + "</text>";
    });
    out += "</svg>";
    return out;
  }

  function field(label, input) {
    var l = el("label", { class: "flex items-center gap-2 text-xs" });
    l.appendChild(el("span", { class: "w-20 opacity-70", text: label }));
    l.appendChild(input);
    return l;
  }

  function textInput(value, on) {
    var i = el("input", { class: "input input-bordered input-xs flex-1", value: value == null ? "" : value });
    i.addEventListener("input", function () { on(i.value); });
    return i;
  }

  function numInput(value, step, on) {
    var i = el("input", { class: "input input-bordered input-xs w-20", type: "number", step: step, value: value == null ? 0 : value });
    i.addEventListener("input", function () { on(num(i.value, 0)); });
    return i;
  }

  function selectInput(value, options, on) {
    var s = el("select", { class: "select select-bordered select-xs" });
    options.forEach(function (o) {
      var op = el("option", { value: o, text: o });
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
    var hidden = root.querySelector(".blocking-json");
    var textarea = root.querySelector(".blocking-textarea");
    var state;
    try {
      var raw = stateEl.value !== undefined && stateEl.value !== null ? stateEl.value : stateEl.textContent;
      state = JSON.parse(raw);
    } catch (e) {
      state = {};
    }
    state.canvas = state.canvas || { width: 1, height: 1 };
    state.camera = state.camera || { shot_size: "medium", angle: "eye", x: 0.05, y: 0.05, w: 0.9, h: 0.9 };
    state.actors = state.actors || [];
    state.props = state.props || [];
    state.arrows = state.arrows || [];
    state.zones = state.zones || [];

    function sync() {
      var json = JSON.stringify(state);
      if (hidden) hidden.value = json;
      if (textarea) textarea.value = json;
      canvas.innerHTML = renderSVG(state);
    }

    function buildControls() {
      controls.innerHTML = "";
      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mb-1", text: "Camera" }));
      var c = state.camera;
      controls.appendChild(field("shot size", selectInput(c.shot_size, ["wide", "medium", "close", "extreme_close"], function (v) { c.shot_size = v; sync(); })));
      controls.appendChild(field("angle", selectInput(c.angle, ["eye", "low", "high", "dutch"], function (v) { c.angle = v; sync(); })));

      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mt-2 mb-1", text: "Actors" }));
      state.actors.forEach(function (a, idx) {
        var box = el("div", { class: "border border-base-300 rounded-box p-2 mb-1 flex flex-col gap-1" });
        box.appendChild(field("name", textInput(a.name, function (v) { a.name = v; sync(); })));
        box.appendChild(field("x", numInput(a.x, 0.01, function (v) { a.x = v; sync(); })));
        box.appendChild(field("y", numInput(a.y, 0.01, function (v) { a.y = v; sync(); })));
        box.appendChild(field("pose", textInput(a.pose, function (v) { a.pose = v; sync(); })));
        box.appendChild(field("facing", selectInput(a.facing || "camera", ["camera", "left", "right", "away"], function (v) { a.facing = v; sync(); })));
        box.appendChild(field("gaze x", numInput(a.gaze_x, 0.01, function (v) { a.gaze_x = v; sync(); })));
        box.appendChild(field("gaze y", numInput(a.gaze_y, 0.01, function (v) { a.gaze_y = v; sync(); })));
        var del = el("button", { class: "btn btn-xs btn-error btn-outline", type: "button", text: "Remove actor" });
        del.addEventListener("click", function () { state.actors.splice(idx, 1); buildControls(); sync(); });
        box.appendChild(del);
        controls.appendChild(box);
      });
      var addA = el("button", { class: "btn btn-xs", type: "button", text: "+ actor" });
      addA.addEventListener("click", function () {
        state.actors.push({ name: "Subject", x: 0.5, y: 0.55, body_rotation: 0, pose: "standing", gaze_x: 0.5, gaze_y: 0.4, facing: "camera" });
        buildControls(); sync();
      });
      controls.appendChild(addA);

      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mt-2 mb-1", text: "Props" }));
      state.props.forEach(function (p, idx) {
        var box = el("div", { class: "border border-base-300 rounded-box p-2 mb-1 flex flex-col gap-1" });
        box.appendChild(field("name", textInput(p.name, function (v) { p.name = v; sync(); })));
        box.appendChild(field("x", numInput(p.x, 0.01, function (v) { p.x = v; sync(); })));
        box.appendChild(field("y", numInput(p.y, 0.01, function (v) { p.y = v; sync(); })));
        var del = el("button", { class: "btn btn-xs btn-error btn-outline", type: "button", text: "Remove prop" });
        del.addEventListener("click", function () { state.props.splice(idx, 1); buildControls(); sync(); });
        box.appendChild(del);
        controls.appendChild(box);
      });
      var addP = el("button", { class: "btn btn-xs", type: "button", text: "+ prop" });
      addP.addEventListener("click", function () { state.props.push({ name: "prop", x: 0.7, y: 0.6 }); buildControls(); sync(); });
      controls.appendChild(addP);

      controls.appendChild(el("div", { class: "text-xs uppercase opacity-60 mt-2 mb-1", text: "Arrows" }));
      state.arrows.forEach(function (ar, idx) {
        var box = el("div", { class: "border border-base-300 rounded-box p-2 mb-1 flex flex-col gap-1" });
        box.appendChild(field("kind", selectInput(ar.kind || "movement", ["movement", "camera"], function (v) { ar.kind = v; sync(); })));
        ["x1", "y1", "x2", "y2"].forEach(function (k, i) {
          box.appendChild(field(k, numInput(ar[k], 0.01, function (v) { ar[k] = v; sync(); })));
        });
        var del = el("button", { class: "btn btn-xs btn-error btn-outline", type: "button", text: "Remove arrow" });
        del.addEventListener("click", function () { state.arrows.splice(idx, 1); buildControls(); sync(); });
        box.appendChild(del);
        controls.appendChild(box);
      });
      var addAr = el("button", { class: "btn btn-xs", type: "button", text: "+ arrow" });
      addAr.addEventListener("click", function () { state.arrows.push({ kind: "movement", x1: 0.4, y1: 0.55, x2: 0.6, y2: 0.55 }); buildControls(); sync(); });
      controls.appendChild(addAr);
    }

    if (textarea) {
      textarea.addEventListener("input", function () {
        try { state = JSON.parse(textarea.value); buildControls(); canvas.innerHTML = renderSVG(state); if (hidden) hidden.value = textarea.value; } catch (e) {}
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