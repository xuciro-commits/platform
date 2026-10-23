// Test-only external services. The gateway still loads the real Go driver.
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("../..", import.meta.url)));
const packages = process.env.PLATFORM_PACKAGES_DIR;
if (!packages) throw Error("Set PLATFORM_PACKAGES_DIR to the output of make packages.");
const state = mkdtempSync(join(tmpdir(), "platform-protocol-test-"));
const gateway = spawn(join(root, ".build/protocol/bin/gateway" + (process.platform === "win32" ? ".exe" : "")), [
  "-packages", packages, "-state", state, "-listen", "127.0.0.1:18510", "-token-env", "PROTOCOL_GATEWAY_TOKEN",
], { env: { ...process.env, PROTOCOL_GATEWAY_TOKEN: "protocol-browser-only" }, stdio: ["ignore", "ignore", "inherit"] });
const modbus = spawn(join(root, ".build/protocol/bin/driver-modbus" + (process.platform === "win32" ? ".exe" : "")), [
  "-test-listen", "127.0.0.1:18512", "-test-data", join(root, "deploy/examples/hvac/modbus-memory.json"),
], { stdio: ["ignore", "ignore", "inherit"] });
const server = createServer(async (request, response) => {
  response.setHeader("Content-Type", "application/json");
  if (request.url === "/" && (request.method === "GET" || request.method === "HEAD")) {
    response.end('{"status":"ok"}');
    return;
  }
  if (request.url === "/healthz") {
    try {
      await fetch("http://127.0.0.1:18510/", { signal: AbortSignal.timeout(1000) });
      response.end('{"status":"ok"}');
    } catch { response.writeHead(503).end('{"status":"starting"}'); }
    return;
  }
  response.writeHead(404).end('{"error":"not found"}');
});
let stopping = false;
const stop = code => {
  if (stopping) return;
  stopping = true;
  gateway.kill("SIGTERM");
  modbus.kill("SIGTERM");
  server.close();
  process.exitCode = code;
};
for (const child of [gateway, modbus]) {
  child.on("error", error => { console.error(error.message); stop(1); });
  child.on("exit", code => stop(code ?? 1));
}
gateway.on("exit", () => rmSync(state, { recursive: true, force: true }));
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => stop(0));
server.listen(18511, "127.0.0.1");
