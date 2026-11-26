// Opt-in headless browser check for the Bittern UI (ADR 0025).
//
// Run it through `make ui-browser-check`, which provisions the daemon and the
// pinned Node package. The harness starts its own bitternd, signs in through
// the real auth flow, walks tests/browser/routes.json at every width, writes
// one full-page screenshot per route and width under build/ui-screenshots for
// visual review, plus a second image with every closed disclosure opened, and
// fails on page overflow, an unreachable control, an overlapped control, a
// visible image outside the viewport, or a control below its role's touch
// target measured in the viewport.

import { existsSync } from "node:fs";
import { mkdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { checkLayout } from "./assertions.mjs";
import { runLivePlayerChecks } from "./live.mjs";
import { startBitternd } from "./server.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, "../..");
const SCREENSHOT_DIR = path.join(ROOT, "build", "ui-screenshots");
const WIDTHS = [
  { name: "phone-narrow", width: 360, height: 800 },
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 800 },
  { name: "desktop-wide", width: 1920, height: 1080 },
];
const ACTION_TIMEOUT = 15_000;
const LIVE_CHECK_COUNT = 12;
const WAIT_STATES = new Set(["attached", "detached", "visible", "hidden"]);

// The manifest actions are deliberately a short list, not a test framework:
// each one performs one browser step before the screenshot is written.
const ACTIONS = {
  click: async (page, action) => {
    await page.locator(action.selector).first().click();
  },
  wait: async (page, action) => {
    const state = action.state ?? "visible";
    if (!WAIT_STATES.has(state)) {
      throw new Error(
        `unknown wait state ${JSON.stringify(state)}; ` +
          `supported states are ${[...WAIT_STATES].join(", ")}`,
      );
    }
    await page
      .locator(action.selector)
      .first()
      .waitFor({ state, timeout: ACTION_TIMEOUT });
    if (action.decoded) {
      await page.waitForFunction(
        (selector) => {
          const element = document.querySelector(selector);
          return (
            element instanceof HTMLImageElement &&
            element.complete &&
            element.naturalWidth > 0
          );
        },
        action.selector,
        { timeout: ACTION_TIMEOUT },
      );
    }
  },
};

async function readJSON(file) {
  return JSON.parse(await readFile(file, "utf8"));
}

function slugFor(entry) {
  return entry.variant ? `${entry.route}-${entry.variant}` : entry.route;
}

async function waitForPage(page) {
  await page.evaluate(() => document.fonts.ready.then(() => true));
}

async function runActions(page, entry) {
  const actions = entry.actions ?? [];
  if (!Array.isArray(actions)) {
    throw new Error(`entry ${slugFor(entry)} actions must be a list`);
  }
  for (const [index, action] of actions.entries()) {
    const name = action && typeof action === "object" ? action.action : undefined;
    const run = Object.hasOwn(ACTIONS, name) ? ACTIONS[name] : undefined;
    if (!run) {
      throw new Error(
        `entry ${slugFor(entry)} action ${index + 1} names unknown action ` +
          `${JSON.stringify(name)}; supported actions are ${Object.keys(ACTIONS).join(", ")}`,
      );
    }
    if (!action || typeof action.selector !== "string" || action.selector.trim() === "") {
      throw new Error(
        `entry ${slugFor(entry)} action ${index + 1} (${name}) needs a selector`,
      );
    }
    try {
      await run(page, action);
    } catch (error) {
      throw new Error(
        `entry ${slugFor(entry)} action ${index + 1} (${name} ${action.selector}) ` +
          `failed: ${error.message}`,
      );
    }
  }
}

// Open every closed <details> after the default state has been measured, so a
// reviewer can see the expanded content too. This is a review artifact only:
// any failure here becomes a warning and never fails the check.
async function captureExpandedDisclosures({ page, screenshot, result }) {
  const expandedScreenshot = screenshot.replace(/\.png$/, "-expanded.png");
  try {
    const report = await page.evaluate(() => {
      const closed = Array.from(document.querySelectorAll("details:not([open])"));
      const warnings = [];
      let opened = 0;
      for (const details of closed) {
        try {
          if (!details.isConnected) {
            throw new Error("element disappeared before it could be opened");
          }
          details.open = true;
          if (!details.open) {
            throw new Error("element did not open");
          }
          if (!details.isConnected) {
            throw new Error("element disappeared while it was being opened");
          }
          opened += 1;
        } catch (error) {
          const label = details.id ? `details#${details.id}` : "details";
          warnings.push(`could not open ${label}: ${error.message}`);
        }
      }
      return { opened, warnings };
    });
    result.warnings.push(...report.warnings);
    if (report.opened === 0) {
      return;
    }
    await page.screenshot({ path: expandedScreenshot, fullPage: true });
    result.expanded = true;
  } catch (error) {
    result.warnings.push(
      `could not capture the expanded disclosure state: ${error.message}`,
    );
  }
}

