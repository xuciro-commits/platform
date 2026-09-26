// The routes of docs/Testing.md as browser smoke tests (F-35): a development
// host of the hospitality solution, in memory on development tokens, serving
// the workspace's build. `scripts/verify.sh web` builds the workspace first.
import { defineConfig } from "@playwright/test";

const port = 18496;

export default defineConfig({
  testDir: "tests",
  timeout: 30_000,
  workers: 1,
  reporter: [["list"]],
  // On a Mac the installed Google Chrome runs the tests; CI installs Playwright's Chromium.
  use: { baseURL: `http://127.0.0.1:${port}`, locale: "en-US", trace: "retain-on-failure", channel: process.env.CI ? undefined : "chrome" },
  webServer: {
    command: `go run ./cmd/hospitality-server -addr 127.0.0.1:${port} -web ../../web/apps/workspace/dist`,
    cwd: "../../solutions/hospitality",
    url: `http://127.0.0.1:${port}/healthz`,
    timeout: 180_000,
    reuseExistingServer: false,
  },
});
