// Browser tasks against development
// hosts of the hospitality and manufacturing solutions, on development tokens, serving
// the workspace's build. `scripts/verify.sh web` builds the workspace first.
import process from "node:process";
import { defineConfig } from "@playwright/test";

const port = 18496;
const protocolPackages = process.env.PLATFORM_PACKAGES_DIR ? ' -packages "$PLATFORM_PACKAGES_DIR"' : "";
const protocolServices = process.argv.some(argument => argument.includes("heating-application") || argument.includes("protocol-packages")) || !process.argv.some(argument => argument.includes(".spec."));

export default defineConfig({
  testDir: "tests",
  timeout: 30_000,
  workers: 1,
  reporter: [["list"]],
  // On a Mac the installed Google Chrome runs the tests; CI installs Playwright's Chromium.
  use: { baseURL: `http://127.0.0.1:${port}`, locale: "en-US", trace: "retain-on-failure", screenshot: process.env.PLATFORM_SCREENSHOTS ? "on" : "only-on-failure", channel: process.env.CI ? undefined : "chrome" },
  webServer: [{
    command: `go run ./cmd/hospitality-server -addr 127.0.0.1:${port} -web ../../web/apps/workspace/dist${protocolPackages}`,
    cwd: "../../solutions/hospitality",
    url: `http://127.0.0.1:${port}/healthz`,
    timeout: 180_000,
    reuseExistingServer: false,
    env: protocolServices ? {
      PLATFORM_SECRET_HEATING_GATEWAY: process.env.PLATFORM_SECRET_HEATING_GATEWAY ?? "Bearer heating-browser-only",
      PLATFORM_SECRET_HEATING_PROPERTY: process.env.PLATFORM_SECRET_HEATING_PROPERTY ?? "Bearer heating-property-browser-only",
      PLATFORM_SECRET_PROTOCOL_GATEWAY: process.env.PLATFORM_SECRET_PROTOCOL_GATEWAY ?? "Bearer heating-browser-only",
    } : undefined,
  }, {
    command: `go run ./cmd/manufacturing-server -addr 127.0.0.1:18497 -web ../../web/apps/workspace/dist${protocolPackages}`,
    cwd: "../../solutions/manufacturing",
    url: "http://127.0.0.1:18497/healthz",
    timeout: 180_000,
    reuseExistingServer: false,
  }, ...(protocolServices ? [{
    command: "node heating-services.mjs",
    cwd: ".",
    url: "http://127.0.0.1:18511/healthz",
    timeout: 30_000,
    reuseExistingServer: false,
  }] : [])],
});
