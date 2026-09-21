// vid-build dictation island (spec §12.1). Adds speech-to-text capture to a
// textarea using the browser Web Speech API. It degrades to plain typing when
// unavailable and never becomes a source of truth.
(function () {
  "use strict";

  function attach(button) {
    var targetId = button.getAttribute("data-dictate-target");
    var area = targetId ? document.getElementById(targetId) : null;
    if (!area) return;
    var SR = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!SR) {
      button.disabled = true;
      button.title = "Speech recognition is not available in this browser";
      return;
    }
    var rec = new SR();
    rec.continuous = true;
    rec.interimResults = false;
    rec.lang = document.documentElement.lang || "en";
    var active = false;

    rec.addEventListener("result", function (e) {
      for (var i = e.resultIndex; i < e.results.length; i++) {
        if (e.results[i].isFinal) {
          var text = e.results[i][0].transcript.trim();
          area.value = area.value ? area.value + " " + text : text;
          area.dispatchEvent(new Event("input", { bubbles: true }));
        }
      }
    });
    rec.addEventListener("end", function () {
      if (active) {
        try { rec.start(); } catch (err) { active = false; button.classList.remove("dictation-active"); }
      }
    });

    button.addEventListener("click", function () {
      active = !active;
      if (active) {
        button.classList.add("dictation-active");
        button.textContent = "Stop dictation";
        try { rec.start(); } catch (err) {}
      } else {
        button.classList.remove("dictation-active");
        button.textContent = "Dictate";
        try { rec.stop(); } catch (err) {}
      }
    });
  }

  function init() {
    Array.prototype.forEach.call(document.querySelectorAll("[data-dictate-target]"), attach);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
