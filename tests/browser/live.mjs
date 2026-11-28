// Browser checks for the Bittern live wall: the six layouts, the per-pane
// quality control, the capacity model and its hysteresis, the overlay, full
// screen, snapshots and the keyboard model (ADR 0037). The checks use a stub
// WebSocket, MediaSource and playback-quality counter so every state can be
// walked without a camera, a go2rtc process or a Frigate container.

import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const SCREENSHOT_DIR = path.resolve(HERE, "../../build/ui-screenshots");

const GRID_PATH = "/ui/live";
const SINGLE_PATH = "/ui/live/front_door";
const LAYOUTS = [1, 4, 6, 8, 9, 16];
// A shown pane overlay may take at most 30 percent of its pane at sixteen
// panes. The old select, hint and submit stack took far more; this budget is
// measured at the smallest pane on a phone and on a desktop.
const OVERLAY_AREA_BUDGET = 0.3;
const WIDTHS = [
  { width: 360, height: 800 },
  { width: 390, height: 844 },
  { width: 1280, height: 800 },
  { width: 1920, height: 1080 },
];
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

// Player states (8), class priors (4), layouts at four widths (24), overlay
// (2), manual quality (1), capacity hysteresis (4), snapshot panes (1),
// keyboard (1), full screen and its refusal (2), in-place controls (1), the
// icon picker (1), placeholders (1), the stream selector (1), compact controls
// (1), click-to-dismiss (1), large streams at four widths (8).
export const LIVE_CHECK_COUNT = 8 + 4 + 24 + 2 + 1 + 4 + 1 + 1 + 2 + 1 + 1 + 1 + 1 + 1 + 1 + 8;

const STUB = ({ mode, failingCamera, videoWidth, videoHeight, drop }) => {
  window.__liveStub = {
    mode,
    failingCamera: failingCamera || "",
    videoWidth: videoWidth || 0,
    videoHeight: videoHeight || 0,
    drop: drop || 0,
    startAt: 0,
    sockets: [],
  };

  class FakeBuffer extends EventTarget {
    constructor() {
      super();
      this.updating = false;
      this.buffered = { length: 1, start: () => 0, end: () => 1 };
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
        setTimeout(
          () => this.deliver(JSON.stringify({ type: "error", value: "mse: codec is not supported" })),
          5,
        );
        return;
      }
      setTimeout(() => {
        this.deliver(JSON.stringify({ type: "mse", value: 'video/mp4; codecs="avc1.42E01E"' }));
        if (mode === "stalled") return;
        this.deliver(new ArrayBuffer(32));
        this.keepAlive = setInterval(() => {
          if (this.readyState === FakeWebSocket.OPEN) this.deliver(new ArrayBuffer(32));
        }, 500);
      }, 5);
    }

    deliver(data) {
      this.dispatchEvent(new MessageEvent("message", { data }));
    }

    send(data) {
      window.__liveStub.lastSent = data;
    }

    close() {
      if (this.keepAlive) clearInterval(this.keepAlive);
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
  Object.defineProperty(HTMLVideoElement.prototype, "videoWidth", {
    configurable: true,
    get() {
      return window.__liveStub.videoWidth || 0;
    },
  });
  Object.defineProperty(HTMLVideoElement.prototype, "videoHeight", {
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
  // The ADR's capacity signal. Every hundred frames carries the configured
  // share as dropped frames, measured from the first read, so a synthetic
  // sequence is deterministic. getVideoPlaybackQuality lives on
  // HTMLVideoElement, not HTMLMediaElement.
  HTMLVideoElement.prototype.getVideoPlaybackQuality = function () {
    if (!window.__liveStub.startAt) window.__liveStub.startAt = Date.now();
    const total = Math.max(0, Math.floor((Date.now() - window.__liveStub.startAt) / 10));
    const dropped = Math.floor(total * (window.__liveStub.drop || 0));
    return { droppedVideoFrames: dropped, totalVideoFrames: total };
  };
  // Chromium reports "maybe" for HLS. The state walk is deterministic MSE
  // only; the real HLS fallback is covered by the proxy and page checks.
  HTMLMediaElement.prototype.canPlayType = () => "";
};

function layoutCookie(layout, cameras, qualities = []) {
  const values = new URLSearchParams();
  values.set("v", "2");
  values.set("p", String(layout));
  for (const camera of cameras) values.append("t", camera);
  for (const quality of qualities) values.append("q", quality);
  return values.toString();
}

async function setLayoutCookie(context, baseURL, layout, cameras, qualities) {
  const host = new URL(baseURL).hostname;
  await context.addCookies([
    {
      name: "bittern_live_layout",
      value: layoutCookie(layout, cameras, qualities),
      domain: host,
      path: "/ui/live",
      httpOnly: true,
      secure: false,
      sameSite: "Strict",
    },
  ]);
}

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

async function openWall(browser, baseURL, password, options = {}) {
  const context = await browser.newContext({
    viewport: options.viewport || { width: 1280, height: 800 },
    hasTouch: options.hasTouch === true,
  });
  if (options.layout) {
    await setLayoutCookie(context, baseURL, options.layout, options.cameras || [], options.qualities);
  }
  const page = await context.newPage();
  if (options.clock) await page.clock.install();
  await signIn(page, baseURL, password);
  await page.addInitScript(STUB, {
    mode: options.mode || "playing",
    failingCamera: options.failingCamera || "",
    videoWidth: options.videoWidth || 0,
    videoHeight: options.videoHeight || 0,
    drop: options.drop || 0,
  });
  await page.goto(`${baseURL}${options.path || GRID_PATH}`, { waitUntil: "load" });
  return { context, page };
}

async function waitForState(page, selector, state, timeout = 15000) {
  await page.waitForFunction(
    ({ selector, state }) =>
      document.querySelector(selector)?.getAttribute("data-player-state") === state,
    { selector, state },
    { timeout },
  );
}

async function waitForPlayingCount(page, count, timeout = 20000) {
  await page.waitForFunction(
    (expected) =>
      document.querySelectorAll(
        '[data-live-pane]:not([hidden]) [data-live-player][data-player-state="playing"]',
      ).length === expected,
    count,
    { timeout },
  );
}

async function showOverlay(page) {
  const box = await page.locator("[data-live-grid]").boundingBox();
  // Two moves, so a pointer that already sits on the reveal point still
  // produces a pointermove event.
  await page.mouse.move(box.x + 2, box.y + 2);
  await page.waitForTimeout(300);
  await page.mouse.move(box.x + box.width / 2, box.y + box.height * 0.2);
  // The reveal throttle can swallow the second move on a narrow wall. The
  // helper is for reaching the controls; the pointer-reveal test uses the
  // mouse directly.
  await page.evaluate(() => {
    const wall = window.BitternLiveWall;
    if (wall.selected) wall.selected.root.focus();
    wall.showOverlay();
    wall.suspendHide();
  });
  try {
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]")?.getAttribute("data-live-overlay") === "shown",
      null,
      { timeout: 5000 },
    );
  } catch (error) {
    const state = await page.evaluate(() => ({
      wall: Boolean(document.querySelector("[data-live-grid]")),
      overlay: document.querySelector("[data-live-grid]")?.getAttribute("data-live-overlay"),
      idle: document.querySelector("[data-live-grid]")?.getAttribute("data-live-idle"),
      activePane: document.querySelector("[data-live-pane][data-live-selected='true']")?.getAttribute("data-live-pane"),
      instance: Boolean(window.BitternLiveWall),
      panel: Boolean(document.querySelector("[data-live-overlay-panel]")),
    }));
    throw new Error(`${error.message} ${JSON.stringify(state)}`);
  }
}

function focusSelectedPane(page) {
  return page.evaluate(() => {
    document.querySelector("[data-live-pane][data-live-selected='true']")?.focus();
  });
}

async function selectPane(page, number) {
  const box = await page.locator(`[data-live-pane="${number}"]`).boundingBox();
  // A point low in the pane avoids the wall controls at the top of the wall.
  await page.mouse.click(box.x + box.width / 2, box.y + box.height * 0.8);
  await page.waitForFunction(
    (n) =>
      document
        .querySelector(`[data-live-pane="${n}"]`)
        ?.getAttribute("data-live-selected") === "true",
    number,
    { timeout: 5000 },
  );
  // A click on the video dismisses the overlay by design. Move the pointer back
  // over the selected pane after the reveal throttle so the caller can inspect
  // its controls.
  await page.waitForTimeout(300);
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.waitForFunction(
    (n) =>
      document
        .querySelector(`[data-live-pane="${n}"] [data-live-pane-overlay]`)
        ?.getAttribute("data-live-pane-overlay-state") === "shown",
    number,
    { timeout: 5000 },
  );
}

async function chooseQuality(page, pane, value) {
  await page
    .locator(`[data-live-pane="${pane}"] label.live-quality-choice:has(input[value="${value}"])`)
    .first()
    .click();
}

// paneGeometry measures the wall and every pane in one read, so the assertions
// below compare rectangles from the same layout pass.
function paneGeometry(page) {
  return page.evaluate(() => {
    const wall = document.querySelector("[data-live-grid]");
    const rect = (element) => {
      const box = element.getBoundingClientRect();
      return {
        left: box.left,
        top: box.top,
        right: box.right,
        bottom: box.bottom,
        width: box.width,
        height: box.height,
      };
    };
    return {
      wall: rect(wall),
      panes: Array.from(document.querySelectorAll("[data-live-pane]:not([hidden])")).map((pane) =>
        rect(pane),
      ),
      viewport: { width: window.innerWidth, height: window.innerHeight },
      scrollWidth: document.documentElement.scrollWidth,
      scrollHeight: document.documentElement.scrollHeight,
      header: rect(document.querySelector(".site-header")),
      overlay: document.querySelector("[data-live-overlay-panel]")
        ? document.querySelector("[data-live-overlay-panel]").getAttribute("data-live-overlay")
        : null,
      overlayHidden: wall.getAttribute("data-live-overlay"),
      fullscreen: Boolean(document.fullscreenElement),
      fullscreenRoot: document.fullscreenElement
        ? document.fullscreenElement.getAttribute("data-live-grid")
        : null,
    };
  });
}

// assertPaneGeometry proves the panes fill the wall with only the one pixel
// gap between them: no border, padding, card, toolbar or gap can creep back in.
function assertPaneGeometry(geometry, label, failures, expectedPanes) {
  const wall = geometry.wall;
  const panes = geometry.panes;
  if (panes.length !== expectedPanes) {
    failures.push(`${label}: ${panes.length} panes, want ${expectedPanes}`);
    return;
  }
  if (geometry.scrollWidth > geometry.viewport.width + 1) {
    failures.push(
      `${label}: page scrollWidth ${geometry.scrollWidth} exceeds ${geometry.viewport.width}`,
    );
  }
  if (geometry.scrollHeight > geometry.viewport.height + 1) {
    failures.push(
      `${label}: page scrollHeight ${geometry.scrollHeight} exceeds ${geometry.viewport.height}`,
    );
  }
  if (Math.abs(wall.left) > 1 || Math.abs(wall.right - geometry.viewport.width) > 1) {
    failures.push(
      `${label}: the wall spans ${Math.round(wall.left)} to ${Math.round(wall.right)}, ` +
        `not the ${geometry.viewport.width}px viewport`,
    );
  }
  if (Math.abs(wall.bottom - geometry.viewport.height) > 1) {
    failures.push(
      `${label}: the wall ends at ${Math.round(wall.bottom)}, not the ` +
        `${geometry.viewport.height}px viewport bottom`,
    );
  }
  if (Math.abs(wall.top - geometry.header.bottom) > 1.5) {
    failures.push(
      `${label}: ${Math.round(wall.top - geometry.header.bottom)}px of chrome sits between ` +
        `the header and the wall`,
    );
  }
  let covered = 0;
  for (const pane of panes) {
    covered += pane.width * pane.height;
    if (
      pane.left < wall.left - 1 ||
      pane.top < wall.top - 1 ||
      pane.right > wall.right + 1 ||
      pane.bottom > wall.bottom + 1
    ) {
      failures.push(`${label}: a pane leaves the wall`);
    }
  }
  const wallArea = wall.width * wall.height;
  if (wallArea > 0 && covered / wallArea < 0.96) {
    failures.push(
      `${label}: the panes cover ${(100 * covered / wallArea).toFixed(1)} percent of the ` +
        `wall; the hairline gap should leave more than 96 percent`,
    );
  }
  // No two panes overlap. Each pane's nearest neighbour in every direction is
  // one hairline away, and a pane with no neighbour reaches the wall edge, so
  // no border, padding, card or toolbar can creep back between panes.
  // "vertical" means the two vertical ranges overlap, which is the condition
  // for a neighbour to the left or right.
  const overlaps = (a, b, axis) =>
    axis === "vertical"
      ? Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 0.5
      : Math.min(a.right, b.right) - Math.max(a.left, b.left) > 0.5;
  for (let i = 0; i < panes.length; i += 1) {
    for (let j = i + 1; j < panes.length; j += 1) {
      if (overlaps(panes[i], panes[j], "vertical") && overlaps(panes[i], panes[j], "horizontal")) {
        failures.push(`${label}: panes ${i + 1} and ${j + 1} overlap`);
      }
    }
  }
  const directions = [
    {
      name: "right",
      axis: "vertical",
      separation: (pane, other) => other.left - pane.right,
      wallEdge: (pane) => wall.right - pane.right,
    },
    {
      name: "left",
      axis: "vertical",
      separation: (pane, other) => pane.left - other.right,
      wallEdge: (pane) => pane.left - wall.left,
    },
    {
      name: "below",
      axis: "horizontal",
      separation: (pane, other) => other.top - pane.bottom,
      wallEdge: (pane) => wall.bottom - pane.bottom,
    },
    {
      name: "above",
      axis: "horizontal",
      separation: (pane, other) => pane.top - other.bottom,
      wallEdge: (pane) => pane.top - wall.top,
    },
  ];
  for (let i = 0; i < panes.length; i += 1) {
    for (const direction of directions) {
      let nearest = null;
      for (let j = 0; j < panes.length; j += 1) {
        if (i === j) continue;
        if (!overlaps(panes[i], panes[j], direction.axis)) continue;
        const separation = direction.separation(panes[i], panes[j]);
        if (separation < -0.5) continue;
        if (nearest === null || separation < nearest) nearest = separation;
      }
      if (nearest === null) {
        if (Math.abs(direction.wallEdge(panes[i])) > 1) {
          failures.push(
            `${label}: pane ${i + 1} has no neighbour ${direction.name} and does not ` +
              `reach the wall edge (${direction.wallEdge(panes[i]).toFixed(1)}px)`,
          );
        }
        continue;
      }
      if (nearest > 1.5) {
        failures.push(
          `${label}: pane ${i + 1} is ${nearest.toFixed(1)}px from its nearest ` +
            `neighbour ${direction.name}; the gap is one hairline`,
        );
      }
    }
  }
}

async function enterFullScreen(page) {
  await showOverlay(page);
  await page.evaluate(() => window.BitternLiveWall.showOverlay());
  await page.evaluate(() => {
    const button = document.querySelector("[data-live-fullscreen]");
    if (button) button.hidden = false;
  });
  await page.click("[data-live-fullscreen]");
  await page.waitForFunction(() => Boolean(document.fullscreenElement), null, { timeout: 5000 });
}

async function leaveFullScreen(page) {
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => !document.fullscreenElement, null, { timeout: 5000 });
}

