// Landing and gate checks for the three device states. Each state starts from
// a fresh daemon so the gate is the one the device actually serves, not a
// leftover session from an earlier case.

import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  CAMERA_APPLIANCE_YAML,
  NO_CAMERA_APPLIANCE_YAML,
  startBitternd,
} from "./server.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const SCREENSHOT_DIR = path.resolve(HERE, "../../build/ui-screenshots");

const WIDTHS = [
  { name: "phone-narrow", width: 360, height: 800 },
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 800 },
  { name: "desktop-wide", width: 1920, height: 1080 },
];

export const ROUTING_CHECK_COUNT = 3 * WIDTHS.length + 2;

function oneCameraYAML() {
  const start = CAMERA_APPLIANCE_YAML.indexOf("cameras:\n");
  const end = CAMERA_APPLIANCE_YAML.indexOf("\nstorage:", start);
  const block = CAMERA_APPLIANCE_YAML.slice(start, end);
  const first = block.match(/cameras:\n  - [\s\S]*?(?=\n  - |$)/);
  if (!first) throw new Error("the camera fixture has no first camera");
  return CAMERA_APPLIANCE_YAML.slice(0, start) + first[0] + CAMERA_APPLIANCE_YAML.slice(end);
}

async function completeSetup(page, server) {
  await page.goto(`${server.baseURL}/ui/setup`, { waitUntil: "load" });
  await page.fill("#setup-token", server.setupToken);
  await page.fill("#setup-password", server.adminPassword);
  await page.fill("#setup-password-confirm", server.adminPassword);
  await Promise.all([
    page.waitForURL("**/ui/wizard", { timeout: 20000 }),
    page.click("button[type='submit']"),
  ]);
}

async function signIn(page, server) {
  await page.fill("#login-username", "admin");
  await page.fill("#login-password", server.adminPassword);
  await Promise.all([
    page.waitForURL(
      (url) => url.pathname === "/ui/live" || url.pathname === "/ui/status",
      { timeout: 20000 },
    ),
    page.click("button[type='submit']"),
  ]);
}

async function navState(page) {
  return page.evaluate(() => ({
    current: document.querySelector("nav a[aria-current='page']")?.textContent.trim() || "",
    live: Array.from(document.querySelectorAll("nav a")).some(
      (link) => link.getAttribute("href") === "/ui/live",
    ),
  }));
}

async function runState(browser, binary, state, width) {
  const failures = [];
  const server = await startBitternd({
    binary,
    applianceYAML:
      state.cameras === 0 ? NO_CAMERA_APPLIANCE_YAML : CAMERA_APPLIANCE_YAML,
    setupComplete: state.initialized,
  });
  try {
    if (state.initialized) {
      const bootstrap = await browser.newContext({
        viewport: { width: width.width, height: width.height },
      });
      const setupPage = await bootstrap.newPage();
      await completeSetup(setupPage, server);
      await bootstrap.close();
    }

    const context = await browser.newContext({
      viewport: { width: width.width, height: width.height },
    });
    const page = await context.newPage();
    await page.goto(`${server.baseURL}/ui`, { waitUntil: "load" });

    if (state.initialized) {
      if (!page.url().includes("/ui/login")) {
        failures.push(
          `${state.name} at ${width.width}px: the gate was ${page.url()}, want sign-in`,
        );
      }
      await signIn(page, server);
    } else {
      if (!page.url().includes("/ui/setup")) {
        failures.push(
          `${state.name} at ${width.width}px: the gate was ${page.url()}, want setup`,
        );
      }
      await completeSetup(page, server);
    }

    const expectedPath = state.expectedPath;
    if (new URL(page.url()).pathname !== expectedPath) {
      failures.push(
        `${state.name} at ${width.width}px: landed on ` +
          `${new URL(page.url()).pathname}, want ${expectedPath}`,
      );
    }
    const nav = await navState(page);
    const expectedNav =
      state.expectedPath === "/ui/live"
        ? "Live"
        : state.expectedPath === "/ui/status"
          ? "Status"
          : "Setup";
    if (nav.current !== expectedNav) {
      failures.push(
        `${state.name} at ${width.width}px: current nav is ` +
          `${JSON.stringify(nav.current)}, want ${expectedNav}`,
      );
    }
    if (state.cameras === 0 && nav.live) {
      failures.push(
        `${state.name} at ${width.width}px: the nav offers Live with no cameras`,
      );
    }
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    await page.screenshot({
      path: path.join(
        SCREENSHOT_DIR,
        `landing-${state.name}-${width.width}x${width.height}.png`,
      ),
      fullPage: true,
    });
    await context.close();
  } catch (error) {
    failures.push(`${state.name} at ${width.width}px: ${error.message}`);
  } finally {
    await server.stop();
  }
  return failures;
}

