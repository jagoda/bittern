// Show the test picture only after the browser has decoded it. A failed fetch
// leaves the sentence in the card instead of a broken image icon. The image is
// hidden in the markup, so a browser without JavaScript gets the no-script
// link rather than an empty frame.
(function () {
  "use strict";

  var figures = document.querySelectorAll("[data-picture]");
  for (var index = 0; index < figures.length; index += 1) {
    (function (figure) {
      var image = figure.querySelector("[data-picture-image]");
      var fallback = figure.querySelector("[data-picture-error]");
      if (!image || !fallback) {
        return;
      }

      function show() {
        image.hidden = false;
        fallback.hidden = true;
        figure.setAttribute("data-picture-state", "ready");
      }

      function fail() {
        image.hidden = true;
        fallback.hidden = false;
        figure.setAttribute("data-picture-state", "failed");
      }

      image.addEventListener("load", show);
      image.addEventListener("error", fail);
      if (image.complete) {
        if (image.naturalWidth > 0) {
          show();
        } else {
          fail();
        }
      }
    })(figures[index]);
  }
})();