async function runLiveRefreshCheck({ page, width, result }) {
  const reconnectScreenshot = path.join(
    SCREENSHOT_DIR,
    `status-live-reconnect-${width.width}x${width.height}.png`,
  );

  const readState = () =>
    page.evaluate(() => {
      const details = document.getElementById("status-technical-details");
      const table = document.getElementById("status-config-problems-scroll");
      const card = document.getElementById("update-card-live");
      return {
        open: Boolean(details && details.open),
        focus: document.activeElement ? document.activeElement.id : "",
        left: table ? table.scrollLeft : 0,
        top: table ? table.scrollTop : 0,
        marker: table ? table.dataset.liveMarker : "",
        card: card ? card.textContent.trim() : "",
      };
    });

  const initial = await page.evaluate(() => {
    const details = document.getElementById("status-technical-details");
    const summary = document.getElementById("status-technical-summary");
    const table = document.getElementById("status-config-problems-scroll");
    if (!details || !summary) {
      throw new Error("the status technical details disclosure is missing");
    }
    details.open = true;
    summary.focus();
    if (table) {
      table.dataset.liveMarker = "kept";
      table.scrollLeft = Math.min(24, table.scrollWidth - table.clientWidth);
      table.scrollTop = Math.min(12, table.scrollHeight - table.clientHeight);
    }
    window.__bitternLivePolls = 0;
    document.body.addEventListener("htmx:afterRequest", (event) => {
      const element = event.detail.elt;
      if (
        element &&
        element.id === "update-card-live" &&
        event.detail.successful
      ) {
        window.__bitternLivePolls += 1;
      }
    });
    const card = document.getElementById("update-card-live");
    return {
      open: details.open,
      focus: document.activeElement ? document.activeElement.id : "",
      left: table ? table.scrollLeft : 0,
      top: table ? table.scrollTop : 0,
      marker: table ? table.dataset.liveMarker : "",
      card: card ? card.textContent.trim() : "",
    };
  });

  if (!initial.open || initial.focus !== "status-technical-summary") {
    throw new Error("the disclosure did not open or keep focus before the poll");
  }

  await page.waitForFunction(() => window.__bitternLivePolls >= 2, null, {
    timeout: 15_000,
  });
  const afterPolls = await readState();
  if (!afterPolls.open) {
    throw new Error("the disclosure closed after two live polls");
  }
  if (afterPolls.focus !== "status-technical-summary") {
    throw new Error(`focus moved to ${afterPolls.focus || "nothing"} after two live polls`);
  }
  if (afterPolls.left !== initial.left || afterPolls.top !== initial.top) {
    throw new Error("the table scroll position changed after two live polls");
  }
  if (afterPolls.marker !== "kept") {
    throw new Error("the table wrapper was replaced by the live poll");
  }

  const abortFragment = (route) => route.abort("failed");
  await page.route("**/ui/status/updates/fragment*", abortFragment);
  await page.locator("#update-card-reconnect").waitFor({
    state: "visible",
    timeout: 10_000,
  });
  const duringOutage = await readState();
  if (!duringOutage.open) {
    throw new Error("the disclosure closed while the live poll was offline");
  }
  if (duringOutage.focus !== "status-technical-summary") {
    throw new Error("focus was lost while the live poll was offline");
  }
  if (
    duringOutage.left !== initial.left ||
    duringOutage.top !== initial.top ||
    duringOutage.marker !== "kept"
  ) {
    throw new Error("the table state changed while the live poll was offline");
  }
  if (duringOutage.card !== afterPolls.card) {
    throw new Error("the live card content was clobbered while the poll was offline");
  }
  await page.screenshot({ path: reconnectScreenshot, fullPage: true });
  result.liveScreenshot = true;

  await page.unroute("**/ui/status/updates/fragment*", abortFragment);
  const pollsBeforeRecovery = await page.evaluate(() => window.__bitternLivePolls);
  await page.waitForFunction(
    (count) => window.__bitternLivePolls > count,
    pollsBeforeRecovery,
    { timeout: 15_000 },
  );
  await page.locator("#update-card-reconnect").waitFor({
    state: "hidden",
    timeout: 5_000,
  });
  const recovered = await readState();
  if (!recovered.open || recovered.focus !== "status-technical-summary") {
    throw new Error("the disclosure or focus was lost after the poll recovered");
  }
  if (
    recovered.left !== initial.left ||
    recovered.top !== initial.top ||
    recovered.marker !== "kept"
  ) {
    throw new Error("the table state changed after the poll recovered");
  }
}