async function walkRequestedTarget(browser, binary) {
  const failures = [];
  const server = await startBitternd({
    binary,
    applianceYAML: CAMERA_APPLIANCE_YAML,
    setupComplete: true,
  });
  try {
    const bootstrap = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const setupPage = await bootstrap.newPage();
    await completeSetup(setupPage, server);
    await bootstrap.close();

    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const page = await context.newPage();
    await page.goto(`${server.baseURL}/ui/status`, { waitUntil: "load" });
    if (!page.url().includes("/ui/login")) {
      failures.push(`requested target: the gate was ${page.url()}, want sign-in`);
    }
    await signIn(page, server);
    if (new URL(page.url()).pathname !== "/ui/status") {
      failures.push(
        `requested target: landed on ${new URL(page.url()).pathname}, want /ui/status`,
      );
    }

    await context.close();

    const redirectContext = await browser.newContext({
      viewport: { width: 1280, height: 800 },
    });
    const redirectPage = await redirectContext.newPage();
    await redirectPage.goto(
      `${server.baseURL}/ui/login?next=${encodeURIComponent("https://evil.example/ui/live")}`,
      { waitUntil: "load" },
    );
    await redirectPage.fill("#login-username", "admin");
    await redirectPage.fill("#login-password", server.adminPassword);
    await Promise.all([
      redirectPage.waitForURL(
        (url) => url.pathname === "/ui/live" || url.pathname === "/ui/status",
        { timeout: 20000 },
      ),
      redirectPage.click("button[type='submit']"),
    ]);
    const pathName = new URL(redirectPage.url()).pathname;
    if (pathName !== "/ui/live" && pathName !== "/ui/status") {
      failures.push(`open redirect: landed on ${pathName}`);
    }
    await redirectContext.close();
  } catch (error) {
    failures.push(`requested target: ${error.message}`);
  } finally {
    await server.stop();
  }
  return failures;
}

async function walkLastCameraRemoved(browser, binary) {
  const failures = [];
  const server = await startBitternd({
    binary,
    applianceYAML: oneCameraYAML(),
    setupComplete: true,
  });
  try {
    const bootstrap = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const setupPage = await bootstrap.newPage();
    await completeSetup(setupPage, server);
    await bootstrap.close();

    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const page = await context.newPage();
    await page.goto(`${server.baseURL}/ui`, { waitUntil: "load" });
    await signIn(page, server);
    if (new URL(page.url()).pathname !== "/ui/live") {
      failures.push(
        `last camera: first landing was ${new URL(page.url()).pathname}, want /ui/live`,
      );
    }

    await page.goto(`${server.baseURL}/ui/cameras`, { waitUntil: "load" });
    await page.click('a[href="/ui/cameras/front_door/remove"]');
    await Promise.all([
      page.waitForURL("**/ui/cameras**", { timeout: 20000 }),
      page.click('button[type="submit"].destructive'),
    ]);
    // The mock applier marks the operation successful without rewriting the
    // configuration. Write the configuration a real remove would have applied,
    // then visit the root again instead of reloading the live page.
    await server.setApplianceYAML(NO_CAMERA_APPLIANCE_YAML);
    await page.goto(`${server.baseURL}/ui`, { waitUntil: "load" });
    const after = new URL(page.url()).pathname;
    if (after !== "/ui/status") {
      failures.push(`last camera: the next visit landed on ${after}, want /ui/status`);
    }
    const nav = await navState(page);
    if (nav.live) {
      failures.push("last camera: the nav still offers Live");
    }
    await context.close();
  } catch (error) {
    failures.push(`last camera: ${error.message}`);
  } finally {
    await server.stop();
  }
  return failures;
}

const STATES = [
  {
    name: "uninitialized",
    initialized: false,
    cameras: 4,
    expectedPath: "/ui/wizard",
  },
  {
    name: "initialized-no-cameras",
    initialized: true,
    cameras: 0,
    expectedPath: "/ui/status",
  },
  {
    name: "initialized-with-cameras",
    initialized: true,
    cameras: 4,
    expectedPath: "/ui/live",
  },
];

export async function runRoutingChecks({ browser, binary }) {
  const failures = [];
  for (const state of STATES) {
    for (const width of WIDTHS) {
      failures.push(...(await runState(browser, binary, state, width)));
    }
  }
  failures.push(...(await walkRequestedTarget(browser, binary)));
  failures.push(...(await walkLastCameraRemoved(browser, binary)));
  return failures;
}