function playerMessage(page, pane = 1) {
  return page.evaluate((number) => {
    const root = document.querySelector(`[data-live-pane="${number}"]`);
    const player = root ? root.querySelector("[data-live-player]") : null;
    const note = root ? root.querySelector("[data-player-note]") : null;
    return {
      state: player ? player.getAttribute("data-player-state") : null,
      quality: player ? player.getAttribute("data-quality") : null,
      profile: player ? player.getAttribute("data-profile") : null,
      manual: player ? player.getAttribute("data-manual-quality") : null,
      note: note && !note.hidden ? note.textContent.trim() : "",
    };
  }, pane);
}

// 1. The player states on the single-camera route still work.
async function walkState(browser, baseURL, password, mode) {
  const failures = [];
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
  try {
    let page;
    if (mode === "stalled" || mode === "playing") {
      page = await context.newPage();
      await page.clock.install();
      await signIn(page, baseURL, password);
      await page.addInitScript(STUB, { mode });
      await page.goto(`${baseURL}${SINGLE_PATH}`, { waitUntil: "load" });
      await page.clock.runFor(200);
    } else if (mode === "session") {
      page = await context.newPage();
      await signIn(page, baseURL, password);
      await page.addInitScript(STUB, { mode });
      await page.route("**/snapshot.jpg*", (route) => route.fulfill({ status: 401, body: "" }));
      await page.goto(`${baseURL}${SINGLE_PATH}`, { waitUntil: "load" });
    } else {
      page = await context.newPage();
      await signIn(page, baseURL, password);
      await page.addInitScript(STUB, { mode });
      await page.goto(`${baseURL}${SINGLE_PATH}`, { waitUntil: "load" });
    }
    if (mode === "switching") {
      await waitForState(page, "[data-live-player]", "playing");
    } else {
      await waitForState(page, "[data-live-player]", mode, mode === "offline" ? 25000 : 15000);
    }
    const player = await page.evaluate(() => {
      const root = document.querySelector("[data-live-player]");
      const video = root ? root.querySelector("video") : null;
      const inSwap = (node) =>
        !!node && !!node.closest("[hx-get],[hx-post],[hx-target],[hx-swap],[hx-trigger]");
      return {
        player: !!root,
        video: !!video,
        playerSwapped: inSwap(root),
        videoSwapped: inSwap(video),
      };
    });
    if (!player.player || !player.video) failures.push(`${mode}: player or video is missing`);
    if (player.playerSwapped || player.videoSwapped) {
      failures.push(`${mode}: the player island is inside an HTMX swap region`);
    }
    if (mode === "offline") {
      const message = await playerMessage(page);
      if (!message.note.toLowerCase().includes("offline")) {
        failures.push(`offline: the note does not name the offline state: ${message.note}`);
      }
    }
    if (mode === "session") {
      const message = await playerMessage(page);
      if (!message.note.toLowerCase().includes("sign in")) {
        failures.push(`session: the note does not name the next action: ${message.note}`);
      }
    }
    if (mode === "playing") {
      await page.evaluate(() => {
        const player = document.querySelector("[data-live-player]");
        player.dataset.liveMarker = "keep";
        const mesh = {
          player: player,
          state: player.getAttribute("data-player-state"),
        };
        window.__liveMesh = mesh;
      });
      await showOverlay(page);
      const request = page.waitForRequest("**/ui/live/front_door/status");
      await page.evaluate(() => {
        const target = document.querySelector("[data-live-camera-state-region]");
        window.htmx.ajax("GET", target.getAttribute("hx-get"), {
          target: "[data-live-camera-state-region]",
          swap: "innerHTML",
        });
      });
      await request;
      await page.waitForFunction(
        () =>
          document
            .querySelector("[data-live-camera-state-region]")
            ?.textContent.includes("enabled"),
        { timeout: 5000 },
      );
      const after = await page.evaluate(() => {
        const player = document.querySelector("[data-live-player]");
        return {
          sameNode: player === window.__liveMesh.player,
          state: player.getAttribute("data-player-state"),
          marker: player.dataset.liveMarker,
        };
      });
      if (!after.sameNode) failures.push("playing: the camera poll replaced the player");
      if (after.state !== "playing" || after.marker !== "keep") {
        failures.push(`playing: the player is ${after.state} after the camera poll`);
      }
    }
    if (mode === "switching") {
      await showOverlay(page);
      await page.click('[data-profile-link="sub"]');
      await waitForState(page, "[data-live-player]", "switching");
      if (!page.url().includes("profile=sub")) {
        failures.push("switching: the profile URL did not change");
      }
      await waitForState(page, "[data-live-player]", "playing");
      const message = await playerMessage(page);
      if (message.profile !== "sub") {
        failures.push(`switching: the player is on ${message.profile}, want sub`);
      }
    }
  } catch (error) {
    failures.push(`${mode}: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 2. The class prior is 4, 8 or 16 by viewport and is exposed on the wall.
async function walkCeilings(browser, baseURL, password) {
  const failures = [];
  const cases = [
    { width: 360, height: 800, prior: 4 },
    { width: 390, height: 844, prior: 4 },
    { width: 1280, height: 800, prior: 16 },
    { width: 1920, height: 1080, prior: 16 },
  ];
  for (const testCase of cases) {
    const label = `${testCase.width}px prior`;
    try {
      const { context, page } = await openWall(browser, baseURL, password, {
        viewport: testCase,
        path: SINGLE_PATH,
      });
      try {
        await page.waitForFunction(
          (prior) =>
            document.querySelector("[data-live-grid]")?.getAttribute("data-device-prior") ===
            String(prior),
          testCase.prior,
          { timeout: 10000 },
        );
        const state = await page.evaluate(() => {
          const wall = document.querySelector("[data-live-grid]");
          return {
            prior: wall.getAttribute("data-device-prior"),
            capacity: wall.getAttribute("data-live-capacity"),
            live: wall.getAttribute("data-live-count"),
          };
        });
        if (state.capacity !== String(testCase.prior)) {
          failures.push(`${label}: capacity ${state.capacity}, want ${testCase.prior}`);
        }
        if (state.live !== "1") failures.push(`${label}: ${state.live} live panes, want 1`);
      } finally {
        await context.close();
      }
    } catch (error) {
      failures.push(`${label}: ${error.message}`);
    }
  }
  // The tablet class is its own prior of 8.
  try {
    const { context, page } = await openWall(browser, baseURL, password, {
      viewport: { width: 900, height: 800 },
      layout: 16,
      cameras: ["front_door", "driveway", "side_gate", "back_garden"],
      path: SINGLE_PATH,
    });
    try {
      const prior = await page.evaluate(() =>
        document.querySelector("[data-live-grid]").getAttribute("data-device-prior"),
      );
      if (prior !== "8") failures.push(`tablet prior: ${prior}, want 8`);
    } finally {
      await context.close();
    }
  } catch (error) {
    failures.push(`tablet prior: ${error.message}`);
  }
  return failures;
}

// 3. Every layout at every width: the panes fill the wall with only the
// hairline gap, no page or container overflow, and full screen fills the
// viewport exactly for the same layout.
async function walkLayouts(browser, baseURL, password) {
  const failures = [];
  await mkdir(SCREENSHOT_DIR, { recursive: true });
  for (const width of WIDTHS) {
    const { context, page } = await openWall(browser, baseURL, password, {
      viewport: width,
      layout: 4,
      cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    });
    try {
      for (const layout of LAYOUTS) {
        const label = `${layout} panes at ${width.width}x${width.height}`;
        await setLayoutCookie(
          context,
          baseURL,
          layout,
          ["front_door", "driveway", "side_gate", "back_garden"],
        );
        await page.goto(`${baseURL}${GRID_PATH}`, { waitUntil: "load" });
        await page.waitForFunction(
          (count) =>
            document.querySelectorAll("[data-live-pane]:not([hidden])").length === count,
          layout,
          { timeout: 10000 },
        );
        const geometry = await paneGeometry(page);
        assertPaneGeometry(geometry, label, failures, layout);
        // Read the overlay shown and hidden at every layout and width. This
        // is a capture, not the interaction test, so it selects pane one
        // directly instead of clicking through a tall wall overlay.
        await page.evaluate(() => {
          const wall = window.BitternLiveWall;
          const pane = wall.panes.find((candidate) => candidate.number === 1);
          wall.select(pane, false);
          wall.showPaneOverlay(pane);
          wall.showOverlay();
        });
        await page.waitForFunction(
          () =>
            document
              .querySelector('[data-live-pane="1"] [data-live-pane-overlay]')
              ?.getAttribute("data-live-pane-overlay-state") === "shown",
          null,
          { timeout: 5000 },
        );
        await page.screenshot({
          path: path.join(
            SCREENSHOT_DIR,
            `live-wall-${layout}-overlay-${width.width}x${width.height}.png`,
          ),
          fullPage: false,
        });
        await page.evaluate(() => window.BitternLiveWall.hideAllOverlays());
        await page.screenshot({
          path: path.join(SCREENSHOT_DIR, `live-wall-${layout}-${width.width}x${width.height}.png`),
          fullPage: false,
        });
        await enterFullScreen(page);
        const full = await paneGeometry(page);
        if (!full.fullscreen) failures.push(`${label}: the browser did not enter full screen`);
        if (full.fullscreenRoot !== "true") {
          failures.push(`${label}: full screen is not the wall`);
        }
        if (
          Math.abs(full.wall.left) > 1 ||
          Math.abs(full.wall.top) > 1 ||
          Math.abs(full.wall.right - width.width) > 1 ||
          Math.abs(full.wall.bottom - width.height) > 1
        ) {
          failures.push(
            `${label}: the full screen wall is ${Math.round(full.wall.left)},` +
              `${Math.round(full.wall.top)} to ${Math.round(full.wall.right)},` +
              `${Math.round(full.wall.bottom)} in ${width.width}x${width.height}`,
          );
        }
        if (full.scrollHeight > width.height + 1) {
          failures.push(`${label}: full screen scrolls (${full.scrollHeight}px)`);
        }
        assertPaneGeometry(
          { ...full, header: { bottom: 0 } },
          `${label} full screen`,
          failures,
          layout,
        );
        // Wait out the overlay so the full-screen image shows the wall only.
        await page.waitForTimeout(3300);
        await page.screenshot({
          path: path.join(
            SCREENSHOT_DIR,
            `live-wall-${layout}-full-${width.width}x${width.height}.png`,
          ),
          fullPage: false,
        });
        await leaveFullScreen(page);
      }
    } catch (error) {
      failures.push(`layouts at ${width.width}x${width.height}: ${error.message}`);
    } finally {
      await context.close();
    }
  }
  return failures;
}

// 4. The overlay: hidden and inert when idle, click-through while hidden,
// revealed by pointer movement, a key press, a tap and focus, with Tab
// revealing it and focusing the quality control. The no-JavaScript path shows
// the same overlay without the script hiding it.
async function walkOverlay(browser, baseURL, password) {
  const failures = [];
  let step = "load";
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    hasTouch: true,
  });
  try {
    step = "boot";
    await page.waitForFunction(
      () =>
        document.querySelector("[data-live-grid]")?.getAttribute("data-live-overlay") ===
          "hidden" &&
        Array.from(document.querySelectorAll("[data-live-pane-overlay]")).every(
          (overlay) => overlay.getAttribute("data-live-pane-overlay-state") === "hidden",
        ),
      null,
      { timeout: 10000 },
    );
    await page.waitForTimeout(250);
    const hidden = await page.evaluate(() => {
      const wall = document.querySelector("[data-live-grid]");
      const panel = document.querySelector("[data-live-overlay-panel]");
      const panes = Array.from(document.querySelectorAll("[data-live-pane]:not([hidden])"));
      const last = panes[panes.length - 1].getBoundingClientRect();
      const point = document.elementFromPoint(
        last.left + last.width / 2,
        last.top + last.height / 2,
      );
      const overlays = Array.from(document.querySelectorAll("[data-live-pane-overlay]"));
      return {
        inert: panel.hasAttribute("inert"),
        pointerEvents: getComputedStyle(panel).pointerEvents,
        visibility: getComputedStyle(panel).visibility,
        topmostInPanel: Boolean(point && point.closest("[data-live-overlay-panel]")),
        opaque: getComputedStyle(panel).opacity,
        wallState: wall.getAttribute("data-live-overlay"),
        hiddenPaneOverlays: overlays.filter(
          (overlay) => overlay.getAttribute("data-live-pane-overlay-state") === "hidden",
        ).length,
        paneOverlays: overlays.length,
        paneOverlayInert: overlays.every((overlay) => overlay.hasAttribute("inert")),
        paneOverlayPointer: overlays.every(
          (overlay) => getComputedStyle(overlay).pointerEvents === "none",
        ),
      };
    });
    if (!hidden.inert) failures.push("overlay: the hidden wall panel is not inert");
    if (hidden.pointerEvents !== "none") {
      failures.push(`overlay: the hidden wall panel takes pointer events (${hidden.pointerEvents})`);
    }
    if (hidden.visibility !== "hidden") failures.push("overlay: the hidden wall panel is still visible");
    if (hidden.opaque !== "0") failures.push(`overlay: the hidden wall panel is opaque (${hidden.opaque})`);
    if (hidden.topmostInPanel) failures.push("overlay: the hidden wall panel covers the video");
    if (hidden.wallState !== "hidden") failures.push("overlay: the wall is not in the hidden state");
    if (hidden.hiddenPaneOverlays !== hidden.paneOverlays) {
      failures.push(
        `overlay: ${hidden.paneOverlays - hidden.hiddenPaneOverlays} pane overlays stay shown`,
      );
    }
    if (!hidden.paneOverlayInert) failures.push("overlay: a hidden pane overlay is not inert");
    if (!hidden.paneOverlayPointer) {
      failures.push("overlay: a hidden pane overlay takes pointer events");
    }
    // A tap on the last pane selects it and shows only its overlay. The pane
    // overlay is inside that pane, so no pane control sits outside it.
    step = "tap";
    const tapBox = await page.locator('[data-live-pane="4"]').boundingBox();
    await page.touchscreen.tap(tapBox.x + tapBox.width / 2, tapBox.y + tapBox.height / 2);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="4"]')
          ?.getAttribute("data-live-selected") === "true",
      null,
      { timeout: 5000 },
    );
    step = "tap reveal";
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="4"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    const shown = await page.evaluate(() => {
      const pane = document.querySelector('[data-live-pane="4"]');
      const overlay = pane.querySelector("[data-live-pane-overlay]");
      return {
        shown: Array.from(document.querySelectorAll("[data-live-pane-overlay]")).filter(
          (candidate) => candidate.getAttribute("data-live-pane-overlay-state") === "shown",
        ).length,
        qualityInside: Boolean(overlay.querySelector("[data-live-quality-group]")),
        cameraInside: Boolean(overlay.querySelector("[data-live-camera-select]")),
        controlsOutside: document.querySelectorAll(
          "[data-live-overlay-panel] [data-live-quality-group], [data-live-overlay-panel] [data-live-quality-choice], [data-live-overlay-panel] [data-live-camera-select]",
        ).length,
      };
    });
    if (shown.shown !== 1) failures.push(`overlay: ${shown.shown} pane overlays are shown`);
    if (!shown.qualityInside) failures.push("overlay: the quality selector is not in its pane");
    if (!shown.cameraInside) failures.push("overlay: the camera chooser is not in its pane");
    if (shown.controlsOutside !== 0) {
      failures.push("overlay: a pane control sits in the wall overlay");
    }
    // Idle hides it after three seconds.
    step = "idle hide";
    await page.waitForFunction(
      () =>
        document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") ===
          "hidden" &&
        document
          .querySelector('[data-live-pane="4"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "hidden",
      null,
      { timeout: 8000 },
    );
    // Pointer movement over a pane reveals that pane's overlay.
    step = "pointer reveal";
    const paneBox = await page.locator('[data-live-pane="1"]').boundingBox();
    await page.mouse.move(paneBox.x + 4, paneBox.y + 4);
    await page.mouse.move(
      paneBox.x + paneBox.width / 2,
      paneBox.y + paneBox.height / 2,
    );
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    const pointerShown = await page.evaluate(
      () =>
        Array.from(document.querySelectorAll("[data-live-pane-overlay]")).filter(
          (overlay) => overlay.getAttribute("data-live-pane-overlay-state") === "shown",
        ).length,
    );
    if (pointerShown !== 1) {
      failures.push(`overlay: pointer movement showed ${pointerShown} pane overlays`);
    }
    await page.waitForTimeout(3400);
    const afterIdle = await page.evaluate(() =>
      document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
    );
    if (afterIdle !== "hidden") failures.push("overlay: pointer reveal did not idle out");
    // A key press reveals the selected pane's overlay.
    step = "key reveal";
    await focusSelectedPane(page);
    await page.keyboard.press("ArrowUp");
    await page.waitForFunction(
      () =>
        document
          .querySelector("[data-live-pane][data-live-selected='true'] [data-live-pane-overlay]")
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    // Focus inside suspends hiding.
    await page.evaluate(() =>
      document
        .querySelector(
          "[data-live-pane][data-live-selected='true'] input[data-live-quality-choice]:not([disabled])",
        )
        ?.focus(),
    );
    await page.waitForTimeout(3400);
    const focused = await page.evaluate(() => {
      const overlay = document
        .querySelector("[data-live-pane][data-live-selected='true']")
        ?.querySelector("[data-live-pane-overlay]");
      return {
        state: overlay?.getAttribute("data-live-pane-overlay-state"),
        inPane: Boolean(document.activeElement.closest("[data-live-pane-overlay]")),
      };
    });
    if (focused.state !== "shown") failures.push("overlay: focus inside did not suspend hiding");
    if (!focused.inPane) failures.push("overlay: focus left the pane overlay");
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-overlay-1280x800.png"),
      fullPage: false,
    });
    await page.evaluate(() => document.activeElement.blur());
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "hidden",
      null,
      { timeout: 8000 },
    );
    // Tab from the pane reveals its overlay with focus on the first control.
    step = "tab reveal";
    await focusSelectedPane(page);
    await page.evaluate(() => window.BitternLiveWall.hideAllOverlays());
    await page.keyboard.press("Tab");
    const revealed = await page.evaluate(() => ({
      state: document
        .querySelector("[data-live-pane][data-live-selected='true'] [data-live-pane-overlay]")
        ?.getAttribute("data-live-pane-overlay-state"),
      name: document.activeElement.getAttribute("name"),
      inPane: Boolean(document.activeElement.closest("[data-live-pane-overlay]")),
    }));
    if (revealed.state !== "shown") failures.push("overlay: Tab did not reveal the pane overlay");
    if (!revealed.inPane || revealed.name !== "quality") {
      failures.push(`overlay: Tab did not focus the pane's quality selector (${revealed.name})`);
    }
    // Shift+Tab from the pane focuses the last control in its overlay.
    step = "shift tab";
    await page.evaluate(() => document.activeElement.blur());
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "hidden",
      null,
      { timeout: 8000 },
    );
    await focusSelectedPane(page);
    await page.evaluate(() => window.BitternLiveWall.hideAllOverlays());
    await page.keyboard.press("Shift+Tab");
    const back = await page.evaluate(() => ({
      tag: document.activeElement.tagName,
      name: document.activeElement.getAttribute("name"),
      value: document.activeElement.getAttribute("value"),
      inPane: Boolean(document.activeElement.closest("[data-live-pane-overlay]")),
    }));
    if (!back.inPane || back.tag !== "INPUT" || back.name !== "quality" || back.value !== "snapshot") {
      failures.push(
        `overlay: Shift+Tab did not focus the pane's last control (${back.tag} ${back.name} ${back.value})`,
      );
    }
    await page.evaluate(() => document.activeElement.blur());
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "hidden",
      null,
      { timeout: 8000 },
    );
    await page.waitForTimeout(250);
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-overlay-hidden-1280x800.png"),
      fullPage: false,
    });
  } catch (error) {
    failures.push(`overlay (${step}): ${error.message}`);
  } finally {
    await context.close();
  }
  // The same pane overlay at phone width, for the four-width read.
  const phone = await openWall(browser, baseURL, password, {
    viewport: { width: 360, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    hasTouch: true,
  });
  try {
    const box = await phone.page.locator('[data-live-pane="1"]').boundingBox();
    await phone.page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2);
    await phone.page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    await phone.page.waitForTimeout(300);
    await phone.page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-overlay-360x800.png"),
      fullPage: false,
    });
  } catch (error) {
    failures.push(`overlay at 360px: ${error.message}`);
  } finally {
    await phone.context.close();
  }
  return failures;
}

