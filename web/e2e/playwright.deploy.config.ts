// Browser route against the disposable OIDC/PostgreSQL rehearsal hosts.
// deploy/local/rehearse.sh starts them and runs this config twice.
import process from "node:process";
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "deploy",
  timeout: 90_000,
  workers: 1,
  outputDir: `test-results/deploy-${process.env.PLATFORM_DEPLOY_PHASE ?? "unset"}`,
  reporter: [["list"]],
  use: { locale: "en-US", trace: "retain-on-failure", screenshot: process.env.PLATFORM_SCREENSHOTS ? "on" : "only-on-failure", channel: process.env.CI ? undefined : "chrome" },
});
