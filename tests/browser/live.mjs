// Browser checks for the Bittern live player islands and the grid. The checks
// use a stub WebSocket and MediaSource so every player state can be walked
// without a camera, a go2rtc process or a Frigate container.

import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const SCREENSHOT_DIR = path.resolve(HERE, "../../build/ui-screenshots");

const LIVE_PATH = "/ui/live/front_door";
const GRID_PATH = "/ui/live";
const LIVE_CAMERAS = ["front_door", "driveway", "side_gate", "back_garden"];
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

// One named check per player state, four device-ceiling widths, and six grid
// checks. check.mjs prints this count with the pass or fail line.
export const LIVE_CHECK_COUNT = PLAYER_STATES.length + 4 + 6 + 8 + 1;

const STUB = ({ mode, failingCamera, videoWidth, videoHeight }) => {
  window.__liveStub = {
    mode,
    failingCamera: failingCamera || "",
    videoWidth: videoWidth || 0,
    videoHeight: videoHeight || 0,
    sockets: [],
  };

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
      this.mode =
        window.__liveStub.failingCamera &&
        url.includes("/media/" + window.__liveStub.failingCamera + "/")
          ? "offline"
          : window.__liveStub.mode;
      window.__liveStub.sockets.push(this);
      setTimeout(() => this.begin(), 5);
    }

    begin() {
      const mode = this.mode;
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
  Object.defineProperty(HTMLMediaElement.prototype, "videoWidth", {
    configurable: true,
    get() {
      return window.__liveStub.videoWidth || 0;
    },
  });
  Object.defineProperty(HTMLMediaElement.prototype, "videoHeight", {
    configurable: true,
    get() {
      return window.__liveStub.videoHeight || 0;
    },
  });
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
    page.waitForURL(
      (url) =>
        url.pathname === "/ui/wizard" ||
        url.pathname === "/ui/live" ||
        url.pathname === "/ui/status",
      { timeout: 15000 },
    ),
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

async function waitForTileState(page, index, state, timeout = 10000) {
  await page.waitForFunction(
    ({ index, state }) => {
      const player = document.querySelector(
        `[data-live-tile="${index}"] [data-live-player]`,
      );
      return player?.getAttribute("data-player-state") === state;
    },
    { index, state },
    { timeout },
  );
}

async function waitForTileProfile(page, index, profile, timeout = 10000) {
  await page.waitForFunction(
    ({ index, profile }) => {
      const player = document.querySelector(
        `[data-live-tile="${index}"] [data-live-player]`,
      );
      return (
        player?.getAttribute("data-profile") === profile &&
        player?.getAttribute("data-player-state") === "playing"
      );
    },
    { index, profile },
    { timeout },
  );
}

async function openLive(context, baseURL, password, mode, route, options = {}) {
  const page = await context.newPage();
  await signIn(page, baseURL, password);
  await page.addInitScript(STUB, {
    mode,
    videoWidth: options.videoWidth || 0,
    videoHeight: options.videoHeight || 0,
  });
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
    const video = player?.querySelector("video");
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
      await waitForState(page, mode, mode === "offline" ? 15000 : 12000);
    }
    await assertIsland(page, `live player ${mode}`, failures);
    const message = await playerMessage(page);
    if (!message.title) failures.push(`${mode}: the state has no title sentence`);
    if (!message.detail) failures.push(`${mode}: the state has no detail sentence`);
    if (mode === "playing") {
      await page.evaluate(() => {
        const player = document.querySelector("[data-live-player]");
        player.querySelector("video").dataset.liveMarker = "keep";
        player.dataset.liveMarker = "keep";
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
      const afterPoll = await page.evaluate(() => {
        const player = document.querySelector("[data-live-player]");
        return {
          state: player?.getAttribute("data-player-state"),
          videoMarker: player?.querySelector("video")?.dataset.liveMarker,
          playerMarker: player?.dataset.liveMarker,
          videoSwapped: !!player
            ?.querySelector("video")
            ?.closest("[hx-get],[hx-post],[hx-target],[hx-swap],[hx-trigger]"),
        };
      });
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

// The layout cookie value uses the same query form the server writes.
function layoutCookie(preset, cameras) {
  const values = new URLSearchParams();
  values.set("v", "1");
  values.set("p", String(preset));
  for (const camera of cameras) values.append("t", camera);
  return values.toString();
}

async function openGrid(browser, baseURL, password, options = {}) {
  const context = await browser.newContext({
    viewport: options.viewport || { width: 1280, height: 800 },
  });
  if (options.preset) {
    const host = new URL(baseURL).hostname;
    await context.addCookies([
      {
        name: "bittern_live_layout",
        value: layoutCookie(options.preset, options.cameras || []),
        domain: host,
        path: "/ui/live",
        httpOnly: true,
        secure: false,
        sameSite: "Strict",
      },
    ]);
  }
  const page = await context.newPage();
  await signIn(page, baseURL, password);
  await page.addInitScript(STUB, {
    mode: options.mode || "playing",
    failingCamera: options.failingCamera || "",
    videoWidth: options.videoWidth || 0,
    videoHeight: options.videoHeight || 0,
  });
  await page.goto(`${baseURL}${GRID_PATH}`, { waitUntil: "load" });
  return { context, page };
}

async function gridStates(page) {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll("[data-live-player]")).map((player) => ({
      camera: player.getAttribute("data-camera-id"),
      state: player.getAttribute("data-player-state"),
      profile: player.getAttribute("data-profile"),
      marker: player.dataset.liveMarker || "",
    })),
  );
}

async function waitForPlayingCount(page, count, timeout = 12000) {
  await page.waitForFunction(
    (expected) =>
      document.querySelectorAll(
        '[data-live-player][data-player-state="playing"]',
      ).length === expected,
    count,
    { timeout },
  );
}

async function clickPreset(page, preset) {
  const disclosure = page.locator("details.live-layout-disclosure");
  const open = await disclosure.evaluate((details) => details.open);
  if (!open) await disclosure.locator("summary").click();
  await Promise.all([
    page.waitForNavigation({ waitUntil: "load", timeout: 15000 }),
    page.click(`[data-live-preset="${preset}"]`),
  ]);
}

// 1. All three presets switch and are remembered by the device cookie.
async function walkGridPresets(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openGrid(browser, baseURL, password, {
    preset: 1,
    cameras: ["front_door"],
  });
  try {
    await waitForPlayingCount(page, 1);
    if ((await page.locator("[data-live-tile]").count()) !== 1) {
      failures.push("presets: one-camera layout does not render one tile");
    }
    if (
      (await page.locator('[data-live-preset="1"]').getAttribute("aria-pressed")) !==
      "true"
    ) {
      failures.push("presets: the stored preset is not marked current");
    }
    await clickPreset(page, "4");
    if ((await page.locator("[data-live-tile]").count()) !== 4) {
      failures.push("presets: four-camera layout does not render four tiles");
    }
    if (
      (await page.locator('[data-live-preset="4"]').getAttribute("aria-pressed")) !==
      "true"
    ) {
      failures.push("presets: the four-camera preset is not marked current");
    }
    if (
      (await page.locator('[data-live-tile="0"] [data-camera-id]').getAttribute("data-camera-id")) !==
      "front_door"
    ) {
      failures.push("presets: switching the preset lost the tile assignment");
    }
    await clickPreset(page, "9");
    if ((await page.locator("[data-live-tile]").count()) !== 9) {
      failures.push("presets: nine-camera layout does not render nine tiles");
    }
    await clickPreset(page, "1");
    if ((await page.locator("[data-live-tile]").count()) !== 1) {
      failures.push("presets: returning to one camera does not render one tile");
    }
    await page.reload({ waitUntil: "load" });
    if ((await page.locator("[data-live-tile]").count()) !== 1) {
      failures.push("presets: the chosen layout was not remembered by the device");
    }
    await waitForPlayingCount(page, 1);
  } catch (error) {
    failures.push(`presets: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 2. Assigning a camera to one tile replaces only that tile's island.
async function walkGridAssignment(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openGrid(browser, baseURL, password, {
    preset: 4,
    cameras: ["front_door"],
  });
  try {
    await waitForPlayingCount(page, 1);
    await page.evaluate(() => {
      document
        .querySelector('[data-live-tile="0"] [data-live-player]')
        .setAttribute("data-live-marker", "keep");
    });
    await page.selectOption('[data-live-tile="1"] select', "driveway");
    const response = page.waitForResponse(
      (result) =>
        result.url().includes("/ui/live/tile") &&
        result.request().method() === "POST",
      { timeout: 15000 },
    );
    await page.click('[data-live-tile="1"] button[type="submit"]');
    const replaced = await response;
    if (replaced.status() !== 200) {
      failures.push(`assignment: tile replacement status ${replaced.status()}`);
    }
    await waitForTileState(page, 1, "playing");
    if ((await page.locator("[data-live-tile]").count()) !== 4) {
      failures.push("assignment: assigning a camera changed the tile count");
    }
    if ((await page.locator("[data-live-player]").count()) !== 2) {
      failures.push("assignment: the new tile did not become a player");
    }
    const first = await page.evaluate(() => {
      const player = document.querySelector(
        '[data-live-tile="0"] [data-live-player]',
      );
      return {
        marker: player?.getAttribute("data-live-marker"),
        state: player?.getAttribute("data-player-state"),
        camera: player?.getAttribute("data-camera-id"),
      };
    });
    if (first.marker !== "keep") {
      failures.push("assignment: the other tile's player was replaced");
    }
    if (first.state !== "playing" || first.camera !== "front_door") {
      failures.push("assignment: the other tile stopped playing");
    }
    await page.reload({ waitUntil: "load" });
    await waitForTileState(page, 1, "playing");
    if ((await page.locator("[data-live-player]").count()) !== 2) {
      failures.push("assignment: the assignment was not remembered by the device");
    }
  } catch (error) {
    failures.push(`assignment: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 3. A status poll during playback leaves every player and the open picker.
async function walkGridPoll(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openGrid(browser, baseURL, password, {
    preset: 4,
    cameras: LIVE_CAMERAS,
  });
  try {
    await waitForPlayingCount(page, 4);
    await page.evaluate(() => {
      document.querySelectorAll("[data-live-player]").forEach((player) => {
        player.setAttribute("data-live-marker", "keep");
      });
      const details = document.querySelector('[data-live-tile="0"] details');
      details.open = true;
      details.querySelector("summary").focus();
    });
    const request = page.waitForRequest("**/ui/live/front_door/status");
    await page.evaluate(() => {
      const target = document.querySelector('[data-live-tile="0"] .live-camera-state');
      window.htmx.ajax("GET", target.getAttribute("hx-get"), {
        target: "#" + target.id,
        swap: "innerHTML",
      });
    });
    await request;
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-tile="0"] .live-camera-state')
          ?.textContent.includes("enabled"),
      { timeout: 5000 },
    );
    const after = await page.evaluate(() => {
      const players = Array.from(document.querySelectorAll("[data-live-player]"));
      const details = document.querySelector('[data-live-tile="0"] details');
      return {
        states: players.map((player) => player.getAttribute("data-player-state")),
        markers: players.map((player) => player.getAttribute("data-live-marker")),
        open: details.open,
        focus: document.activeElement?.tagName === "SUMMARY",
        tiles: document.querySelectorAll("[data-live-tile]").length,
        preset: document
          .querySelector('[data-live-preset="4"]')
          ?.getAttribute("aria-pressed"),
        swapped: players.some((player) =>
          player.closest("[hx-get],[hx-post],[hx-target],[hx-swap],[hx-trigger]"),
        ),
      };
    });
    if (after.tiles !== 4 || after.preset !== "true") {
      failures.push("poll: the poll changed the chosen layout");
    }
    if (after.states.some((state) => state !== "playing")) {
      failures.push(`poll: a player left playing after the poll: ${after.states}`);
    }
    if (after.markers.some((marker) => marker !== "keep")) {
      failures.push("poll: the poll replaced a player island");
    }
    if (!after.open) failures.push("poll: the poll closed the open camera picker");
    if (!after.focus) failures.push("poll: the poll moved focus out of the picker");
    if (after.swapped) failures.push("poll: a player is inside the polled region");
  } catch (error) {
    failures.push(`poll: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 4. One camera fails while the other three keep playing.
async function walkGridCameraFailure(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openGrid(browser, baseURL, password, {
    preset: 4,
    cameras: LIVE_CAMERAS,
    failingCamera: "driveway",
  });
  try {
    await waitForTileState(page, 1, "offline", 20000);
    await Promise.all([
      waitForTileState(page, 0, "playing"),
      waitForTileState(page, 2, "playing"),
      waitForTileState(page, 3, "playing"),
    ]);
    const states = await gridStates(page);
    if (states.length !== 4) {
      failures.push(`failure: ${states.length} tiles are playing or failed, want 4`);
    }
    if (states[1].state !== "offline") {
      failures.push(`failure: the failing camera is ${states[1].state}, want offline`);
    }
    for (const index of [0, 2, 3]) {
      if (states[index].state !== "playing") {
        failures.push(`failure: camera ${states[index].camera} stopped at ${states[index].state}`);
      }
    }
    const message = await page.evaluate(() => {
      const player = document.querySelector('[data-live-tile="1"] [data-live-player]');
      return {
        title: player.querySelector("[data-player-title]").textContent.trim(),
        detail: player.querySelector("[data-player-detail]").textContent.trim(),
      };
    });
    if (!message.title.includes("Driveway")) {
      failures.push(`failure: the failing tile does not name its camera: ${message.title}`);
    }
    if (!message.detail.toLowerCase().includes("offline")) {
      failures.push(`failure: the failing tile does not name the offline state: ${message.detail}`);
    }
  } catch (error) {
    failures.push(`failure: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 5. Full width switches one tile to the main profile and back without
// touching the other islands.
async function walkGridFullWidth(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openGrid(browser, baseURL, password, {
    preset: 4,
    cameras: LIVE_CAMERAS,
  });
  try {
    await waitForPlayingCount(page, 4);
    await page.evaluate(() => {
      document.querySelectorAll("[data-live-player]").forEach((player) => {
        player.setAttribute("data-live-marker", "keep");
      });
    });
    await page.click('[data-live-tile="1"] [data-live-expand]');
    await page.waitForFunction(
      () =>
        document
          .querySelector("[data-live-grid]")
          ?.getAttribute("data-live-full") === "true",
      { timeout: 5000 },
    );
    await waitForTileProfile(page, 1, "main");
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-full-width-1280x800.png"),
      fullPage: true,
    });
    const full = await gridStates(page);
    if (full.length !== 4) {
      failures.push("full width: a tile was removed from the page");
    }
    if (full[1].profile !== "main") {
      failures.push(`full width: the selected tile profile is ${full[1].profile}, want main`);
    }
    for (const index of [0, 2, 3]) {
      if (full[index].state !== "playing" || full[index].marker !== "keep") {
        failures.push(`full width: camera ${full[index].camera} did not keep playing`);
      }
    }
    await page.click('[data-live-tile="1"] [data-live-collapse]');
    await page.waitForFunction(
      () =>
        document.querySelector("[data-live-grid]")?.getAttribute("data-live-full") ===
        null,
      { timeout: 5000 },
    );
    await waitForTileProfile(page, 1, "sub");
    const grid = await gridStates(page);
    if (grid.length !== 4) {
      failures.push("full width: a tile was lost on the way back to the grid");
    }
    for (const index of [0, 1, 2, 3]) {
      if (grid[index].state !== "playing") {
        failures.push(`full width: camera ${grid[index].camera} is ${grid[index].state} after returning`);
      }
      if (grid[index].marker !== "keep") {
        failures.push(`full width: camera ${grid[index].camera} was replaced`);
      }
    }
  } catch (error) {
    failures.push(`full width: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 6. A phone clamps a four-tile layout to one live tile and says so.
async function walkGridCeilingNote(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openGrid(browser, baseURL, password, {
    viewport: { width: 390, height: 844 },
    preset: 4,
    cameras: LIVE_CAMERAS,
  });
  try {
    await waitForPlayingCount(page, 1, 15000);
    await page.waitForFunction(
      () => document.querySelectorAll("[data-live-player]").length === 4,
      { timeout: 5000 },
    );
    const state = await page.evaluate(() => {
      const players = Array.from(document.querySelectorAll("[data-live-player]"));
      const note = document.querySelector("[data-live-ceiling-note]");
      return {
        tiles: document.querySelectorAll("[data-live-tile]").length,
        playing: players.filter(
          (player) => player.getAttribute("data-player-state") === "playing",
        ).length,
        idle: players.filter(
          (player) => player.getAttribute("data-player-state") === "idle",
        ).length,
        sockets: window.__liveStub.sockets.length,
        ceiling: players[0]?.getAttribute("data-device-ceiling"),
        note: note && !note.hidden ? note.textContent.trim() : "",
        failed: document.querySelectorAll(".state-failed").length,
      };
    });
    if (state.tiles !== 4) failures.push(`phone ceiling: ${state.tiles} tiles, want 4`);
    if (state.playing !== 1) failures.push(`phone ceiling: ${state.playing} playing, want 1`);
    if (state.idle !== 3) failures.push(`phone ceiling: ${state.idle} idle, want 3`);
    if (state.sockets !== 1) failures.push(`phone ceiling: ${state.sockets} sockets, want 1`);
    if (state.ceiling !== "1") failures.push(`phone ceiling: ceiling ${state.ceiling}, want 1`);
    if (!state.note.includes("one camera")) {
      failures.push(`phone ceiling: the note does not name the limit: ${state.note}`);
    }
    if (state.failed !== 0) failures.push("phone ceiling: a clamped tile shows an error state");
  } catch (error) {
    failures.push(`phone ceiling: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

function overflowState(page) {
  return page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    scrollHeight: document.documentElement.scrollHeight,
    viewportWidth: window.innerWidth,
    viewportHeight: window.innerHeight,
    fullscreen: Boolean(document.fullscreenElement),
    bodyOverflow: getComputedStyle(document.body).overflow,
  }));
}

async function assertNoOverflow(page, label, failures) {
  const state = await overflowState(page);
  if (state.scrollWidth > state.viewportWidth + 1) {
    failures.push(
      `${label}: scrollWidth ${state.scrollWidth} exceeds ${state.viewportWidth}`,
    );
  }
  if (state.scrollHeight > state.viewportHeight + 1) {
    failures.push(
      `${label}: scrollHeight ${state.scrollHeight} exceeds ${state.viewportHeight}`,
    );
  }
}

async function assertFullscreenNoOverflow(page, label, failures) {
  const state = await overflowState(page);
  if (!state.fullscreen) {
    failures.push(`${label}: the browser did not enter full screen`);
  }
  await assertNoOverflow(page, `${label} in full screen`, failures);
  const player = await page.evaluate(() => {
    const root = document.fullscreenElement;
    if (!root) return null;
    const media = root.querySelector(".live-media");
    const video = root.querySelector("video");
    return {
      root: root.getBoundingClientRect().toJSON(),
      media: media ? media.getBoundingClientRect().toJSON() : null,
      video: video ? video.getBoundingClientRect().toJSON() : null,
    };
  });
  if (!player || !player.media || !player.video) {
    failures.push(`${label}: full screen has no media box or video`);
    return;
  }
  for (const [name, rect] of [["media", player.media], ["video", player.video]]) {
    if (
      rect.left < -1 ||
      rect.top < -1 ||
      rect.right > state.viewportWidth + 1 ||
      rect.bottom > state.viewportHeight + 1
    ) {
      failures.push(`${label}: the full-screen ${name} box leaves the viewport`);
    }
  }
}

// 7. A realistic stream cannot change the player geometry or make the page
// overflow, and the full-screen view contains the same stream.
async function walkLargeStream(browser, baseURL, password, width, resolution) {
  const failures = [];
  const context = await browser.newContext({
    viewport: { width: width.width, height: width.height },
  });
  try {
    const { page } = await openGrid(browser, baseURL, password, {
      viewport: { width: width.width, height: width.height },
      preset: 4,
      cameras: LIVE_CAMERAS,
      mode: "playing",
      videoWidth: resolution.width,
      videoHeight: resolution.height,
    });
    await waitForPlayingCount(page, width.width < 704 ? 1 : 4);
    const label = `${resolution.width}x${resolution.height} at ${width.width}px`;

    const before = await page.evaluate(() => {
      const media = document.querySelector(
        '[data-live-tile="0"] .live-media',
      );
      return media.getBoundingClientRect().toJSON();
    });
    const after = await page.evaluate(() => {
      const video = document.querySelector('[data-live-tile="0"] video');
      window.__liveStub.videoWidth = 0;
      window.__liveStub.videoHeight = 0;
      video.dispatchEvent(new Event("loadedmetadata"));
      const empty = document
        .querySelector('[data-live-tile="0"] .live-media')
        .getBoundingClientRect()
        .toJSON();
      window.__liveStub.videoWidth = 0;
      window.__liveStub.videoHeight = 0;
      return empty;
    });
    if (
      Math.abs(before.width - after.width) > 1 ||
      Math.abs(before.height - after.height) > 1
    ) {
      failures.push(
        `${label}: the player box moved when metadata changed ` +
          `(${Math.round(before.width)}x${Math.round(before.height)} to ` +
          `${Math.round(after.width)}x${Math.round(after.height)})`,
      );
    }
    await assertNoOverflow(page, `${label} grid`, failures);
    await page.screenshot({
      path: path.join(
        SCREENSHOT_DIR,
        `live-large-${resolution.width}x${resolution.height}-${width.width}x${width.height}.png`,
      ),
      fullPage: true,
    });

    await page.click('[data-live-tile="0"] [data-live-fullscreen]');
    await page.waitForFunction(() => Boolean(document.fullscreenElement), null, {
      timeout: 5000,
    });
    await assertFullscreenNoOverflow(page, label, failures);
    await page.screenshot({
      path: path.join(
        SCREENSHOT_DIR,
        `live-full-screen-${resolution.width}x${resolution.height}-${width.width}x${width.height}.png`,
      ),
      fullPage: true,
    });
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => !document.fullscreenElement, null, {
      timeout: 5000,
    });
    if (await page.evaluate(() => document.fullscreenElement)) {
      failures.push(`${label}: Escape did not leave full screen`);
    }

    await page.click('[data-live-tile="0"] [data-live-fullscreen]');
    await page.waitForFunction(() => Boolean(document.fullscreenElement), null, {
      timeout: 5000,
    });
    await page.evaluate(() => window.history.back());
    await page.waitForFunction(() => !document.fullscreenElement, null, {
      timeout: 5000,
    });
    if (await page.evaluate(() => document.fullscreenElement)) {
      failures.push(`${label}: Back did not leave full screen`);
    }
  } catch (error) {
    failures.push(
      `${resolution.width}x${resolution.height} at ${width.width}px: ${error.message}`,
    );
  } finally {
    await context.close();
  }
  return failures;
}

// 8. The media box is reserved before the first frame and does not move when
// video metadata arrives.
async function walkReservedBox(browser, baseURL, password) {
  const failures = [];
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
  });
  try {
    const { page } = await openGrid(browser, baseURL, password, {
      viewport: { width: 1280, height: 800 },
      preset: 4,
      cameras: LIVE_CAMERAS,
      mode: "connecting",
    });
    const before = await page.evaluate(() => {
      const media = document.querySelector(
        '[data-live-tile="0"] .live-media',
      );
      return {
        media: media.getBoundingClientRect().toJSON(),
        video: media.querySelector("video").getBoundingClientRect().toJSON(),
      };
    });
    await page.evaluate(() => {
      const video = document.querySelector('[data-live-tile="0"] video');
      window.__liveStub.videoWidth = 3840;
      window.__liveStub.videoHeight = 2160;
      video.dispatchEvent(new Event("loadedmetadata"));
    });
    const after = await page.evaluate(() => {
      const media = document.querySelector(
        '[data-live-tile="0"] .live-media',
      );
      return {
        media: media.getBoundingClientRect().toJSON(),
        video: media.querySelector("video").getBoundingClientRect().toJSON(),
      };
    });
    for (const name of ["media", "video"]) {
      if (
        Math.abs(before[name].width - after[name].width) > 1 ||
        Math.abs(before[name].height - after[name].height) > 1
      ) {
        failures.push(
          `reserved box: ${name} moved from ` +
            `${Math.round(before[name].width)}x${Math.round(before[name].height)} to ` +
            `${Math.round(after[name].width)}x${Math.round(after[name].height)}`,
        );
      }
    }
  } catch (error) {
    failures.push(`reserved box: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

export async function runLivePlayerChecks({ browser, baseURL, adminPassword }) {
  const failures = [];
  for (const state of PLAYER_STATES) {
    failures.push(...(await walkState(browser, baseURL, adminPassword, state)));
  }
  failures.push(...(await walkCeilings(browser, baseURL, adminPassword)));
  failures.push(...(await walkGridPresets(browser, baseURL, adminPassword)));
  failures.push(...(await walkGridAssignment(browser, baseURL, adminPassword)));
  failures.push(...(await walkGridPoll(browser, baseURL, adminPassword)));
  failures.push(...(await walkGridCameraFailure(browser, baseURL, adminPassword)));
  failures.push(...(await walkGridFullWidth(browser, baseURL, adminPassword)));
  failures.push(...(await walkGridCeilingNote(browser, baseURL, adminPassword)));
  const largeWidths = [
    { width: 360, height: 800 },
    { width: 390, height: 844 },
    { width: 1280, height: 800 },
    { width: 1920, height: 1080 },
  ];
  for (const resolution of [
    { width: 1920, height: 1080 },
    { width: 3840, height: 2160 },
  ]) {
    for (const width of largeWidths) {
      failures.push(
        ...(await walkLargeStream(
          browser,
          baseURL,
          adminPassword,
          width,
          resolution,
        )),
      );
    }
  }
  failures.push(...(await walkReservedBox(browser, baseURL, adminPassword)));
  return failures;
}
