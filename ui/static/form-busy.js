// Slow form submissions show a busy state after a short delay. The form still
// posts normally without JavaScript; this only adds feedback while the request
// is in flight. A page restored from history can keep the busy state, so
// pageshow resets it instead of leaving a stale label behind.
(function () {
  "use strict";

  var DELAY = 250;
  var forms = document.querySelectorAll("form[data-busy]");
  if (forms.length === 0) {
    return;
  }

  var states = new WeakMap();
  var live = document.createElement("p");
  live.className = "visually-hidden";
  live.setAttribute("aria-live", "polite");
  live.setAttribute("aria-atomic", "true");
  document.body.appendChild(live);

  function submitButton(form, event) {
    var submitter = event && event.submitter;
    if (submitter && submitter.form === form && submitter.tagName === "BUTTON") {
      return submitter;
    }
    return form.querySelector("button[type='submit'], button:not([type])");
  }

  function showBusy(form, button) {
    var busyLabel = form.getAttribute("data-busy-label") || "Working...";
    var originalLabel = button.textContent;
    button.setAttribute("data-busy-default-label", originalLabel);

    var spinner = document.createElement("span");
    spinner.className = "button-spinner";
    spinner.setAttribute("aria-hidden", "true");
    var content = document.createElement("span");
    content.className = "button-busy-content";
    content.appendChild(spinner);
    content.appendChild(document.createTextNode(busyLabel));
    button.textContent = "";
    button.appendChild(content);
    button.setAttribute("aria-label", busyLabel);
    button.disabled = true;

    form.setAttribute("aria-busy", "true");
    form.classList.add("is-busy");
    live.textContent =
      busyLabel.replace(/\.{3}$/, "") +
      ". The page will change when it finishes.";
  }

  function reset(form) {
    var state = states.get(form);
    if (state && state.timer) {
      window.clearTimeout(state.timer);
    }
    states.delete(form);

    form.removeAttribute("aria-busy");
    form.classList.remove("is-busy");

    var button = state
      ? state.button
      : form.querySelector("button[type='submit'], button:not([type])");
    if (!button) {
      return;
    }

    var originalLabel = button.getAttribute("data-busy-default-label");
    if (originalLabel !== null) {
      button.textContent = originalLabel;
      button.removeAttribute("data-busy-default-label");
      button.removeAttribute("aria-label");
    }
    var content = button.querySelector(".button-busy-content");
    if (content) {
      button.removeChild(content);
    }
    var spinner = button.querySelector(".button-spinner");
    if (spinner) {
      button.removeChild(spinner);
    }
    button.disabled = false;
  }

  function start(form, event) {
    if (states.has(form)) {
      return;
    }
    var button = submitButton(form, event);
    if (!button) {
      return;
    }

    var state = { button: button, timer: 0 };
    state.timer = window.setTimeout(function () {
      state.timer = 0;
      if (!form.isConnected) {
        states.delete(form);
        return;
      }
      showBusy(form, button);
    }, DELAY);
    states.set(form, state);
  }

  for (var index = 0; index < forms.length; index += 1) {
    forms[index].addEventListener("submit", function (event) {
      start(this, event);
    });
  }

  window.addEventListener("pageshow", function () {
    live.textContent = "";
    for (var index = 0; index < forms.length; index += 1) {
      reset(forms[index]);
    }
  });
})();
