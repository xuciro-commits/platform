import { createHash } from "node:crypto";
import { execFile, execFileSync, spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { measure, note } from "./timing.mjs";

const platform = fileURLToPath(new URL("../", import.meta.url));
const lockRaw = readFileSync(new URL("../package-fixtures.lock.json", import.meta.url), "utf8");
const lock = JSON.parse(lockRaw);
const sha = (data) => "sha256:" + createHash("sha256").update(data).digest("hex");

function sanitizedEnv(extra = {}) {
  const env = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (!key.startsWith("GIT_") && value !== undefined) env[key] = value;
  }
  return { ...env, ...extra };
}

const download = promisify(execFile);
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
  const root = process.env.PLATFORM_TEST_PACKAGES_DIR;
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

async function prepare(root) {
  const source = measure("拉取锁定包源码", () => checkout(root));
  const pkg = join(root, "pkg");
  measure("编译包工具", () => command(process.env.GO || "go", ["build", "-o", pkg, "./cmd/pkg"], join(platform, "capabilities/server")));
  const results = await Promise.allSettled(Object.entries(lock.packages).map(async ([name, entry]) => {
    const file = join(root, name + ".json");
    if (entry.url) {
      await measure("下载包 " + name, () => download("curl", ["--fail", "--silent", "--show-error", "--location", "--max-time", "60", "--output", file, entry.url], { cwd: platform, env: sanitizedEnv(), timeout: 60_000 }));
    } else {
      measure("构建包 " + name, () => command(pkg, ["build", "-o", file, join(source, "packages", entry.source)]));
    }
    const raw = readFileSync(file), checksum = sha(raw), descriptor = JSON.parse(raw);
    if (descriptor.id !== entry.id || descriptor.version !== entry.version || (entry.sha256 && entry.sha256 !== checksum)) throw Error("Package identity or digest mismatch: " + name);
    console.error("package input:", name, entry.version, entry.url ? "release" : "remote source " + lock.revision.slice(0, 8));
    return [name, { ...entry, sha256: checksum }];
  }));
  const failed = results.filter(result => result.status === "rejected");
  if (failed.length) throw new AggregateError(failed.map(result => result.reason), "Package input preparation failed");
  const inputs = Object.fromEntries(results.map(result => result.value));
  writeFileSync(join(root, "inputs.json"), JSON.stringify({ revision: lock.revision, packages: inputs }));
}

async function prepareInputs(root) {
  await measure("外部包输入准备", () => prepare(root));
  if (!verifyReady(root)) throw Error("Prepared package inputs do not match the lock");
}

async function runWithInputs(args) {
  const inherited = process.env.PLATFORM_TEST_PACKAGES_DIR;
  if (inherited && !verifyReady(inherited)) throw Error("Inherited package inputs do not match the lock");
  const root = inherited || mkdtempSync(join(tmpdir(), "platform-package-inputs-"));
  try {
    if (!inherited) await prepareInputs(root);
    else note("复用本次验收的包输入");
    const child = spawn(args[0], args.slice(1), { stdio: "inherit", env: { ...process.env, PLATFORM_TEST_PACKAGES_DIR: root } });
    const stop = (signal) => child.kill(signal);
    process.once("SIGINT", () => stop("SIGINT"));
    process.once("SIGTERM", () => stop("SIGTERM"));
    process.exitCode = await new Promise((resolve, reject) => { child.once("error", reject); child.once("exit", (code) => resolve(code ?? 1)); });
  } finally {
    if (!inherited) rmSync(root, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  if (args[0] === "--revision") console.log(lock.revision);
  else if (args[0] === "--check") {
    const root = mkdtempSync(join(tmpdir(), "platform-package-check-"));
    try {
      await prepareInputs(root);
      const source = join(root, "source");
      const pkg = join(root, "pkg");
      const fixtures = ["application.production-provider", "connectortrial-contract", "connectortrial"].map((name) => join(source, "packages", name));
      console.log(measure("验收包契约校验", () => command(pkg, ["check", "-install=" + (args[1] === "--install"), ...fixtures])));
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  } else if (args[0] === "--" && args[1]) await runWithInputs(args.slice(1));
  else throw Error("Use --revision, --check [--install], or -- <command>");
}
