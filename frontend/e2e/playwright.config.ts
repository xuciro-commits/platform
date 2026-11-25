import { copyFileSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

const root = fileURLToPath(new URL("../..", import.meta.url));
const work = `${root}.build/trunk`;
const offset = Number(process.env.TRUNK_PORT_OFFSET ?? 0);
const hostPort = 18521 + offset;
const pagePort = 8497 + offset;
const host = `http://127.0.0.1:${hostPort}`;
const page = `http://localhost:${pagePort}`;

mkdirSync(work, { recursive: true });
copyFileSync(`${root}scripts/frontend-tenants.json`, `${work}/tenants.json`);

export default defineConfig({
  testDir: ".",
  outputDir: `${work}/results`,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: true,
  workers: 3,
  reporter: [["list"]],
  use: { baseURL: page, locale: "en-US", channel: "chrome", trace: "retain-on-failure", actionTimeout: 10_000 },
  webServer: [
    {
      command: `go run ./cmd/platform-server -addr 127.0.0.1:${hostPort} -tenants ${work}/tenants.json -packages ../../deploy/packages -host-admins admin,root -allow-loopback-packages`,
      cwd: `${root}capabilities/server`,
      url: `${host}/healthz`,
      timeout: 240_000,
      reuseExistingServer: false,
      env: { GOPROXY: "https://goproxy.cn,direct" },
    },
    {
      command: `node ../scripts/frontend-seed.mjs && pnpm exec vite --mode trunk --port ${pagePort} --strictPort`,
      cwd: `${root}frontend`,
      url: page,
      timeout: 180_000,
      reuseExistingServer: false,
      env: { PLATFORM_HOST: host },
    },
  ],
});
