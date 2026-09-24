import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Sales workspace (#91): the composed CRM + Hotel software on sales-server (8495).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  server: { port: 5176, strictPort: true },
  build: { target: "safari17", outDir: "dist" },
});
