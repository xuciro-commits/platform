import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Settings (#93): the platform app's workspace, for any platform host.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  server: { port: 5177, strictPort: true },
  build: { target: "safari17", outDir: "dist" },
});
