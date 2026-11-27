import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createServer } from "node:https";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));

async function freePorts(count) {
  const servers = await Promise.all(Array.from({ length: count }, () => new Promise((resolve, reject) => {
    const srv = createNetServer();
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", () => resolve(srv));
  })));
  const ports = servers.map((srv) => srv.address().port);
  await Promise.all(servers.map((srv) => new Promise((resolve) => srv.close(resolve))));
  return ports;
}

const [command, ...args] = process.argv.slice(2);
const baseEnv = { ...process.env };
let ephemeralWorkDir = "";
if (!baseEnv.TRUNK_PORT_OFFSET && !baseEnv.PLATFORM_TRUNK_HOST_PORT) {
  const [hostPort, controlPort, pagePort] = await freePorts(3);
  baseEnv.PLATFORM_TRUNK_HOST_PORT = String(hostPort);
  baseEnv.PLATFORM_TRUNK_CONTROL_PORT = String(controlPort);
  baseEnv.PLATFORM_TRUNK_PAGE_PORT = String(pagePort);
  ephemeralWorkDir = `${root}.build/trunk-${hostPort}`;
}
const cleanEphemeral = (code) => {
  if (code === 0 && ephemeralWorkDir) rmSync(ephemeralWorkDir, { recursive: true, force: true });
};
const selected = args.filter((arg) => /\.spec\.ts(?::\d+)*$/.test(arg));
const named = (spec) => selected.some((arg) => new RegExp(`(?:^|/)${spec}\\.spec\\.ts(?::\\d+)*$`).test(arg));
const runsConnector = !selected.length || named("connector") || (named("official") && selected.some((arg) => !/official\.spec\.ts/.test(arg)));
if (!runsConnector) {
  const child = spawn(command, args, { stdio: "inherit", env: baseEnv });
  child.on("exit", (code, signal) => {
    const exitCode = code ?? (signal ? 1 : 0);
    cleanEphemeral(exitCode);
    process.exitCode = exitCode;
  });
} else {
  const dir = mkdtempSync(join(tmpdir(), "platform-connector-"));
  const cert = join(dir, "cert.pem");
  const key = join(dir, "key.pem");
  const generated = spawnSync("openssl", ["req", "-x509", "-nodes", "-newkey", "rsa:2048", "-keyout", key, "-out", cert, "-days", "1", "-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1"], { stdio: "ignore" });
  if (generated.status !== 0) { rmSync(dir, { recursive: true, force: true }); throw new Error("Cannot generate HTTPS fixture certificate"); }
  const counts = new Map();
  const server = createServer({ key: readFileSync(key), cert: readFileSync(cert) }, async (req, res) => {
    const reply = (status, body) => { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(body)); };
    if (req.method === "GET" && req.url === "/counts") return reply(200, Object.fromEntries(counts));
    if (req.method !== "POST" || req.url !== "/dispatch") return reply(404, {});
    let body = "";
    for await (const chunk of req) { body += chunk; if (body.length > 65536) return reply(413, {}); }
    const id = String(req.headers["idempotency-key"] ?? "");
    const signed = ["POST", req.url, id, createHash("sha256").update(body).digest("hex")].join("\n");
    const expected = Buffer.from("sha256=" + createHmac("sha256", "kit-trunk-signing-key").update(signed).digest("hex"));
    const actual = Buffer.from(String(req.headers["x-platform-signature"] ?? ""));
    const basic = req.headers.authorization === "Basic " + Buffer.from("kit-user:kit-trunk-password").toString("base64");
    if (!basic && !(actual.length === expected.length && timingSafeEqual(actual, expected))) return reply(401, {});
    let input;
    try { input = JSON.parse(body); } catch { return reply(400, {}); }
    if (typeof input.to !== "string" || input.channel !== "fixture" || !id) return reply(422, {});
    const count = (counts.get(input.to) ?? 0) + 1;
    counts.set(input.to, count);
    if (input.to === "timeout" || (input.to === "retry" && count === 1)) return;
    return reply(200, { receipt: "received-" + input.to });
  });
  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolve); });
  const origin = "https://127.0.0.1:" + server.address().port;
  const env = { ...baseEnv, PLATFORM_TRUNK_CONNECTOR_ORIGIN: origin, PLATFORM_TRUNK_CONNECTOR_CERT: cert, PLATFORM_SECRET_KIT_TRUNK_WRONG: "kit-user:kit-wrong-password", PLATFORM_SECRET_KIT_TRUNK_BASIC: "kit-user:kit-trunk-password", PLATFORM_SECRET_KIT_TRUNK_HMAC: "kit-trunk-signing-key" };
  const child = spawn(command, args, { stdio: "inherit", env });
  const stop = (signal) => child.kill(signal);
  process.once("SIGINT", () => stop("SIGINT"));
  process.once("SIGTERM", () => stop("SIGTERM"));
  child.on("error", () => { process.exitCode = 1; server.closeAllConnections(); server.close(); rmSync(dir, { recursive: true, force: true }); });
  child.on("exit", (code) => {
    const exitCode = code ?? 1;
    server.closeAllConnections();
    server.close();
    rmSync(dir, { recursive: true, force: true });
    cleanEphemeral(exitCode);
    process.exitCode = exitCode;
  });
}
