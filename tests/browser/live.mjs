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
// keyboard (1), large streams at four widths (8).
export const LIVE_CHECK_COUNT = 8 + 4 + 24 + 2 + 1 + 4 + 1 + 1 + 8;

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
      document.querySelectorAll('[data-live-player][data-player-state="playing"]').length ===
      expected,
    count,
    { timeout },
  );
}

async function showOverlay(page) {
  const box = await page.locator("[data-live-grid]").boundingBox();
  // Two moves, so a pointer that already sits on the reveal point still
  // produces a pointermove event.
  await page.mouse.move(box.x + 2, box.y + 2);
  await page.mouse.move(box.x + box.width / 2, box.y + box.height * 0.2);
  await page.waitForFunction(
    () => document.querySelector("[data-live-grid]")?.getAttribute("data-live-overlay") === "shown",
    null,
    { timeout: 5000 },
  );
}

function focusSelectedPane(page) {
  return page.evaluate(() => {
    document.querySelector("[data-live-pane][data-live-selected='true']")?.focus();
  });
}

async function selectPane(page, number) {
  const box = await page.locator(`[data-live-pane="${number}"]`).boundingBox();
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.waitForFunction(
    (n) =>
      document
        .querySelector(`[data-live-pane="${n}"]`)
        ?.getAttribute("data-live-selected") === "true",
    number,
    { timeout: 5000 },
  );
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
      panes: Array.from(document.querySelectorAll("[data-live-pane]")).map((pane) => rect(pane)),
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
          (count) => document.querySelectorAll("[data-live-pane]").length === count,
          layout,
          { timeout: 10000 },
        );
        const geometry = await paneGeometry(page);
        assertPaneGeometry(geometry, label, failures, layout);
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
      () => document.querySelector("[data-live-grid]")?.getAttribute("data-live-overlay") === "hidden",
      null,
      { timeout: 10000 },
    );
    await page.waitForTimeout(250);
    const hidden = await page.evaluate(() => {
      const wall = document.querySelector("[data-live-grid]");
      const panel = document.querySelector("[data-live-overlay-panel]");
      const panes = Array.from(document.querySelectorAll("[data-live-pane]"));
      const last = panes[panes.length - 1].getBoundingClientRect();
      const point = document.elementFromPoint(last.left + last.width / 2, last.top + last.height / 2);
      return {
        inert: panel.hasAttribute("inert"),
        pointerEvents: getComputedStyle(panel).pointerEvents,
        visibility: getComputedStyle(panel).visibility,
        topmostInPanel: Boolean(point && point.closest("[data-live-overlay-panel]")),
        opaque: getComputedStyle(panel).opacity,
        wallState: wall.getAttribute("data-live-overlay"),
      };
    });
    if (!hidden.inert) failures.push("overlay: the hidden panel is not inert");
    if (hidden.pointerEvents !== "none") {
      failures.push(`overlay: the hidden panel takes pointer events (${hidden.pointerEvents})`);
    }
    if (hidden.visibility !== "hidden") failures.push("overlay: the hidden panel is still visible");
    if (hidden.opaque !== "0") failures.push(`overlay: the hidden panel is opaque (${hidden.opaque})}`);
    if (hidden.topmostInPanel) failures.push("overlay: the hidden panel covers the video");
    if (hidden.wallState !== "hidden") failures.push("overlay: the wall is not in the hidden state");
    // A tap on the last pane reaches the video, not the panel, selects that
    // pane and reveals the overlay. A touch has no pointer move before it, so
    // this is the reveal path the ADR names for touch.
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
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "shown",
      null,
      { timeout: 5000 },
    );
    // Idle hides it after three seconds.
    step = "idle hide";
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "hidden",
      null,
      { timeout: 8000 },
    );
    // Pointer movement reveals it.
    step = "pointer reveal";
    await showOverlay(page);
    await page.waitForTimeout(3400);
    const afterIdle = await page.evaluate(() =>
      document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
    );
    if (afterIdle !== "hidden") failures.push("overlay: pointer reveal did not idle out");
    // A key press reveals it.
    step = "key reveal";
    await focusSelectedPane(page);
    await page.keyboard.press("ArrowUp");
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "shown",
      null,
      { timeout: 5000 },
    );
    // Focus inside suspends hiding.
    await page.evaluate(() => document.querySelector("input[name='quality']:checked").focus());
    await page.waitForTimeout(3400);
    const focused = await page.evaluate(() => ({
      state: document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
      inPanel: Boolean(document.activeElement.closest("[data-live-overlay-panel]")),
    }));
    if (focused.state !== "shown") failures.push("overlay: focus inside did not suspend hiding");
    if (!focused.inPanel) failures.push("overlay: focus left the overlay");
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, "live-wall-overlay-1280x800.png"),
      fullPage: false,
    });
    await page.evaluate(() => document.activeElement.blur());
    // Tab from the pane reveals the overlay with focus on the quality control.
    step = "tab reveal";
    await focusSelectedPane(page);
    await page.keyboard.press("Tab");
    const revealed = await page.evaluate(() => ({
      state: document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
      quality: Boolean(document.activeElement.closest("[data-live-quality]")),
      name: document.activeElement.getAttribute("name"),
    }));
    if (revealed.state !== "shown") failures.push("overlay: Tab did not reveal the overlay");
    if (!revealed.quality || revealed.name !== "quality") {
      failures.push("overlay: Tab did not focus the quality control");
    }
    // Shift+Tab from the pane reveal focuses the last control.
    step = "shift tab";
    await page.evaluate(() => document.activeElement.blur());
    await page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "hidden",
      null,
      { timeout: 8000 },
    );
    await focusSelectedPane(page);
    const pre = await page.evaluate(() => {
      const selected = document.querySelector("[data-live-pane][data-live-selected='true']");
      return {
        overlay: document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
        active: document.activeElement === selected,
        selectedCount: document.querySelectorAll("[data-live-pane][data-live-selected='true']").length,
        number: selected ? selected.getAttribute("data-live-pane") : "none",
        registered: typeof window.BitternLiveWall.onKeydown,
      };
    });
    await page.keyboard.press("Shift+Tab");
    const back = await page.evaluate(() => ({
      state: document.querySelector("[data-live-grid]").getAttribute("data-live-overlay"),
      exit: document.activeElement.hasAttribute("data-live-fullscreen"),
      active: document.activeElement.tagName + " " + (document.activeElement.getAttribute("name") || "") + " " + (document.activeElement.getAttribute("data-live-pane") || "") + " " + (document.activeElement.closest("[data-live-overlay-panel]") ? "in panel" : "outside"),
      hidden: document.activeElement.hasAttribute("hidden"),
    }));
    if (back.state !== "shown") failures.push("overlay: Shift+Tab did not reveal the overlay");
    if (!back.exit) {
      failures.push(
        `overlay: Shift+Tab did not focus the exit control (${back.active}; before ${JSON.stringify(pre)})`,
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
  // The same overlay at phone width, for the four-width read.
  const phone = await openWall(browser, baseURL, password, {
    viewport: { width: 360, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    hasTouch: true,
  });
  try {
    await selectPane(phone.page, 1);
    await phone.page.waitForFunction(
      () => document.querySelector("[data-live-grid]").getAttribute("data-live-overlay") === "shown",
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
    if ((await page.locator("[data-live-quality] input").count()) !== 3) {
      failures.push("no script: the quality control does not have three choices");
    }
    const action = await page.locator("[data-live-pane-form]").getAttribute("action");
    if (action !== "/ui/live/pane") {
      failures.push("no script: the quality control is not a real form");
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
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 4,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
    clock: true,
  });
  try {
    await page.clock.runFor(300);
    await waitForPlayingCount(page, 4);
    await selectPane(page, 1);
    await showOverlay(page);
    await page.locator("#live-quality-main").click();
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
    await page.clock.runFor(240000);
    const afterPressure = await page.evaluate(() => {
      const panes = Array.from(document.querySelectorAll("[data-live-pane]")).map((pane) => ({
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
    // A layout change keeps the per-pane quality on the server.
    await showOverlay(page);
    await Promise.all([
      page.waitForNavigation({ waitUntil: "load", timeout: 15000 }),
      page.locator("#live-layout-6").click(),
    ]);
    const stored = await page.evaluate(() => {
      const wall = document.querySelector("[data-live-grid]");
      const player = document.querySelector('[data-live-pane="1"] [data-live-player]');
      return {
        layout: wall.getAttribute("data-layout"),
        manual: player.getAttribute("data-manual-quality"),
        quality: player.getAttribute("data-quality"),
        profile: player.getAttribute("data-profile"),
        panes: document.querySelectorAll("[data-live-pane]").length,
      };
    });
    if (stored.layout !== "6" || stored.panes !== 6) {
      failures.push(`manual: the layout change made layout ${stored.layout} with ${stored.panes} panes`);
    }
    if (stored.manual !== "main" || stored.quality !== "main" || stored.profile !== "main") {
      failures.push("manual: the layout change lost the manual quality");
    }
  } catch (error) {
    failures.push(`manual: ${error.message}`);
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
    await showOverlay(page);
    await page.locator("#live-quality-snapshot").click();
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
  const { context, page } = await openWall(browser, baseURL, password, {
    viewport: { width: 1280, height: 800 },
    layout: 6,
    cameras: ["front_door", "driveway", "side_gate", "back_garden"],
  });
  try {
    const stops = await page.evaluate(() => {
      const panes = Array.from(document.querySelectorAll("[data-live-pane]"));
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
    await enterFullScreen(page);
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
    failures.push(`keyboard: ${error.message}`);
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
