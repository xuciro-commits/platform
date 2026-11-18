import { copyFileSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

const root = fileURLToPath(new URL("../..", import.meta.url));
const work = `${root}.build/trunk`;
const host = "http://127.0.0.1:18521";
const page = "http://localhost:8497";

mkdirSync(work, { recursive: true });
copyFileSync(`${root}scripts/frontend-tenants.json`, `${work}/tenants.json`);

export default defineConfig({
  testDir: ".",
  outputDir: `${work}/results`,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  workers: 1,
  reporter: [["list"]],
  use: { baseURL: page, locale: "en-US", channel: "chrome", trace: "retain-on-failure", actionTimeout: 10_000 },
  webServer: [
    {
      command: `go run ./cmd/platform-server -addr 127.0.0.1:18521 -tenants ${work}/tenants.json`,
      cwd: `${root}capabilities/server`,
      url: `${host}/healthz`,
      timeout: 240_000,
      reuseExistingServer: false,
      env: { GOPROXY: "https://goproxy.cn,direct" },
    },
    {
      command: "node ../scripts/frontend-seed.mjs && pnpm exec vite --mode trunk --port 8497 --strictPort",
      cwd: `${root}frontend`,
      url: page,
      timeout: 180_000,
      reuseExistingServer: false,
      env: { PLATFORM_HOST: host },
    },
  ],
});
