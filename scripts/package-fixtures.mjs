import { createHash } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const platform = fileURLToPath(new URL("../", import.meta.url));
const lockRaw = readFileSync(new URL("./package-fixtures.lock.json", import.meta.url), "utf8");
const lock = JSON.parse(lockRaw);
const sha = (data) => "sha256:" + createHash("sha256").update(data).digest("hex");
const lockKey = sha(lockRaw).slice(7, 23);
const defaultCache = join(platform, ".build", "package-inputs", lockKey);

function sanitizedEnv(extra = {}) {
  const env = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (!key.startsWith("GIT_") && value !== undefined) env[key] = value;
  }
  return { ...env, ...extra };
}

const command = (file, args, cwd = platform) =>
  execFileSync(file, args, { cwd, env: sanitizedEnv(), encoding: "utf8", stdio: ["ignore", "pipe", "inherit"], timeout: 180_000 }).trim();

function verifyReady(root, selected = Object.keys(lock.packages)) {
  if (!root || !existsSync(join(root, "inputs.json"))) return false;
  let inputs;
  try {
    inputs = JSON.parse(readFileSync(join(root, "inputs.json"), "utf8"));
  } catch {
    return false;
  }
  if (inputs.revision !== lock.revision || !inputs.packages) return false;
  for (const name of selected) {
    const spec = lock.packages[name];
    const entry = inputs.packages[name];
    const file = join(root, name + ".json");
    if (!spec || !entry || !existsSync(file)) return false;
    const digest = sha(readFileSync(file));
    if (digest !== entry.sha256 || (spec.sha256 && spec.sha256 !== digest)) return false;
  }
  return true;
}

export function fixtureRoot() {
  const root = process.env.PLATFORM_TEST_PACKAGES_DIR || (verifyReady(defaultCache) ? defaultCache : "");
  if (!root || !existsSync(join(root, "inputs.json"))) throw Error("Run tests through node scripts/package-fixtures.mjs -- <command>");
  const inputs = JSON.parse(readFileSync(join(root, "inputs.json"), "utf8"));
  if (inputs.revision !== lock.revision) throw Error("Test package inputs differ from the pinned repository revision");
  return root;
}

export function packageInput(name) {
  const root = fixtureRoot();
  const entry = JSON.parse(readFileSync(join(root, "inputs.json"), "utf8")).packages[name];
  if (!entry) throw Error("Unknown package input " + name);
  const file = join(root, name + ".json");
  const bytes = readFileSync(file);
  const spec = lock.packages[name];
  if (!spec || entry.id !== spec.id || entry.version !== spec.version || entry.url !== spec.url || entry.source !== spec.source ||
      sha(bytes) !== entry.sha256 || (spec.sha256 && spec.sha256 !== entry.sha256)) throw Error("Test package identity or digest mismatch: " + name);
  return { ...entry, file, bytes };
}

export function fixtureRelease(name) {
  const entry = packageInput(name);
  return entry.url ? { url: entry.url, expectedSha256: entry.sha256 } : undefined;
}

export function seedPackageRequest(name) {
  const entry = packageInput(name);
  return { contentBase64: entry.bytes.toString("base64"), expectedSha256: entry.sha256 };
}

function checkout(root) {
  const dir = join(root, "source");
  mkdirSync(dir, { recursive: true });
  command("git", ["init", "-q"], dir);
  command("git", ["fetch", "--quiet", "--depth=1", lock.repository, lock.revision], dir);
  command("git", ["checkout", "--quiet", "--detach", "FETCH_HEAD"], dir);
  if (command("git", ["rev-parse", "HEAD"], dir) !== lock.revision) throw Error("Unexpected package repository revision");
  return dir;
}

