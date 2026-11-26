// Host-side browser assertion for the QEMU first-boot scenario. It signs in to
// the forwarded appliance UI, opens the applied virtual camera and waits for
// the real MSE player to reach playing. The password arrives in the
// environment so it is not part of the process argument list.

import path from "node:path";

import { chromium } from "@playwright/test";

const [, , baseURL, screenshotPath] = process.argv;
const password = process.env.BITTERN_ADMIN_PASSWORD ?? "";
const camera = "camera_1";

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
    await page.goto(`${baseURL}/ui/live/${camera}`, {
      waitUntil: "load",
      timeout: 30000,
    });
    await page.waitForFunction(
      () =>
        document
          .querySelector("[data-live-player]")
          ?.getAttribute("data-player-state") === "playing",
      { timeout: 60000 },
    );
    await page.waitForFunction(
      () => {
        const video = document.querySelector("#live-video");
        return (
          video instanceof HTMLVideoElement &&
          video.videoWidth > 0 &&
          video.currentTime > 0
        );
      },
      { timeout: 30000 },
    );
    const evidence = await page.evaluate(() => {
      const player = document.querySelector("[data-live-player]");
      const video = document.querySelector("#live-video");
      const body = document.body.innerHTML;
      return {
        state: player ? player.getAttribute("data-player-state") : null,
        videoWidth: video instanceof HTMLVideoElement ? video.videoWidth : 0,
        videoHeight: video instanceof HTMLVideoElement ? video.videoHeight : 0,
        currentTime: video instanceof HTMLVideoElement ? video.currentTime : 0,
        profile: player ? player.getAttribute("data-profile") : null,
        leaked:
          body.includes("rtsp://") ||
          body.includes("camera_1_main") ||
          body.includes("127.0.0.1:1984"),
      };
    });
    await page.screenshot({ path: path.resolve(screenshotPath), fullPage: true });
    console.log(JSON.stringify(evidence));
    if (evidence.state !== "playing" || evidence.videoWidth <= 0 || evidence.leaked) {
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
