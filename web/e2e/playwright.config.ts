// The routes of docs/Testing.md as browser smoke tests (F-35): a development
// hosts of the hospitality and manufacturing solutions, on development tokens, serving
// the workspace's build. `scripts/verify.sh web` builds the workspace first.
import process from "node:process";
import { defineConfig } from "@playwright/test";

const port = 18496;

export default defineConfig({
  testDir: "tests",
  timeout: 30_000,
  workers: 1,
  reporter: [["list"]],
  // On a Mac the installed Google Chrome runs the tests; CI installs Playwright's Chromium.
  use: { baseURL: `http://127.0.0.1:${port}`, locale: "en-US", trace: "retain-on-failure", screenshot: process.env.PLATFORM_SCREENSHOTS ? "on" : "only-on-failure", channel: process.env.CI ? undefined : "chrome" },
  webServer: [{
    command: `go run ./cmd/hospitality-server -addr 127.0.0.1:${port} -web ../../web/apps/workspace/dist`,
    cwd: "../../solutions/hospitality",
    url: `http://127.0.0.1:${port}/healthz`,
    timeout: 180_000,
    reuseExistingServer: false,
  }, {
    command: `go run ./cmd/manufacturing-server -addr 127.0.0.1:18497 -web ../../web/apps/workspace/dist`,
    cwd: "../../solutions/manufacturing",
    url: "http://127.0.0.1:18497/healthz",
    timeout: 180_000,
    reuseExistingServer: false,
  }],
});
