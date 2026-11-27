// Start bitternd for the browser check.
//
// The daemon gets a throwaway config, auth state, and state root, so the check
// never reads or writes the appliance paths. The UI mock serves the documented
// status states and discovery samples; the auth store still uses its real
// setup and login flow.

import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";

export const SETUP_TOKEN = "browser-check-setup-token";
export const ADMIN_PASSWORD = "browser-check-password";

const APPLIANCE_YAML = `schema_version: 1

identity:
  hostname: bittern
  timezone: UTC

network:
  lan0:
    mode: dhcp
    subnet: 192.168.1.0/24
    gateway: 192.168.1.1
  cam0:
    mode: static
    address: 10.77.0.1/24
    dhcp:
      pool_start: 10.77.0.50
      pool_end: 10.77.0.199
      reservation_start: 10.77.0.10
      reservation_end: 10.77.0.49
      gateway: 10.77.0.1
      ntp_server: 10.77.0.1

cameras:
  - id: front_door
    name: Front door
    address: 10.77.0.20
    rtsp_port: 554
    rtsp_path: /stream1
  - id: driveway
    name: Driveway
    address: 10.77.0.21
    rtsp_port: 554
    rtsp_path: /stream2
  - id: side_gate
    name: Side gate
    address: 10.77.0.22
    rtsp_port: 554
    rtsp_path: /stream3
  - id: back_garden
    name: Back garden
    address: 10.77.0.23
    rtsp_port: 554
    rtsp_path: /stream4

storage:
  recording:
    device_role: recording
    retention_days: 14
    free_space_reserve_gb: 50

updates:
  channel: stable

privacy:
  tailscale:
    enabled: false
    camera_subnet_route: false
`;

const SECRETS_YAML = `schema_version: 1
identity:
  id: browser-check
setup_token: ${SETUP_TOKEN}
tls: {}
`;

// The live view checks get their own daemon with the same fixed four-camera
// inventory, so the grid checks do not depend on the route walk's auth state.
export const LIVE_APPLIANCE_YAML = APPLIANCE_YAML;
export const CAMERA_APPLIANCE_YAML = APPLIANCE_YAML;
export const NO_CAMERA_APPLIANCE_YAML = APPLIANCE_YAML.replace(
  /cameras:\n(?:  - .*\n(?:    .*\n)*)+/,
  "cameras: []\n",
);

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
  });
}

async function waitForServer(baseURL, child, logs) {
  const deadline = Date.now() + 15_000;
  let lastError = "no response";
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(
        `bitternd exited with code ${child.exitCode} before the UI was ready:\n${logs.join("")}`,
      );
    }
    try {
      const response = await fetch(`${baseURL}/ui/setup`, { redirect: "manual" });
      if (response.status === 200) {
        return;
      }
      lastError = `GET /ui/setup returned ${response.status}`;
    } catch (error) {
      lastError = error.message;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`bitternd did not answer on ${baseURL}: ${lastError}\n${logs.join("")}`);
}

export async function startBitternd({
  binary,
  applianceYAML = APPLIANCE_YAML,
  setupComplete = false,
}) {
  if (!existsSync(binary)) {
    throw new Error(
      `bitternd binary not found at ${binary}; run: make ui-browser-check`,
    );
  }

  const root = await mkdtemp(path.join(os.tmpdir(), "bittern-ui-browser-"));
  const stateRoot = path.join(root, "state");
  const configPath = path.join(root, "appliance.yaml");
  const secretsPath = path.join(root, "secrets.yaml");
  const authPath = path.join(stateRoot, "auth.json");
  await mkdir(stateRoot, { recursive: true });
  await writeFile(configPath, applianceYAML, { mode: 0o644 });
  await writeFile(secretsPath, SECRETS_YAML, { mode: 0o600 });
  if (setupComplete) {
    await writeFile(
      path.join(stateRoot, "setup-complete.json"),
      JSON.stringify({ schema_version: 1, setup_complete: true }) + "\n",
      { mode: 0o600 },
    );
  }

  const apiPort = await freePort();
  const uiPort = await freePort();
  const baseURL = `http://127.0.0.1:${uiPort}`;
  const logs = [];

  const child = spawn(
    binary,
    [
      "--listen",
      `127.0.0.1:${apiPort}`,
      "--ui",
      "--ui-mock",
      "--ui-mock-apply",
      "--ui-insecure-http",
      "--ui-listen",
      `127.0.0.1:${uiPort}`,
      "--config",
      configPath,
      "--state-root",
      stateRoot,
      "--ui-secrets",
      secretsPath,
      "--ui-auth-state",
      authPath,
    ],
    { env: { ...process.env, TZ: "UTC" }, stdio: ["ignore", "pipe", "pipe"] },
  );
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (chunk) => logs.push(chunk));
  child.stderr.on("data", (chunk) => logs.push(chunk));

  try {
    await waitForServer(baseURL, child, logs);
  } catch (error) {
    child.kill("SIGKILL");
    await rm(root, { recursive: true, force: true });
    throw error;
  }

  return {
    baseURL,
    setupToken: SETUP_TOKEN,
    adminPassword: ADMIN_PASSWORD,
    stateRoot,
    configPath,
    async setApplianceYAML(nextYAML) {
      await writeFile(configPath, nextYAML, { mode: 0o644 });
    },
    async stop() {
      if (child.exitCode === null) {
        child.kill("SIGTERM");
        await new Promise((resolve) => {
          const timer = setTimeout(() => {
            child.kill("SIGKILL");
            resolve();
          }, 5_000);
          child.once("exit", () => {
            clearTimeout(timer);
            resolve();
          });
        });
      }
      await rm(root, { recursive: true, force: true });
    },
  };
}
