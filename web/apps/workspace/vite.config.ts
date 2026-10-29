import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  publicDir: "../../public",
  clearScreen: false,
  server: { port: 8495, strictPort: true, proxy: Object.fromEntries(["/auth", "/v1", "/healthz", "/mcp", "/a2a"].map((path) => [path, process.env.PLATFORM_HOST ?? "http://127.0.0.1:18495"])) },
  build: { target: "safari17", outDir: "dist" },
});
