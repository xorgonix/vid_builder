// vid-build prompt workbench island: add/remove layer rows without changing
// the authoritative state model. Saving submits the normal form route.
(function () {
  "use strict";
  function renumberHint() {
    var count = document.querySelectorAll(".layer-row").length;
    var hidden = document.querySelector(".layers-count");
    if (hidden) hidden.value = String(count);
  }
  function rowTemplate(count, scopes, modes) {
    var row = document.createElement("div");
    row.className = "layer-row grid grid-cols-12 gap-2 items-start border-b border-base-300 py-2";
    var scope = '<select class="select select-bordered select-xs col-span-3" name="scope_' + count + '">';
    scopes.forEach(function (s) { scope += '<option value="' + s + '">' + s + "</option>"; });
    scope += "</select>";
    var mode = '<select class="select select-bordered select-xs col-span-2" name="mode_' + count + '">';
    modes.forEach(function (m) { mode += '<option value="' + m + '">' + m + "</option>"; });
    mode += "</select>";
    row.innerHTML =
      scope + mode +
      '<textarea class="textarea textarea-bordered text-xs col-span-5" name="text_' + count + '" rows="2"></textarea>' +
      '<label class="col-span-1 flex items-center gap-1 text-xs"><input type="checkbox" class="checkbox checkbox-xs" name="locked_' + count + '"/>lock</label>' +
      '<button type="button" class="btn btn-xs btn-ghost col-span-1 remove-layer">✕</button>';
    return row;
  }
  function init() {
    var container = document.querySelector(".layer-rows");
    if (!container) return;
    var addBtn = document.querySelector(".add-layer");
    var scopes = (addBtn && addBtn.dataset.scopes ? addBtn.dataset.scopes.split(",") : ["experiment"]);
    var modes = (addBtn && addBtn.dataset.modes ? addBtn.dataset.modes.split(",") : ["append"]);
    if (addBtn) {
      addBtn.addEventListener("click", function () {
        container.appendChild(rowTemplate(container.querySelectorAll(".layer-row").length, scopes, modes));
        renumberHint();
      });
    }
    container.addEventListener("click", function (e) {
      var t = e.target;
      if (t && t.classList && t.classList.contains("remove-layer")) {
        var row = t.closest(".layer-row");
        if (row) row.remove();
        renumberHint();
      }
    });
    renumberHint();
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();