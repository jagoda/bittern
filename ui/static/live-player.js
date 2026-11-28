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

  var ONE_STREAM_REASON =
    "One stream. This camera does not report a second stream, so main and sub use the same picture.";
  var FULLSCREEN_SWIPE = 120;
  var NOTICE_HOLD = 8000;

  function qualityLabel(quality) {
    if (quality === "main") return "Main stream";
    if (quality === "sub") return "Sub stream";
    if (quality === "snapshot") return "Snapshots";
    return "Automatic";
  }

  function qualityShortLabel(quality) {
    if (quality === "main") return "Main";
    if (quality === "sub") return "Sub";
    if (quality === "snapshot") return "Still";
    return "Automatic";
  }

  function qualitySuffix(quality) {
    if (quality === "main" || quality === "sub") return " stream";
    if (quality === "snapshot") return " picture";
    return "";
  }

  // Pane is one pane slot of the wall. It owns one player island and one
  // overlay on that pane. The overlay holds the pane's stream selector and
  // camera chooser; a hidden overlay is inert and click-through.
  function Pane(root, wall) {
    this.root = root;
    this.wall = wall;
    this.number = Number(root.getAttribute("data-live-pane")) || 0;
    this.id = "pane-" + this.number;
    this.overlay = root.querySelector("[data-live-pane-overlay]");
    this.content = root.querySelector("[data-live-pane-content]");
    this.placeholder = root.querySelector("[data-live-placeholder]");
    this.nameElement = root.querySelector("[data-live-pane-name]");
    this.currentElement = root.querySelector("[data-live-current-stream]");
    this.currentPrefix = root.querySelector("[data-live-current-prefix]");
    this.currentValue = root.querySelector("[data-live-current-stream-value]");
    this.currentSuffix = root.querySelector("[data-live-current-suffix]");
    this.noteElement = root.querySelector("[data-player-note]");
    this.announceElement = root.querySelector("[data-player-announce]");
    this.qualityBlock = root.querySelector("[data-live-quality-block]");
    this.qualityForm = root.querySelector("[data-live-quality-form]");
    this.qualityGroup = root.querySelector("[data-live-quality-group]");
    this.qualitySingle = root.querySelector("[data-live-quality-single]");
    this.qualityInputs = root.querySelectorAll("[data-live-quality-choice]");
    this.oneStreamHint = root.querySelector("[data-live-one-stream]");
    this.cameraForm = root.querySelector("[data-live-camera-form]");
    this.cameraSelect = root.querySelector("[data-live-camera-select]");
    this.playerRoot = null;
    this.player = null;
    this.live = null;
    this.manual = "";
    this.quality = "snapshot";
    this.autoQuality = wall.autoQuality();
    this.stillNote = "";
    this.oneStream = false;
    this.name = "";
    this.cameraID = "";
    this.load();
  }

  // load reads the pane's actual DOM after a render, a layout change or a
  // camera move. It is the one place a Pane picks up a player island.
  Pane.prototype.load = function () {
    this.visible = this.root.getAttribute("data-live-visible") !== "false" && !this.root.hidden;
    this.occupied = this.root.getAttribute("data-live-occupied") === "true";
    this.playerRoot = this.root.querySelector("[data-live-player]");
    this.cameraID = this.root.getAttribute("data-camera-id") || "";
    this.oneStream = this.root.getAttribute("data-one-stream") === "true";
    this.autoQuality = this.wall.autoQuality();
    if (this.playerRoot) {
      this.manual = this.playerRoot.getAttribute("data-manual-quality") || "";
      this.quality = this.playerRoot.getAttribute("data-quality") || this.autoQuality;
      this.name = this.playerRoot.getAttribute("data-camera-name") || "";
      this.player = new LivePlayer(this.playerRoot, this.wall, this);
    } else {
      this.player = null;
      this.manual = "";
      this.quality = "snapshot";
      this.live = false;
      this.name = this.root.getAttribute("aria-label") || "Empty pane " + this.number;
    }
    this.root.setAttribute("data-live-occupied", this.occupied ? "true" : "false");
    if (this.placeholder) this.placeholder.hidden = this.occupied;
    this.updateStreamUI();
    this.updateCameraUI();
  };

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
    window.setTimeout(() => {
      if (this.announceElement) this.announceElement.textContent = text;
    }, 30);
  };

  // updateStreamUI is the current-stream indicator and the icon state. The
  // visible word is short enough for a phone pane; the hidden prefix and suffix
  // complete the same sentence for assistive tech. The selected choice carries
  // a check glyph and a thicker border, not colour alone.
  Pane.prototype.updateStreamUI = function () {
    if (this.qualityBlock) this.qualityBlock.hidden = !this.occupied;
    var oneStream = this.oneStream;
    if (this.qualityGroup) this.qualityGroup.hidden = oneStream || !this.occupied;
    if (this.qualitySingle) this.qualitySingle.hidden = !oneStream || !this.occupied;
    var shown = this.live === false ? "snapshot" : this.quality;
    Array.prototype.forEach.call(this.qualityInputs, (input) => {
      input.disabled = oneStream || !this.occupied;
      input.checked = !oneStream && input.value === shown;
    });
    each("[data-profile-link]", this.root, (link) => {
      if (!oneStream && link.getAttribute("data-profile-link") === shown) {
        link.setAttribute("aria-current", "true");
      } else {
        link.removeAttribute("aria-current");
      }
    });
    if (this.oneStreamHint) {
      this.oneStreamHint.hidden = !oneStream;
      this.oneStreamHint.textContent = ONE_STREAM_REASON;
    }
    if (!this.currentElement) return;
    if (this.currentPrefix) this.currentPrefix.hidden = !this.occupied;
    if (this.currentSuffix) this.currentSuffix.hidden = true;
    if (!this.occupied) {
      if (this.currentValue) this.currentValue.textContent = "No camera assigned";
      return;
    }
    if (oneStream) {
      if (this.currentValue) this.currentValue.textContent = "One stream";
      return;
    }
    if (this.currentValue) this.currentValue.textContent = qualityShortLabel(shown);
    if (this.currentSuffix) {
      this.currentSuffix.textContent = qualitySuffix(shown);
      this.currentSuffix.hidden = false;
    }
  };

  Pane.prototype.updateCameraUI = function () {
    if (this.nameElement) {
      this.nameElement.textContent = this.occupied ? this.name : "Empty pane " + this.number;
    }
    if (this.cameraSelect) this.cameraSelect.value = this.cameraID;
    this.root.setAttribute(
      "aria-label",
      this.occupied ? this.name : "Choose a camera for pane " + this.number,
    );
    // An empty pane is a button only while its chooser is hidden. Once the
    // chooser is shown the pane is a group, so a form is never nested in a
    // button.
    this.root.setAttribute(
      "role",
      !this.occupied && !this.isOverlayShown() ? "button" : "group",
    );
  };

  Pane.prototype.moveLabel = function () {
    if (this.placeholder) this.placeholder.hidden = this.occupied;
  };

  // attachPlayer puts a player island into this pane, either moved from the
  // camera's old pane or cloned from its template. It never reloads the page.
  Pane.prototype.attachPlayer = function (node, cameraID, cameraName, oneStream) {
    this.clearPlayer();
    var quality = this.wall.autoQuality();
    node.setAttribute("data-live-player", "true");
    node.setAttribute("data-pane", String(this.number));
    node.setAttribute("data-camera-id", cameraID);
    node.setAttribute("data-camera-name", cameraName);
    node.setAttribute("data-quality", quality);
    node.setAttribute("data-manual-quality", "");
    node.setAttribute("data-profile", quality);
    node.setAttribute("data-profile-name", quality === "main" ? "Main stream" : "Sub stream");
    node.setAttribute("data-one-stream", oneStream ? "true" : "false");
    node.setAttribute("data-media-base", "/ui/live/media/" + encodeURIComponent(cameraID) + "/" + quality);
    node.setAttribute(
      "data-snapshot-url",
      "/ui/live/media/" + encodeURIComponent(cameraID) + "/" + quality + "/snapshot.jpg",
    );
    node.setAttribute("data-status-url", "/ui/live/" + encodeURIComponent(cameraID) + "/status");
    node.setAttribute("data-login-url", "/ui/login?next=" + encodeURIComponent("/ui/live"));
    node.id = "live-tile-player-" + this.number;
    var video = node.querySelector("video");
    if (video) video.id = node.id + "-video";
    this.root.removeAttribute("hidden");
    this.root.removeAttribute("data-live-pane-state");
    this.root.setAttribute("data-live-occupied", "true");
    this.root.setAttribute("data-camera-id", cameraID);
    this.root.setAttribute("data-camera-name", cameraName);
    this.root.setAttribute("data-quality", quality);
    this.root.setAttribute("data-manual-quality", "");
    this.root.setAttribute("data-one-stream", oneStream ? "true" : "false");
    this.root.insertBefore(node, this.content || this.overlay);
    this.load();
    this.live = null;
    this.stillNote = "";
    this.moveLabel();
  };

  Pane.prototype.clearPlayer = function () {
    if (this.player && this.player.dispose) this.player.dispose();
    if (this.playerRoot && this.playerRoot.parentNode) {
      this.playerRoot.parentNode.removeChild(this.playerRoot);
    }
    this.playerRoot = null;
    this.player = null;
    this.live = false;
    this.occupied = false;
    this.cameraID = "";
    this.name = "";
    this.manual = "";
    this.quality = "snapshot";
    this.oneStream = false;
    this.stillNote = "";
    this.root.removeAttribute("data-live-pane-state");
    this.root.setAttribute("data-live-occupied", "false");
    this.root.setAttribute("data-camera-id", "");
    this.root.setAttribute("data-camera-name", "");
    this.root.setAttribute("data-quality", "snapshot");
    this.root.setAttribute("data-manual-quality", "");
    this.root.setAttribute("data-one-stream", "false");
    this.moveLabel();
    this.note("");
    this.updateStreamUI();
    this.updateCameraUI();
  };

  // takePlayer removes the island from this pane and returns it, so a camera
  // moving pane keeps its video node and its stream does not get a second copy.
  Pane.prototype.takePlayer = function () {
    var node = this.playerRoot;
    if (this.player && this.player.dispose) this.player.dispose();
    if (node && node.parentNode) node.parentNode.removeChild(node);
    this.playerRoot = null;
    this.player = null;
    this.live = false;
    this.occupied = false;
    this.cameraID = "";
    this.name = "";
    this.manual = "";
    this.quality = "snapshot";
    this.oneStream = false;
    this.stillNote = "";
    this.root.removeAttribute("data-live-pane-state");
    this.root.setAttribute("data-live-occupied", "false");
    this.root.setAttribute("data-camera-id", "");
    this.root.setAttribute("data-camera-name", "");
    this.root.setAttribute("data-quality", "snapshot");
    this.root.setAttribute("data-manual-quality", "");
    this.root.setAttribute("data-one-stream", "false");
    this.moveLabel();
    this.note("");
    this.updateStreamUI();
    this.updateCameraUI();
    return node;
  };

  Pane.prototype.setOverlayShown = function (shown) {
    if (!this.overlay) return;
    this.overlay.setAttribute("data-live-pane-overlay-state", shown ? "shown" : "hidden");
    if (shown) this.overlay.removeAttribute("inert");
    else this.overlay.setAttribute("inert", "");
    if (!this.occupied) this.root.setAttribute("role", shown ? "group" : "button");
  };

  Pane.prototype.isOverlayShown = function () {
    return this.overlay && this.overlay.getAttribute("data-live-pane-overlay-state") === "shown";
  };

  // LiveWall owns the wall: capacity, selection, the pane overlays, the wall
  // controls and full screen.
  function LiveWall(root) {
    this.root = root;
    this.panes = [];
    this.selected = null;
    this.activePane = null;
    this.layout = Number(root.getAttribute("data-layout")) || 1;
    this.storedLayout = root.getAttribute("data-live-layout-value") || String(this.layout);
    this.autoLayout = Number(root.getAttribute("data-live-auto-layout")) || this.layout;
    this.single = !root.querySelector("[data-live-layout-form]");
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
    this.layoutForm = root.querySelector("[data-live-layout-form]");
    this.layoutRadios = root.querySelector("[data-live-layout-radios]");
    this.layoutButtons = root.querySelector("[data-live-layout-buttons]");
    this.stateRegion = root.querySelector("[data-live-camera-state-region]");
    this.fullscreenButton = root.querySelector("[data-live-fullscreen]");
    this.fullscreenHint = root.querySelector("[data-live-fullscreen-hint]");
    this.fullscreenHintTimer = null;
    this.notice = root.querySelector("[data-live-notice]");
    this.noticeTimer = null;
    this.noticeShown = false;
    this.touchStart = null;
    this.pointerDownHadOverlay = false;
    this.pointerDownInOverlay = false;
  }

  LiveWall.prototype.init = function () {
    var wall = this;
    each("[data-live-pane]", this.root, (element) => {
      wall.panes.push(new Pane(element, wall));
    });
    if (!this.panes.length) return;
    this.select(this.firstVisible() || this.panes[0], false);
    this.enableEnhancements();
    this.initCapacity();
    this.apply();
    if (this.fullscreenButton) {
      this.fullscreenButton.addEventListener("click", () => wall.requestFullscreen());
    }
    document.addEventListener("fullscreenchange", () => this.syncFullscreen());
    this.root.setAttribute("data-live-enhanced", "true");
    document.addEventListener("keydown", (event) => this.onDocumentKeydown(event), true);
    this.root.addEventListener("keydown", (event) => this.onKeydown(event));
    this.root.addEventListener("click", (event) => this.onClick(event));
    this.root.addEventListener("pointerdown", (event) => this.onPointerDown(event), true);
    this.root.addEventListener("pointerup", (event) => this.onPointerUp(event), true);
    this.root.addEventListener("pointermove", (event) => this.onPointerMove(event), true);
    this.root.addEventListener("focusin", (event) => this.onFocusIn(event), true);
    this.root.addEventListener("focusout", (event) => this.onFocusOut(event), true);
    window.addEventListener("resize", () => this.syncPrior());
    document.addEventListener("visibilitychange", () => this.onVisibility());
    this.syncFullscreen();
    this.hideAllOverlays();
  };

  LiveWall.prototype.autoQuality = function () {
    return this.layout === 1 ? "main" : "sub";
  };

  LiveWall.prototype.firstVisible = function () {
    for (var index = 0; index < this.panes.length; index += 1) {
      if (this.panes[index].visible) return this.panes[index];
    }
    return null;
  };

  LiveWall.prototype.visiblePanes = function () {
    return this.panes.filter((pane) => pane.visible);
  };

  // enableEnhancements makes every wall control act in place. The forms stay
  // in the markup for the no-JavaScript path; here their submits are captured
  // and the wall changes without a navigation or a reload.
  LiveWall.prototype.enableEnhancements = function () {
    var wall = this;
    if (this.layoutRadios) this.layoutRadios.hidden = false;
    if (this.layoutButtons) this.layoutButtons.hidden = true;
    each("[data-live-inline-hide]", this.root, (element) => {
      element.hidden = true;
    });
    if (this.layoutForm) {
      this.layoutForm.addEventListener("submit", (event) => {
        event.preventDefault();
        var checked = wall.layoutForm.querySelector("input[name='layout']:checked");
        if (checked) wall.applyLayout(checked.value);
      });
      this.layoutForm.addEventListener("change", (event) => {
        if (event.target && event.target.name === "layout") wall.applyLayout(event.target.value);
      });
    }
    this.panes.forEach((pane) => {
      Array.prototype.forEach.call(pane.qualityInputs, (input) => {
        input.addEventListener("change", () => {
          if (input.checked) wall.setQuality(pane, input.value);
        });
      });
      if (pane.qualityForm) {
        pane.qualityForm.addEventListener("submit", (event) => {
          event.preventDefault();
          var checked = pane.qualityForm.querySelector("input[name='quality']:checked");
          if (checked && !checked.disabled) wall.setQuality(pane, checked.value);
        });
      }
      if (pane.cameraSelect) {
        pane.cameraSelect.addEventListener("change", () => {
          wall.assignCamera(pane, pane.cameraSelect.value);
        });
      }
      if (pane.cameraForm) {
        pane.cameraForm.addEventListener("submit", (event) => {
          event.preventDefault();
          if (pane.cameraSelect) wall.assignCamera(pane, pane.cameraSelect.value);
        });
      }
    });
    this.root.addEventListener("click", (event) => {
      var link = event.target.closest("[data-profile-link]");
      if (!link) return;
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      wall.setProfileLink(link);
    });
  };

  // setProfileLink keeps the single-camera route's shareable main and sub link
  // control. It switches at runtime and updates the URL without a navigation.
  LiveWall.prototype.setProfileLink = function (link) {
    var pane = this.selected;
    if (!pane || !pane.player) return;
    var profile = link.getAttribute("data-profile-link");
    this.setQuality(pane, profile);
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

  // view is the capacity model's input: every visible pane's live state,
  // quality, frame counters and rendered area, in reading order.
  LiveWall.prototype.view = function (at) {
    return {
      panes: this.panes
        .filter((pane) => pane.visible && pane.player)
        .map((pane) => {
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
      pane.updateStreamUI();
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
      pane.updateStreamUI();
    }
  };

  // apply turns the capacity model's decisions into live or snapshot panes. A
  // manual choice always wins: main and sub are always live, snapshot is never
  // live. A hidden pane holds no connection.
  LiveWall.prototype.apply = function () {
    var wall = this;
    this.panes.forEach((pane) => {
      if (!pane.player) return;
      if (!pane.visible) {
        if (pane.live !== false) {
          pane.live = false;
          pane.player.stopLive();
          pane.player.state = "snapshot";
          pane.playerRoot.setAttribute("data-player-state", "snapshot");
          pane.playerRoot.setAttribute("data-player-active", "false");
          pane.setState("snapshot");
        }
        return;
      }
      var live;
      if (pane.manual === "snapshot") live = false;
      else if (pane.manual === "main" || pane.manual === "sub") live = true;
      else live = !wall.capacity.isDemoted(pane.id);
      if (live !== pane.live) {
        pane.live = live;
        var stillNote = "";
        if (!live && pane.manual !== "snapshot") {
          stillNote = pane.stillNote || "Still picture. This screen is above its live capacity.";
        }
        pane.player.setLive(live, stillNote);
      }
      pane.updateStreamUI();
    });
    this.publish();
  };

  LiveWall.prototype.publish = function () {
    var live = 0;
    this.panes.forEach((pane) => {
      if (pane.visible && pane.live) live += 1;
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
  // focus ring and the pane's overlay, never by a border.
  LiveWall.prototype.select = function (pane, focus) {
    if (!pane || !pane.visible) return;
    this.selected = pane;
    this.panes.forEach((candidate) => {
      candidate.setSelected(candidate === pane);
      if (candidate !== pane && candidate.announceElement) {
        candidate.announceElement.textContent = "";
      }
    });
    this.updateCameraStateURL();
    if (focus) pane.root.focus();
  };

  LiveWall.prototype.updateCameraStateURL = function () {
    var pane = this.selected;
    if (!this.stateRegion) return;
    if (pane && pane.playerRoot) {
      var status = pane.playerRoot.getAttribute("data-status-url") || "";
      if (status) {
        this.stateRegion.setAttribute("hx-get", status);
        this.stateRegion.setAttribute("hx-trigger", "every 15s");
        this.stateRegion.setAttribute("hx-target", "this");
        this.stateRegion.setAttribute("hx-swap", "innerHTML");
      }
    } else {
      this.stateRegion.removeAttribute("hx-get");
      this.stateRegion.removeAttribute("hx-trigger");
      this.stateRegion.removeAttribute("hx-target");
      this.stateRegion.removeAttribute("hx-swap");
    }
  };

  LiveWall.prototype.onPointerMove = function (event) {
    var at = now();
    if (at - this.lastPointerAt < POINTER_THROTTLE) return;
    this.lastPointerAt = at;
    var pane = this.paneForEvent(event);
    if (pane) this.showPaneOverlay(pane);
    this.showOverlay();
  };

  LiveWall.prototype.paneForEvent = function (event) {
    var element = event.target.closest ? event.target.closest("[data-live-pane]") : null;
    return element ? this.paneFor(element) : null;
  };

  LiveWall.prototype.onPointerDown = function (event) {
    if (event.pointerType === "touch" || event.pointerType === "pen") {
      this.touchStart = { x: event.clientX, y: event.clientY };
    }
    this.pointerDownHadOverlay = this.anyOverlayShown();
    this.pointerDownInOverlay = Boolean(
      event.target.closest && event.target.closest("[data-live-overlay-panel], [data-live-pane-overlay]"),
    );
    var pane = this.paneForEvent(event);
    if (!pane) return;
    var inOverlay = Boolean(event.target.closest("[data-live-pane-overlay]"));
    this.select(pane, pane.occupied && !inOverlay);
    this.showPaneOverlay(pane);
    this.showOverlay();
    if (!pane.occupied && !inOverlay) {
      if (pane.cameraSelect) window.setTimeout(() => pane.cameraSelect.focus(), 50);
    }
  };

  // A click that starts with an overlay already shown dismisses it. On a video
  // the pane overlay and the wall overlay both close; anywhere else on the wall
  // closes the wall overlay. An empty pane is opening its chooser, and a click
  // that starts with the overlays hidden is a reveal, so neither dismisses.
  LiveWall.prototype.onClick = function (event) {
    if (this.pointerDownInOverlay) return;
    if (event.target.closest && event.target.closest("[data-live-overlay-panel], [data-live-pane-overlay]")) {
      return;
    }
    if (!this.pointerDownHadOverlay) return;
    var pane = this.paneForEvent(event);
    var video = event.target.closest ? event.target.closest("video, [data-player-snapshot]") : null;
    if (video) {
      this.hideAllOverlays();
      return;
    }
    if (pane && !pane.occupied) return;
    this.hideWallOverlay();
  };

  LiveWall.prototype.hideWallOverlay = function () {
    if (!this.overlay) return;
    this.overlay.setAttribute("inert", "");
    this.overlayHidden = true;
    this.root.setAttribute("data-live-overlay", "hidden");
  };

  // A touch swipe down of at least 120 CSS pixels exits full screen. It is the
  // gesture the hint names when full screen is entered.
  LiveWall.prototype.onPointerUp = function (event) {
    if (!this.touchStart) return;
    var start = this.touchStart;
    this.touchStart = null;
    if (!this.fullscreen) return;
    var dx = event.clientX - start.x;
    var dy = event.clientY - start.y;
    if (dy >= FULLSCREEN_SWIPE && Math.abs(dx) <= dy) {
      event.preventDefault();
      this.exitFullscreen();
    }
  };

  LiveWall.prototype.onFocusIn = function (event) {
    if (this.suppressFocusReveal) return;
    var pane = this.paneForEvent(event);
    if (pane) {
      this.select(pane, false);
      this.showPaneOverlay(pane);
      return;
    }
    if (this.overlay && this.overlay.contains(event.target)) this.showOverlay();
  };

  LiveWall.prototype.onFocusOut = function (event) {
    var wall = this;
    window.setTimeout(() => {
      if (!wall.root.contains(document.activeElement)) {
        // Full screen moves focus to the document while the top-layer element
        // settles. That is not the owner leaving the wall.
        if (wall.fullscreen && (document.activeElement === document.body || !document.activeElement)) {
          wall.showOverlay();
          if (wall.selected) wall.showPaneOverlay(wall.selected);
          return;
        }
        wall.hideAllOverlays();
        return;
      }
      wall.armHide();
    }, 0);
  };

  LiveWall.prototype.paneFor = function (element) {
    for (var index = 0; index < this.panes.length; index += 1) {
      if (this.panes[index].root === element) return this.panes[index];
    }
    return null;
  };

  // Escape exits full screen from anywhere, overlay open or not. Outside full
  // screen it hides the overlays and returns focus to the selected pane.
  LiveWall.prototype.onDocumentKeydown = function (event) {
    if (event.key !== "Escape") return;
    if (document.fullscreenElement) {
      event.preventDefault();
      this.exitFullscreen();
      return;
    }
    if (!this.anyOverlayShown()) return;
    event.preventDefault();
    this.hideAllOverlays();
    if (this.selected) {
      this.suppressFocusReveal = true;
      this.selected.root.focus();
      window.setTimeout(() => {
        this.suppressFocusReveal = false;
      }, 0);
    }
  };

  LiveWall.prototype.onKeydown = function (event) {
    var pane = this.selected;
    var overlay = event.target.closest ? event.target.closest("[data-live-pane-overlay]") : null;
    if (overlay && event.key === "Tab") {
      var owner = this.paneForEvent(event);
      if (owner) {
        var controls = this.focusableIn(owner.overlay);
        if (controls.length) {
          if (!event.shiftKey && document.activeElement === controls[controls.length - 1]) {
            event.preventDefault();
            this.showOverlay();
            this.focusFirstWallControl();
            return;
          }
          if (event.shiftKey && document.activeElement === controls[0]) {
            event.preventDefault();
            this.select(owner, true);
            return;
          }
        }
      }
      return;
    }
    if (this.overlay && this.overlay.contains(event.target) && event.key === "Tab") {
      var wallControls = this.focusableIn(this.overlay);
      if (wallControls.length) {
        if (!event.shiftKey && document.activeElement === wallControls[wallControls.length - 1]) {
          event.preventDefault();
          if (this.selected) this.select(this.selected, true);
          return;
        }
        if (event.shiftKey && document.activeElement === wallControls[0]) {
          event.preventDefault();
          if (this.selected) {
            this.showPaneOverlay(this.selected);
            this.focusLastPaneControl(this.selected);
          }
          return;
        }
      }
      return;
    }
    var onPane = pane && event.target === pane.root;
    if (!onPane) return;
    if (event.key === "Shift" || event.key === "Control" || event.key === "Alt" || event.key === "Meta") {
      return;
    }
    if (event.key !== "Tab" && event.key !== "Escape") {
      this.showPaneOverlay(pane);
      this.showOverlay();
    }
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
        var first = this.firstVisible();
        if (first) this.select(first, true);
        return;
      case "End":
        event.preventDefault();
        var visible = this.visiblePanes();
        if (visible.length) this.select(visible[visible.length - 1], true);
        return;
      case "Enter":
      case " ":
        if (!pane.occupied) {
          event.preventDefault();
          this.showPaneOverlay(pane);
          this.showOverlay();
          if (pane.cameraSelect) pane.cameraSelect.focus();
        }
        return;
      case "Tab":
        if (!pane.isOverlayShown() || event.shiftKey) {
          event.preventDefault();
          this.showPaneOverlay(pane);
          this.showOverlay();
          if (event.shiftKey) this.focusLastPaneControl(pane);
          else this.focusFirstPaneControl(pane);
        }
        return;
      default:
        if (event.key && event.key.length === 1) {
          this.showPaneOverlay(pane);
          this.showOverlay();
        }
    }
  };

  // move is the arrow-key geometry: the nearest visible pane in that
  // direction by the centre points of the rendered areas.
  LiveWall.prototype.move = function (direction) {
    var from = centreOf(this.selected.root);
    var best = null;
    var bestScore = Infinity;
    this.panes.forEach((pane) => {
      if (pane === this.selected || !pane.visible) return;
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
    if (best) {
      this.select(best, true);
      this.showPaneOverlay(best);
    }
  };

  LiveWall.prototype.focusableIn = function (element) {
    if (!element) return [];
    return Array.prototype.filter.call(
      element.querySelectorAll("button, input:not([type='hidden']), select, a[href], [tabindex='0']"),
      (node) => {
        if (node.disabled || node.hidden) return false;
        var style = window.getComputedStyle(node);
        if (style.display === "none" || style.visibility === "hidden") return false;
        return node.getClientRects().length > 0;
      },
    );
  };

  LiveWall.prototype.focusFirstPaneControl = function (pane) {
    var quality = pane.overlay ? pane.overlay.querySelector("input[data-live-quality-choice]:not([disabled])") : null;
    if (quality) {
      quality.focus();
      return;
    }
    var controls = this.focusableIn(pane.overlay);
    if (controls.length) controls[0].focus();
  };

  LiveWall.prototype.focusLastPaneControl = function (pane) {
    var controls = this.focusableIn(pane.overlay);
    if (controls.length) controls[controls.length - 1].focus();
  };

  LiveWall.prototype.focusFirstWallControl = function () {
    var controls = this.focusableIn(this.overlay);
    if (controls.length) controls[0].focus();
  };

  LiveWall.prototype.showPaneOverlay = function (pane) {
    if (!pane || !pane.visible) return;
    this.activePane = pane;
    this.panes.forEach((candidate) => {
      if (candidate !== pane) candidate.setOverlayShown(false);
    });
    pane.setOverlayShown(true);
    this.root.setAttribute("data-live-idle", "false");
    this.armHide();
  };

  LiveWall.prototype.hidePaneOverlays = function () {
    this.panes.forEach((pane) => pane.setOverlayShown(false));
    this.activePane = null;
  };

  LiveWall.prototype.showOverlay = function () {
    if (!this.overlay) return;
    this.overlay.removeAttribute("inert");
    this.overlayHidden = false;
    this.root.setAttribute("data-live-overlay", "shown");
    this.root.setAttribute("data-live-idle", "false");
    this.armHide();
  };

  LiveWall.prototype.anyOverlayShown = function () {
    if (!this.overlayHidden) return true;
    return this.panes.some((pane) => pane.isOverlayShown());
  };

  LiveWall.prototype.suspendHide = function () {
    if (this.hideTimer) window.clearTimeout(this.hideTimer);
    this.hideTimer = null;
  };

  LiveWall.prototype.armHide = function () {
    var wall = this;
    this.suspendHide();
    if (this.noticeShown) return;
    this.hideTimer = window.setTimeout(() => wall.hideIfIdle(), OVERLAY_IDLE);
  };

  LiveWall.prototype.hideIfIdle = function () {
    this.hideTimer = null;
    if (this.noticeShown) return;
    if (this.overlay && this.overlay.contains(document.activeElement)) return;
    if (this.activePane && this.activePane.overlay && this.activePane.overlay.contains(document.activeElement)) {
      return;
    }
    this.hideAllOverlays();
  };

  LiveWall.prototype.hideAllOverlays = function () {
    this.suspendHide();
    if (this.overlay) {
      this.overlay.setAttribute("inert", "");
      this.overlayHidden = true;
      this.root.setAttribute("data-live-overlay", "hidden");
    }
    this.hidePaneOverlays();
    this.root.setAttribute("data-live-idle", "true");
  };

  LiveWall.prototype.hideOverlay = function () {
    this.hideAllOverlays();
  };

  LiveWall.prototype.showNotice = function (text) {
    if (!this.notice) return;
    var wall = this;
    this.notice.textContent = text;
    this.notice.hidden = false;
    this.noticeShown = true;
    this.showOverlay();
    this.suspendHide();
    if (this.noticeTimer) window.clearTimeout(this.noticeTimer);
    this.noticeTimer = window.setTimeout(() => {
      wall.notice.hidden = true;
      wall.notice.textContent = "";
      wall.noticeShown = false;
      wall.armHide();
    }, NOTICE_HOLD);
  };

  // requestFullscreen is called directly from the click handler so the
  // browser's user activation is intact. The wall is the target, and a refusal
  // is a plain-language notice instead of silence.
  LiveWall.prototype.requestFullscreen = function () {
    var wall = this;
    if (!this.root.requestFullscreen) {
      this.showNotice(
        "This browser cannot show the wall full screen. Use the browser's own full-screen control.",
      );
      return;
    }
    var request;
    try {
      request = this.root.requestFullscreen();
    } catch (error) {
      this.showNotice(
        "The browser did not allow full screen. Try again, or use the browser's own full-screen control.",
      );
      return;
    }
    if (request && request.catch) {
      request.catch(() => {
        wall.showNotice(
          "The browser did not allow full screen. Try again, or use the browser's own full-screen control.",
        );
      });
    }
  };

  LiveWall.prototype.exitFullscreen = function () {
    if (!document.fullscreenElement) return;
    var wall = this;
    var exiting = document.exitFullscreen();
    if (exiting && exiting.then) {
      exiting
        .then(() => wall.syncFullscreen())
        .catch(() => wall.syncFullscreen());
    }
    window.setTimeout(() => wall.syncFullscreen(), 0);
  };

  LiveWall.prototype.syncFullscreen = function () {
    var active = document.fullscreenElement === this.root;
    this.fullscreen = active;
    document.body.classList.toggle("live-fullscreen-active", Boolean(document.fullscreenElement));
    this.root.setAttribute("data-live-fullscreen-active", active ? "true" : "false");
    if (this.fullscreenButton) this.fullscreenButton.hidden = active;
    if (active) {
      this.showOverlay();
      if (this.selected) {
        this.showPaneOverlay(this.selected);
        this.selected.root.focus();
      }
      this.showFullscreenHint();
    } else if (this.fullscreenHint) {
      this.fullscreenHint.hidden = true;
      this.fullscreenHint.textContent = "";
    }
  };

  LiveWall.prototype.showFullscreenHint = function () {
    if (!this.fullscreenHint) return;
    var wall = this;
    this.fullscreenHint.textContent =
      "Full screen. Press Escape, or swipe down at least 120 pixels on a touch screen, to exit.";
    this.fullscreenHint.hidden = false;
    if (this.fullscreenHintTimer) window.clearTimeout(this.fullscreenHintTimer);
    this.fullscreenHintTimer = window.setTimeout(() => {
      wall.fullscreenHint.hidden = true;
      wall.fullscreenHint.textContent = "";
    }, OVERLAY_IDLE);
  };

  LiveWall.prototype.applyLayout = function (value) {
    var count = value === "auto" ? this.autoLayout : Number(value);
    if (!count) return;
    this.layout = count;
    this.storedLayout = value;
    this.root.setAttribute("data-layout", String(count));
    this.root.setAttribute("data-live-layout-value", value);
    this.panes.forEach((pane) => {
      var visible = pane.number <= count;
      pane.visible = visible;
      pane.root.hidden = !visible;
      pane.root.setAttribute("data-live-visible", visible ? "true" : "false");
      if (!visible) {
        pane.setOverlayShown(false);
        if (pane.live !== false) {
          pane.live = false;
          if (pane.player) pane.player.stopLive();
        }
      } else if (!pane.player || pane.live === false) {
        pane.live = null;
      }
    });
    if (this.selected && !this.selected.visible) {
      var first = this.firstVisible();
      if (first) this.select(first, false);
    }
    // Newly visible panes must respect the capacity the current session has,
    // not just the class prior.
    this.capacity.clamp(now(), this.view(now()));
    this.syncLayoutPicker(value);
    this.apply();
    if (this.selected) {
      this.showPaneOverlay(this.selected);
      this.updateCameraStateURL();
    }
    this.postLayout(value);
  };

  LiveWall.prototype.syncLayoutPicker = function (value) {
    if (!this.layoutForm) return;
    each("input[name='layout']", this.layoutForm, (input) => {
      input.checked = input.value === value;
    });
    each("[data-live-layout-buttons] button", this.layoutForm, (button) => {
      button.setAttribute("aria-pressed", button.value === value ? "true" : "false");
    });
  };

  LiveWall.prototype.csrfToken = function () {
    var input = this.root.querySelector("input[name='csrf_token']");
    return input ? input.value : "";
  };

  LiveWall.prototype.postLayout = function (value) {
    if (!this.layoutForm || !window.fetch) return;
    var wall = this;
    var body = new URLSearchParams();
    body.set("csrf_token", this.csrfToken());
    body.set("layout", value);
    window
      .fetch(this.layoutForm.getAttribute("action"), {
        method: "POST",
        body: body,
        credentials: "same-origin",
        cache: "no-store",
        headers: { "X-Bittern-Inline": "1", "HX-Request": "true" },
      })
      .then((response) => {
        if (!response.ok) throw new Error("layout");
      })
      .catch(() => {
        wall.showNotice(
          "The layout changed on this screen but could not be saved. It returns to the saved layout after a reload.",
        );
      });
  };

  LiveWall.prototype.assignCamera = function (pane, cameraID) {
    if (!pane || !pane.cameraForm || !window.fetch) return;
    var wall = this;
    var body = new URLSearchParams();
    body.set("csrf_token", this.csrfToken());
    body.set("pane", String(pane.number));
    body.set("action", "camera");
    body.set("camera", cameraID);
    if (pane.cameraForm.querySelector("[name='next']")) {
      body.set("next", pane.cameraForm.querySelector("[name='next']").value);
    }
    window
      .fetch(pane.cameraForm.getAttribute("action"), {
        method: "POST",
        body: body,
        credentials: "same-origin",
        cache: "no-store",
        headers: { "X-Bittern-Inline": "1", "HX-Request": "true" },
      })
      .then((response) => {
        if (!response.ok) throw new Error("camera");
        wall.moveCamera(pane, cameraID);
      })
      .catch(() => {
        wall.showNotice(
          "The camera choice could not be saved. The pane did not change. Try again.",
        );
      });
  };

  LiveWall.prototype.moveCamera = function (target, cameraID) {
    if (!target) return;
    if (target.cameraID === cameraID) return;
    var source = null;
    this.panes.forEach((pane) => {
      if (pane !== target && pane.cameraID === cameraID) source = pane;
    });
    var node = null;
    var name = "";
    var oneStream = false;
    if (source) {
      name = source.name;
      oneStream = source.oneStream;
      node = source.takePlayer();
    }
    target.clearPlayer();
    if (!cameraID) {
      this.apply();
      this.select(target, true);
      this.updateCameraStateURL();
      return;
    }
    if (!node) {
      var template = this.root.querySelector(
        'template[data-live-camera-template="' + cameraID.replace(/"/g, '\\"') + '"]',
      );
      if (!template || !template.content || !template.content.firstElementChild) {
        this.showNotice("This camera has no player yet. Reload the page and try again.");
        return;
      }
      node = template.content.firstElementChild.cloneNode(true);
      name = node.getAttribute("data-camera-name") || cameraID;
      oneStream = node.getAttribute("data-one-stream") === "true";
    }
    target.attachPlayer(node, cameraID, name, oneStream);
    if (source) source.updateCameraUI();
    this.panes.forEach((pane) => {
      if (pane.cameraSelect) pane.cameraSelect.value = pane.cameraID;
    });
    this.select(target, true);
    this.showPaneOverlay(target);
    this.apply();
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
      if (pane.visible && pane.player && pane.live === false) pane.player.refreshSnapshot();
    });
  };

  // setQuality is the owner's choice for one pane. It wins for good: the pane
  // becomes manual and no automation touches it again.
  LiveWall.prototype.setQuality = function (pane, quality) {
    if (!pane || !pane.player) return;
    pane.manual = quality;
    pane.playerRoot.setAttribute("data-manual-quality", quality);
    this.capacity.setManual(pane.id, quality !== "");
    pane.quality = quality;
    pane.player.setQuality(quality);
    if (quality !== "snapshot") pane.note("");
    this.apply();
    pane.updateStreamUI();
    this.postQuality(pane, quality);
  };

  LiveWall.prototype.postQuality = function (pane, quality) {
    if (!pane.qualityForm || !window.fetch) return;
    var wall = this;
    var body = new URLSearchParams();
    body.set("csrf_token", this.csrfToken());
    body.set("pane", String(pane.number));
    body.set("action", "quality");
    body.set("quality", quality);
    if (pane.qualityForm.querySelector("[name='next']")) {
      body.set("next", pane.qualityForm.querySelector("[name='next']").value);
    }
    window
      .fetch(pane.qualityForm.getAttribute("action"), {
        method: "POST",
        body: body,
        credentials: "same-origin",
        cache: "no-store",
        headers: { "X-Bittern-Inline": "1", "HX-Request": "true" },
      })
      .then((response) => {
        if (!response.ok) throw new Error("quality");
      })
      .catch(() => {
        wall.showNotice(
          "The stream choice played on this screen but could not be saved. It returns to the saved choice after a reload.",
        );
      });
  };

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

  // dispose is a camera leaving a pane or a layout hiding it for good. It is
  // not a connection failure and never shows a note.
  LivePlayer.prototype.dispose = function () {
    if (this.watchdog) window.clearInterval(this.watchdog);
    this.watchdog = null;
    this.stopLive();
    this.state = "idle";
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