// 5. Without JavaScript the overlay is visible, the layout picker is six
// submit buttons, and the quality control is a real form.
async function walkNoScript(browser, baseURL, password) {
  const failures = [];
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    javaScriptEnabled: false,
  });
  try {
    await setLayoutCookie(context, baseURL, 6, ["front_door", "driveway"], ["main", ""]);
    const page = await context.newPage();
    await signIn(page, baseURL, password);
    await page.goto(`${baseURL}${GRID_PATH}`, { waitUntil: "load" });
    const wallState = await page.locator("[data-live-grid]").getAttribute("data-live-overlay");
    const panel = page.locator("[data-live-overlay-panel]");
    if (wallState !== "shown") failures.push("no script: the overlay is not shown by default");
    if ((await panel.getAttribute("inert")) !== null) {
      failures.push("no script: the overlay is inert without the script");
    }
    if (!(await panel.isVisible())) failures.push("no script: the overlay is hidden");
    const buttons = page.locator("[data-live-layout-buttons] button");
    if ((await buttons.count()) !== 7) {
      failures.push(`no script: ${await buttons.count()} layout buttons, want 7`);
    }
    const pressed = page.locator('[data-live-layout-buttons] button[aria-pressed="true"]');
    if ((await pressed.count()) !== 1) failures.push("no script: the stored layout is not pressed");
    if (await page.locator("[data-live-layout-radios]").isVisible()) {
      failures.push("no script: the radio group replaced the submit buttons");
    }
    const qualityChoices = page.locator(
      '[data-live-pane="1"] [data-live-quality-group] label.live-quality-choice',
    );
    if ((await qualityChoices.count()) !== 3) {
      failures.push("no script: the quality control does not offer three pictograms");
    }
    const qualityGlyphs = page.locator(
      '[data-live-pane="1"] [data-live-quality-group] label.live-quality-choice svg.live-quality-glyph',
    );
    if ((await qualityGlyphs.count()) !== 3) {
      failures.push("no script: a quality choice has no glyph");
    }
    if (
      (await page
        .locator('[data-live-pane="1"] [data-live-quality-form] button[type="submit"]')
        .count()) !== 1
    ) {
      failures.push("no script: the quality control has no submit button");
    }
    const action = await page
      .locator('[data-live-pane="1"] [data-live-quality-form]')
      .getAttribute("action");
    if (action !== "/ui/live/pane") {
      failures.push("no script: the quality control is not a real form");
    }
    const paneOverlay = page.locator('[data-live-pane="1"] [data-live-pane-overlay]');
    if (!(await paneOverlay.isVisible())) {
      failures.push("no script: the pane overlay is hidden");
    }
    if ((await page.locator("[data-live-overlay-panel] [data-live-quality-group]").count()) !== 0) {
      failures.push("no script: a quality control sits in the wall overlay");
    }
  } catch (error) {
    failures.push(`no script: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 6. A manual pane choice survives demotion pressure, a page poll and a
// layout change.
async function walkManualQuality(browser, baseURL, password) {
  const failures = [];
  let step = "load";
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    clock: true,
  });
  try {
    step = "playing";
    await page.clock.runFor(300);
    await waitForPlayingCount(page, 4);
    step = "select pane";
    await selectPane(page, 1);
    step = "quality main";
    await chooseQuality(page, 1, "main");
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-player]')
          ?.getAttribute("data-profile") === "main",
      null,
      { timeout: 10000 },
    );
    let avatar = await playerMessage(page, 1);
    if (avatar.manual !== "main") failures.push("manual: the pane is not stored as manual main");
    // Demotion pressure: every live pane drops a fifth of its frames.
    await page.evaluate(() => {
      window.__liveStub.drop = 0.2;
    });
    step = "pressure";
    await page.clock.runFor(240000);
    const afterPressure = await page.evaluate(() => {
      const panes = Array.from(document.querySelectorAll("[data-live-pane]:not([hidden])")).map((pane) => ({
        number: pane.getAttribute("data-live-pane"),
        state: pane.querySelector("[data-live-player]")?.getAttribute("data-player-state"),
        manual: pane.querySelector("[data-live-player]")?.getAttribute("data-manual-quality"),
      }));
      return {
        panes: panes,
        live: Number(document.querySelector("[data-live-grid]").getAttribute("data-live-count")),
        capacity: Number(
          document.querySelector("[data-live-grid]").getAttribute("data-live-capacity"),
        ),
      };
    });
    if (afterPressure.live >= 4) {
      failures.push(`manual: demotion pressure left ${afterPressure.live} panes live`);
    }
    const manualPane = afterPressure.panes.find((pane) => pane.number === "1");
    if (manualPane.state !== "playing" || manualPane.manual !== "main") {
      failures.push(
        `manual: the manual pane is ${manualPane.state} and ${manualPane.manual} after pressure`,
      );
    }
    // A page poll does not touch the manual choice or the player.
    step = "poll";
    await showOverlay(page);
    await page.evaluate(() => {
      const player = document.querySelector('[data-live-pane="1"] [data-live-player]');
      player.dataset.liveMarker = "keep";
    });
    const request = page.waitForRequest("**/ui/live/front_door/status");
    await page.evaluate(() => {
      const target = document.querySelector("[data-live-camera-state-region]");
      window.htmx.ajax("GET", target.getAttribute("hx-get"), {
        target: "[data-live-camera-state-region]",
        swap: "innerHTML",
      });
    });
    await request;
    const afterPoll = await playerMessage(page, 1);
    const marker = await page.evaluate(
      () =>
        document.querySelector('[data-live-pane="1"] [data-live-player]').dataset.liveMarker,
    );
    if (afterPoll.manual !== "main" || afterPoll.profile !== "main") {
      failures.push("manual: the camera poll changed the manual choice");
    }
    if (marker !== "keep") failures.push("manual: the camera poll replaced the player");
    // A layout change acts in place, keeps the manual quality, and keeps the
    // player node and the URL. A reload then shows the stored state.
    step = "layout";
    await showOverlay(page);
    const beforeLayout = await page.evaluate(() => {
      window.__manualMarker = "keep";
      return { url: document.location.href };
    });
    await page.locator("#live-layout-6").click();
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-layout") === "6",
      null,
      { timeout: 5000 },
    );
    const inPlace = await page.evaluate((before) => {
      const player = document.querySelector('[data-live-pane="1"] [data-live-player]');
      return {
        url: document.location.href,
        marker: window.__manualMarker,
        samePlayer: Boolean(player),
        playing:
          document.querySelectorAll(
            '[data-live-pane]:not([hidden]) [data-live-player][data-player-state="playing"]',
          ).length > 0,
        layout: document.querySelector("[data-live-grid]").getAttribute("data-layout"),
        panes: document.querySelectorAll("[data-live-pane]:not([hidden])").length,
        manual: player.getAttribute("data-manual-quality"),
        quality: player.getAttribute("data-quality"),
        profile: player.getAttribute("data-profile"),
        beforeURL: before.url,
      };
    }, beforeLayout);
    if (inPlace.url !== inPlace.beforeURL) {
      failures.push(`manual: the layout change navigated to ${inPlace.url}`);
    }
    if (inPlace.marker !== "keep") failures.push("manual: the layout change reloaded the page");
    if (!inPlace.playing) failures.push("manual: no player is playing after the layout change");
    if (inPlace.layout !== "6" || inPlace.panes !== 6) {
      failures.push(
        `manual: the layout change made layout ${inPlace.layout} with ${inPlace.panes} panes`,
      );
    }
    if (inPlace.manual !== "main" || inPlace.quality !== "main" || inPlace.profile !== "main") {
      failures.push("manual: the layout change lost the manual quality");
    }
    step = "reload";
    await page.reload({ waitUntil: "load" });
    const stored = await page.evaluate(() => {
      const wall = document.querySelector("[data-live-grid]");
      const player = document.querySelector('[data-live-pane="1"] [data-live-player]');
      return {
        layout: wall.getAttribute("data-layout"),
        manual: player.getAttribute("data-manual-quality"),
        quality: player.getAttribute("data-quality"),
        profile: player.getAttribute("data-profile"),
        panes: document.querySelectorAll("[data-live-pane]:not([hidden])").length,
      };
    });
    if (stored.layout !== "6" || stored.panes !== 6) {
      failures.push(`manual: the stored layout is ${stored.layout} with ${stored.panes} panes`);
    }
    if (stored.manual !== "main" || stored.quality !== "main" || stored.profile !== "main") {
      failures.push("manual: the reload lost the manual quality");
    }
  } catch (error) {
    failures.push(`manual (${step}): ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// capacityRun drives the shipped capacity model through a synthetic sequence
// of two second samples. It runs inside the page, against the same module the
// wall uses, so the assertions are the delivery's own hysteresis and not a
// second copy of it.
function capacityRun(spec) {
  const model = BitternCapacity.create({ prior: spec.prior || 16 });
  const panes = spec.panes.map((pane, index) => ({
    id: "p" + (index + 1),
    order: index + 1,
    area: pane.area || 100,
    manual: pane.manual || false,
    quality: pane.quality || "sub",
    dropped: 0,
    total: 0,
  }));
  const events = [];
  const rateAt = (at) => {
    for (const segment of spec.segments) {
      if (at >= segment.from && at < segment.to) return segment.rate;
    }
    return 0;
  };
  for (let at = 0; at <= spec.duration; at += 2000) {
    const rate = rateAt(at);
    panes.forEach((pane) => {
      pane.total += 1000;
      pane.dropped += Math.round(1000 * rate);
    });
    const view = {
      panes: panes.map((pane) => ({
        id: pane.id,
        order: pane.order,
        area: pane.area,
        live: !model.isDemoted(pane.id),
        manual: pane.manual,
        quality: pane.quality,
        dropped: pane.dropped,
        total: pane.total,
      })),
    };
    const action = model.evaluate(at, view);
    if (action) events.push({ at: at, kind: action.kind, id: action.id, last: action.last });
  }
  return {
    events: events,
    capacity: model.capacity(),
    prior: model.prior(),
    demoted: panes.filter((pane) => model.isDemoted(pane.id)).map((pane) => pane.id),
  };
}

async function runCapacityScenario(page, spec) {
  return page.evaluate(capacityRun, spec);
}

function demotions(result) {
  return result.events.filter((event) => event.kind === "demote");
}

function promotions(result) {
  return result.events.filter((event) => event.kind === "promote");
}

async function walkCapacityHysteresis(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door"],
  });
  try {
    const eightPanes = Array.from({ length: 8 }, () => ({}));
    // Window boundaries: two windows of evidence before the first demotion,
    // then one demotion per twenty second window.
    const hot = await runCapacityScenario(page, {
      panes: eightPanes,
      duration: 200000,
      segments: [{ from: 0, to: 200000, rate: 0.05 }],
    });
    const hotDemotions = demotions(hot);
    if (hotDemotions.length < 2) {
      failures.push(`hysteresis: ${hotDemotions.length} demotions from 200s of hot frames`);
    }
    if (hotDemotions.length && hotDemotions[0].at < 20000) {
      failures.push(
        `hysteresis: the first demotion is at ${hotDemotions[0].at}ms, before twenty seconds of evidence`,
      );
    }
    for (let index = 1; index < hotDemotions.length; index += 1) {
      const gap = hotDemotions[index].at - hotDemotions[index - 1].at;
      if (gap < 20000) {
        failures.push(`hysteresis: two demotions ${gap}ms apart, closer than the twenty second window`);
      }
    }
    if (hotDemotions.length !== 8) {
      failures.push(`hysteresis: ${hotDemotions.length} demotions of eight hot panes`);
    }
    // One pane per cooldown and the last pane is named as such.
    const last = hotDemotions[hotDemotions.length - 1];
    if (!last || !last.last) {
      failures.push("hysteresis: the last automatic pane is not marked last");
    }
    // Determinism: the same sequence gives the same state.
    const repeat = await runCapacityScenario(page, {
      panes: eightPanes,
      duration: 200000,
      segments: [{ from: 0, to: 200000, rate: 0.05 }],
    });
    if (JSON.stringify(repeat.events) !== JSON.stringify(hot.events)) {
      failures.push("hysteresis: the same frame sequence produced different events");
    }
  } catch (error) {
    failures.push(`hysteresis windows: ${error.message}`);
  }
  try {
    // Promotion needs both quiet conditions: three hundred seconds since the
    // last demotion and sixty quiet seconds, and never sooner.
    const quiet = await runCapacityScenario(page, {
      panes: Array.from({ length: 4 }, () => ({})),
      duration: 480000,
      segments: [
        { from: 0, to: 40000, rate: 0.05 },
        { from: 40000, to: 480000, rate: 0.0001 },
      ],
    });
    const demotedAt = demotions(quiet)[0]?.at ?? null;
    const promotedAt = promotions(quiet)[0]?.at ?? null;
    if (demotedAt === null) {
      failures.push("hysteresis: the hot phase did not demote a pane");
    }
    if (promotedAt === null) {
      failures.push("hysteresis: the quiet phase did not promote a pane");
    }
    if (demotedAt !== null && promotedAt !== null) {
      const wait = promotedAt - demotedAt;
      if (wait < 300000) {
        failures.push(`hysteresis: promotion came ${wait}ms after the last demotion, want 300000`);
      }
      if (wait > 360000) {
        failures.push(
          `hysteresis: promotion came ${wait}ms after the last demotion, want at most 360000 ` +
            `(the quiet window restarts when the evidence resets)`,
        );
      }
    }
    const promotionGaps = promotions(quiet);
    for (let index = 1; index < promotionGaps.length; index += 1) {
      const gap = promotionGaps[index].at - promotionGaps[index - 1].at;
      if (gap < 60000) {
        failures.push(`hysteresis: two promotions ${gap}ms apart, closer than sixty seconds`);
      }
    }
  } catch (error) {
    failures.push(`hysteresis promotion: ${error.message}`);
  }
  try {
    // The dead band: one percent is above the promotion threshold and below
    // the demotion threshold, so nothing moves in either direction.
    const band = await runCapacityScenario(page, {
      panes: Array.from({ length: 4 }, () => ({})),
      duration: 200000,
      segments: [{ from: 0, to: 200000, rate: 0.01 }],
    });
    if (band.events.length !== 0) {
      failures.push(
        `hysteresis: ${band.events.length} actions inside the dead band (${JSON.stringify(band.events[0])})`,
      );
    }
  } catch (error) {
    failures.push(`hysteresis dead band: ${error.message}`);
  }
  try {
    // A manual pane is never a demotion target, and a manual pane's hot frames
    // are still evidence that the device is overloaded.
    const manual = await runCapacityScenario(page, {
      panes: [{ manual: true, area: 10 }, { area: 100 }, { manual: true, area: 10 }],
      duration: 200000,
      segments: [{ from: 0, to: 200000, rate: 0.05 }],
    });
    const targets = demotions(manual).map((event) => event.id);
    if (targets.includes("p1") || targets.includes("p3")) {
      failures.push(`hysteresis: a manual pane was demoted (${targets.join(",")})`);
    }
    if (!targets.includes("p2")) {
      failures.push("hysteresis: the automatic pane was not demoted");
    }
  } catch (error) {
    failures.push(`hysteresis manual panes: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 7. A snapshot pane refreshes on the interval and never opens a live session.
async function walkSnapshotPanes(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    clock: true,
  });
  try {
    await page.clock.runFor(300);
    await waitForPlayingCount(page, 4);
    const socketsBefore = await page.evaluate(
      () => window.__liveStub.sockets.filter((socket) => socket.url.includes("/driveway/")).length,
    );
    await selectPane(page, 2);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="2"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    await chooseQuality(page, 2, "snapshot");
    await waitForState(page, '[data-live-pane="2"] [data-live-player]', "snapshot");
    const first = await page.evaluate(() => {
      const pane = document.querySelector('[data-live-pane="2"]');
      const image = pane.querySelector("[data-player-snapshot]");
      return {
        src: image.getAttribute("src"),
        visible: !image.hidden,
        live: document.querySelector("[data-live-grid]").getAttribute("data-live-count"),
      };
    });
    if (!first.visible) failures.push("snapshot: the still picture is not shown");
    if (!first.src || !first.src.includes("/snapshot.jpg")) {
      failures.push(`snapshot: the still has no snapshot source (${first.src})`);
    }
    if (!first.src.includes("driveway")) {
      failures.push("snapshot: the still is not the selected camera");
    }
    if (first.live !== "3") failures.push(`snapshot: ${first.live} live panes, want 3`);
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    await page.waitForTimeout(3400);
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-snapshot-1280x800.png"),
      fullPage: false,
    });
    const refresh = page.waitForRequest(
      (request) => request.url().includes("/driveway/") && request.url().includes("snapshot.jpg"),
      { timeout: 10000 },
    );
    await page.clock.runFor(16000);
    await refresh;
    const second = await page.evaluate(() => {
      const pane = document.querySelector('[data-live-pane="2"]');
      const image = pane.querySelector("[data-player-snapshot]");
      return { src: image.getAttribute("src"), hidden: image.hidden };
    });
    if (second.src === first.src) failures.push("snapshot: the still did not refresh after fifteen seconds");
    if (second.hidden) failures.push("snapshot: the refreshed still was hidden");
    const socketsAfter = await page.evaluate(
      () => window.__liveStub.sockets.filter((socket) => socket.url.includes("/driveway/")).length,
    );
    if (socketsAfter !== socketsBefore) {
      failures.push("snapshot: the snapshot pane opened a live media session");
    }
  } catch (error) {
    failures.push(`snapshot: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 8. One tab stop, arrow keys by geometry, Escape leaves full screen.
async function walkKeyboard(browser, baseURL, password) {
  const failures = [];
  let step = "load";
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 6,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
  });
  try {
    const stops = await page.evaluate(() => {
      const panes = Array.from(document.querySelectorAll("[data-live-pane]:not([hidden])"));
      return {
        zero: panes.filter((pane) => pane.getAttribute("tabindex") === "0").length,
        minus: panes.filter((pane) => pane.getAttribute("tabindex") === "-1").length,
        first: panes[0].getAttribute("tabindex"),
      };
    });
    if (stops.zero !== 1) failures.push(`keyboard: ${stops.zero} pane tab stops, want 1`);
    if (stops.minus !== 5) failures.push(`keyboard: ${stops.minus} panes out of the tab order, want 5`);
    if (stops.first !== "0") failures.push("keyboard: the first pane is not the tab stop");
    const before = await page.locator('[data-live-pane="1"]').boundingBox();
    await page.locator('[data-live-pane="1"]').focus();
    await page.keyboard.press("ArrowRight");
    const right = await page.evaluate(() => {
      const selected = document.querySelector("[data-live-pane][data-live-selected='true']");
      return {
        number: selected.getAttribute("data-live-pane"),
        focused: document.activeElement === selected,
        box: selected.getBoundingClientRect().toJSON(),
      };
    });
    if (right.box.left <= before.x) {
      failures.push("keyboard: ArrowRight did not move to a pane on the right");
    }
    if (!right.focused) failures.push("keyboard: the arrow key did not move focus");
    await page.keyboard.press("End");
    const end = await page.evaluate(
      () => document.querySelector("[data-live-pane][data-live-selected='true']")?.getAttribute("data-live-pane"),
    );
    if (end !== "6") failures.push(`keyboard: End selected pane ${end}, want 6`);
    await page.keyboard.press("Home");
    const home = await page.evaluate(
      () => document.querySelector("[data-live-pane][data-live-selected='true']")?.getAttribute("data-live-pane"),
    );
    if (home !== "1") failures.push(`keyboard: Home selected pane ${home}, want 1`);
    await page.keyboard.press("ArrowDown");
    const down = await page.evaluate(() => {
      const selected = document.querySelector("[data-live-pane][data-live-selected='true']");
      return { number: selected.getAttribute("data-live-pane"), box: selected.getBoundingClientRect().toJSON() };
    });
    if (down.box.top <= before.y) {
      failures.push("keyboard: ArrowDown did not move to a pane below");
    }
    step = "enter full screen";
    await enterFullScreen(page);
    step = "escape full screen";
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => !document.fullscreenElement, null, { timeout: 5000 });
    if (await page.evaluate(() => document.fullscreenElement)) {
      failures.push("keyboard: Escape did not leave full screen");
    }
    // Escape outside full screen hides the revealed overlay and returns focus.
    await showOverlay(page);
    await page.keyboard.press("Escape");
    const escaped = await page.evaluate(() => ({
      state: document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
      pane: document.activeElement.hasAttribute("data-live-pane"),
    }));
    if (escaped.state !== "hidden") failures.push("keyboard: Escape did not hide the overlay");
    if (!escaped.pane) failures.push("keyboard: Escape did not return focus to the pane");
  } catch (error) {
    failures.push(`keyboard (${step}): ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 9. A realistic stream cannot change the pane geometry or overflow the page.
async function walkLargeStream(browser, baseURL, password, width, resolution) {
  const failures = [];
  try {
    const { context, page } = await openWall(browser, baseURL, password, {
      viewport: width,
      layout: 4,
      cameras: ["front_door", "driveway", "side_gate", "back_garden"],
      videoWidth: resolution.width,
      videoHeight: resolution.height,
    });
    try {
      const before = await page.evaluate(() => {
        const media = document.querySelector('[data-live-pane="1"] .live-media');
        return media.getBoundingClientRect().toJSON();
      });
      await page.evaluate(() => {
        window.__liveStub.videoWidth = 0;
        window.__liveStub.videoHeight = 0;
        document
          .querySelector('[data-live-pane="1"] video')
          .dispatchEvent(new Event("loadedmetadata"));
      });
      const after = await page.evaluate(() => {
        const media = document.querySelector('[data-live-pane="1"] .live-media');
        return media.getBoundingClientRect().toJSON();
      });
      if (Math.abs(before.width - after.width) > 1 || Math.abs(before.height - after.height) > 1) {
        failures.push(
          `${resolution.width}x${resolution.height} at ${width.width}px: the media box moved when metadata changed`,
        );
      }
      const geometry = await paneGeometry(page);
      assertPaneGeometry(
        geometry,
        `${resolution.width}x${resolution.height} at ${width.width}px`,
        failures,
        4,
      );
    } finally {
      await context.close();
    }
  } catch (error) {
    failures.push(`${resolution.width}x${resolution.height} at ${width.width}px: ${error.message}`);
  }
  return failures;
}

// 10. Full screen actually engages on the wall, refuses with a visible
// message, and exits with Escape or a touch swipe.
async function walkFullScreenControl(browser, baseURL, password) {
  const failures = [];
  let step = "load";
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 6,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    hasTouch: true,
  });
  try {
    step = "enter";
    await showOverlay(page);
    await page.click("[data-live-fullscreen]");
    await page.waitForFunction(() => Boolean(document.fullscreenElement), null, {
      timeout: 5000,
    });
    step = "hint";
    await page
      .locator("[data-live-fullscreen-hint]")
      .waitFor({ state: "visible", timeout: 5000 });
    const entered = await page.evaluate(() => {
      const hint = document.querySelector("[data-live-fullscreen-hint]");
      return {
        root: document.fullscreenElement?.getAttribute("data-live-grid"),
        exit: document.querySelectorAll("[data-live-exit]").length,
        hint: hint && !hint.hidden ? hint.textContent : "",
      };
    });
    if (entered.root !== "true") failures.push("full screen: the wall did not enter full screen");
    if (entered.exit !== 0) failures.push("full screen: an exit button is still rendered");
    if (!entered.hint.toLowerCase().includes("swipe down")) {
      failures.push("full screen: the entry hint does not name the swipe gesture");
    }
    if (!entered.hint.includes("120 pixels")) {
      failures.push("full screen: the entry hint does not name the swipe threshold");
    }
    if (!entered.hint.includes("Escape")) {
      failures.push("full screen: the entry hint does not name Escape");
    }
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-fullscreen-hint-1280x800.png"),
      fullPage: false,
    });
    // A layout change inside full screen stays in full screen and fits the new
    // layout to the same screen.
    await page.locator("#live-layout-16").click();
    await page.waitForFunction(
      () =>
        document.querySelector("[data-live-grid]").getAttribute("data-layout") === "16" &&
        document.querySelectorAll("[data-live-pane]:not([hidden])").length === 16,
      null,
      { timeout: 5000 },
    );
    const afterLayout = await page.evaluate(() => ({
      fullscreen: Boolean(document.fullscreenElement),
      active: document.querySelector("[data-live-grid]").getAttribute("data-live-fullscreen-active"),
    }));
    if (!afterLayout.fullscreen) failures.push("full screen: a layout change left full screen");
    if (afterLayout.active !== "true") {
      failures.push("full screen: a layout change lost the full-screen state");
    }
    // Escape still exits when an overlay control has focus.
    step = "escape with overlay";
    await page.evaluate(() =>
      document
        .querySelector('[data-live-pane="1"] input[data-live-quality-choice]:not([disabled])')
        ?.focus(),
    );
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => !document.fullscreenElement, null, { timeout: 5000 });
    // A swipe down of 140 pixels exits on a touch device.
    step = "swipe enter";
    await page.waitForFunction(
      () =>
        document.querySelector("[data-live-grid]").getAttribute("data-live-fullscreen-active") ===
        "false",
      null,
      { timeout: 5000 },
    );
    await showOverlay(page);
    await page.evaluate(() => window.BitternLiveWall.showOverlay());
    await page.locator("[data-live-fullscreen]").waitFor({ state: "visible", timeout: 5000 });
    await page.click("[data-live-fullscreen]");
    await page.waitForFunction(() => Boolean(document.fullscreenElement), null, {
      timeout: 5000,
    });
    step = "swipe";
    await page.evaluate(() => {
      const wall = document.querySelector("[data-live-grid]");
      const box = wall.getBoundingClientRect();
      const x = box.left + box.width / 2;
      wall.dispatchEvent(
        new PointerEvent("pointerdown", {
          bubbles: true,
          pointerType: "touch",
          clientX: x,
          clientY: box.top + 80,
        }),
      );
      wall.dispatchEvent(
        new PointerEvent("pointerup", {
          bubbles: true,
          pointerType: "touch",
          clientX: x,
          clientY: box.top + 220,
        }),
      );
    });
    await page.waitForFunction(() => !document.fullscreenElement, null, { timeout: 5000 });
  } catch (error) {
    failures.push(`full screen (${step}): ${error.message}`);
  } finally {
    await context.close();
  }
  // A refused request is a visible plain-language message, not silence.
  const refused = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
  });
  try {
    await refused.page.evaluate(() => {
      Element.prototype.requestFullscreen = () => Promise.reject(new Error("denied"));
    });
    await showOverlay(refused.page);
    await refused.page.click("[data-live-fullscreen]");
    await refused.page.waitForFunction(
      () => {
        const notice = document.querySelector("[data-live-notice]");
        return notice && !notice.hidden && notice.textContent.trim().length > 0;
      },
      null,
      { timeout: 5000 },
    );
    const state = await refused.page.evaluate(() => {
      const notice = document.querySelector("[data-live-notice]");
      return {
        text: notice.textContent.trim(),
        hidden: notice.hidden,
        fullscreen: Boolean(document.fullscreenElement),
        wall: document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
      };
    });
    if (state.hidden || state.text.length === 0) {
      failures.push("full screen: a refused request produced no message");
    }
    if (!state.text.toLowerCase().includes("did not allow full screen")) {
      failures.push(`full screen: the refusal message is not plain language (${state.text})`);
    }
    if (!state.text.toLowerCase().includes("full-screen control")) {
      failures.push("full screen: the refusal message does not name the fallback");
    }
    if (state.fullscreen) failures.push("full screen: a refused request still entered full screen");
    if (state.wall !== "shown") failures.push("full screen: the refusal message is not visible");
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    await refused.page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-fullscreen-refused-1280x800.png"),
      fullPage: false,
    });
  } catch (error) {
    failures.push(`full screen refusal: ${error.message}`);
  } finally {
    await refused.context.close();
  }
  return failures;
}

// 11. The layout and quality controls act in place: no navigation, no reload,
// and the players that stay keep playing. A camera assignment also acts in
// place.
async function walkInPlaceControls(browser, baseURL, password) {
  const failures = [];
  let step = "load";
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway"],
  });
  try {
    step = "playing";
    await waitForPlayingCount(page, 2);
    const start = await page.evaluate(() => {
      window.__inPlaceMarker = "keep";
      window.__inPlacePlayer = document.querySelector(
        '[data-live-pane="1"] [data-live-player]',
      );
      return { url: document.location.href };
    });
    let navigations = 0;
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) navigations += 1;
    });
    const check = async (label) =>
      page.evaluate((name) => {
        const player = document.querySelector('[data-live-pane="1"] [data-live-player]');
        return {
          label: name,
          url: document.location.href,
          marker: window.__inPlaceMarker,
          samePlayer: window.__inPlacePlayer === player,
          playing:
            document.querySelectorAll(
              '[data-live-pane]:not([hidden]) [data-live-player][data-player-state="playing"]',
            ).length,
        };
      }, label);
    // Layout 4 to 16.
    step = "layout 16";
    await showOverlay(page);
    await page.locator("#live-layout-16").click();
    await page.waitForFunction(
      () =>
        document.querySelector("[data-live-grid]").getAttribute("data-layout") === "16" &&
        document.querySelectorAll("[data-live-pane]:not([hidden])").length === 16,
      null,
      { timeout: 5000 },
    );
    let state = await check("layout");
    if (state.url !== start.url) failures.push(`in place: layout navigated to ${state.url}`);
    if (state.marker !== "keep") failures.push("in place: layout reloaded the page");
    if (!state.samePlayer) failures.push("in place: layout replaced the player node");
    if (state.playing < 1) failures.push("in place: no player is playing after the layout change");
    if (navigations !== 0) failures.push("in place: layout caused a navigation");
    // Quality main, sub and snapshot through the pane selector.
    step = "select quality pane";
    await selectPane(page, 1);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    step = "quality main";
    await chooseQuality(page, 1, "main");
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-player]')
          ?.getAttribute("data-profile") === "main",
      null,
      { timeout: 5000 },
    );
    state = await check("quality main");
    if (state.url !== start.url) failures.push(`in place: quality navigated to ${state.url}`);
    if (state.marker !== "keep") failures.push("in place: quality reloaded the page");
    if (!state.samePlayer) failures.push("in place: quality replaced the player node");
    if (state.playing < 1) failures.push("in place: no player is playing after quality main");
    step = "quality sub";
    await chooseQuality(page, 1, "sub");
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-player]')
          ?.getAttribute("data-profile") === "sub",
      null,
      { timeout: 5000 },
    );
    state = await check("quality sub");
    if (state.url !== start.url || state.marker !== "keep" || state.playing < 1) {
      failures.push("in place: quality sub navigated, reloaded or stopped every player");
    }
    step = "quality snapshot";
    await chooseQuality(page, 1, "snapshot");
    await waitForState(page, '[data-live-pane="1"] [data-live-player]', "snapshot");
    state = await check("quality snapshot");
    if (state.url !== start.url) failures.push("in place: snapshot navigated");
    if (state.marker !== "keep") failures.push("in place: snapshot reloaded the page");
    if (state.playing < 1) failures.push("in place: snapshot stopped every player");
    if (navigations !== 0) failures.push("in place: a quality choice caused a navigation");
    // Layout 16 back to 4, then 4 to auto.
    step = "layout back";
    await showOverlay(page);
    await page.locator("#live-layout-4").click();
    await page.waitForFunction(
      () =>
        document.querySelector("[data-live-grid]").getAttribute("data-layout") === "4" &&
        document.querySelectorAll("[data-live-pane]:not([hidden])").length === 4,
      null,
      { timeout: 5000 },
    );
    state = await check("layout back");
    if (state.marker !== "keep") failures.push("in place: shrinking the layout reloaded the page");
    if (state.playing < 1) failures.push("in place: no player is playing after shrinking");
    step = "layout auto";
    await showOverlay(page);
    await page.locator("#live-layout-auto").click();
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-layout") === "4",
      null,
      { timeout: 5000 },
    );
    state = await check("layout auto");
    if (state.marker !== "keep") failures.push("in place: auto layout reloaded the page");
    if (navigations !== 0) failures.push("in place: a layout change caused a navigation");
    // Camera assignment fills an empty pane in place, with the existing
    // players still running.
    step = "camera select";
    await selectPane(page, 3);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="3"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    step = "camera assign";
    await page.selectOption('[data-live-pane="3"] [data-live-camera-select]', "side_gate");
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="3"]')
          ?.getAttribute("data-camera-id") === "side_gate",
      null,
      { timeout: 10000 },
    );
    const assigned = await page.evaluate(() => ({
      url: document.location.href,
      marker: window.__inPlaceMarker,
      target: document.querySelector('[data-live-pane="3"]').getAttribute("data-camera-id"),
      placeholder:
        document.querySelector('[data-live-pane="3"]').getAttribute("data-live-occupied") ===
        "true",
      qualityVisible: Boolean(
        document
          .querySelector('[data-live-pane="3"] [data-live-quality-single]')
          ?.getClientRects().length,
      ),
      qualityDisabled: Array.from(
        document.querySelectorAll('[data-live-pane="3"] [data-live-quality-choice]'),
      ).every((input) => input.disabled),
      playing: document.querySelectorAll(
        '[data-live-pane]:not([hidden]) [data-live-player][data-player-state="playing"]',
      ).length,
    }));
    if (assigned.url !== start.url) failures.push("in place: camera assignment navigated");
    if (assigned.marker !== "keep") failures.push("in place: camera assignment reloaded the page");
    if (assigned.target !== "side_gate") failures.push("in place: camera assignment did not fill the pane");
    if (!assigned.placeholder) failures.push("in place: camera assignment left the pane empty");
    if (!assigned.qualityVisible) failures.push("in place: the assigned pane has no stream selector");
    if (!assigned.qualityDisabled) {
      failures.push("in place: a one-stream camera offers a stream choice after assignment");
    }
    if (assigned.playing < 1) failures.push("in place: camera assignment stopped every player");
    if (navigations !== 0) failures.push("in place: camera assignment caused a navigation");
  } catch (error) {
    failures.push(`in place (${step}): ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 12. The layout picker is an icon radio group with accessible names and a
// selected state that does not rely on colour alone.
async function walkLayoutPicker(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
  });
  try {
    await showOverlay(page);
    const picker = await page.evaluate(() => {
      const inputs = Array.from(document.querySelectorAll("input[name='layout']"));
      return {
        count: inputs.length,
        svgs: inputs.filter((input) => input.closest("label").querySelector("svg")).length,
        names: inputs.map((input) => input.closest("label").textContent.trim()),
        rects: inputs.map((input) => input.closest("label").querySelectorAll("rect").length),
        values: inputs.map((input) => input.value),
        checked: inputs.filter((input) => input.checked).map((input) => input.value),
        selectedBorder: getComputedStyle(
          document.querySelector("#live-layout-4").closest("label"),
        ).borderTopWidth,
        unselectedBorder: getComputedStyle(
          document.querySelector("#live-layout-6").closest("label"),
        ).borderTopWidth,
        buttonsHidden: document.querySelector("[data-live-layout-buttons]").hidden,
      };
    });
    if (picker.count !== 7) failures.push(`layout picker: ${picker.count} radios, want 7`);
    if (picker.svgs !== 7) failures.push("layout picker: not every layout has an inline SVG");
    if (picker.names.some((name) => name.length === 0)) {
      failures.push("layout picker: an option has no accessible name");
    }
    const wants = { auto: 4, "1": 1, "4": 4, "6": 6, "8": 8, "9": 9, "16": 16 };
    picker.values.forEach((value, index) => {
      if (picker.rects[index] !== wants[value]) {
        failures.push(
          `layout picker: the ${value} icon has ${picker.rects[index]} rectangles, want ${wants[value]}`,
        );
      }
    });
    if (picker.checked.length !== 1 || picker.checked[0] !== "4") {
      failures.push(`layout picker: checked options are ${picker.checked.join(",")}, want 4`);
    }
    if (parseFloat(picker.selectedBorder) <= parseFloat(picker.unselectedBorder)) {
      failures.push("layout picker: the selected label has no non-colour border change");
    }
    if (!picker.buttonsHidden) failures.push("layout picker: the no-script buttons are still shown");
    // Arrow keys move the native radio group and apply in place.
    await page.locator("#live-layout-4").focus();
    await page.keyboard.press("ArrowRight");
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-layout") === "6",
      null,
      { timeout: 5000 },
    );
    const after = await page.evaluate(() => ({
      checked: document.querySelector("input[name='layout']:checked")?.value,
      playing: document.querySelectorAll(
        '[data-live-pane]:not([hidden]) [data-live-player][data-player-state="playing"]',
      ).length,
    }));
    if (after.checked !== "6") failures.push("layout picker: ArrowRight did not check the next layout");
    if (after.playing < 1) failures.push("layout picker: no player is playing after the keyboard change");
  } catch (error) {
    failures.push(`layout picker: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 13. Sixteen panes with two cameras leaves fourteen placeholders that do not
// look like a form and never open a media session until a camera is assigned.
async function walkPlaceholders(browser, baseURL, password) {
  const failures = [];
  await mkdir(SCREENSHOT_DIR, { recursive: true });
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1920, height: 1080 },
    layout: 16,
    cameras: ["front_door", "driveway"],
  });
  try {
    await page.waitForFunction(
      () => document.querySelectorAll("[data-live-pane]:not([hidden])").length === 16,
      null,
      { timeout: 10000 },
    );
    const before = await page.evaluate(() => {
      const empty = Array.from(
        document.querySelectorAll('[data-live-pane][data-live-occupied="false"]'),
      );
      const occupied = document.querySelector('[data-live-pane][data-live-occupied="true"]');
      return {
        empty: empty.length,
        playersInEmpty: empty.reduce(
          (count, pane) => count + pane.querySelectorAll("[data-live-player]").length,
          0,
        ),
        visibleControls: empty.reduce(
          (count, pane) =>
            count +
            Array.from(
              pane.querySelectorAll("select, button, input, a[href]"),
            ).filter((control) => {
              const style = getComputedStyle(control);
              return (
                control.getClientRects().length > 0 &&
                style.display !== "none" &&
                style.visibility !== "hidden"
              );
            }).length,
          0,
        ),
        placeholderText: empty[0]?.querySelector("[data-live-placeholder]")?.textContent.trim(),
        emptyBackground: getComputedStyle(empty[0]).backgroundColor,
        occupiedBackground: getComputedStyle(occupied).backgroundColor,
        sockets: window.__liveStub.sockets.length,
      };
    });
    if (before.empty !== 14) failures.push(`placeholders: ${before.empty} placeholders, want 14`);
    if (before.playersInEmpty !== 0) {
      failures.push("placeholders: an empty pane opened a media session");
    }
    if (before.visibleControls !== 0) {
      failures.push(`placeholders: ${before.visibleControls} form controls are visible at rest`);
    }
    if (!before.placeholderText?.includes("Choose camera")) {
      failures.push("placeholders: the placeholder does not say how to assign a camera");
    }
    if (before.emptyBackground === before.occupiedBackground) {
      failures.push("placeholders: the placeholder is not visually distinct");
    }
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-placeholder-16-1920x1080.png"),
      fullPage: false,
    });
    // A click opens the camera chooser on that pane and still opens no media.
    const box = await page.locator('[data-live-pane="3"]').boundingBox();
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="3"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    await page.waitForFunction(
      () => document.activeElement?.getAttribute("name") === "camera",
      null,
      { timeout: 2000 },
    );
    const after = await page.evaluate(() => ({
      players: document.querySelector('[data-live-pane="3"]').querySelectorAll("[data-live-player]")
        .length,
      sockets: window.__liveStub.sockets.length,
      camera: Boolean(
        document.querySelector('[data-live-pane="3"] [data-live-camera-select]'),
      ),
      focused: document.activeElement.getAttribute("name"),
    }));
    if (after.players !== 0) failures.push("placeholders: the click created a player");
    if (after.sockets !== before.sockets) failures.push("placeholders: the click opened a socket");
    if (!after.camera) failures.push("placeholders: the click did not show the camera chooser");
    if (after.focused !== "camera") {
      failures.push(`placeholders: the click did not focus the camera chooser (${after.focused})`);
    }
  } catch (error) {
    failures.push(`placeholders: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 13b. The pane overlay is pictorial and compact. Three stream glyphs, one
// camera glyph, accessible names and a check state replace the select stack,
// and the shown overlay stays within the area budget at sixteen panes.
async function walkCompactPaneControls(browser, baseURL, password) {
  const failures = [];
  await mkdir(SCREENSHOT_DIR, { recursive: true });
  for (const width of WIDTHS.filter((candidate) => candidate.width === 360 || candidate.width === 1280)) {
    const { context, page } = await openWall(browser, baseURL, password, {
      viewport: width,
      layout: 16,
      cameras: ["front_door", "driveway", "side_gate", "back_garden"],
      hasTouch: width.width === 360,
    });
    try {
      await page.waitForFunction(
        () => document.querySelectorAll("[data-live-pane]:not([hidden])").length === 16,
        null,
        { timeout: 10000 },
      );
      await page.locator('[data-live-pane="1"]').scrollIntoViewIfNeeded();
      await selectPane(page, 1);
      const compact = await page.evaluate(() => {
        const paneElement = document.querySelector('[data-live-pane="1"]');
        const overlay = paneElement.querySelector("[data-live-pane-overlay]");
        const paneBox = paneElement.getBoundingClientRect();
        const overlayBox = overlay.getBoundingClientRect();
        const choices = Array.from(overlay.querySelectorAll("label.live-quality-choice"));
        const checked = overlay.querySelector(
          "[data-live-quality-group] input[data-live-quality-choice]:checked",
        );
        const camera = overlay.querySelector("[data-live-camera-select]");
        return {
          paneArea: paneBox.width * paneBox.height,
          overlayArea: overlayBox.width * overlayBox.height,
          paneHeight: paneBox.height,
          overlayHeight: overlayBox.height,
          glyphs: overlay.querySelectorAll("label.live-quality-choice .live-quality-glyph").length,
          cameraGlyph: Boolean(overlay.querySelector(".live-camera-form, [data-live-camera-form] .live-camera-glyph svg")),
          names: choices.map((choice) => choice.textContent.trim()),
          checked: checked ? checked.value : "",
          checkShown: checked
            ? getComputedStyle(checked.closest("label").querySelector(".live-quality-check"))
                .display !== "none"
            : false,
          cameraName: camera?.labels?.[0]?.textContent.trim() || "",
          current: overlay.querySelector("[data-live-current-stream]")?.textContent.trim(),
        };
      });
      const ratio = compact.overlayArea / compact.paneArea;
      if (ratio > OVERLAY_AREA_BUDGET) {
        failures.push(
          `compact controls at ${width.width}x${width.height}: overlay takes ${(ratio * 100).toFixed(1)}% of the pane (${Math.round(compact.overlayHeight)} of ${Math.round(compact.paneHeight)}px), budget ${(OVERLAY_AREA_BUDGET * 100).toFixed(0)}%`,
        );
      }
      if (compact.glyphs !== 3) {
        failures.push(`compact controls at ${width.width}x${width.height}: ${compact.glyphs} stream glyphs, want 3`);
      }
      if (!compact.cameraGlyph) {
        failures.push(`compact controls at ${width.width}x${width.height}: no camera glyph`);
      }
      for (const name of ["Main stream", "Sub stream", "Snapshots"]) {
        if (!compact.names.includes(name)) {
          failures.push(`compact controls at ${width.width}x${width.height}: no accessible name ${name}`);
        }
      }
      if (!compact.checked || !compact.checkShown) {
        failures.push(`compact controls at ${width.width}x${width.height}: the selected glyph has no check state`);
      }
      if (!compact.cameraName.includes("Camera for")) {
        failures.push(`compact controls at ${width.width}x${width.height}: the camera icon has no accessible name (${compact.cameraName})`);
      }
      if (!compact.current.includes("Current stream:")) {
        failures.push(`compact controls at ${width.width}x${width.height}: the pane line is ${compact.current}`);
      }
      await page.evaluate(() => window.BitternLiveWall.hideWallOverlay());
      await page.screenshot({
        path: path.join(
          SCREENSHOT_DIR,
          `live-wall-compact-16-${width.width}x${width.height}.png`,
        ),
        fullPage: false,
      });
    } catch (error) {
      failures.push(`compact controls at ${width.width}x${width.height}: ${error.message}`);
    } finally {
      await context.close();
    }
  }
  return failures;
}

// 13c. Clicking the video dismisses that pane's overlay and the wall overlay
// without navigating and without stopping playback.
async function walkClickDismiss(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
  });
  try {
    await waitForPlayingCount(page, 4);
    const start = await page.evaluate(() => {
      window.__dismissMarker = "keep";
      window.__dismissPlayer = document.querySelector('[data-live-pane="1"] [data-live-player]');
      return { url: document.location.href };
    });
    let navigations = 0;
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) navigations += 1;
    });
    await selectPane(page, 1);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown" &&
        document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "shown",
      null,
      { timeout: 5000 },
    );
    await page.locator('[data-live-pane="1"] video').click();
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "hidden" &&
        document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "hidden",
      null,
      { timeout: 5000 },
    );
    const after = await page.evaluate(() => {
      const player = document.querySelector('[data-live-pane="1"] [data-live-player]');
      return {
        url: document.location.href,
        marker: window.__dismissMarker,
        samePlayer: window.__dismissPlayer === player,
        state: player.getAttribute("data-player-state"),
        active: player.getAttribute("data-player-active"),
      };
    });
    if (after.url !== start.url) failures.push(`dismiss: the click navigated to ${after.url}`);
    if (after.marker !== "keep" || !after.samePlayer) {
      failures.push("dismiss: the click reloaded or replaced the player");
    }
    if (after.state !== "playing" || after.active !== "true") {
      failures.push(`dismiss: the player is ${after.state} after the click`);
    }
    if (navigations !== 0) failures.push("dismiss: the click caused a navigation");
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-click-dismiss-1280x800.png"),
      fullPage: false,
    });
  } catch (error) {
    failures.push(`dismiss: ${error.message}`);
  } finally {
    await context.close();
  }
  return failures;
}

