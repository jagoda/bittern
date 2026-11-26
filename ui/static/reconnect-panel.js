// A live panel that loses its polling connection says so instead of leaving a
// stale card or replacing the panel with an error page. The notice sits beside
// the swapped region, so it never becomes part of the content that is replaced.
(function () {
  "use strict";

  function noticeFor(element) {
    if (!element || !element.getAttribute) {
      return null;
    }
    var id = element.getAttribute("data-live-reconnect");
    return id ? document.getElementById(id) : null;
  }

  function showReconnect(event) {
    var notice = noticeFor(event.detail.elt);
    if (notice) {
      notice.hidden = false;
    }
  }

  function hideReconnect(event) {
    var notice = noticeFor(event.detail.elt);
    if (notice && event.detail.successful) {
      notice.hidden = true;
    }
  }

  document.addEventListener("htmx:sendError", showReconnect);
  document.addEventListener("htmx:timeout", showReconnect);
  document.addEventListener("htmx:afterRequest", hideReconnect);
})();
