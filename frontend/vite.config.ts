import stylex from "@stylexjs/unplugin";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv, normalizePath, type Plugin } from "vite";
import { begin } from "../scripts/timing.mjs";

function acyclicSource(): Plugin {
  let sourceRoot = "";
  return {
    name: "platform-acyclic-source",
    apply: "build",
    configResolved(config) {
      sourceRoot = normalizePath(config.root).replace(/\/$/, "") + "/src/";
    },
    generateBundle() {
      const local = (id: string) => id.startsWith(sourceRoot) && !id.includes("?");
      const modules = [...this.getModuleIds()].filter(local).sort();
      const visited = new Set<string>();
      const active = new Set<string>();
      const stack: string[] = [];
      const visit = (id: string) => {
        if (active.has(id)) {
          const cycle = [...stack.slice(stack.indexOf(id)), id].map((path) => path.slice(sourceRoot.length));
          this.error("Source dependency cycle: " + cycle.join(" -> "));
        }
        if (visited.has(id)) return;
        active.add(id);
        stack.push(id);
        const info = this.getModuleInfo(id);
        const dependencies = [...(info?.importedIds ?? []), ...(info?.dynamicallyImportedIds ?? [])].filter(local).sort();
        for (const dependency of dependencies) visit(dependency);
        stack.pop();
        active.delete(id);
        visited.add(id);
      };
      for (const id of modules) visit(id);
      console.log("architecture: " + modules.length + " source modules, no dependency cycles");
    },
  };
}

export default defineConfig(({ mode }) => {
  const host = loadEnv(mode, ".", "PLATFORM_").PLATFORM_HOST ?? "http://127.0.0.1:18495";
  const started = mode === "trunk" ? begin("Vite 前端启动") : undefined;
  const startup: Plugin = {
    name: "platform-trunk-startup",
    apply: "serve",
    configureServer(server) {
      server.httpServer?.once("listening", () => started?.());
    },
  };
  return {
    plugins: [stylex.vite(), react(), acyclicSource(), ...(started ? [startup] : [])],
    clearScreen: false,
    cacheDir: `node_modules/.vite/${mode}`,
    server: {
      port: 8496,
      strictPort: true,
      proxy: Object.fromEntries(["/auth", "/v1", "/healthz", "/mcp", "/autoloop"].map((path) => [path, host])),
    },
    build: { target: "safari17", outDir: "dist" },
  };
});
