// Browser checks for the Bittern live player island. The checks use a stub
// WebSocket and MediaSource so every player state can be walked without a
// camera, a go2rtc process or a Frigate container.

const LIVE_PATH = "/ui/live/front_door";
const PLAYER_STATES = [
  "connecting",
  "playing",
  "stalled",
  "reconnecting",
  "offline",
  "unsupported",
  "session",
  "switching",
];

const STUB = ({ mode }) => {
  window.__liveStub = { mode, sockets: [] };

  class FakeBuffer extends EventTarget {
    constructor() {
      super();
      this.updating = false;
      this.buffered = {
        length: 1,
        start: () => 0,
        end: () => 1,
      };
    }
    appendBuffer() {
      this.updating = true;
      setTimeout(() => {
        this.updating = false;
        this.dispatchEvent(new Event("updateend"));
      }, 0);
    }
    remove() {}
  }

  class FakeMediaSource extends EventTarget {
    constructor() {
      super();
      this.readyState = "closed";
      this.sourceBuffers = [];
      setTimeout(() => {
        this.readyState = "open";
        this.dispatchEvent(new Event("sourceopen"));
      }, 0);
    }
    static isTypeSupported() {
      return true;
    }
    addSourceBuffer() {
      const buffer = new FakeBuffer();
      this.sourceBuffers.push(buffer);
      return buffer;
    }
    removeSourceBuffer() {}
    endOfStream() {
      this.readyState = "ended";
    }
  }

  class FakeWebSocket extends EventTarget {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;

    constructor(url) {
      super();
      this.url = url;
      this.readyState = FakeWebSocket.CONNECTING;
      this.binaryType = "arraybuffer";
      window.__liveStub.sockets.push(this);
      setTimeout(() => this.begin(), 5);
    }

    begin() {
      if (mode === "connecting") return;
      this.readyState = FakeWebSocket.OPEN;
      this.dispatchEvent(new Event("open"));
      const closeSoon = () => {
        this.readyState = FakeWebSocket.CLOSED;
        this.dispatchEvent(new Event("close"));
      };
      if (mode === "reconnecting" || mode === "offline" || mode === "session") {
        setTimeout(closeSoon, 5);
        return;
      }
      if (mode === "unsupported") {
        setTimeout(() => this.deliver(JSON.stringify({ type: "error", value: "mse: codec is not supported" })), 5);
        return;
      }
      setTimeout(() => {
        this.deliver(JSON.stringify({ type: "mse", value: 'video/mp4; codecs="avc1.42E01E"' }));
        if (mode === "stalled") return;
        this.deliver(new ArrayBuffer(32));
      }, 5);
    }

    deliver(data) {
      this.dispatchEvent(new MessageEvent("message", { data }));
    }

    send(data) {
      window.__liveStub.lastSent = data;
    }

    close() {
      this.readyState = FakeWebSocket.CLOSED;
      this.dispatchEvent(new Event("close"));
    }
  }

  window.WebSocket = FakeWebSocket;
  window.MediaSource = FakeMediaSource;
  window.ManagedMediaSource = undefined;
  URL.createObjectURL = () => "blob:bittern-live-player";
  URL.revokeObjectURL = () => {};
  HTMLMediaElement.prototype.play = () => Promise.resolve();
  HTMLMediaElement.prototype.pause = () => {};
  Object.defineProperty(HTMLMediaElement.prototype, "srcObject", {
    configurable: true,
    get() {
      return this.__liveSrcObject || null;
    },
    set(value) {
      this.__liveSrcObject = value;
    },
  });
  // Chromium reports "maybe" for HLS. The state walk is deterministic MSE
  // only; the real HLS fallback is covered by the proxy and page checks.
  HTMLMediaElement.prototype.canPlayType = () => "";
};

async function signIn(page, baseURL, password) {
  await page.goto(`${baseURL}/ui/login`, { waitUntil: "load" });
  await page.fill("#login-username", "admin");
  await page.fill("#login-password", password);
  await Promise.all([
    page.waitForURL("**/ui/status", { timeout: 15000 }),
    page.click("button[type='submit']"),
  ]);
}

async function waitForState(page, state, timeout = 8000) {
  await page.waitForFunction(
    (expected) =>
      document
        .querySelector("[data-live-player]")
        ?.getAttribute("data-player-state") === expected,
    state,
    { timeout },
  );
}

async function openLive(context, baseURL, password, mode, route) {
  const page = await context.newPage();
  await signIn(page, baseURL, password);
  await page.addInitScript(STUB, { mode });
  if (route) await route(page);
  await page.goto(`${baseURL}${LIVE_PATH}`, { waitUntil: "load" });
  return page;
}

function playerMessage(page) {
  return page.evaluate(() => {
    const title = document.querySelector("[data-player-title]");
    const detail = document.querySelector("[data-player-detail]");
    return {
      title: title ? title.textContent.trim() : "",
      detail: detail ? detail.textContent.trim() : "",
      state: document
        .querySelector("[data-live-player]")
        ?.getAttribute("data-player-state"),
    };
  });
}

async function assertIsland(page, label, failures) {
  const result = await page.evaluate(() => {
    const player = document.querySelector("[data-live-player]");
    const video = document.querySelector("#live-video");
    const inSwap = (node) =>
      !!node &&
      !!node.closest(
        "[hx-get],[hx-post],[hx-target],[hx-swap],[hx-trigger]",
      );
    return {
      player: !!player,
      video: !!video,
      playerSwapped: inSwap(player),
      videoSwapped: inSwap(video),
      state: player ? player.getAttribute("data-player-state") : null,
    };
  });
  if (!result.player || !result.video) {
    failures.push(`${label}: player or video element is missing`);
  }
  if (result.playerSwapped || result.videoSwapped) {
    failures.push(`${label}: the player island is inside an HTMX swap region`);
  }
}

