import { createHash } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { measure, note } from "./timing.mjs";

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
    if (!spec || !entry || entry.id !== spec.id || entry.version !== spec.version || entry.url !== spec.url || entry.source !== spec.source || !existsSync(file)) return false;
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
  const source = needSource ? measure("拉取锁定包源码", () => checkout(root)) : "";
  const pkg = join(root, "pkg");
  if (needSource) measure("编译包工具", () => command(process.env.GO || "go", ["build", "-o", pkg, "./cmd/pkg"], join(platform, "capabilities/server")));
  const inputs = existsSync(join(root, "inputs.json")) ? JSON.parse(readFileSync(join(root, "inputs.json"), "utf8")).packages ?? {} : {};
  for (const name of selected) {
    const entry = lock.packages[name];
    if (!entry) throw Error("Unknown package input " + name);
    const file = join(root, name + ".json");
    if (entry.url) {
      measure("下载包 " + name, () => command("curl", ["--fail", "--silent", "--show-error", "--location", "--max-time", "60", "--output", file, entry.url]));
    } else {
      measure("构建包 " + name, () => command(pkg, ["build", "-o", file, join(source, "packages", entry.source)]));
    }
    const raw = readFileSync(file), checksum = sha(raw), descriptor = JSON.parse(raw);
    if (descriptor.id !== entry.id || descriptor.version !== entry.version || (entry.sha256 && entry.sha256 !== checksum)) throw Error("Package identity or digest mismatch: " + name);
    inputs[name] = { ...entry, sha256: checksum };
    console.error("package input:", name, entry.version, entry.url ? "release" : "remote source " + lock.revision.slice(0, 8));
  }
  writeFileSync(join(root, "inputs.json"), JSON.stringify({ revision: lock.revision, packages: inputs }));
}

function ensureCached(selected = Object.keys(lock.packages)) {
  return measure("外部包输入准备", () => ensureCachedInputs(selected));
}

function ensureCachedInputs(selected) {
  for (const name of selected) if (!Object.hasOwn(lock.packages, name)) throw Error("Unknown package input " + name);
  if (process.env.PLATFORM_TEST_PACKAGES_DIR && verifyReady(process.env.PLATFORM_TEST_PACKAGES_DIR, selected)) {
    note("包输入缓存命中（指定目录）");
    return process.env.PLATFORM_TEST_PACKAGES_DIR;
  }
  if (verifyReady(defaultCache, selected)) { note("包输入缓存命中"); return defaultCache; }
  mkdirSync(dirname(defaultCache), { recursive: true });
  const cacheLock = defaultCache + ".lock";
  measure("等待包缓存写锁", () => {
    const deadline = performance.now() + 180_000;
    while (true) {
      try { mkdirSync(cacheLock); return; } catch (err) {
        if (err.code !== "EEXIST" || performance.now() >= deadline) throw Error("Package cache busy: " + cacheLock, { cause: err });
      }
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 250);
    }
  });
  let staging;
  try {
    if (verifyReady(defaultCache, selected)) { note("包输入缓存命中（等待后）"); return defaultCache; }
    staging = mkdtempSync(join(dirname(defaultCache), ".prepare-"));
    const missing = selected.filter((name) => !verifyReady(defaultCache, [name]));
    note("包输入缓存未命中：" + missing.join(", "));
    prepare(staging, missing);
    if (!existsSync(defaultCache)) {
      renameSync(staging, defaultCache);
    } else {
      const fresh = JSON.parse(readFileSync(join(staging, "inputs.json"), "utf8"));
      const previous = existsSync(join(defaultCache, "inputs.json")) ? (() => {
        try { return JSON.parse(readFileSync(join(defaultCache, "inputs.json"), "utf8")); } catch { return {}; }
      })() : {};
      for (const name of missing) renameSync(join(staging, name + ".json"), join(defaultCache, name + ".json"));
      if (existsSync(join(staging, "source")) && !existsSync(join(defaultCache, "source"))) renameSync(join(staging, "source"), join(defaultCache, "source"));
      fresh.packages = { ...(previous.revision === lock.revision ? previous.packages : {}), ...fresh.packages };
      writeFileSync(join(staging, "inputs.json"), JSON.stringify(fresh));
      renameSync(join(staging, "inputs.json"), join(defaultCache, "inputs.json"));
    }
    return defaultCache;
  } finally {
    if (staging) rmSync(staging, { recursive: true, force: true });
    rmSync(cacheLock, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  if (args[0] === "--revision") console.log(lock.revision);
  else if (args[0] === "--materialize") {
    console.log(ensureCached(args.length > 1 ? args.slice(1) : undefined));
  } else if (args[0] === "--check") {
    const root = ensureCached();
    const tempDir = mkdtempSync(join(tmpdir(), "platform-package-check-"));
    try {
      let source = join(root, "source");
      if (!existsSync(source) || command("git", ["rev-parse", "HEAD"], source) !== lock.revision) {
        source = measure("拉取验收包源码", () => checkout(tempDir));
      }
      const pkg = join(tempDir, "pkg");
      measure("编译当前候选包工具", () => command(process.env.GO || "go", ["build", "-o", pkg, "./cmd/pkg"], join(platform, "capabilities/server")));
      const fixtures = ["application.production-provider", "connectortrial-contract", "connectortrial"].map((name) => join(source, "packages", name));
      console.log(measure("验收包契约校验", () => command(pkg, ["check", "-install=" + (args[1] === "--install"), ...fixtures])));
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
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
