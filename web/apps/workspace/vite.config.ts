import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The workspace (ADR-0018): one page for every app of a host. The host serves
// the build at "/"; in development Vite proxies the API to the host named by
// PLATFORM_HOST (default: the sales host, 8495).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  server: { port: 5176, strictPort: true, proxy: { "/v1": process.env.PLATFORM_HOST ?? "http://127.0.0.1:8495" } },
  build: { target: "safari17", outDir: "dist" },
});