async function walkState(browser, baseURL, password, mode) {
  const failures = [];
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
  try {
    let page;
    if (mode === "stalled") {
      page = await context.newPage();
      await page.clock.install();
      await signIn(page, baseURL, password);
      await page.addInitScript(STUB, { mode });
      await page.goto(`${baseURL}${LIVE_PATH}`, { waitUntil: "load" });
      await page.clock.fastForward(9000);
    } else if (mode === "session") {
      page = await openLive(context, baseURL, password, mode, async (target) => {
        await target.route("**/snapshot.jpg*", (route) =>
          route.fulfill({ status: 401, body: "" }),
        );
      });
    } else {
      page = await openLive(context, baseURL, password, mode);
    }
    if (mode === "switching") {
      await waitForState(page, "playing");
    } else {
      await waitForState(page, mode, mode === "offline" ? 15000 : 8000);
    }
    await assertIsland(page, `live player ${mode}`, failures);
    const message = await playerMessage(page);
    if (!message.title) failures.push(`${mode}: the state has no title sentence`);
    if (!message.detail) failures.push(`${mode}: the state has no detail sentence`);
    if (mode === "playing") {
      await page.evaluate(() => {
        document.querySelector("#live-video").dataset.liveMarker = "keep";
        document.querySelector("[data-live-player]").dataset.liveMarker = "keep";
      });
      const request = page.waitForRequest("**/ui/live/front_door/status");
      await page.evaluate(() => {
        const target = document.querySelector("#live-camera-state");
        window.htmx.ajax("GET", target.getAttribute("hx-get"), {
          target: "#live-camera-state",
          swap: "innerHTML",
        });
      });
      await request;
      await page.waitForFunction(
        () =>
          document.querySelector("#live-camera-state")?.textContent.includes(
            "enabled",
          ),
        { timeout: 5000 },
      );
      const afterPoll = await page.evaluate(() => ({
        state: document
          .querySelector("[data-live-player]")
          ?.getAttribute("data-player-state"),
        videoMarker: document.querySelector("#live-video")?.dataset.liveMarker,
        playerMarker: document
          .querySelector("[data-live-player]")
          ?.dataset.liveMarker,
        videoSwapped: !!document
          .querySelector("#live-video")
          ?.closest("[hx-get],[hx-post],[hx-target],[hx-swap],[hx-trigger]"),
      }));
      if (afterPoll.state !== "playing") {
        failures.push(`playing: state became ${afterPoll.state} after the camera poll`);
      }
      if (afterPoll.videoMarker !== "keep" || afterPoll.playerMarker !== "keep") {
        failures.push("playing: the poll replaced the player or video element");
      }
      if (afterPoll.videoSwapped) {
        failures.push("playing: the video is inside the polled region");
      }
    }
    if (mode === "switching") {
      await page.click('[data-profile-link="sub"]');
      await waitForState(page, "switching");
      const switching = await playerMessage(page);
      if (!switching.title.toLowerCase().includes("switching")) {
        failures.push(`switching: unexpected title ${JSON.stringify(switching.title)}`);
      }
      if (!page.url().includes("profile=sub")) {
        failures.push("switching: the profile URL did not change");
      }
    }
    if (mode === "offline") {
      const message = await playerMessage(page);
      if (!message.detail.toLowerCase().includes("offline")) {
        failures.push(`offline: the detail does not name the offline state: ${message.detail}`);
      }
    }
  } catch (error) {
    failures.push(`${mode}: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

async function walkCeilings(browser, baseURL, password) {
  const failures = [];
  const widths = [
    { width: 360, height: 800, ceiling: 1 },
    { width: 390, height: 844, ceiling: 1 },
    { width: 1280, height: 800, ceiling: 9 },
    { width: 1920, height: 1080, ceiling: 9 },
  ];
  for (const width of widths) {
    const context = await browser.newContext({
      viewport: { width: width.width, height: width.height },
    });
    try {
      const page = await openLive(context, baseURL, password, "playing");
      await waitForState(page, "playing");
      const ceiling = await page.evaluate(() =>
        document
          .querySelector("[data-live-player]")
          ?.getAttribute("data-device-ceiling"),
      );
      if (ceiling !== String(width.ceiling)) {
        failures.push(
          `device ceiling at ${width.width}px is ${ceiling}, want ${width.ceiling}`,
        );
      }
      const active = await page.evaluate(() =>
        document
          .querySelector("[data-live-player]")
          ?.getAttribute("data-active-tiles"),
      );
      if (active !== "1") {
        failures.push(
          `active tiles at ${width.width}px is ${active}, want 1 for the single-tile view`,
        );
      }
    } catch (error) {
      failures.push(`device ceiling at ${width.width}px: ${error.message}`);
    } finally {
      await context.close();
    }
  }
  return failures;
}

export async function runLivePlayerChecks({ browser, baseURL, adminPassword }) {
  const failures = [];
  for (const state of PLAYER_STATES) {
    failures.push(...(await walkState(browser, baseURL, adminPassword, state)));
  }
  failures.push(...(await walkCeilings(browser, baseURL, adminPassword)));
  return failures;
}
