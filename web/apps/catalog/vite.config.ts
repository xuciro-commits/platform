import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  publicDir: "../../public", // the shared, script-free Dockview popout host
  clearScreen: false,
  server: { port: 5174, strictPort: true },
  build: { target: "safari17", outDir: "dist" },
});
