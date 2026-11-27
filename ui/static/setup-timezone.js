// The review step prefills the time zone from the browser's IANA zone. The
// server has already rendered UTC as the working default, so this script only
// improves the value when the owner has not chosen one. When JavaScript is off
// or Intl is unavailable, the form still submits UTC and setup completes.
(function () {
  "use strict";

  var input = document.getElementById("setup-timezone");
  if (!input) {
    return;
  }
  if (input.getAttribute("data-timezone-set") === "true") {
    return;
  }
  var zone = "";
  try {
    if (window.Intl && window.Intl.DateTimeFormat) {
      zone = window.Intl.DateTimeFormat().resolvedOptions().timeZone || "";
    }
  } catch (error) {
    zone = "";
  }
  if (!zone) {
    return;
  }
  input.value = zone;
  // Keep the read-only summary above the form in step with the field, so the
  // review page never shows two different zones.
  var summaries = document.querySelectorAll("[data-timezone]");
  for (var index = 0; index < summaries.length; index += 1) {
    summaries[index].textContent = zone;
  }
})();
