import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The workspace: one page for every app of a host. The host serves the build
// at "/"; in development Vite proxies the API to the host named by
// PLATFORM_HOST (default: the local host, 8495).
export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  server: { host: "0.0.0.0", port: 5176, strictPort: true, proxy: Object.fromEntries(["/v1", "/healthz", "/mcp", "/a2a"].map((path) => [path, process.env.PLATFORM_HOST ?? "http://127.0.0.1:8495"])) },
  build: { target: "safari17", outDir: "dist" },
});