async function runEntry({ page, entry, width, baseURL, results }) {
  const slug = slugFor(entry);
  const label = `${width.name} ${slug} (${entry.path})`;
  const target = `${baseURL}${entry.path}`;
  const screenshot = path.join(
    SCREENSHOT_DIR,
    `${slug}-${width.width}x${width.height}.png`,
  );
  const result = {
    index: entry.index,
    label,
    slug,
    width: width.name,
    failures: [],
    warnings: [],
    screenshot: false,
    expanded: false,
  };

  try {
    await page.goto(target, { waitUntil: "load", timeout: 15_000 });
    await waitForPage(page);
    await runActions(page, entry);
    // Full-page capture is a review artifact, not a measurement change: the
    // layout assertions below still measure the viewport. Write the image
    // before any assertion so a failing screen still produces its screenshot.
    await page.screenshot({ path: screenshot, fullPage: true });
    result.screenshot = true;
    const expectedPath = new URL(target).pathname;
    const actualPath = new URL(page.url()).pathname;
    if (actualPath !== expectedPath) {
      result.failures.push(
        `landed on ${actualPath} instead of ${expectedPath}; the session or route state is wrong`,
      );
    }
    const layout = await checkLayout(page);
    result.failures.push(...layout.failures);
    result.warnings.push(...layout.warnings);
    if (entry.live === "refresh") {
      try {
        await runLiveRefreshCheck({ page, width, result });
      } catch (error) {
        result.failures.push(`live refresh check failed: ${error.message}`);
      }
    }
    await captureExpandedDisclosures({ page, screenshot, result });
  } catch (error) {
    result.failures.push(`could not load the page: ${error.message}`);
  }

  results.push(result);
}

async function completeSetup(page, setupToken, adminPassword) {
  try {
    await page.fill("#setup-token", setupToken);
    await page.fill("#setup-password", adminPassword);
    await page.fill("#setup-password-confirm", adminPassword);
    await Promise.all([
      page.waitForURL("**/ui/wizard", { timeout: 15_000 }),
      page.click("button[type='submit']"),
    ]);
  } catch (error) {
    const alert = await page.locator(".alert").innerText().catch(() => "");
    throw new Error(
      `setup did not complete: ${error.message}${alert ? ` (${alert})` : ""}`,
    );
  }
}

async function signIn(page, adminPassword) {
  try {
    await page.fill("#login-username", "admin");
    await page.fill("#login-password", adminPassword);
    await Promise.all([
      page.waitForURL("**/ui/status", { timeout: 15_000 }),
      page.click("button[type='submit']"),
    ]);
  } catch (error) {
    const alert = await page.locator(".alert").innerText().catch(() => "");
    throw new Error(
      `sign-in did not complete: ${error.message}${alert ? ` (${alert})` : ""}`,
    );
  }
}

async function addCamera(page, baseURL, values) {
  try {
    await page.goto(`${baseURL}/ui/wizard/camera`, { waitUntil: "load" });
    // The sign-in fields sit in a closed disclosure. Open it before filling,
    // the same step a keyboard user takes.
    await page
      .locator("details.advanced-settings")
      .evaluate((details) => { details.open = true; });
    await page.fill("#camera-name", values.name);
    await page.fill("#camera-address", values.address);
    await page.fill("#camera-port", values.port);
    await page.fill("#camera-path", values.path);
    await page.fill("#camera-username", values.username);
    await page.fill("#camera-password", values.password);
    await page.locator("#camera-form button[type='submit']").click();
    await page
      .locator(".camera-card h3", { hasText: values.name })
      .waitFor({ timeout: 15_000 });
  } catch (error) {
    const alert = await page.locator(".alert").innerText().catch(() => "");
    throw new Error(
      `the camera step did not add ${values.name}: ${error.message}${alert ? ` (${alert})` : ""}`,
    );
  }
}

