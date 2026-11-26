// Bittern live player. The player container is a stable node outside every
// HTMX swap region. This file owns the video element, the MediaSource, the
// WebSocket and the recovery state machine; no server fragment may replace it.
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
    limit: "failed",
    error: "failed",
  };

  function ceilingForWidth(width) {
    if (width < 704) return 1;
    if (width < 1280) return 4;
    return 9;
  }

  function now() {
    return Date.now();
  }

  function LivePlayer(root) {
    this.root = root;
    this.video = root.querySelector("video");
    this.snapshot = root.querySelector("[data-player-snapshot]");
    this.badge = root.querySelector("[data-player-badge]");
    this.title = root.querySelector("[data-player-title]");
    this.detail = root.querySelector("[data-player-detail]");
    this.retryButton = root.querySelector("[data-player-retry]");
    this.headingBadge = document.querySelector("[data-player-state-label]");
    this.cameraName = root.getAttribute("data-camera-name") || "this camera";
    this.mediaBase = root.getAttribute("data-media-base") || "";
    this.snapshotURL = root.getAttribute("data-snapshot-url") || "";
    this.loginURL = root.getAttribute("data-login-url") || "/ui/login";
    this.profile = root.getAttribute("data-profile") || "main";
    this.profileName = root.getAttribute("data-profile-name") || "Main stream";
    this.state = "connecting";
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
    this.retryButton.addEventListener("click", () => {
      if (this.retryAction) {
        this.retryAction();
        return;
      }
      this.retry();
    });
    Array.prototype.forEach.call(
      this.root.querySelectorAll("[data-profile-link]"),
      (link) => {
        link.addEventListener("click", (event) => {
          if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
          event.preventDefault();
          this.switchProfile(
            link.getAttribute("data-profile-link"),
            link.getAttribute("href"),
            link.textContent.trim(),
          );
        });
      },
    );
    this.root.addEventListener("bittern-player-stop", () => this.stop());
    this.applyCeiling();
    window.addEventListener("resize", () => this.applyCeiling());
    this.watchdog = window.setInterval(() => this.tick(), 1000);
    this.startMSE();
  };

  LivePlayer.prototype.applyCeiling = function () {
    var ceiling = ceilingForWidth(window.innerWidth || 0);
    this.root.setAttribute("data-device-ceiling", String(ceiling));
    var players = Array.prototype.slice.call(
      document.querySelectorAll("[data-live-player]"),
    );
    var active = 0;
    players.forEach((player) => {
      var playing = player.getAttribute("data-player-active") === "true";
      if (playing) active += 1;
      if (active > ceiling) {
        player.dispatchEvent(new CustomEvent("bittern-player-stop"));
      }
    });
    this.refreshActiveCount();
  };

  LivePlayer.prototype.setState = function (name, title, detail) {
    this.state = name;
    this.root.setAttribute("data-player-state", name);
    this.root.setAttribute("data-player-active", name === "playing" ? "true" : "false");
    var stateClass = STATE_CLASS[name] || "unknown";
    if (this.badge) {
      this.badge.textContent = name.charAt(0).toUpperCase() + name.slice(1);
      this.badge.className = "state-badge state-" + stateClass;
    }
    if (this.headingBadge) {
      this.headingBadge.textContent = name.charAt(0).toUpperCase() + name.slice(1);
      this.headingBadge.className = "state-badge state-" + stateClass;
    }
    if (this.title) this.title.textContent = title;
    if (this.detail) this.detail.textContent = detail;
    this.refreshActiveCount();
    this.root.dispatchEvent(
      new CustomEvent("bittern-player-state", { detail: { state: name } }),
    );
  };

  LivePlayer.prototype.refreshActiveCount = function () {
    var active = 0;
    Array.prototype.forEach.call(
      document.querySelectorAll("[data-live-player]"),
      (player) => {
        if (player.getAttribute("data-player-active") === "true") active += 1;
      },
    );
    var ceiling = Number(this.root.getAttribute("data-device-ceiling") || 9);
    this.root.setAttribute(
      "data-active-tiles",
      String(Math.min(active, ceiling)),
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
    var url =
      scheme +
      "//" +
      window.location.host +
      this.mediaBase +
      "/ws";
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
    if (this.snapshot) {
      this.snapshot.hidden = false;
      this.snapshot.onerror = () => {
        this.snapshot.hidden = true;
      };
      this.snapshot.src =
        this.snapshotURL + "?at=" + encodeURIComponent(String(now()));
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
    this.startMSE();
  };

  LivePlayer.prototype.switchProfile = function (profile, url, label) {
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
    Array.prototype.forEach.call(
      this.root.querySelectorAll("[data-profile-link]"),
      (link) => {
        if (link.getAttribute("data-profile-link") === profile) {
          link.setAttribute("aria-current", "true");
        } else {
          link.removeAttribute("aria-current");
        }
      },
    );
    if (url && window.history && window.history.replaceState) {
      window.history.replaceState(null, "", url);
    }
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

  LivePlayer.prototype.stop = function () {
    this.clearTimers();
    this.closeSocket();
    this.clearVideo();
    this.action("", null);
    this.setState(
      "offline",
      "This camera stopped",
      "Another player took the device's active tile, so Bittern stopped this one.",
    );
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
    Array.prototype.forEach.call(
      document.querySelectorAll("[data-live-player]"),
      (root) => new LivePlayer(root).init(),
    );
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