function prepare(root, selected = Object.keys(lock.packages)) {
  const needSource = selected.some((name) => !lock.packages[name]?.url);
  const source = needSource ? checkout(root) : "";
  const pkg = join(root, "pkg");
  if (needSource) command(process.env.GO || "go", ["build", "-o", pkg, "./cmd/pkg"], join(platform, "capabilities/server"));
  const inputs = existsSync(join(root, "inputs.json")) ? JSON.parse(readFileSync(join(root, "inputs.json"), "utf8")).packages ?? {} : {};
  for (const name of selected) {
    const entry = lock.packages[name];
    if (!entry) throw Error("Unknown package input " + name);
    const file = join(root, name + ".json");
    if (entry.url) {
      command("curl", ["--fail", "--silent", "--show-error", "--location", "--max-time", "60", "--output", file, entry.url]);
    } else {
      command(pkg, ["build", "-o", file, join(source, "packages", entry.source)]);
    }
    const raw = readFileSync(file), checksum = sha(raw), descriptor = JSON.parse(raw);
    if (descriptor.id !== entry.id || descriptor.version !== entry.version || (entry.sha256 && entry.sha256 !== checksum)) throw Error("Package identity or digest mismatch: " + name);
    inputs[name] = { ...entry, sha256: checksum };
    console.error("package input:", name, entry.version, entry.url ? "release" : "remote source " + lock.revision.slice(0, 8));
  }
  writeFileSync(join(root, "inputs.json"), JSON.stringify({ revision: lock.revision, packages: inputs }));
}

function ensureCached(selected = Object.keys(lock.packages)) {
  if (process.env.PLATFORM_TEST_PACKAGES_DIR && verifyReady(process.env.PLATFORM_TEST_PACKAGES_DIR, selected)) {
    return process.env.PLATFORM_TEST_PACKAGES_DIR;
  }
  if (verifyReady(defaultCache, selected)) return defaultCache;
  mkdirSync(dirname(defaultCache), { recursive: true });
  const staging = mkdtempSync(join(tmpdir(), "platform-package-inputs-"));
  try {
    if (existsSync(defaultCache)) cpSync(defaultCache, staging, { recursive: true });
    const missing = selected.filter((name) => !verifyReady(staging, [name]));
    if (missing.length) prepare(staging, missing);
    const swap = defaultCache + ".tmp-" + process.pid;
    rmSync(swap, { recursive: true, force: true });
    cpSync(staging, swap, { recursive: true });
    rmSync(defaultCache, { recursive: true, force: true });
    renameSync(swap, defaultCache);
    return defaultCache;
  } finally {
    rmSync(staging, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  if (args[0] === "--revision") console.log(lock.revision);
  else if (args[0] === "--materialize") {
    console.log(ensureCached(args.length > 1 ? args.slice(1) : undefined));
  } else if (args[0] === "--check") {
    const root = ensureCached();
    let source = join(root, "source", "packages");
    let pkg = join(root, "pkg");
    let tempDir = "";
    try {
      if (!existsSync(source) || !existsSync(pkg)) {
        tempDir = mkdtempSync(join(tmpdir(), "platform-package-check-"));
        source = join(checkout(tempDir), "packages");
        pkg = join(tempDir, "pkg");
        command(process.env.GO || "go", ["build", "-o", pkg, "./cmd/pkg"], join(platform, "capabilities/server"));
      }
      const fixtures = ["application.production-provider", "connectortrial-contract", "connectortrial"].map((name) => join(source, name));
      console.log(command(pkg, ["check", "-install=" + (args[1] === "--install"), ...fixtures]));
    } finally {
      if (tempDir) rmSync(tempDir, { recursive: true, force: true });
    }
  } else if (args[0] === "--") {
    if (!args[1]) throw Error("Missing test command");
    const root = ensureCached();
    const child = spawn(args[1], args.slice(2), { stdio: "inherit", env: { ...process.env, PLATFORM_TEST_PACKAGES_DIR: root } });
    const stop = (signal) => child.kill(signal);
    process.once("SIGINT", () => stop("SIGINT"));
    process.once("SIGTERM", () => stop("SIGTERM"));
    process.exitCode = await new Promise((resolve, reject) => { child.once("error", reject); child.once("exit", (code) => resolve(code ?? 1)); });
  } else console.log(ensureCached());
}
