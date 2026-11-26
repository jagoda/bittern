// Host-side browser assertion for the QEMU first-boot scenario. It signs in to
// the forwarded appliance UI, asks for the four-tile layout, and waits for both
// applied virtual cameras to reach playing in their own tile. The password
// arrives in the environment so it is not part of the process argument list.

import path from "node:path";

import { chromium } from "@playwright/test";

const [, , baseURL, screenshotPath] = process.argv;
const password = process.env.BITTERN_ADMIN_PASSWORD ?? "";

async function run() {
  if (!baseURL || !screenshotPath || !password) {
    throw new Error("usage: qemu-live.mjs <base-url> <screenshot> with BITTERN_ADMIN_PASSWORD");
  }
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({
      ignoreHTTPSErrors: true,
      viewport: { width: 1280, height: 800 },
    });
    const page = await context.newPage();
    await page.goto(`${baseURL}/ui/login`, { waitUntil: "load", timeout: 30000 });
    await page.fill("#login-username", "admin");
    await page.fill("#login-password", password);
    await Promise.all([
      page.waitForURL("**/ui/status", { timeout: 30000 }),
      page.click("button[type='submit']"),
    ]);
    await page.goto(`${baseURL}/ui/live`, {
      waitUntil: "load",
      timeout: 30000,
    });
    // The browser has its own cookie jar, so it chooses the four-tile preset
    // itself. The page must resolve both cameras into their own tiles.
    const disclosure = page.locator("details.live-layout-disclosure");
    if (!(await disclosure.evaluate((details) => details.open))) {
      await disclosure.locator("summary").click();
    }
    await Promise.all([
      page.waitForNavigation({ waitUntil: "load", timeout: 30000 }),
      page.click('[data-live-preset="4"]'),
    ]);
    await page.waitForFunction(
      () => document.querySelectorAll("[data-live-tile]").length === 4,
      { timeout: 30000 },
    );
    await page.waitForFunction(
      () => {
        const players = Array.from(document.querySelectorAll("[data-live-player]"));
        return (
          players.length === 2 &&
          players.every(
            (player) => player.getAttribute("data-player-state") === "playing",
          )
        );
      },
      { timeout: 90000 },
    );
    await page.waitForFunction(
      () => {
        const players = Array.from(document.querySelectorAll("[data-live-player]"));
        return players.every((player) => {
          const video = player.querySelector("video");
          return (
            video instanceof HTMLVideoElement &&
            video.videoWidth > 0 &&
            video.currentTime > 0
          );
        });
      },
      { timeout: 30000 },
    );
    const evidence = await page.evaluate(() => {
      const body = document.body.innerHTML;
      return {
        tiles: document.querySelectorAll("[data-live-tile]").length,
        players: Array.from(document.querySelectorAll("[data-live-player]")).map(
          (player) => {
            const video = player.querySelector("video");
            return {
              camera: player.getAttribute("data-camera-id"),
              state: player.getAttribute("data-player-state"),
              profile: player.getAttribute("data-profile"),
              videoWidth: video instanceof HTMLVideoElement ? video.videoWidth : 0,
              videoHeight: video instanceof HTMLVideoElement ? video.videoHeight : 0,
              currentTime: video instanceof HTMLVideoElement ? video.currentTime : 0,
            };
          },
        ),
        leaked:
          body.includes("rtsp://") ||
          body.includes("camera_1_main") ||
          body.includes("camera_2_sub") ||
          body.includes("127.0.0.1:1984"),
      };
    });
    await page.screenshot({ path: path.resolve(screenshotPath), fullPage: true });
    console.log(JSON.stringify(evidence));
    if (
      evidence.tiles !== 4 ||
      evidence.players.length !== 2 ||
      evidence.leaked ||
      evidence.players.some(
        (player) => player.state !== "playing" || player.videoWidth <= 0,
      )
    ) {
      process.exitCode = 1;
    }
  } finally {
    await browser.close();
  }
}

run().catch((error) => {
  console.error(error.stack || error.message);
  process.exitCode = 1;
});
