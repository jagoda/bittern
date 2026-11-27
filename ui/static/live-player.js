// Bittern live wall. One wall owns the panes, the capacity, the selected pane,
// the overlay and full screen; each pane owns its own player island, video,
// MediaSource and WebSocket. A player root is a stable node outside every HTMX
// swap region, and this file refuses to mount one inside a region HTMX can
// replace (ADR 0034 section 3, ADR 0037).
(function () {
  "use strict";

  var MAX_RETRIES = 3;
  var CONNECT_TIMEOUT = 8000;
  var STALL_TIMEOUT = 10000;
  var HLS_START_TIMEOUT = 12000;
  var SWITCH_DELAY = 250;
  var CAPACITY_TICK = 2000;
  var SNAPSHOT_REFRESH = 15000;
  var OVERLAY_IDLE = 3000;
  var POINTER_THROTTLE = 250;
  var CODECS = [
    "avc1.640029",
    "avc1.64002A",
    "avc1.640033",
    "avc1.42E01E",
    "hvc1.1.6.L153.B0",
    "mp4a.40.2",
    "mp4a.40.5",
    "flac",
    "opus",
  ];

  var STATE_CLASS = {
    connecting: "unknown",
    playing: "ok",
    switching: "unknown",
    stalled: "degraded",
    reconnecting: "degraded",
    snapshot: "unknown",
    offline: "failed",
    unsupported: "failed",
    session: "failed",
    error: "failed",
  };

  var STATE_LABEL = {
    connecting: "Connecting",
    playing: "Live",
    switching: "Switching",
    stalled: "Stalled",
    reconnecting: "Reconnecting",
    snapshot: "Still picture",
    offline: "Offline",
    unsupported: "Not supported",
    session: "Signed out",
    error: "Not working",
  };

  function now() {
    return Date.now();
  }

  function each(selector, root, callback) {
    Array.prototype.forEach.call(root.querySelectorAll(selector), callback);
  }

  function supportsPlaybackQuality() {
    return (
      typeof HTMLVideoElement !== "undefined" &&
      typeof HTMLVideoElement.prototype.getVideoPlaybackQuality === "function"
    );
  }

  function areaOf(element) {
    var rect = element.getBoundingClientRect();
    return Math.max(0, rect.width) * Math.max(0, rect.height);
  }

  function centreOf(element) {
    var rect = element.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
  }

  // Pane is one pane of the wall: the player island plus the overlay layer that
  // carries the camera name and the one sentence of a pane that cannot play.
  function Pane(root, wall) {
    this.root = root;
    this.wall = wall;
    this.number = Number(root.getAttribute("data-live-pane")) || 0;
    this.id = "pane-" + this.number;
    this.playerRoot = root.querySelector("[data-live-player]");
    this.noteElement = root.querySelector("[data-player-note]");
    this.announceElement = root.querySelector("[data-player-announce]");
    this.player = null;
    this.live = null;
    this.manual = "";
    this.quality = "snapshot";
    this.autoQuality = "sub";
    this.stillNote = "";
    this.oneStream = false;
    this.name = "";
    if (this.playerRoot) {
      this.manual = this.playerRoot.getAttribute("data-manual-quality") || "";
      this.quality = this.playerRoot.getAttribute("data-quality") || "sub";
      this.autoQuality = this.manual ? "sub" : this.quality;
      this.oneStream = this.playerRoot.getAttribute("data-one-stream") === "true";
      this.name = this.playerRoot.getAttribute("data-camera-name") || "Camera";
      this.player = new LivePlayer(this.playerRoot, wall, this);
    } else {
      this.name = root.getAttribute("aria-label") || "Empty pane";
    }
  }

  Pane.prototype.setSelected = function (selected) {
    this.root.setAttribute("tabindex", selected ? "0" : "-1");
    if (selected) this.root.setAttribute("data-live-selected", "true");
    else this.root.removeAttribute("data-live-selected");
  };

  Pane.prototype.note = function (text) {
    if (!this.noteElement) return;
    this.noteElement.textContent = text || "";
    this.noteElement.hidden = !text;
  };

  Pane.prototype.setState = function (state) {
    this.root.setAttribute("data-live-pane-state", state);
  };

  Pane.prototype.announce = function (text) {
    if (!this.announceElement) return;
    this.announceElement.textContent = "";
    if (!text) return;
    // A microtask keeps a repeated announcement audible.
    window.setTimeout(() => {
      if (this.announceElement) this.announceElement.textContent = text;
    }, 30);
  };

  // LiveWall owns the wall: capacity, selection, overlay and full screen.
  function LiveWall(root) {
    this.root = root;
    this.panes = [];
    this.selected = null;
    this.prior = BitternCapacity.priorForWidth(window.innerWidth || 0);
    this.capacity = BitternCapacity.create({
      prior: this.prior,
      enabled: supportsPlaybackQuality(),
    });
    this.overlay = root.querySelector("[data-live-overlay-panel]");
    this.overlayHidden = false;
    this.hideTimer = null;
    this.lastPointerAt = 0;
    this.fullscreen = false;
    this.started = false;
    this.capacityTimer = null;
    this.snapshotTimer = null;
    this.note = root.querySelector("[data-live-capacity-note]");
    this.announceElement = root.querySelector("[data-live-announce]");
    this.form = root.querySelector("[data-live-pane-form]");
    this.layoutForm = root.querySelector("[data-live-layout-form]");
    this.qualityRadios = root.querySelector("[data-live-quality]");
    this.oneStreamHint = root.querySelector("[data-live-one-stream]");
    this.qualitySubmit = root.querySelector("[data-live-quality-submit]");
    this.panePicker = root.querySelector("[data-live-pane-picker]");
    this.cameraPicker = root.querySelector("[data-live-camera-picker]");
    this.selectedName = root.querySelector("[data-live-selected-name]");
    this.selectedQuality = root.querySelector("[data-live-selected-quality]");
    this.selectedNumber = root.querySelector("[data-live-selected-number]");
    this.stateRegion = root.querySelector("[data-live-camera-state-region]");
    this.fullscreenButton = root.querySelector("[data-live-fullscreen]");
    this.exitButton = root.querySelector("[data-live-exit]");
  }

  LiveWall.prototype.init = function () {
    var wall = this;
    each("[data-live-pane]", this.root, (element) => {
      var pane = new Pane(element, wall);
      wall.panes.push(pane);
    });
    if (!this.panes.length) return;
    this.select(this.paneAt(1) || this.panes[0], false);
    this.enableEnhancements();
    this.initOverlay();
    this.initCapacity();
    this.apply();
    this.updateSelected();
    if (this.fullscreenButton) {
      this.fullscreenButton.addEventListener("click", () => this.enterFullscreen());
    }
    if (this.exitButton) {
      this.exitButton.addEventListener("click", () => this.exitFullscreen());
    }
    document.addEventListener("fullscreenchange", () => this.syncFullscreen());
    document.addEventListener("keydown", (event) => this.onDocumentKeydown(event), true);
    this.root.addEventListener("keydown", (event) => this.onKeydown(event));
    this.root.addEventListener("pointerdown", (event) => this.onPointerDown(event), true);
    window.addEventListener("resize", () => this.syncPrior());
    document.addEventListener("visibilitychange", () => this.onVisibility());
    this.syncFullscreen();
    this.hideOverlay();
  };

  // enableEnhancements swaps the no-JavaScript controls for the ones that act
  // in place: the native radio groups replace the submit buttons and the pane
  // picker, and the quality change no longer needs a page load.
  LiveWall.prototype.enableEnhancements = function () {
    var wall = this;
    each("[data-live-layout-radios]", this.root, (element) => {
      element.hidden = false;
    });
    each("[data-live-layout-buttons]", this.root, (element) => {
      element.hidden = true;
    });
    each("[data-live-pane-picker]", this.root, (element) => {
      element.hidden = true;
    });
    each("[data-live-quality-submit]", this.root, (element) => {
      element.hidden = true;
    });
    if (this.layoutForm) {
      this.layoutForm.addEventListener("change", (event) => {
        if (event.target && event.target.name === "layout") {
          wall.layoutForm.requestSubmit();
        }
      });
    }
    each("[data-live-choose-form]", this.root, (form) => {
      var select = form.querySelector("[data-live-choose-select]");
      var button = form.querySelector("[data-live-choose-button]");
      if (select) select.hidden = true;
      if (!button) return;
      button.addEventListener("click", (event) => {
        event.preventDefault();
        var pane = wall.paneFor(form.closest("[data-live-pane]"));
        if (!pane) return;
        wall.select(pane, true);
        wall.showOverlay();
        var picker = wall.overlay ? wall.overlay.querySelector("select[name='camera']") : null;
        if (picker) picker.focus();
      });
    });
    if (this.overlay) {
      this.overlay.addEventListener("change", (event) => {
        if (event.target && event.target.name === "quality") {
          wall.setQuality(event.target.value);
        }
      });
      this.overlay.addEventListener("click", (event) => {
        var link = event.target.closest("[data-profile-link]");
        if (!link) return;
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        wall.setProfileLink(link);
      });
    }
  };

  // setProfileLink is the single-camera route's link-based profile control. It
  // switches at runtime and keeps the URL shareable.
  LiveWall.prototype.setProfileLink = function (link) {
    var pane = this.selected;
    if (!pane || !pane.player) return;
    var profile = link.getAttribute("data-profile-link");
    pane.quality = profile;
    pane.playerRoot.setAttribute("data-quality", profile);
    each("[data-profile-link]", this.root, (candidate) => {
      if (candidate === link) candidate.setAttribute("aria-current", "true");
      else candidate.removeAttribute("aria-current");
    });
    pane.player.setQuality(profile);
    this.updateSelected();
    var href = link.getAttribute("href");
    if (href && window.history && window.history.replaceState) {
      window.history.replaceState(null, "", href);
    }
  };

  LiveWall.prototype.initCapacity = function () {
    var wall = this;
    var at = now();
    this.capacity.clamp(at, this.view(at));
    this.capacityTimer = window.setInterval(() => wall.tick(), CAPACITY_TICK);
    this.snapshotTimer = window.setInterval(() => wall.refreshSnapshots(), SNAPSHOT_REFRESH);
  };

  LiveWall.prototype.paneAt = function (number) {
    for (var index = 0; index < this.panes.length; index += 1) {
      if (this.panes[index].number === number) return this.panes[index];
    }
    return null;
  };

  LiveWall.prototype.paneById = function (id) {
    for (var index = 0; index < this.panes.length; index += 1) {
      if (this.panes[index].id === id) return this.panes[index];
    }
    return null;
  };

  // view is the capacity model's input: every pane's live state, quality, frame
  // counters and rendered area, in reading order.
  LiveWall.prototype.view = function (at) {
    return {
      // Only panes with a player can hold a media connection, so an empty pane
      // is not capacity and is not a demotion target.
      panes: this.panes.filter((pane) => pane.player).map((pane) => {
        var sample = pane.player ? pane.player.sample() : null;
        return {
          id: pane.id,
          order: pane.number,
          area: areaOf(pane.root),
          live: pane.live === true,
          manual: pane.manual !== "",
          quality: pane.quality,
          dropped: sample ? sample.dropped : undefined,
          total: sample ? sample.total : undefined,
        };
      }),
    };
  };

  LiveWall.prototype.tick = function () {
    if (document.hidden) return;
    if (!this.capacity.isEnabled()) return;
    var at = now();
    var action = this.capacity.evaluate(at, this.view(at));
    if (action) this.applyAction(action, at);
    this.apply();
  };

  LiveWall.prototype.applyAction = function (action, at) {
    var pane = this.paneById(action.id);
    if (!pane || !pane.player) return;
    if (action.kind === "step-down") {
      pane.quality = "sub";
      pane.player.setQuality("sub");
      pane.note("Sub stream, reduced after dropped frames.");
      return;
    }
    if (action.kind === "demote") {
      pane.stillNote = action.last
        ? "This device cannot play this camera live. The picture is a still."
        : "Still picture. This screen is above its live capacity.";
      return;
    }
    if (action.kind === "promote") {
      pane.stillNote = "";
      pane.quality = pane.autoQuality;
      pane.player.setQuality(pane.autoQuality);
      pane.note("");
    }
  };

  // apply turns the capacity model's decisions into live or snapshot panes.
  // A manual choice always wins: main and sub are always live, snapshot is
  // never live.
  LiveWall.prototype.apply = function () {
    var wall = this;
    this.panes.forEach((pane) => {
      if (!pane.player) return;
      var live;
      if (pane.manual === "snapshot") live = false;
      else if (pane.manual === "main" || pane.manual === "sub") live = true;
      else live = !wall.capacity.isDemoted(pane.id);
      if (live === pane.live) return;
      pane.live = live;
      var stillNote = "";
      if (!live && pane.manual !== "snapshot") {
        stillNote = pane.stillNote || "Still picture. This screen is above its live capacity.";
      }
      pane.player.setLive(live, stillNote);
    });
    this.publish();
  };

  LiveWall.prototype.publish = function () {
    var live = 0;
    this.panes.forEach((pane) => {
      if (pane.live) live += 1;
    });
    this.root.setAttribute("data-device-prior", String(this.prior));
    this.root.setAttribute("data-live-count", String(live));
    this.root.setAttribute("data-live-capacity", String(this.capacity.capacity()));
    if (!this.note) return;
    if (live > this.prior) {
      this.note.textContent =
        "This wall is above the tested capacity for this screen. " +
        live +
        " panes are live and this screen is tested for " +
        this.prior +
        ".";
      this.note.hidden = false;
      return;
    }
    this.note.textContent = "";
    this.note.hidden = true;
  };

  LiveWall.prototype.syncPrior = function () {
    var next = BitternCapacity.priorForWidth(window.innerWidth || 0);
    if (next === this.prior) return;
    this.prior = next;
    var at = now();
    this.capacity.setPrior(next, at, this.view(at));
    this.apply();
  };

  // select is the selected pane. It is the focused pane, shown by the inset
  // focus ring and the overlay text, never by a border.
  LiveWall.prototype.select = function (pane, focus) {
    if (!pane) return;
    var previous = this.selected;
    this.selected = pane;
    this.panes.forEach((candidate) => {
      candidate.setSelected(candidate === pane);
      if (candidate !== pane && candidate.announceElement) {
        candidate.announceElement.textContent = "";
      }
    });
    if (previous && previous !== pane) previous.note(previous.noteElement ? previous.noteElement.textContent : "");
    this.updateSelected();
    if (focus) pane.root.focus();
  };

  LiveWall.prototype.updateSelected = function () {
    var pane = this.selected;
    if (!pane) return;
    if (this.selectedName) this.selectedName.textContent = pane.name;
    if (this.selectedQuality) {
      this.selectedQuality.textContent = pane.player ? qualityLabel(pane.quality) : "";
    }
    if (this.selectedNumber) this.selectedNumber.textContent = String(pane.number);
    if (this.form) {
      var paneField = this.form.querySelector("select[name='pane']");
      if (paneField) paneField.value = String(pane.number);
      var cameraField = this.form.querySelector("select[name='camera']");
      if (cameraField) cameraField.value = pane.playerRoot ? pane.playerRoot.getAttribute("data-camera-id") : "";
    }
    if (this.qualityRadios) {
      each("input[name='quality']", this.qualityRadios, (input) => {
        input.checked = input.value === pane.quality;
        input.disabled = pane.oneStream;
      });
    }
    if (this.oneStreamHint) {
      this.oneStreamHint.hidden = !pane.oneStream;
      if (pane.oneStream) {
        this.oneStreamHint.textContent = "One stream. This camera does not report a second stream.";
      }
    }
    if (this.qualityRadios) this.qualityRadios.hidden = pane.oneStream;
    if (this.stateRegion && pane.playerRoot) {
      var status = pane.playerRoot.getAttribute("data-status-url") || "";
      if (status) this.stateRegion.setAttribute("hx-get", status);
    }
    var state = pane.player ? pane.player.state : "";
    pane.announce(STATE_LABEL[state] ? pane.name + ": " + STATE_LABEL[state] : pane.name);
  };

  LiveWall.prototype.onPointerDown = function (event) {
    var paneElement = event.target.closest("[data-live-pane]");
    if (!paneElement) return;
    var pane = this.paneFor(paneElement);
    if (!pane) return;
    // A pointer press selects the pane and moves focus to it (ADR 0037
    // section 9).
    this.select(pane, true);
    this.showOverlay();
  };

  LiveWall.prototype.paneFor = function (element) {
    for (var index = 0; index < this.panes.length; index += 1) {
      if (this.panes[index].root === element) return this.panes[index];
    }
    return null;
  };

  // Escape exits full screen from anywhere, and outside full screen it hides
  // the overlay and returns focus to the selected pane (ADR 0037 section 7).
  LiveWall.prototype.onDocumentKeydown = function (event) {
    if (event.key !== "Escape") return;
    if (document.fullscreenElement) {
      event.preventDefault();
      var exiting = document.exitFullscreen();
      if (exiting && exiting.catch) exiting.catch(() => {});
      return;
    }
    if (this.overlayHidden) return;
    event.preventDefault();
    this.hideOverlay();
    if (this.selected) this.selected.root.focus();
  };

  LiveWall.prototype.onKeydown = function (event) {
    var pane = this.selected;
    var onPane = pane && event.target === pane.root;
    if (!onPane) return;
    switch (event.key) {
      case "ArrowLeft":
        event.preventDefault();
        this.move("left");
        return;
      case "ArrowRight":
        event.preventDefault();
        this.move("right");
        return;
      case "ArrowUp":
        event.preventDefault();
        this.move("up");
        return;
      case "ArrowDown":
        event.preventDefault();
        this.move("down");
        return;
      case "Home":
        event.preventDefault();
        this.select(this.panes[0], true);
        return;
      case "End":
        event.preventDefault();
        this.select(this.panes[this.panes.length - 1], true);
        return;
      case "Tab":
        if (this.overlayHidden) {
          event.preventDefault();
          this.showOverlay();
          if (event.shiftKey) this.focusLastControl();
          else this.focusQuality();
        }
        return;
      default:
        if (event.key && event.key.length === 1) this.showOverlay();
    }
  };

  // move is the arrow-key geometry: the nearest pane in that direction by the
  // centre points of the rendered areas.
  LiveWall.prototype.move = function (direction) {
    var from = centreOf(this.selected.root);
    var best = null;
    var bestScore = Infinity;
    this.panes.forEach((pane) => {
      if (pane === this.selected) return;
      var to = centreOf(pane.root);
      var dx = to.x - from.x;
      var dy = to.y - from.y;
      var along;
      var across;
      if (direction === "left") {
        if (dx >= -1) return;
        along = -dx;
        across = Math.abs(dy);
      } else if (direction === "right") {
        if (dx <= 1) return;
        along = dx;
        across = Math.abs(dy);
      } else if (direction === "up") {
        if (dy >= -1) return;
        along = -dy;
        across = Math.abs(dx);
      } else {
        if (dy <= 1) return;
        along = dy;
        across = Math.abs(dx);
      }
      var score = across * 8 + along;
      if (score < bestScore) {
        bestScore = score;
        best = pane;
      }
    });
    if (best) this.select(best, true);
  };

  // initOverlay is the overlay contract: shown on pointer movement, a key
  // press, a tap or focus, hidden after three seconds, inert and click-through
  // while hidden (ADR 0037 section 7).
  LiveWall.prototype.initOverlay = function () {
    var wall = this;
    if (!this.overlay) return;
    this.root.addEventListener(
      "pointermove",
      () => {
        var at = now();
        if (at - wall.lastPointerAt < POINTER_THROTTLE) return;
        wall.lastPointerAt = at;
        wall.showOverlay();
      },
      true,
    );
    this.root.addEventListener("pointerdown", () => wall.showOverlay(), true);
    this.root.addEventListener(
      "keydown",
      (event) => {
        // Tab is the reveal-with-focus path, and a modifier on its own is not
        // a key press that should change what Tab does.
        if (event.key === "Tab") return;
        if (event.key === "Shift" || event.key === "Control" || event.key === "Alt" || event.key === "Meta") {
          return;
        }
        wall.showOverlay();
      },
      true,
    );
    this.overlay.addEventListener("focusin", () => wall.suspendHide());
    this.overlay.addEventListener("focusout", () => {
      window.setTimeout(() => {
        if (!wall.overlay.contains(document.activeElement)) wall.armHide();
      }, 0);
    });
    this.overlay.addEventListener("keydown", (event) => {
      if (event.key !== "Tab") return;
      var focusable = wall.focusableControls();
      if (!focusable.length) return;
      var first = focusable[0];
      var last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        wall.hideOverlay();
        if (wall.selected) wall.selected.root.focus();
        return;
      }
      if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        if (wall.selected) wall.selected.root.focus();
      }
    });
  };

  LiveWall.prototype.focusableControls = function () {
    if (!this.overlay) return [];
    return Array.prototype.filter.call(
      this.overlay.querySelectorAll(
        "button, input:not([type='hidden']), select, a[href], [tabindex='0']",
      ),
      (element) => {
        if (element.disabled) return false;
        var style = window.getComputedStyle(element);
        if (style.display === "none" || style.visibility === "hidden") return false;
        return element.getClientRects().length > 0;
      },
    );
  };

  LiveWall.prototype.focusQuality = function () {
    if (!this.overlay) return;
    var checked = this.overlay.querySelector("input[name='quality']:checked:not([disabled])");
    if (checked) {
      checked.focus();
      return;
    }
    var first = this.overlay.querySelector("input[name='quality']:not([disabled])");
    if (first) {
      first.focus();
      return;
    }
    var select = this.overlay.querySelector("select[name='camera']");
    if (select) {
      select.focus();
      return;
    }
    this.focusLastControl();
  };

  LiveWall.prototype.focusLastControl = function () {
    var focusable = this.focusableControls();
    if (focusable.length) focusable[focusable.length - 1].focus();
  };

  LiveWall.prototype.showOverlay = function () {
    if (!this.overlay) return;
    if (this.overlayHidden) {
      this.root.setAttribute("data-live-overlay", "shown");
      this.overlay.removeAttribute("inert");
      this.overlayHidden = false;
    }
    this.armHide();
  };

  LiveWall.prototype.suspendHide = function () {
    if (this.hideTimer) window.clearTimeout(this.hideTimer);
    this.hideTimer = null;
  };

  LiveWall.prototype.armHide = function () {
    var wall = this;
    this.suspendHide();
    this.hideTimer = window.setTimeout(() => wall.hideIfIdle(), OVERLAY_IDLE);
  };

  LiveWall.prototype.hideIfIdle = function () {
    this.hideTimer = null;
    if (!this.overlay || this.overlayHidden) return;
    if (this.overlay.contains(document.activeElement)) return;
    this.hideOverlay();
  };

  LiveWall.prototype.hideOverlay = function () {
    if (!this.overlay) return;
    this.suspendHide();
    this.root.setAttribute("data-live-overlay", "hidden");
    this.overlay.setAttribute("inert", "");
    this.overlayHidden = true;
  };

  LiveWall.prototype.enterFullscreen = function () {
    if (!this.root.requestFullscreen) return;
    var entering = this.root.requestFullscreen();
    if (entering && entering.catch) {
      entering.catch(() => {});
    }
  };

  LiveWall.prototype.exitFullscreen = function () {
    if (!document.fullscreenElement) return;
    var exiting = document.exitFullscreen();
    if (exiting && exiting.catch) exiting.catch(() => {});
  };

  LiveWall.prototype.syncFullscreen = function () {
    var active = document.fullscreenElement === this.root;
    this.fullscreen = active;
    document.body.classList.toggle("live-fullscreen-active", Boolean(document.fullscreenElement));
    this.root.setAttribute("data-live-fullscreen-active", active ? "true" : "false");
    if (this.fullscreenButton) this.fullscreenButton.hidden = active;
    if (this.exitButton) this.exitButton.hidden = !active;
    if (active) this.showOverlay();
  };

  LiveWall.prototype.onVisibility = function () {
    var wall = this;
    if (document.hidden) {
      this.suspendHide();
      return;
    }
    this.capacity.reset();
    this.refreshSnapshots();
    this.apply();
    this.armHide();
    window.setTimeout(() => wall.publish(), 0);
  };

  LiveWall.prototype.refreshSnapshots = function () {
    if (document.hidden) return;
    this.panes.forEach((pane) => {
      if (pane.player && pane.live === false) pane.player.refreshSnapshot();
    });
  };

  // setQuality is the owner's choice for the selected pane. It wins for good:
  // the pane becomes manual and no automation touches it again.
  LiveWall.prototype.setQuality = function (quality) {
    var pane = this.selected;
    if (!pane || !pane.player) return;
    pane.manual = quality;
    pane.playerRoot.setAttribute("data-manual-quality", quality);
    this.capacity.setManual(pane.id, quality !== "");
    pane.quality = quality;
    pane.player.setQuality(quality);
    if (quality !== "snapshot") pane.note("");
    this.apply();
    this.updateSelected();
    this.postQuality(pane, quality);
  };

  LiveWall.prototype.postQuality = function (pane, quality) {
    if (!this.form || !window.fetch) return;
    var token = this.form.querySelector("input[name='csrf_token']");
    var body = new URLSearchParams();
    body.set("csrf_token", token ? token.value : "");
    body.set("pane", String(pane.number));
    body.set("action", "quality");
    body.set("quality", quality);
    window
      .fetch(this.form.getAttribute("action"), {
        method: "POST",
        body: body,
        credentials: "same-origin",
        cache: "no-store",
        headers: { "X-Bittern-Inline": "1", "HX-Request": "true" },
      })
      .catch(() => {});
  };

  function qualityLabel(quality) {
    if (quality === "main") return "Main stream";
    if (quality === "sub") return "Sub stream";
    if (quality === "snapshot") return "Snapshots";
    return "Automatic";
  }

  // LivePlayer is one pane's island. It never reads another pane's nodes.
  function LivePlayer(root, wall, pane) {
    this.root = root;
    this.wall = wall;
    this.pane = pane;
    this.video = root.querySelector("video");
    this.snapshot = root.querySelector("[data-player-snapshot]");
    this.cameraName = root.getAttribute("data-camera-name") || "this camera";
    this.mediaBase = root.getAttribute("data-media-base") || "";
    this.snapshotURL = root.getAttribute("data-snapshot-url") || "";
    this.loginURL = root.getAttribute("data-login-url") || "/ui/login";
    this.profile = root.getAttribute("data-profile") || "main";
    this.profileName = root.getAttribute("data-profile-name") || "Main stream";
    this.quality = root.getAttribute("data-quality") || this.profile;
    this.snapshotSeconds = Number(root.getAttribute("data-snapshot-seconds")) || 15;
    this.state = "connecting";
    this.live = true;
    this.started = false;
    this.attempt = 0;
    this.ws = null;
    this.mediaSource = null;
    this.sourceBuffer = null;
    this.queue = [];
    this.lastMediaAt = 0;
    this.ignoreClose = false;
    this.retryTimer = null;
    this.connectTimer = null;
    this.watchdog = null;
    this.hlsStarted = false;
    this.init();
  }

  LivePlayer.prototype.init = function () {
    if (!this.root || !this.video || !this.mediaBase) return;
    var swapped = this.root.closest("[hx-get],[hx-post],[hx-target],[hx-swap],[hx-trigger]");
    if (swapped) {
      this.setState(
        "error",
        "The player could not start",
        "Bittern tried to place the player inside a live update region. Reload the page.",
      );
      return;
    }
    this.root.setAttribute("data-player-state", this.state);
    this.watchdog = window.setInterval(() => this.tick(), 1000);
  };

  // setLive is the wall's decision: live opens one media connection, a
  // snapshot pane shows the latest picture and opens none.
  LivePlayer.prototype.setLive = function (live, stillNote) {
    this.live = live;
    if (live) {
      if (this.state === "snapshot" || !this.started) {
        this.started = true;
        this.attempt = 0;
        this.startMSE();
      }
      return;
    }
    this.showStill(stillNote);
  };

  LivePlayer.prototype.setQuality = function (quality) {
    if (!quality) return;
    if (quality === this.quality) return;
    this.quality = quality;
    this.root.setAttribute("data-quality", quality);
    if (quality === "snapshot") {
      this.showStill("");
      return;
    }
    this.setProfile(quality);
  };

  LivePlayer.prototype.showStill = function (note) {
    this.stopLive();
    if (this.video) this.video.hidden = true;
    if (this.snapshot) {
      this.snapshot.hidden = false;
      this.loadSnapshot();
    }
    this.state = "snapshot";
    this.root.setAttribute("data-player-state", "snapshot");
    this.root.setAttribute("data-player-active", "false");
    this.pane.setState("snapshot");
    this.pane.note(note || "");
  };

  LivePlayer.prototype.loadSnapshot = function () {
    if (!this.snapshot || !this.snapshotURL) return;
    this.snapshot.onerror = () => {
      // Keep the last picture and do not retry faster than the interval.
      if (this.pane.live === false) {
        this.pane.note("The picture did not refresh. It tries again in a few seconds.");
      }
    };
    this.snapshot.src = this.snapshotURL + "?at=" + encodeURIComponent(String(now()));
  };

  LivePlayer.prototype.refreshSnapshot = function () {
    if (this.live !== false) return;
    if (!this.snapshot || this.snapshot.hidden) return;
    this.loadSnapshot();
  };

  LivePlayer.prototype.setState = function (name, title, detail) {
    this.state = name;
    var changed = this.root.getAttribute("data-player-state") !== name;
    this.root.setAttribute("data-player-state", name);
    this.root.setAttribute("data-player-active", name === "playing" ? "true" : "false");
    this.root.setAttribute("data-player-title", title || "");
    if (detail) this.pane.note(detail);
    var failed = name === "offline" || name === "unsupported" || name === "session" || name === "error";
    this.pane.setState(failed ? "failed" : name === "snapshot" ? "snapshot" : "live");
    if (name === "snapshot" && title === "Still picture") {
      this.pane.setState("snapshot");
    }
    if (changed && this.wall.selected === this.pane) {
      this.pane.announce(this.cameraName + ": " + (STATE_LABEL[name] || name));
    }
    this.root.dispatchEvent(
      new CustomEvent("bittern-player-state", { detail: { state: name } }),
    );
  };

  LivePlayer.prototype.startMSE = function () {
    this.clearTimers();
    this.closeSocket();
    this.pane.note("");
    if (!window.MediaSource && typeof window.ManagedMediaSource !== "function") {
      this.startHLS();
      return;
    }
    this.video.hidden = false;
    if (this.snapshot) this.snapshot.hidden = true;
    this.video.removeAttribute("src");
    this.mediaSource = null;
    this.sourceBuffer = null;
    this.queue = [];
    this.hlsStarted = false;
    this.ignoreClose = false;
    if (this.attempt === 0) {
      this.setState("connecting", "Connecting to " + this.cameraName, "");
    } else {
      this.setState("reconnecting", "Reconnecting to " + this.cameraName, "");
    }
    this.openMediaSource();
    this.connectSocket();
  };

  LivePlayer.prototype.openMediaSource = function () {
    var managed = typeof window.ManagedMediaSource === "function";
    var Source = managed ? window.ManagedMediaSource : window.MediaSource;
    if (!Source) return;
    this.mediaSource = new Source();
    if (managed) {
      this.video.disableRemotePlayback = true;
      this.video.srcObject = this.mediaSource;
    } else {
      this.video.src = URL.createObjectURL(this.mediaSource);
      this.video.srcObject = null;
    }
    this.mediaSource.addEventListener(
      "sourceopen",
      () => {
        if (this.ws && this.ws.readyState === 1) this.sendCodecs();
      },
      { once: true },
    );
    this.playVideo();
  };

  LivePlayer.prototype.sendCodecs = function () {
    var Source =
      typeof window.ManagedMediaSource === "function" ? window.ManagedMediaSource : window.MediaSource;
    var supported = CODECS;
    if (Source && typeof Source.isTypeSupported === "function") {
      supported = CODECS.filter((codec) => Source.isTypeSupported('video/mp4; codecs="' + codec + '"'));
      if (supported.length === 0) supported = CODECS;
    }
    this.ws.send(JSON.stringify({ type: "mse", value: supported.join(",") }));
  };

  LivePlayer.prototype.connectSocket = function () {
    var scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
    var url = scheme + "//" + window.location.host + this.mediaBase + "/ws";
    var socket;
    try {
      socket = new WebSocket(url);
    } catch (error) {
      this.handleDrop();
      return;
    }
    this.ws = socket;
    socket.binaryType = "arraybuffer";
    socket.addEventListener("open", () => {
      if (this.mediaSource && this.mediaSource.readyState === "open") this.sendCodecs();
      this.connectTimer = window.setTimeout(() => {
        if (this.state !== "playing") this.markStalled("The stream did not start in time.");
      }, CONNECT_TIMEOUT);
    });
    socket.addEventListener("message", (event) => this.onMessage(event));
    socket.addEventListener("close", () => {
      if (socket !== this.ws) return;
      if (!this.ignoreClose) this.handleDrop();
    });
    socket.addEventListener("error", () => {
      // The close event follows and owns the recovery.
    });
  };

  LivePlayer.prototype.onMessage = function (event) {
    if (typeof event.data === "string") {
      var message = null;
      try {
        message = JSON.parse(event.data);
      } catch (error) {
        return;
      }
      if (!message) return;
      if (message.type === "error") {
        this.closeSocket();
        this.fail(
          "unsupported",
          "This browser cannot play the stream",
          "The camera uses a video format this browser cannot decode. Bittern is showing the latest picture instead.",
        );
        return;
      }
      if (message.type === "mse" && message.value) this.addSourceBuffer(message.value);
      return;
    }
    this.lastMediaAt = now();
    if (this.state !== "playing") {
      this.clearConnectTimer();
      this.pane.note("");
      this.setState(
        "playing",
        "Live from " + this.cameraName,
        "",
      );
    }
    this.appendMedia(event.data);
  };

  LivePlayer.prototype.addSourceBuffer = function (mime) {
    if (!this.mediaSource) return;
    try {
      if (this.sourceBuffer) this.mediaSource.removeSourceBuffer(this.sourceBuffer);
      this.sourceBuffer = this.mediaSource.addSourceBuffer(mime);
      this.sourceBuffer.mode = "segments";
      this.sourceBuffer.addEventListener("updateend", () => this.flushMedia());
    } catch (error) {
      this.fail(
        "unsupported",
        "This browser cannot play the stream",
        "The camera stream codec is not supported here. Bittern is showing the latest picture instead.",
      );
    }
  };

  LivePlayer.prototype.appendMedia = function (data) {
    if (!this.sourceBuffer) return;
    this.queue.push(data);
    this.flushMedia();
  };

  LivePlayer.prototype.flushMedia = function () {
    var buffer = this.sourceBuffer;
    if (!buffer || buffer.updating) return;
    var next = this.queue.shift();
    if (!next) return;
    try {
      buffer.appendBuffer(next);
    } catch (error) {
      this.queue = [];
      if (error && error.name === "QuotaExceededError") {
        try {
          buffer.remove(0, Math.max(0, buffer.buffered.end(0) - 1));
        } catch (ignored) {
          // The next segment starts a fresh buffer.
        }
      }
    }
  };

  LivePlayer.prototype.sample = function () {
    if (!this.live) return null;
    if (!this.video || typeof this.video.getVideoPlaybackQuality !== "function") return null;
    var quality = this.video.getVideoPlaybackQuality();
    if (!quality) return null;
    return { dropped: quality.droppedVideoFrames, total: quality.totalVideoFrames };
  };

  LivePlayer.prototype.tick = function () {
    if (this.state !== "playing") return;
    if (!this.lastMediaAt || now() - this.lastMediaAt > STALL_TIMEOUT) {
      this.markStalled("The stream stopped sending video.");
    }
  };

  LivePlayer.prototype.markStalled = function (reason) {
    if (this.state === "stalled" || this.state === "offline") return;
    this.clearConnectTimer();
    this.setState("stalled", "The stream stopped sending video", reason);
    this.retryTimer = window.setTimeout(() => this.handleDrop(), 2000);
  };

  LivePlayer.prototype.handleDrop = function () {
    if (this.state === "session" || this.state === "offline") return;
    this.closeSocket();
    this.clearTimers();
    this.root.setAttribute("data-player-active", "false");
    this.probeSession().then((ended) => {
      if (ended) {
        this.fail(
          "session",
          "Your session ended",
          "Sign in again to watch " + this.cameraName + ".",
        );
        return;
      }
      this.recover();
    });
  };

  LivePlayer.prototype.probeSession = function () {
    if (!this.snapshotURL || !window.fetch) return Promise.resolve(false);
    return window
      .fetch(this.snapshotURL, {
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "image/jpeg" },
      })
      .then((response) => response.status === 401 || response.status === 403)
      .catch(() => false);
  };

  LivePlayer.prototype.recover = function () {
    this.attempt += 1;
    if (this.attempt > MAX_RETRIES) {
      this.fallback();
      return;
    }
    var delay = Math.min(1000 * Math.pow(2, this.attempt - 1), 5000);
    var seconds = Math.round(delay / 1000);
    this.setState(
      "reconnecting",
      "Reconnecting to " + this.cameraName,
      "Trying again in " + seconds + (seconds === 1 ? " second" : " seconds") + ". Attempt " + this.attempt + " of " + MAX_RETRIES + ".",
    );
    this.clearRetryTimer();
    this.retryTimer = window.setTimeout(() => {
      this.retryTimer = null;
      this.startMSE();
    }, delay);
  };

  LivePlayer.prototype.fallback = function () {
    if (this.canPlayHLS()) {
      this.startHLS();
      return;
    }
    this.fail(
      "offline",
      this.cameraName + " is offline",
      this.cameraName +
        " is offline. Bittern could not start the live stream after " +
        MAX_RETRIES +
        " attempts. The camera may be off, or the recording service may be stopped.",
    );
  };

  LivePlayer.prototype.canPlayHLS = function () {
    return !!(
      this.video &&
      this.video.canPlayType &&
      this.video.canPlayType("application/vnd.apple.mpegurl")
    );
  };

  LivePlayer.prototype.startHLS = function () {
    this.clearTimers();
    this.closeSocket();
    if (!this.canPlayHLS()) {
      this.fallback();
      return;
    }
    this.attempt = 0;
    this.hlsStarted = false;
    this.lastMediaAt = now();
    this.video.hidden = false;
    if (this.snapshot) this.snapshot.hidden = true;
    this.setState("connecting", "Connecting to " + this.cameraName, "");
    this.video.srcObject = null;
    this.video.src = this.mediaBase + "/stream.m3u8";
    this.playVideo();
    var started = false;
    var onPlaying = () => {
      started = true;
      this.hlsStarted = true;
      this.lastMediaAt = now();
      this.pane.note("");
      this.setState("playing", "Live from " + this.cameraName, "");
    };
    this.video.addEventListener("playing", onPlaying);
    this.video.addEventListener("timeupdate", () => {
      if (this.hlsStarted) this.lastMediaAt = now();
    });
    this.video.addEventListener("stalled", () => {
      if (this.hlsStarted) this.markStalled("The HLS stream stopped.");
    });
    this.video.addEventListener(
      "error",
      () => {
        if (!started) {
          this.fail(
            "offline",
            this.cameraName + " is offline",
            this.cameraName +
              " is offline. Bittern could not start the HLS stream. The camera may be off, or the recording service may be stopped.",
          );
        } else {
          this.handleDrop();
        }
      },
      { once: true },
    );
    this.connectTimer = window.setTimeout(() => {
      if (!started) {
        this.fail(
          "offline",
          this.cameraName + " is offline",
          this.cameraName +
            " is offline. Bittern could not start the HLS stream in time. The camera may be off, or the recording service may be stopped.",
        );
      }
    }, HLS_START_TIMEOUT);
  };

  // fail shows the snapshot and the one sentence that names what happened.
  LivePlayer.prototype.fail = function (state, title, detail) {
    this.closeSocket();
    this.clearTimers();
    this.clearVideo();
    this.root.setAttribute("data-player-active", "false");
    if (this.video) this.video.hidden = true;
    if (this.snapshot) {
      this.snapshot.hidden = false;
      this.loadSnapshot();
    }
    this.setState(state, title, detail);
    this.pane.setState("failed");
    this.pane.note(detail);
  };

  LivePlayer.prototype.setProfile = function (profile) {
    if (!profile || profile === this.profile) {
      if (profile && this.live && !this.started) {
        this.started = true;
        this.startMSE();
      }
      return;
    }
    this.clearTimers();
    this.closeSocket();
    this.profile = profile;
    this.profileName = profile === "sub" ? "Sub stream" : "Main stream";
    this.root.setAttribute("data-profile", profile);
    this.root.setAttribute("data-profile-name", this.profileName);
    this.mediaBase = this.mediaBase.replace(/\/[^/]+$/, "/" + encodeURIComponent(profile));
    this.root.setAttribute("data-media-base", this.mediaBase);
    this.snapshotURL = this.snapshotURL.replace(
      /\/[^/]+\/snapshot\.jpg$/,
      "/" + encodeURIComponent(profile) + "/snapshot.jpg",
    );
    this.root.setAttribute("data-snapshot-url", this.snapshotURL);
    each("[data-profile-link]", document, (link) => {
      if (link.getAttribute("data-profile-link") === profile) {
        link.setAttribute("aria-current", "true");
      } else {
        link.removeAttribute("aria-current");
      }
    });
    if (!this.live) return;
    this.started = true;
    this.attempt = 0;
    this.setState(
      "switching",
      "Switching to " + this.profileName,
      "Switching to " + this.profileName.toLowerCase() + ".",
    );
    window.setTimeout(() => this.startMSE(), SWITCH_DELAY);
  };

  LivePlayer.prototype.stopLive = function () {
    this.clearTimers();
    this.closeSocket();
    this.clearVideo();
    this.started = false;
    this.root.setAttribute("data-player-active", "false");
  };

  LivePlayer.prototype.clearVideo = function () {
    if (!this.video) return;
    try {
      this.video.pause();
    } catch (ignored) {
      // A late video element may already be gone.
    }
    this.video.removeAttribute("src");
    this.video.srcObject = null;
    if (this.mediaSource && this.mediaSource.readyState === "open") {
      try {
        this.mediaSource.endOfStream();
      } catch (ignored) {
        // The media source may already be closed.
      }
    }
  };

  LivePlayer.prototype.closeSocket = function () {
    if (!this.ws) return;
    var socket = this.ws;
    this.ws = null;
    this.ignoreClose = true;
    try {
      socket.close();
    } catch (ignored) {
      // The socket may already be closed.
    }
    this.ignoreClose = false;
  };

  LivePlayer.prototype.playVideo = function () {
    if (!this.video) return;
    var playing = this.video.play();
    if (playing && typeof playing.catch === "function") {
      playing.catch(() => {
        // Muted autoplay is allowed in the product; a rejected play is not a
        // stream failure and the next media event retries it.
      });
    }
  };

  LivePlayer.prototype.clearRetryTimer = function () {
    if (this.retryTimer) window.clearTimeout(this.retryTimer);
    this.retryTimer = null;
  };

  LivePlayer.prototype.clearConnectTimer = function () {
    if (this.connectTimer) window.clearTimeout(this.connectTimer);
    this.connectTimer = null;
  };

  LivePlayer.prototype.clearTimers = function () {
    this.clearRetryTimer();
    this.clearConnectTimer();
  };

  function boot() {
    var root = document.querySelector("[data-live-grid]");
    if (!root) return;
    var wall = new LiveWall(root);
    wall.init();
    window.BitternLiveWall = wall;
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
