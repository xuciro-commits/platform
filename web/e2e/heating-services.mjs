// Test-only external services. The gateway still loads the real Go driver.
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("../..", import.meta.url)));
const packages = process.env.PLATFORM_PACKAGES_DIR;
if (!packages) throw Error("Set PLATFORM_PACKAGES_DIR to the output of make packages.");
const blueprint = JSON.parse(readFileSync(join(root, "deploy/packages/heating.json"), "utf8"));
const policy = blueprint.blueprint.assets.find(asset => asset.ref.kind === "evaluation-set" && asset.ref.name === "heatingadvicequality");
const state = mkdtempSync(join(tmpdir(), "platform-heating-test-"));
const gateway = spawn(join(root, ".build/protocol/bin/gateway" + (process.platform === "win32" ? ".exe" : "")), [
  "-packages", packages, "-state", state, "-listen", "127.0.0.1:18510", "-token-env", "HEATING_GATEWAY_TOKEN",
], { env: { ...process.env, HEATING_GATEWAY_TOKEN: "heating-browser-only" }, stdio: ["ignore", "ignore", "inherit"] });
const modbus = spawn(join(root, ".build/protocol/bin/driver-modbus" + (process.platform === "win32" ? ".exe" : "")), [
  "-test-listen", "127.0.0.1:18512", "-test-data", join(root, "deploy/examples/hvac/modbus-memory.json"),
], { stdio: ["ignore", "ignore", "inherit"] });
const received = [];
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
  if (request.url === "/v1/models") {
    response.end(JSON.stringify({ data: [{ id: "health", name: "Synthetic health advice" }] }));
    return;
  }
  if (request.method === "POST" && request.url === "/v1/chat/completions") {
    request.resume();
    response.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify(policy.body.policy.cases[0].expected) } }], usage: { prompt_tokens: 24, completion_tokens: 16, cost: 0.001 } }));
    return;
  }
  if (request.method === "POST" && request.url === "/alarms") {
    if (request.headers.authorization !== "Bearer heating-property-browser-only") {
      response.writeHead(401).end('{"error":"test credential required"}');
      return;
    }
    let body = "";
    for await (const part of request) body += part;
    received.push({ key: request.headers["idempotency-key"], body: JSON.parse(body) });
    response.end('{"accepted":true}');
    return;
  }
  if (request.url === "/received") { response.end(JSON.stringify(received)); return; }
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