async function ensureCameraList(page, baseURL, state) {
  if (state.cameras) {
    return;
  }
  await addCamera(page, baseURL, {
    name: "Front door",
    address: "10.77.0.20",
    port: "554",
    path: "/stream1",
    username: "viewer",
    password: "camera-password",
  });
  await addCamera(page, baseURL, {
    name: "Driveway",
    address: "10.77.0.21",
    port: "8554",
    path: "/live",
    username: "driveway",
    password: "camera-password-2",
  });
  state.cameras = true;
}

async function ensureCameraReview(page, baseURL, state) {
  if (state.review) {
    return;
  }
  await ensureCameraList(page, baseURL, state);
  try {
    await page.goto(`${baseURL}/ui/wizard/camera`, { waitUntil: "load" });
    await Promise.all([
      page.waitForURL("**/ui/wizard/review", { timeout: 15_000 }),
      page.getByRole("button", { name: "Review configuration" }).click(),
    ]);
    state.review = true;
  } catch (error) {
    const alert = await page.locator(".alert").innerText().catch(() => "");
    throw new Error(
      `the camera step did not reach review: ${error.message}${alert ? ` (${alert})` : ""}`,
    );
  }
}

async function startApply(page, baseURL, state) {
  await ensureCameraReview(page, baseURL, state);
  if (state.started || state.applied) {
    return;
  }
  try {
    await Promise.all([
      page.waitForURL("**/ui/wizard/apply", { timeout: 15_000 }),
      page.getByRole("button", { name: "Apply configuration" }).click(),
    ]);
    state.started = true;
  } catch (error) {
    const alert = await page.locator(".alert").innerText().catch(() => "");
    throw new Error(
      `the apply step did not reach the operation page: ${error.message}${alert ? ` (${alert})` : ""}`,
    );
  }
}

async function ensureApplied(page, baseURL, state) {
  await ensureCameraReview(page, baseURL, state);
  if (state.applied) {
    return;
  }
  if (!state.started) {
    await startApply(page, baseURL, state);
  }
  try {
    await page.waitForURL("**/ui/wizard/result", { timeout: 20_000 });
    state.applied = true;
  } catch (error) {
    const alert = await page.locator(".alert").innerText().catch(() => "");
    throw new Error(
      `the apply step did not reach the result page: ${error.message}${alert ? ` (${alert})` : ""}`,
    );
  }
}

async function prepareEntry(page, entry, baseURL, state) {
  if (entry.prepare === "cameras") {
    await ensureCameraList(page, baseURL, state);
  } else if (entry.prepare === "review") {
    await ensureCameraReview(page, baseURL, state);
  } else if (entry.prepare === "started") {
    await startApply(page, baseURL, state);
  } else if (entry.prepare === "applied") {
    await ensureApplied(page, baseURL, state);
  }
}

