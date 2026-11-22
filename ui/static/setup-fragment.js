// The setup token may arrive in the URL fragment from the console QR. A
// fragment is never sent to the server, so this script moves it into the form,
// clears it from the address bar and history entry, and leaves the no-
// JavaScript path untouched when the script does not run.
(function () {
  "use strict";

  var prefix = "#token=";
  if (window.location.hash.indexOf(prefix) !== 0) {
    return;
  }

  function clearFragment() {
    if (!window.history || !window.history.replaceState) {
      return;
    }
    window.history.replaceState(
      null,
      document.title,
      window.location.pathname + window.location.search
    );
  }

  var token = window.location.hash.slice(prefix.length);
  try {
    token = decodeURIComponent(token);
  } catch (error) {
    token = "";
  }
  clearFragment();

  var input = document.getElementById("setup-token");
  if (!input) {
    return;
  }
  if (token) {
    input.value = token;
  }
  if (input.form) {
    input.form.addEventListener("submit", clearFragment);
  }
})();
