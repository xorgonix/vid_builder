// vid-build offline drafts (spec §7). A bounded IndexedDB queue for safe
// creative drafts only. Production, approvals, deletions, membership and
// workflow configuration always require a live server.
(function () {
  "use strict";

  var DB_NAME = "vidbuild_drafts";
  var STORE = "drafts";
  var SAFE = { concept_note: 1, annotation: 1, shot_text: 1, blocking_draft: 1 };
  var dbPromise = null;

  function openDB() {
    if (dbPromise) return dbPromise;
    dbPromise = new Promise(function (resolve, reject) {
      var req = indexedDB.open(DB_NAME, 1);
      req.onupgradeneeded = function () {
        var db = req.result;
        if (!db.objectStoreNames.contains(STORE)) {
          db.createObjectStore(STORE, { keyPath: "id" });
        }
      };
      req.onsuccess = function () { resolve(req.result); };
      req.onerror = function () { reject(req.error); };
    });
    return dbPromise;
  }

  function tx(mode, fn) {
    return openDB().then(function (db) {
      return new Promise(function (resolve, reject) {
        var t = db.transaction(STORE, mode);
        var store = t.objectStore(STORE);
        var out = fn(store);
        t.oncomplete = function () { resolve(out && out.result !== undefined ? out.result : out); };
        t.onerror = function () { reject(t.error); };
      });
    });
  }

  function uuid() {
    return "draft-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 10);
  }

  function enqueue(draft) {
    if (!SAFE[draft.mutation_type]) {
      return Promise.reject(new Error("This change requires a live server connection."));
    }
    draft.id = draft.id || uuid();
    draft.created_at = draft.created_at || new Date().toISOString();
    return tx("readwrite", function (store) { store.put(draft); return draft; });
  }

  function all() {
    return tx("readonly", function (store) {
      var req = store.getAll();
      var box = {};
      req.onsuccess = function () { box.result = req.result || []; };
      return box;
    });
  }

  function remove(id) {
    return tx("readwrite", function (store) { store.delete(id); return true; });
  }

  function clear() {
    return tx("readwrite", function (store) { store.clear(); return true; });
  }

  function post(url, drafts) {
    var csrf = document.querySelector('meta[name="csrf-token"]');
    var token = csrf ? csrf.getAttribute("content") : "";
    return fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": token, "HX-Request": "true" },
      body: JSON.stringify({ drafts: drafts })
    }).then(function (r) { return r.json(); });
  }

  function sync(url) {
    return all().then(function (drafts) {
      if (!drafts.length) return { results: [] };
      return post(url, drafts).then(function (res) {
        var applied = (res.results || []).filter(function (r) { return r.status === "applied"; });
        return Promise.all(applied.map(function (r) { return remove(r.draft_id); })).then(function () { return res; });
      });
    });
  }

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function render(root) {
    var list = root.querySelector("#drafts-list");
    var status = root.querySelector("#drafts-status");
    all().then(function (drafts) {
      if (!drafts.length) {
        list.innerHTML = '<div class="alert"><span>No drafts queued on this device.</span></div>';
        return;
      }
      list.innerHTML = drafts.map(function (d) {
        return '<div class="card bg-base-100 shadow"><div class="card-body p-3">' +
          '<div class="flex items-center justify-between"><span class="badge badge-ghost badge-sm">' + esc(d.mutation_type) + "</span>" +
          '<span class="text-xs opacity-60">' + esc(d.created_at) + "</span></div>" +
          '<pre class="text-xs whitespace-pre-wrap bg-base-200 rounded p-2 mt-2">' + esc(JSON.stringify(d.payload)) + "</pre>" +
          '<div class="text-xs opacity-60 mt-1">entity ' + esc(d.entity_id) + " · base revision " + esc(String(d.base_revision)) + "</div>" +
          "</div></div>";
      }).join("");
    });

    root.querySelector("#drafts-sync").addEventListener("click", function () {
      status.classList.remove("hidden");
      status.className = "alert alert-info mb-3";
      status.querySelector("span").textContent = "Syncing…";
      sync(root.getAttribute("data-sync-url")).then(function (res) {
        var conflicts = (res.results || []).filter(function (r) { return r.status === "conflict"; });
        var rejected = (res.results || []).filter(function (r) { return r.status === "rejected"; });
        status.className = conflicts.length || rejected.length ? "alert alert-warning mb-3" : "alert alert-success mb-3";
        status.querySelector("span").textContent =
          (res.results || []).length + " draft(s): " +
          (res.results || []).filter(function (r) { return r.status === "applied"; }).length + " applied, " +
          conflicts.length + " conflict, " + rejected.length + " rejected";
        renderConflicts(root, conflicts.concat(rejected));
        render(root);
      }).catch(function (e) {
        status.className = "alert alert-error mb-3";
        status.querySelector("span").textContent = "Sync failed: " + e.message;
      });
    });
  }

  function renderConflicts(root, conflicts) {
    var box = root.querySelector("#drafts-list");
    if (!conflicts.length) return;
    var html = '<div class="card bg-base-100 shadow border border-warning"><div class="card-body"><h2 class="card-title text-sm">Conflicts and rejections</h2>';
    conflicts.forEach(function (c) {
      html += '<div class="border-t border-base-300 pt-2 mt-2 text-xs">';
      html += '<div class="font-semibold">' + esc(c.draft_id) + " — " + esc(c.status) + "</div>";
      if (c.reason) html += "<p>" + esc(c.reason) + "</p>";
      html += '<div class="grid grid-cols-3 gap-2 mt-1">';
      html += '<div><div class="opacity-60">Base</div><pre class="bg-base-200 rounded p-1 whitespace-pre-wrap">' + esc(JSON.stringify(c.base || {})) + "</pre></div>";
      html += '<div><div class="opacity-60">Server current</div><pre class="bg-base-200 rounded p-1 whitespace-pre-wrap">' + esc(JSON.stringify(c.server || {})) + "</pre></div>";
      html += '<div><div class="opacity-60">Your draft</div><pre class="bg-base-200 rounded p-1 whitespace-pre-wrap">' + esc(JSON.stringify(c.draft || {})) + "</pre></div>";
      html += "</div></div>";
    });
    html += "</div></div>";
    box.insertAdjacentHTML("afterbegin", html);
  }

  // Offline capture: intercept safe forms only when the server is unreachable.
  function interceptForms() {
    document.addEventListener("submit", function (e) {
      if (navigator.onLine) return;
      var form = e.target;
      if (!form || !form.action) return;
      var action = form.getAttribute("action") || "";
      var draft = null;

      if (action.indexOf("/discovery/threads/") >= 0 && action.indexOf("/messages") >= 0) {
        var content = form.querySelector('[name="content"]');
        var threadId = action.split("/discovery/threads/")[1].split("/")[0];
        if (content && content.value.trim()) {
          draft = { mutation_type: "concept_note", entity_id: threadId, base_revision: 0,
            payload: { thread_id: threadId, content: content.value.trim() } };
        }
      } else if (action.indexOf("/annotations") >= 0) {
        var body = form.querySelector('[name="body"]');
        if (body && body.value.trim()) {
          var shotEl = form.querySelector('[name="shot_id"]');
          draft = { mutation_type: "annotation", entity_id: shotEl ? shotEl.value : "", base_revision: 0,
            payload: { shot_id: shotEl ? shotEl.value : "", body: body.value.trim() } };
        }
      } else if (/\/shots\/[^/]+$/.test(action) && form.querySelector('[name="_method"]')) {
        var method = form.querySelector('[name="_method"]');
        if (method && method.value.toUpperCase() === "PATCH") {
          var shotId = action.split("/shots/")[1];
          var revision = form.querySelector('[name="revision"]');
          var payload = { base: {} };
          ["title", "purpose", "treatment", "duration_s"].forEach(function (k) {
            var el = form.querySelector('[name="' + k + '"]');
            if (el) { payload[k] = k === "duration_s" ? parseInt(el.value, 10) || 0 : el.value; payload.base[k] = el.value; }
          });
          draft = { mutation_type: "shot_text", entity_id: shotId, base_revision: parseInt(revision ? revision.value : "0", 10) || 0, payload: payload };
        }
      }

      if (draft) {
        e.preventDefault();
        enqueue(draft).then(function () {
          var note = document.getElementById("net-indicator");
          if (note) { note.classList.remove("hidden"); note.querySelector("span").textContent = "Draft saved on this device; it will sync when the connection returns."; }
        });
      }
    });
  }

  function autoSync() {
    var root = document.getElementById("drafts-root");
    if (!root || !navigator.onLine) return;
    sync(root.getAttribute("data-sync-url")).then(function (res) {
      if ((res.results || []).some(function (r) { return r.status === "conflict"; })) {
        renderConflicts(root, (res.results || []).filter(function (r) { return r.status !== "applied"; }));
      }
    }).catch(function () {});
  }

  function init() {
    interceptForms();
    var root = document.getElementById("drafts-root");
    if (root) { render(root); autoSync(); }
    window.addEventListener("online", function () { if (root) autoSync(); });
  }

  window.VidbuildDrafts = {
    ready: true,
    enqueue: enqueue,
    all: all,
    remove: remove,
    clear: clear,
    sync: sync
  };

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