async function main() {
  const packageJSON = await readJSON(path.join(HERE, "package.json"));
  const pinnedPlaywright = packageJSON.devDependencies["@playwright/test"];

  let playwright;
  try {
    playwright = await import("@playwright/test");
  } catch {
    console.error(
      `ui-browser-check needs @playwright/test ${pinnedPlaywright}; ` +
        "run: cd tests/browser && npm ci",
    );
    process.exitCode = 1;
    return;
  }
  const { chromium } = playwright;

  const manifest = await readJSON(path.join(HERE, "routes.json"));
  manifest.forEach((entry, index) => {
    entry.index = index;
  });

  const browserJSON = await readJSON(
    path.join(HERE, "node_modules", "playwright-core", "browsers.json"),
  );
  const chromiumPin = browserJSON.browsers.find((browser) => browser.name === "chromium");
  const executable = chromium.executablePath();
  if (!existsSync(executable)) {
    console.error(
      `ui-browser-check needs the pinned Chromium ${chromiumPin.browserVersion} ` +
        `(revision ${chromiumPin.revision}) at ${executable}; ` +
        "run: cd tests/browser && npx playwright install --with-deps chromium",
    );
    process.exitCode = 1;
    return;
  }

  const binary = process.env.BITTERND_BIN
    ? path.resolve(process.env.BITTERND_BIN)
    : path.join(ROOT, "build", "browser", "bitternd");

  await rm(SCREENSHOT_DIR, { recursive: true, force: true });
  await mkdir(SCREENSHOT_DIR, { recursive: true });

  let server;
  let browser;
  try {
    server = await startBitternd({ binary });
    browser = await chromium.launch({ headless: true });
    const browserVersion = browser.version();

    const manifestRoutes = [...new Set(manifest.map((entry) => entry.route))];
    console.log("Bittern UI browser check (ADR 0025)");
    console.log(
      `Playwright ${pinnedPlaywright}, Chromium ${chromiumPin.revision} ` +
        `(${chromiumPin.browserVersion}, running ${browserVersion})`,
    );
    console.log(`bitternd: ${binary}`);
    console.log(
      `routes: ${manifest.length} screens from ${manifestRoutes.length} routes ` +
        `in tests/browser/routes.json`,
    );
    console.log(
      `widths: ${WIDTHS.map((w) => `${w.width}x${w.height} (${w.name})`).join(", ")}`,
    );
    console.log(`screenshots: ${path.relative(ROOT, SCREENSHOT_DIR)}/`);
    console.log("");

    const results = [];
    const setupEntries = manifest.filter((entry) => entry.flow === "setup");
    const guestEntries = manifest.filter((entry) => entry.flow === "guest");
    const authEntries = manifest.filter((entry) => entry.flow === "auth");

    const setupPages = [];
    for (const width of WIDTHS) {
      const viewport = { width: width.width, height: width.height };
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      for (const entry of setupEntries) {
        await runEntry({ page, entry, width, baseURL: server.baseURL, results });
      }
      setupPages.push({ width, page, context });
    }
    if (setupPages.length === 0) {
      throw new Error("routes.json has no setup route; the auth state cannot be seeded");
    }
    await completeSetup(setupPages[0].page, server.setupToken, server.adminPassword);
    for (const setupPage of setupPages) {
      await setupPage.context.close();
    }

    for (const width of WIDTHS) {
      const viewport = { width: width.width, height: width.height };
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      for (const entry of guestEntries) {
        await runEntry({ page, entry, width, baseURL: server.baseURL, results });
      }
      await signIn(page, server.adminPassword);

      const state = { cameras: false, review: false, started: false, applied: false };
      for (const entry of authEntries) {
        try {
          await prepareEntry(page, entry, server.baseURL, state);
          await runEntry({ page, entry, width, baseURL: server.baseURL, results });
        } catch (error) {
          results.push({
            index: entry.index,
            label: `${width.name} ${slugFor(entry)} (${entry.path})`,
            slug: slugFor(entry),
            width: width.name,
            failures: [error.message],
            warnings: [],
            screenshot: false,
            expanded: false,
          });
        }
      }
      await context.close();
    }

    const widthOrder = new Map(WIDTHS.map((width, index) => [width.name, index]));
    results.sort(
      (left, right) =>
        left.index - right.index || widthOrder.get(left.width) - widthOrder.get(right.width),
    );

    let failureCount = 0;
    let warningCount = 0;
    const liveFailures = await runLivePlayerChecks({
      browser,
      baseURL: server.baseURL,
      adminPassword: server.adminPassword,
    });
    if (liveFailures.length > 0) {
      failureCount += liveFailures.length;
      console.log(`FAIL live-player (${LIVE_CHECK_COUNT} state and ceiling checks)`);
      for (const failure of liveFailures) {
        console.log(`  - ${failure}`);
      }
    } else {
      console.log(
        `passed live-player: ${LIVE_CHECK_COUNT} state and device-ceiling checks`,
      );
    }
    for (const result of results) {
      if (result.failures.length > 0) {
        failureCount += result.failures.length;
        console.log(`FAIL ${result.label}`);
        for (const failure of result.failures) {
          console.log(`  - ${failure}`);
        }
      }
      if (result.warnings.length > 0) {
        warningCount += result.warnings.length;
        console.log(`WARN ${result.label}`);
        for (const warning of result.warnings) {
          console.log(`  - ${warning}`);
        }
      }
    }

    const screenshotCount = results.filter((result) => result.screenshot).length;
    const expandedCount = results.filter((result) => result.expanded).length;
    const expectedScreens = manifest.length * WIDTHS.length;
    console.log("");
    console.log(
      `checked ${results.length} of ${expectedScreens} route-width screens, ` +
        `wrote ${screenshotCount} screenshots (${expandedCount} with expanded ` +
        `disclosures), ${manifestRoutes.length} routes at ${WIDTHS.length} widths`,
    );
    if (failureCount > 0) {
      console.log(`browser check failed: ${failureCount} failure(s), ${warningCount} warning(s)`);
      process.exitCode = 1;
    } else {
      console.log(`browser check passed: 0 failures, ${warningCount} warning(s)`);
    }
  } finally {
    if (browser) {
      await browser.close();
    }
    if (server) {
      await server.stop();
    }
  }
}

main().catch((error) => {
  console.error(`ui-browser-check: ${error.message}`);
  process.exitCode = 1;
});
