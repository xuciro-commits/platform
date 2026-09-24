import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Manufacturing execution client (#83): talks to mes-server on 8490.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  server: { port: 5175, strictPort: true },
  build: { target: "safari17", outDir: "dist" },
});