// 14. The stream control is an icon radio group with a current-stream word.
// The selected choice changes its border and check glyph, not only its colour,
// and a one-stream camera says why there is no choice.
async function walkStreamSelector(browser, baseURL, password) {
  const failures = [];
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
  });
  try {
    await selectPane(page, 1);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    const first = await page.evaluate(() => {
      const pane = document.querySelector('[data-live-pane="1"]');
      const group = pane.querySelector("[data-live-quality-group]");
      const choices = Array.from(group.querySelectorAll("label.live-quality-choice"));
      const checked = group.querySelector("input[data-live-quality-choice]:checked");
      const checkedLabel = checked.closest("label");
      const uncheckedLabel = choices.find((choice) => choice !== checkedLabel);
      return {
        value: checked.value,
        selected: checkedLabel.textContent.trim(),
        current: pane.querySelector("[data-live-current-stream]")?.textContent.trim(),
        options: choices.map((choice) => choice.textContent.trim()),
        glyphs: group.querySelectorAll("label.live-quality-choice svg.live-quality-glyph").length,
        selectedBorder: parseFloat(getComputedStyle(checkedLabel).borderTopWidth),
        unselectedBorder: parseFloat(getComputedStyle(uncheckedLabel).borderTopWidth),
        checkShown: getComputedStyle(checkedLabel.querySelector(".live-quality-check")).display !== "none",
        checkHidden:
          getComputedStyle(uncheckedLabel.querySelector(".live-quality-check")).display === "none",
        disabled: Array.from(group.querySelectorAll("input")).every((input) => input.disabled),
      };
    });
    if (first.disabled) failures.push(`stream selector: ${first.current}`);
    if (first.value !== "sub") failures.push(`stream selector: selected ${first.value}, want sub`);
    if (first.selected !== "Sub stream") {
      failures.push(`stream selector: the selected choice is ${first.selected}`);
    }
    if (!first.current.includes("Current stream: Sub stream")) {
      failures.push(`stream selector: the current indicator is ${first.current}`);
    }
    if (!first.options.includes("Main stream") || !first.options.includes("Snapshots")) {
      failures.push(`stream selector: choices are ${first.options.join(", ")}`);
    }
    if (first.glyphs !== 3) failures.push(`stream selector: ${first.glyphs} glyphs, want 3`);
    if (first.selectedBorder <= first.unselectedBorder) {
      failures.push("stream selector: the selected choice has no non-colour border change");
    }
    if (!first.checkShown || !first.checkHidden) {
      failures.push("stream selector: the selected choice has no check glyph");
    }
    await chooseQuality(page, 1, "main");
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="1"] [data-live-player]')
          ?.getAttribute("data-profile") === "main",
      null,
      { timeout: 5000 },
    );
    const changed = await page.evaluate(() => {
      const pane = document.querySelector('[data-live-pane="1"]');
      const checked = pane.querySelector(
        "[data-live-quality-group] input[data-live-quality-choice]:checked",
      );
      return {
        value: checked.value,
        selected: checked.closest("label").textContent.trim(),
        current: pane.querySelector("[data-live-current-stream]")?.textContent.trim(),
      };
    });
    if (changed.value !== "main" || changed.selected !== "Main stream") {
      failures.push(`stream selector: after the change the choice is ${changed.value} ${changed.selected}`);
    }
    if (!changed.current.includes("Current stream: Main stream")) {
      failures.push(`stream selector: after the change the indicator is ${changed.current}`);
    }
    // A one-stream camera shows one glyph and the reason, not three choices.
    await selectPane(page, 3);
    await page.waitForFunction(
      () =>
        document
          .querySelector('[data-live-pane="3"] [data-live-pane-overlay]')
          ?.getAttribute("data-live-pane-overlay-state") === "shown",
      null,
      { timeout: 5000 },
    );
    const single = await page.evaluate(() => {
      const pane = document.querySelector('[data-live-pane="3"]');
      return {
        groupHidden: pane.querySelector("[data-live-quality-group]")?.hidden,
        singleVisible: Boolean(
          pane.querySelector("[data-live-quality-single]")?.getClientRects().length,
        ),
        current: pane.querySelector("[data-live-current-stream]")?.textContent.trim(),
        reason: pane.querySelector("[data-live-one-stream]")?.textContent.trim(),
      };
    });
    if (!single.groupHidden) failures.push("stream selector: a one-stream camera offers a choice");
    if (!single.singleVisible) failures.push("stream selector: the one-stream glyph is missing");
    if (!single.current.includes("One stream")) {
      failures.push(`stream selector: the one-stream indicator is ${single.current}`);
    }
    if (!single.reason?.includes("second stream")) {
      failures.push("stream selector: the one-stream reason is missing");
    }
  } catch (error) {
    failures.push(`stream selector: ${error.message}`);
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
  failures.push(...(await walkLayouts(browser, baseURL, adminPassword)));
  failures.push(...(await walkOverlay(browser, baseURL, adminPassword)));
  failures.push(...(await walkNoScript(browser, baseURL, adminPassword)));
  failures.push(...(await walkManualQuality(browser, baseURL, adminPassword)));
  failures.push(...(await walkCapacityHysteresis(browser, baseURL, adminPassword)));
  failures.push(...(await walkSnapshotPanes(browser, baseURL, adminPassword)));
  failures.push(...(await walkKeyboard(browser, baseURL, adminPassword)));
  failures.push(...(await walkFullScreenControl(browser, baseURL, adminPassword)));
  failures.push(...(await walkInPlaceControls(browser, baseURL, adminPassword)));
  failures.push(...(await walkLayoutPicker(browser, baseURL, adminPassword)));
  failures.push(...(await walkPlaceholders(browser, baseURL, adminPassword)));
  failures.push(...(await walkCompactPaneControls(browser, baseURL, adminPassword)));
  failures.push(...(await walkClickDismiss(browser, baseURL, adminPassword)));
  failures.push(...(await walkStreamSelector(browser, baseURL, adminPassword)));
  for (const resolution of [
    { width: 1920, height: 1080 },
    { width: 3840, height: 2160 },
  ]) {
    for (const width of WIDTHS) {
      failures.push(...(await walkLargeStream(browser, baseURL, adminPassword, width, resolution)));
    }
  }
  return failures;
}
