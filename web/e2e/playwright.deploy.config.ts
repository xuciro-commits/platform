// Browser route against the disposable OIDC/PostgreSQL rehearsal hosts.
// deploy/local/rehearse.sh starts them; the driver runs this after recovery.
import process from "node:process";
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "deploy",
  timeout: 90_000,
  workers: 1,
  outputDir: "test-results/deploy-recovery",
  reporter: [["list"]],
  use: { locale: "en-US", trace: "retain-on-failure", screenshot: process.env.PLATFORM_SCREENSHOTS ? "on" : "only-on-failure", channel: process.env.CI ? undefined : "chrome" },
});
