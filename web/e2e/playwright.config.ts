// Browser tasks against development
// instances of the common platform, on development tokens, serving
// the workspace's build. `scripts/verify.sh web` builds the workspace first.
import process from "node:process";
import { defineConfig } from "@playwright/test";

const port = 18496;
const declared = process.argv.some(argument => argument.includes("declared-"));
const manufacturingDeclared=process.argv.some(argument=>argument.includes("declared-manufacturing"));
const declaredTenants=manufacturingDeclared?"tenants-manufacturing-declared.json":declared?"tenants-declared.json":"tenants.json";
const protocolPackages = process.env.PLATFORM_PACKAGES_DIR ? ' -packages "$PLATFORM_PACKAGES_DIR"' : "";
const protocolServices = process.argv.some(argument => argument.includes("heating-application") || argument.includes("protocol-packages")) || !process.argv.some(argument => argument.includes(".spec."));

export default defineConfig({
  testDir: "tests",
  ...(!declared ? { testIgnore: "**/declared-*.spec.ts" } : {}),
  timeout: 30_000,
  workers: 1,
  reporter: [["list"]],
  // On a Mac the installed Google Chrome runs the tests; CI installs Playwright's Chromium.
  use: { baseURL: `http://127.0.0.1:${port}`, locale: "en-US", trace: "retain-on-failure", screenshot: process.env.PLATFORM_SCREENSHOTS ? "on" : "only-on-failure", channel: process.env.CI ? undefined : "chrome" },
  webServer: [{
    command: `go run ./cmd/platform-server -addr 127.0.0.1:${port} -tenants ../../web/e2e/${declaredTenants} -web ../../web/apps/workspace/dist${protocolPackages}`,
    cwd: "../../capabilities/server",
    url: `http://127.0.0.1:${port}/healthz`,
    timeout: 180_000,
    reuseExistingServer: false,
    env: {
      ...(protocolServices ? {
      PLATFORM_SECRET_HEATING_GATEWAY: process.env.PLATFORM_SECRET_HEATING_GATEWAY ?? "Bearer heating-browser-only",
      PLATFORM_SECRET_HEATING_PROPERTY: process.env.PLATFORM_SECRET_HEATING_PROPERTY ?? "Bearer heating-property-browser-only",
      PLATFORM_SECRET_PROTOCOL_GATEWAY: process.env.PLATFORM_SECRET_PROTOCOL_GATEWAY ?? "Bearer heating-browser-only",
      } : {}),
      ...(declared ? {PLATFORM_SECRET_LODGING_UPSTREAM: "Bearer lodging-service",PLATFORM_SECRET_LODGING_UPSTREAM_BOUTIQUE:"Bearer boutique-channel"} : {}),
      ...(manufacturingDeclared ? {PLATFORM_SECRET_PRODUCTION_UPSTREAM: "Bearer production-browser-only"} : {}),
    },
  }, ...(!declared ? [{
    command: `go run ./cmd/platform-server -tenants ../../web/e2e/tenants-secondary.json -addr 127.0.0.1:18497 -web ../../web/apps/workspace/dist${protocolPackages}`,
    cwd: "../../capabilities/server",
    url: "http://127.0.0.1:18497/healthz",
    timeout: 180_000,
    reuseExistingServer: false,
  }] : []), ...(protocolServices ? [{
    command: "node heating-services.mjs",
    cwd: ".",
    url: "http://127.0.0.1:18511/healthz",
    timeout: 30_000,
    reuseExistingServer: false,
  }] : [])],
});
