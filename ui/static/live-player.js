// Bittern live player. Each player island owns its own video element, its own
// MediaSource and its own WebSocket; the grid owns the viewport ceiling, the
// full-width selection and the tile replacement. A player root is a stable node
// outside every HTMX swap region, and this file refuses to mount one inside a
// region HTMX can replace (ADR 0034 section 3).
(function () {
  "use strict";

  var MAX_RETRIES = 3;
  var CONNECT_TIMEOUT = 8000;
  var STALL_TIMEOUT = 10000;
  var HLS_START_TIMEOUT = 12000;
  var SWITCH_DELAY = 250;
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

  // The exact device ceilings from ADR 0034 section 7. The proxy enforces its
  // own hard maximum of nine; this one is the UX ceiling for the viewport.
  var CEILING_PHONE = 1;
  var CEILING_TABLET = 4;
  var CEILING_DESKTOP = 9;
  var CEILING_WORDS = { 1: "one", 2: "two", 4: "four", 9: "nine" };

  var STATE_CLASS = {
    connecting: "unknown",
    playing: "ok",
    switching: "unknown",
    stalled: "degraded",
    reconnecting: "degraded",
    snapshot: "unknown",
    idle: "unknown",
    offline: "failed",
    unsupported: "failed",
    session: "failed",
    limit: "failed",
    error: "failed",
  };

  var STATE_LABEL = {
    connecting: "Connecting",
    playing: "Live",
    switching: "Switching",
    stalled: "Stalled",
    reconnecting: "Reconnecting",
    snapshot: "Still picture",
    idle: "Not started",
    offline: "Offline",
    unsupported: "Not supported",
    session: "Signed out",
    limit: "Limit reached",
    error: "Not working",
  };

  function ceilingForWidth(width) {
    if (width < 704) return CEILING_PHONE;
    if (width < 1280) return CEILING_TABLET;
    return CEILING_DESKTOP;
  }

  function now() {
    return Date.now();
  }

  function each(selector, root, callback) {
    Array.prototype.forEach.call(root.querySelectorAll(selector), callback);
  }

  // LiveGrid owns the page-level state: which tiles are wanted, the viewport
  // ceiling, the full-width tile and the ceiling sentence.
  function LiveGrid(root) {
    this.root = root; // [data-live-grid] on the grid page, null for one camera
    this.players = [];
    this.ceiling = ceilingForWidth(window.innerWidth || 0);
    this.fullTile = null;
    this.preFull = null;
    this.note = document.querySelector("[data-live-ceiling-note]");
  }

  LiveGrid.prototype.add = function (player) {
    this.players.push(player);
  };

  LiveGrid.prototype.init = function () {
    this.players.forEach((player) => player.init());
    this.sync(true);
    window.addEventListener("resize", () => this.sync(false));
    if (!this.root) return;
    this.root.addEventListener("submit", (event) => {
      var form = event.target;
      if (!form || !form.matches("[data-live-tile-form]")) return;
      event.preventDefault();
      this.replaceTile(form);
    });
  };

  // sync applies the ceiling in DOM order. On boot it starts the first tiles;
  // on a resize it only ever stops the excess, never starts new players.
  LiveGrid.prototype.sync = function (allowStart) {
    this.ceiling = ceilingForWidth(window.innerWidth || 0);
    if (this.fullTile) {
      this.updateNote();
      this.updateCounts();
      return;
    }
    var allowed = 0;
    this.players.forEach((player) => {
      if (allowed < this.ceiling) {
        allowed += 1;
        player.want(allowStart);
        return;
      }
      player.unwant();
    });
    this.updateNote();
    this.updateCounts();
  };

  LiveGrid.prototype.startTile = function (player) {
    if (!player || player.wanted) {
      if (player) player.want(true);
      return;
    }
    var wanted = this.players.filter((candidate) => candidate.wanted);
    if (wanted.length >= this.ceiling) {
      wanted[wanted.length - 1].unwant();
    }
    player.want(true);
    this.updateNote();
    this.updateCounts();
  };

  LiveGrid.prototype.playerForTile = function (tile) {
    if (!tile) return null;
    for (var index = 0; index < this.players.length; index += 1) {
      if (this.players[index].tile === tile) return this.players[index];
    }
    return null;
  };

  LiveGrid.prototype.expand = function (tile) {
    var player = this.playerForTile(tile);
    if (!player || !this.root) {
      if (player) this.startTile(player);
      return;
    }
    this.preFull = this.players
      .filter((candidate) => candidate.wanted)
      .map((candidate) => candidate.tile);
    this.fullTile = tile;
    this.root.setAttribute("data-live-full", "true");
    tile.setAttribute("data-tile-full", "true");
    if (tile.scrollIntoView) tile.scrollIntoView({ block: "start" });
    if (!player.wanted) {
      var wanted = this.players.filter((candidate) => candidate.wanted);
      if (wanted.length >= this.ceiling) {
        wanted[wanted.length - 1].unwant();
      }
    }
    player.setProfile("main");
    player.want(true);
    this.players.forEach((candidate) =>
      candidate.showFullControls(candidate === player),
    );
    this.updateNote();
    this.updateCounts();
  };

  LiveGrid.prototype.collapse = function () {
    var tile = this.fullTile;
    var player = this.playerForTile(tile);
    var before = this.preFull || [];
    this.clearFull();
    if (player) player.setProfile("sub");
    this.players.forEach((candidate) => {
      if (before.indexOf(candidate.tile) >= 0) {
        candidate.want(true);
        return;
      }
      if (candidate !== player) candidate.unwant();
    });
    if (player && before.indexOf(player.tile) < 0) player.unwant();
    this.updateNote();
    this.updateCounts();
  };

  LiveGrid.prototype.clearFull = function () {
    if (this.fullTile) this.fullTile.removeAttribute("data-tile-full");
    if (this.root) this.root.removeAttribute("data-live-full");
    this.fullTile = null;
    this.preFull = null;
    this.players.forEach((candidate) => candidate.showFullControls(false));
  };

  LiveGrid.prototype.updateNote = function () {
    if (!this.note) return;
    var total = this.players.length;
    if (total <= this.ceiling) {
      this.note.hidden = true;
      this.note.textContent = "";
      return;
    }
    var word = CEILING_WORDS[this.ceiling] || String(this.ceiling);
    var cameras = this.ceiling === 1 ? "one camera" : word + " cameras";
    var live =
      this.ceiling === 1
        ? "only the first tile is live"
        : "only the first " + word + " tiles are live";
    this.note.textContent =
      "This screen plays " +
      cameras +
      " at a time, so " +
      live +
      ". Open a tile to watch it full width.";
    this.note.hidden = false;
  };

  LiveGrid.prototype.updateCounts = function () {
    var active = this.players.filter((player) => player.state === "playing").length;
    var shown = Math.min(active, this.ceiling);
    this.players.forEach((player) => {
      player.root.setAttribute("data-device-ceiling", String(this.ceiling));
      player.root.setAttribute("data-active-tiles", String(shown));
    });
  };

  LiveGrid.prototype.mount = function (slot) {
    var root = slot.querySelector("[data-live-player]");
    if (!root) return null;
    var player = new LivePlayer(root, this);
    this.add(player);
    player.init();
    return player;
  };

  LiveGrid.prototype.replaceTile = function (form) {
    var grid = this;
    var slot = form.closest("[data-live-tile]");
    if (!slot || !window.fetch || !window.FormData) {
      form.submit();
      return;
    }
    var button = form.querySelector("button[type='submit']");
    if (button) button.disabled = true;
    var failure = function () {
      if (button) button.disabled = false;
      form.submit();
    };
    window
      .fetch(form.getAttribute("action"), {
        method: "POST",
        body: new URLSearchParams(new FormData(form)),
        credentials: "same-origin",
        cache: "no-store",
        headers: {
          Accept: "text/html",
          "HX-Request": "true",
          "HX-Current-URL": window.location.href,
          "X-Bittern-Tile": "1",
        },
      })
      .then((response) => {
        if (response.status === 401) {
          window.location.href = response.headers.get("HX-Redirect") || "/ui/login";
          return null;
        }
        if (!response.ok) throw new Error("tile " + response.status);
        return response.text();
      })
      .then((html) => {
        if (html === null) return;
        var old = grid.playerForTile(slot);
        if (grid.fullTile === slot) grid.clearFull();
        if (old) {
          old.destroy();
          grid.players = grid.players.filter((player) => player !== old);
        }
        slot.innerHTML = html;
        grid.mount(slot);
        grid.sync(true);
        // The form and its button are gone; move focus to the new content.
        var heading = slot.querySelector("h2");
        if (heading) heading.focus();
      })
      .catch(failure);
  };

  // LivePlayer is one island. It never reads another island's nodes.
  function LivePlayer(root, grid) {
    this.root = root;
    this.grid = grid;
    this.tile = root.closest("[data-live-tile]");
    this.video = root.querySelector("video");
    this.snapshot = root.querySelector("[data-player-snapshot]");
    this.idlePlaceholder = root.querySelector("[data-player-idle]");
    this.badge = root.querySelector("[data-player-badge]");
    this.title = root.querySelector("[data-player-title]");
    this.detail = root.querySelector("[data-player-detail]");
    this.retryButton = root.querySelector("[data-player-retry]");
    this.headingBadge = root.querySelector("[data-player-state-label]");
    this.expandButton = root.querySelector("[data-live-expand]");
    this.collapseButton = root.querySelector("[data-live-collapse]");
    this.cameraName = root.getAttribute("data-camera-name") || "this camera";
    this.mediaBase = root.getAttribute("data-media-base") || "";
    this.snapshotURL = root.getAttribute("data-snapshot-url") || "";
    this.loginURL = root.getAttribute("data-login-url") || "/ui/login";
    this.profile = root.getAttribute("data-profile") || "main";
    this.profileName = root.getAttribute("data-profile-name") || "Main stream";
    this.state = "idle";
    this.wanted = false;
    this.started = false;
    this.attempt = 0;
    this.ws = null;
    this.mediaSource = null;
    this.sourceBuffer = null;
    this.queue = [];
    this.lastMediaAt = 0;
    this.ignoreClose = false;
    this.retryTimer = null;
    this.stallTimer = null;
    this.connectTimer = null;
    this.watchdog = null;
    this.hlsStarted = false;
    this.retryAction = null;
  }

  LivePlayer.prototype.init = function () {
    if (!this.root || !this.video || !this.mediaBase) return;
    // The runtime half of the player isolation rule in ADR 0034.
    var swapped = this.root.closest(
      "[hx-get],[hx-post],[hx-target],[hx-swap],[hx-trigger]",
    );
    if (swapped) {
      this.setState(
        "error",
        "The player could not start",
        "Bittern tried to place the player inside a live update region. Reload the page.",
      );
      return;
    }
    if (this.retryButton) {
      this.retryButton.addEventListener("click", () => {
        if (this.retryAction) {
          this.retryAction();
          return;
        }
        this.retry();
      });
    }
    each("[data-profile-link]", this.root, (link) => {
      link.addEventListener("click", (event) => {
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        this.setProfile(
          link.getAttribute("data-profile-link"),
          link.textContent.trim(),
          link.getAttribute("href"),
        );
      });
    });
    if (this.expandButton) {
      this.expandButton.hidden = false;
      this.expandButton.addEventListener("click", () => {
        if (this.grid) this.grid.expand(this.tile);
      });
    }
    if (this.collapseButton) {
      this.collapseButton.addEventListener("click", () => {
        if (this.grid) this.grid.collapse();
      });
    }
    this.watchdog = window.setInterval(() => this.tick(), 1000);
  };

  LivePlayer.prototype.showFullControls = function (full) {
    var active = document.activeElement;
    if (this.expandButton) this.expandButton.hidden = full;
    if (this.collapseButton) this.collapseButton.hidden = !full;
    // The control that was just pressed is now hidden. Keep keyboard focus on
    // the matching control instead of dropping it to the document.
    if (active === this.expandButton && full && this.collapseButton) {
      this.collapseButton.focus();
    }
    if (active === this.collapseButton && !full && this.expandButton) {
      this.expandButton.focus();
    }
  };

  LivePlayer.prototype.want = function (start) {
    this.wanted = true;
    this.root.setAttribute("data-player-wanted", "true");
    if (start || this.started) {
      this.start();
      return;
    }
    if (this.state === "idle") this.showIdle();
  };

  LivePlayer.prototype.unwant = function () {
    this.wanted = false;
    this.root.setAttribute("data-player-wanted", "false");
    if (this.started) {
      this.stopToIdle();
      return;
    }
    this.showIdle();
  };

  LivePlayer.prototype.start = function () {
    if (this.started) return;
    this.started = true;
    if (this.idlePlaceholder) this.idlePlaceholder.hidden = true;
    this.startMSE();
  };

  LivePlayer.prototype.stopToIdle = function () {
    this.started = false;
    this.clearTimers();
    this.closeSocket();
    this.clearVideo();
    this.action("", null);
    this.showIdle();
  };

  LivePlayer.prototype.showIdle = function () {
    this.clearTimers();
    this.closeSocket();
    this.clearVideo();
    if (this.video) this.video.hidden = true;
    if (this.snapshot) this.snapshot.hidden = true;
    if (this.idlePlaceholder) this.idlePlaceholder.hidden = false;
    this.setState(
      "idle",
      "Not started on this screen",
      this.wanted
        ? "This camera is not started yet. Start it here, or open the tile full width."
        : "This screen plays a limited number of cameras at a time. Start this camera here, or open this tile full width.",
    );
    this.action("Start this camera", () => {
      if (this.grid) this.grid.startTile(this);
    });
  };

  LivePlayer.prototype.setState = function (name, title, detail) {
    this.state = name;
    this.root.setAttribute("data-player-state", name);
    this.root.setAttribute("data-player-active", name === "playing" ? "true" : "false");
    var stateClass = STATE_CLASS[name] || "unknown";
    var label = STATE_LABEL[name] || name;
    if (this.badge) {
      this.badge.textContent = label;
      this.badge.className = "state-badge state-" + stateClass;
    }
    if (this.headingBadge) {
      this.headingBadge.textContent = label;
      this.headingBadge.className = "state-badge state-" + stateClass;
    }
    if (this.title) this.title.textContent = title;
    if (this.detail) this.detail.textContent = detail;
    if (this.grid) this.grid.updateCounts();
    this.root.dispatchEvent(
      new CustomEvent("bittern-player-state", { detail: { state: name } }),
    );
  };

  LivePlayer.prototype.action = function (label, callback) {
    if (!this.retryButton) return;
    this.retryAction = callback || null;
    this.retryButton.textContent = label;
    this.retryButton.hidden = !label;
    this.retryButton.disabled = !label;
  };

  LivePlayer.prototype.startMSE = function () {
    this.clearTimers();
    this.closeSocket();
    if (this.idlePlaceholder) this.idlePlaceholder.hidden = true;
    if (!window.MediaSource && typeof window.ManagedMediaSource !== "function") {
      this.startHLS();
      return;
    }
    this.action("", null);
    this.video.hidden = false;
    if (this.snapshot) this.snapshot.hidden = true;
    this.video.removeAttribute("src");
    this.mediaSource = null;
    this.sourceBuffer = null;
    this.queue = [];
    this.hlsStarted = false;
    this.ignoreClose = false;
    if (this.attempt === 0) {
      this.setState(
        "connecting",
        "Connecting to " + this.cameraName,
        "Bittern is opening the live stream.",
      );
    } else {
      this.setState(
        "reconnecting",
        "Reconnecting to " + this.cameraName,
        "Bittern is opening the stream again.",
      );
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
      // Chrome's MSE needs an object URL. The CSP allows blob: for that
      // same-origin URL and keeps every other media source at 'self'.
      this.video.src = URL.createObjectURL(this.mediaSource);
      this.video.srcObject = null;
    }
    this.mediaSource.addEventListener(
      "sourceopen",
      () => {
        if (this.ws && this.ws.readyState === 1) {
          this.sendCodecs();
        }
      },
      { once: true },
    );
    this.playVideo();
  };

  LivePlayer.prototype.sendCodecs = function () {
    var Source =
      typeof window.ManagedMediaSource === "function"
        ? window.ManagedMediaSource
        : window.MediaSource;
    var supported = CODECS;
    if (Source && typeof Source.isTypeSupported === "function") {
      supported = CODECS.filter((codec) =>
        Source.isTypeSupported('video/mp4; codecs="' + codec + '"'),
      );
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
      if (this.mediaSource && this.mediaSource.readyState === "open") {
        this.sendCodecs();
      }
      this.connectTimer = window.setTimeout(() => {
        if (this.state !== "playing") {
          this.markStalled("The stream did not start in time.");
        }
      }, CONNECT_TIMEOUT);
    });
    socket.addEventListener("message", (event) => this.onMessage(event));
    socket.addEventListener("close", () => {
      if (socket !== this.ws) return;
      if (!this.ignoreClose) this.handleDrop();
    });
    socket.addEventListener("error", () => {
      // close follows and owns the recovery.
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
        this.showSnapshot(
          "unsupported",
          "This browser cannot play the stream",
          "The camera uses a video format this browser cannot decode. Bittern is showing the latest picture instead.",
        );
        return;
      }
      if (message.type === "mse" && message.value) {
        this.addSourceBuffer(message.value);
      }
      return;
    }
    this.lastMediaAt = now();
    if (this.state !== "playing") {
      this.clearConnectTimer();
      this.setState(
        "playing",
        "Live from " + this.cameraName,
        "Watching the " + this.profileName.toLowerCase() + " through Bittern.",
      );
    }
    this.appendMedia(event.data);
  };

  LivePlayer.prototype.addSourceBuffer = function (mime) {
    if (!this.mediaSource) return;
    try {
      if (this.sourceBuffer) {
        this.mediaSource.removeSourceBuffer(this.sourceBuffer);
      }
      this.sourceBuffer = this.mediaSource.addSourceBuffer(mime);
      this.sourceBuffer.mode = "segments";
      this.sourceBuffer.addEventListener("updateend", () => this.flushMedia());
    } catch (error) {
      this.showSnapshot(
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

  LivePlayer.prototype.tick = function () {
    if (this.state !== "playing") return;
    if (!this.lastMediaAt || now() - this.lastMediaAt > STALL_TIMEOUT) {
      this.markStalled("The stream stopped sending video.");
    }
  };

  LivePlayer.prototype.markStalled = function (reason) {
    if (this.state === "stalled" || this.state === "offline") return;
    this.clearConnectTimer();
    this.setState(
      "stalled",
      "The stream stopped sending video",
      reason + " Bittern is checking the connection before it tries again.",
    );
    this.retryTimer = window.setTimeout(() => this.handleDrop(), 2000);
  };

  LivePlayer.prototype.handleDrop = function () {
    if (this.state === "session" || this.state === "offline") return;
    this.closeSocket();
    this.clearTimers();
    this.root.setAttribute("data-player-active", "false");
    this.probeSession().then((ended) => {
      if (ended) {
        this.setState(
          "session",
          "Your session ended",
          "Sign in again to watch " + this.cameraName + ".",
        );
        this.action("Sign in", () => {
          window.location.href = this.loginURL;
        });
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
      "Trying again in " +
        seconds +
        (seconds === 1 ? " second" : " seconds") +
        ". Attempt " +
        this.attempt +
        " of " +
        MAX_RETRIES +
        ".",
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
    this.showSnapshot(
      "offline",
      this.cameraName + " is offline",
      "Bittern could not start the live stream after " +
        MAX_RETRIES +
        " attempts. The camera may be offline, or the recording service may be stopped. Try again, or open cameras.",
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
    this.action("", null);
    if (!this.canPlayHLS()) {
      this.fallback();
      return;
    }
    this.attempt = 0;
    this.hlsStarted = false;
    this.lastMediaAt = now();
    this.video.hidden = false;
    if (this.snapshot) this.snapshot.hidden = true;
    if (this.idlePlaceholder) this.idlePlaceholder.hidden = true;
    this.setState(
      "connecting",
      "Connecting to " + this.cameraName,
      "Bittern is trying the HLS stream.",
    );
    this.video.srcObject = null;
    this.video.src = this.mediaBase + "/stream.m3u8";
    this.playVideo();
    var started = false;
    var onPlaying = () => {
      started = true;
      this.hlsStarted = true;
      this.lastMediaAt = now();
      this.setState(
        "playing",
        "Live from " + this.cameraName,
        "Watching the " + this.profileName.toLowerCase() + " through Bittern.",
      );
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
          this.showSnapshot(
            "offline",
            this.cameraName + " is offline",
            "Bittern could not start the HLS stream. The camera may be offline, or the recording service may be stopped. Try again, or open cameras.",
          );
        } else {
          this.handleDrop();
        }
      },
      { once: true },
    );
    this.connectTimer = window.setTimeout(() => {
      if (!started) {
        this.showSnapshot(
          "offline",
          this.cameraName + " is offline",
          "Bittern could not start the HLS stream in time. The camera may be offline, or the recording service may be stopped. Try again, or open cameras.",
        );
      }
    }, HLS_START_TIMEOUT);
  };

  LivePlayer.prototype.showSnapshot = function (state, title, detail) {
    this.closeSocket();
    this.clearTimers();
    this.clearVideo();
    this.root.setAttribute("data-player-active", "false");
    if (this.video) this.video.hidden = true;
    if (this.idlePlaceholder) this.idlePlaceholder.hidden = true;
    if (this.snapshot) {
      this.snapshot.hidden = false;
      this.snapshot.onerror = () => {
        this.snapshot.hidden = true;
      };
      this.snapshot.src = this.snapshotURL + "?at=" + encodeURIComponent(String(now()));
    }
    this.setState(state, title, detail);
    this.action("Try again", () => this.retry());
  };

  LivePlayer.prototype.retry = function () {
    this.clearTimers();
    this.closeSocket();
    this.attempt = 0;
    this.hlsStarted = false;
    this.retryAction = null;
    this.started = true;
    this.startMSE();
  };

  LivePlayer.prototype.setProfile = function (profile, label, url) {
    if (!profile || profile === this.profile) return;
    this.clearTimers();
    this.closeSocket();
    this.profile = profile;
    this.profileName = label || (profile === "sub" ? "Sub stream" : "Main stream");
    this.root.setAttribute("data-profile", profile);
    this.root.setAttribute("data-profile-name", this.profileName);
    this.mediaBase = this.mediaBase.replace(
      /\/[^/]+$/,
      "/" + encodeURIComponent(profile),
    );
    this.root.setAttribute("data-media-base", this.mediaBase);
    this.snapshotURL = this.snapshotURL.replace(
      /\/[^/]+\/snapshot\.jpg$/,
      "/" + encodeURIComponent(profile) + "/snapshot.jpg",
    );
    this.root.setAttribute("data-snapshot-url", this.snapshotURL);
    each("[data-profile-link]", this.root, (link) => {
      if (link.getAttribute("data-profile-link") === profile) {
        link.setAttribute("aria-current", "true");
      } else {
        link.removeAttribute("aria-current");
      }
    });
    if (url && window.history && window.history.replaceState) {
      window.history.replaceState(null, "", url);
    }
    if (!this.started || !this.wanted) return;
    this.attempt = 0;
    this.setState(
      "switching",
      "Switching to " + this.profileName,
      "The player is closing the old stream and opening the new one.",
    );
    window.setTimeout(() => this.startMSE(), SWITCH_DELAY);
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

  LivePlayer.prototype.destroy = function () {
    this.clearTimers();
    if (this.watchdog) window.clearInterval(this.watchdog);
    this.watchdog = null;
    this.closeSocket();
    this.clearVideo();
    this.wanted = false;
    this.started = false;
    this.state = "idle";
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
    // A preset change reloads the page. Open the layout control again so the
    // browser-restored focus lands on a visible preset button, not a hidden one.
    if (window.location.hash === "#live-layout") {
      var disclosure = document.querySelector(".live-layout-disclosure");
      if (disclosure) disclosure.open = true;
    }
    var grid = new LiveGrid(document.querySelector("[data-live-grid]"));
    each("[data-live-player]", document, (root) => {
      var player = new LivePlayer(root, grid);
      grid.add(player);
    });
    grid.init();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
