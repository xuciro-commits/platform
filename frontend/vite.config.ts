import stylex from "@stylexjs/unplugin";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const host = loadEnv(mode, ".", "PLATFORM_").PLATFORM_HOST ?? "http://127.0.0.1:18495";
  return {
    plugins: [stylex.vite(), react()],
    clearScreen: false,
    cacheDir: `node_modules/.vite/${mode}`,
    server: {
      port: 8496,
      strictPort: true,
      proxy: Object.fromEntries(["/auth", "/v1", "/healthz"].map((path) => [path, host])),
    },
    build: { target: "safari17", outDir: "dist" },
  };
});
