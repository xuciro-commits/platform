import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The workspace: one page for every app of a host. The host serves
// the build at "/"; in development Vite proxies the API to the host named by
// PLATFORM_HOST (default: the internal platform host, 18495).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  publicDir: "../../public", // the shared, script-free Dockview popout host
  clearScreen: false,
  server: { port: 8495, strictPort: true, proxy: Object.fromEntries(["/auth", "/v1", "/healthz", "/mcp", "/a2a"].map((path) => [path, process.env.PLATFORM_HOST ?? "http://127.0.0.1:18495"])) },
  build: { target: "safari17", outDir: "dist" },
});
