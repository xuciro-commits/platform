import { copyFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { defineConfig } from "@playwright/test";

const port = 18496;
const tenants = join(mkdtempSync(join(tmpdir(), "platform-acceptance-")), "tenants.json");
copyFileSync(new URL("./tenants.json", import.meta.url), tenants);

export default defineConfig({
  testDir: ".",
  testMatch: /F\d+-.*\.spec\.ts$/,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  workers: 1,
  reporter: [["list"]],
  use: { baseURL: `http://127.0.0.1:${port}`, locale: "en-US", trace: "retain-on-failure", screenshot: "only-on-failure", channel: process.env.CI ? undefined : "chrome" },
  webServer: {
    command: `go run ./cmd/platform-server -addr 127.0.0.1:${port} -tenants ${tenants} -web ../../web/apps/workspace/dist`,
    cwd: "../../capabilities/server",
    url: `http://127.0.0.1:${port}/healthz`,
    timeout: 240_000,
    reuseExistingServer: false,
    env: { GOPROXY: "https://goproxy.cn,direct" },
  },
});
