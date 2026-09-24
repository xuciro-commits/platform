import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Component gallery: every @platform/ui component with cross-industry sample data.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  server: { port: 5174, strictPort: true },
  build: { target: "safari17", outDir: "dist" },
});
