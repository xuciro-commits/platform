import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The offline Catalog uses the same view that the authenticated workspace hosts.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  server: { port: 5174, strictPort: true },
  build: { target: "safari17", outDir: "dist" },
});
