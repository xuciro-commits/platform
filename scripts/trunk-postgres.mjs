import { createHash, randomBytes } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { availableParallelism } from "node:os";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { measure, note } from "./timing.mjs";
import { layers, seedPlan, securityAPIPlan, countOption, specSelectors, playwrightSelectors } from "./trunk-seeds.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const build = join(root, ".build");
const cache = join(build, "trunk-baselines");
const hash = (...values) => {
  const h = createHash("sha256");
  for (const value of values) { h.update(value); h.update("\0"); }
  return h.digest("hex");
};
const owner = hash(root).slice(0, 10);
const nonce = randomBytes(10).toString("hex");
const image = "platform-postgres:18-vector";
const poolTune = "fast-v1";
const label = "io.platform.test-pool";
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const run = (file, args, cwd = root) => execFileSync(file, args, { cwd, encoding: "utf8", timeout: 180_000, stdio: ["ignore", "pipe", "inherit"] }).trim();
const docker = (...args) => run("docker", args);
let stopped = false;
const children = new Set();
function interrupt(signal) {
  stopped = true;
  for (const child of children) {
    try { process.kill(-child.pid, signal); } catch {}
  }
}
process.on("SIGINT", () => interrupt("SIGINT"));
process.on("SIGTERM", () => interrupt("SIGTERM"));
function start(file, args, env, cwd = root) {
  if (stopped) throw Error("Interrupted");
  const child = spawn(file, args, { cwd, env, stdio: "inherit", detached: true });
  children.add(child);
  const done = new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve(code ?? (signal ? 1 : 0)));
  }).finally(() => children.delete(child));
  done.catch(() => {});
  return { child, done };
}
async function stop(proc) {
  if (!proc) return;
  const { child, done } = proc;
  if (child.exitCode === null && child.signalCode === null && child.pid) {
    try { process.kill(-child.pid, "SIGTERM"); } catch {}
    const timeout = setTimeout(() => { try { process.kill(-child.pid, "SIGKILL"); } catch {} }, 15_000);
    try { if (await done !== 0) throw Error("Host did not stop cleanly"); } finally { clearTimeout(timeout); }
  } else if (await done !== 0) throw Error("Host exited unsuccessfully");
}
async function checked(file, args, env, cwd) {
  const code = await start(file, args, env, cwd).done;
  if (code !== 0 || stopped) throw Error(file + " exited " + code);
}
async function freePorts(count) {
  const servers = await Promise.all(Array.from({ length: count }, () => new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve(server));
  })));
  const ports = servers.map(s => s.address().port);
  await Promise.all(servers.map(s => new Promise(resolve => s.close(resolve))));
  return ports;
}

async function pool() {
  const imageID = docker("image", "inspect", "--format", "{{.Id}}", image);
  const name = "platform-test-pool-" + hash(imageID, poolTune).slice(0, 12);
  const inspect = () => {
    try { return JSON.parse(execFileSync("docker", ["inspect", name], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }))[0]; }
    catch { return undefined; }
  };
  if (!inspect()) {
    try {
      docker("run", "-d", "--name", name, "--label", label + "=1", "-e", "POSTGRES_HOST_AUTH_METHOD=trust",
        "-p", "127.0.0.1::5432", imageID,
        "-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off",
        "-c", "shared_buffers=256MB", "-c", "max_connections=200");
    } catch (err) { if (!inspect()) throw err; }
  }
  const info = inspect();
  if (!info || info.Config.Labels?.[label] !== "1" || info.Image !== imageID) {
    throw Error("Refusing to use a container not owned by the test pool");
  }
  if (!info.State.Running) docker("start", name);
  const bindings = inspect()?.NetworkSettings.Ports?.["5432/tcp"];
  if (!bindings?.length || bindings.some(binding => binding.HostIp !== "127.0.0.1")) throw Error("Test pool must bind only to loopback");
  const deadline = Date.now() + 60_000;
  while (!stopped && Date.now() < deadline) {
    try {
      execFileSync("docker", ["exec", name, "pg_isready", "-U", "postgres", "-d", "postgres", "-h", "127.0.0.1"], { stdio: "ignore", timeout: 5000 });
      const port = docker("port", name, "5432/tcp").match(/^127\.0\.0\.1:(\d+)$/)?.[1];
      if (port) return { name, id: info.Id, url: db => "postgres://postgres@127.0.0.1:" + port + "/" + db + "?sslmode=disable" };
    } catch {}
    await sleep(250);
  }
  throw Error("PostgreSQL test pool did not become ready");
}

const sql = (pg, query, db = "postgres") => docker("exec", pg.name, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", db, "-Atc", query);
const createDB = (pg, db, template) => measure(template ? "克隆基准数据库" : "创建独立数据库", () => docker("exec", pg.name, "createdb", "-U", "postgres", ...(template ? ["-T", template] : []), db));
const dropDB = (pg, db) => docker("exec", pg.name, "dropdb", "-U", "postgres", "--if-exists", "--force", db);
const existsDB = (pg, db) => sql(pg, "select count(*) from pg_database where datname='" + db + "'") === "1";

function tenantFile(work, plan) {
  const tenants = ["frontend-tenants.json", "frontend-security-tenants.json"].flatMap(name => JSON.parse(readFileSync(join(root, "scripts", name), "utf8")));
  const selected = tenants.filter(tenant => plan.tenants.includes(tenant.id));
  if (selected.length !== plan.tenants.length || selected.some(tenant => !tenant.name)) throw Error("Missing named tenant template");
  writeFileSync(join(work, "tenants.json"), JSON.stringify(selected.map(tenant => ({ ...tenant, settings: { ...tenant.settings, name: tenant.name } }))));
  console.log("trunk: tenants " + selected.map(t => t.name + " (" + t.id + ")").join(", "));
}
function hostArgs(work, pg, db, port) {
  return ["-addr", "127.0.0.1:" + port, "-database", pg.url(db), "-files", pathToFileURL(join(work, "files")).href,
    "-tenants", join(work, "tenants.json"), "-host-admins", "admin,root", "-allow-loopback-packages"];
}
async function ready(proc, port) {
  const deadline = performance.now() + 60_000;
  while (!stopped && performance.now() < deadline) {
    if (proc.child.exitCode !== null || proc.child.signalCode !== null) throw Error("Seed host exited before readiness");
    const health = await fetch("http://127.0.0.1:" + port + "/healthz", { signal: AbortSignal.timeout(2000) }).then(r => r.ok ? r.json() : undefined, () => undefined);
    if (health?.status === "ok") {
      if (health.quarantined !== 0) throw Error("Quarantined tenants: " + health.quarantined);
      return;
    }
    await sleep(100);
  }
  throw Error("Seed host readiness timed out");
}
async function seed(work, bin, pg, db, port, plan) {
  tenantFile(work, plan);
  mkdirSync(join(work, "files"), { recursive: true, mode: 0o700 });
  if (!plan.seed.length) return;
  const proc = start(bin, hostArgs(work, pg, db, port), { ...process.env, PLATFORM_PACKAGES_DIR: "" }, join(root, "capabilities/server"));
  try {
    await measure("种子宿主启动", () => ready(proc, port));
    await measure("安装包与初始化种子", () => checked(process.execPath, [join(root, "scripts/frontend-seed.mjs")], { ...process.env,
      PLATFORM_HOST: "http://127.0.0.1:" + port,
      PLATFORM_SEED_TENANTS: plan.seed.length ? plan.seed.join(",") : "none",
      PLATFORM_ACCEPTANCE_CRM: plan.acceptanceCrm ? "1" : "0" }));
    await ready(proc, port);
  } finally { await measure("停止种子宿主", () => stop(proc)); }
}
async function withCacheLock(fn) {
  mkdirSync(cache, { recursive: true, mode: 0o700 });
  const lock = join(cache, "lock");
  const deadline = performance.now() + 360_000;
  while (true) {
    if (stopped) throw Error("Interrupted");
    try { mkdirSync(lock); break; } catch (err) {
      if (err.code !== "EEXIST" || performance.now() >= deadline) throw Error("Trunk cache busy; check active runs before removing " + lock);
    }
    await sleep(250);
  }
  try { return await fn(); } finally { rmSync(lock, { recursive: true, force: true }); }
}
async function withBaselineLock(key, fn) {
  const lock = join(cache, "lock-" + key);
  await measure("等待场景基准锁 " + key.slice(0, 8), async () => {
    const deadline = performance.now() + 360_000;
    while (true) {
      if (stopped) throw Error("Interrupted");
      const acquired = await withCacheLock(() => {
        try { mkdirSync(lock); return true; } catch (err) {
          if (err.code !== "EEXIST") throw err;
          return false;
        }
      });
      if (acquired) return;
      if (performance.now() >= deadline) throw Error("Trunk baseline busy; check active runs before removing " + lock);
      await sleep(250);
    }
  });
  try { return await fn(); } finally { rmSync(lock, { recursive: true, force: true }); }
}
function treeDigest(dir) {
  const entries = readdirSync(dir, { recursive: true, withFileTypes: true }).filter(e => e.isFile())
    .map(e => join(e.parentPath, e.name)).sort();
  return hash(...entries.flatMap(file => [file.slice(dir.length), readFileSync(file)]));
}
function buildServerBinary() {
  mkdirSync(join(build, "trunk"), { recursive: true });
  const bin = join(build, "trunk", "platform-server-" + nonce);
  process.once("exit", () => rmSync(bin, { force: true }));
  measure("编译本次宿主（各 spec 共用）", () => run(process.env.GO || "go", ["build", "-trimpath", "-o", bin, "./cmd/platform-server"], join(root, "capabilities/server")));
  return bin;
}
async function cloneBaseline(pg, work, bin, db, port, plan) {
  const inputs = readFileSync(join(process.env.PLATFORM_TEST_PACKAGES_DIR, "inputs.json"));
  const seedFiles = ["frontend-seed.mjs", "frontend-seed.json", "frontend-tenants.json", "frontend-security-tenants.json", "demo-roster.mjs", "trunk-seeds.mjs"];
  const schemaFiles = ["capabilities/server/kernel/store/migration.go", "capabilities/server/kernel/protocol/schema.go"];
  const keyParts = [
    pg.id,
    inputs,
    JSON.stringify([plan.tenants, plan.seed, plan.acceptanceCrm]),
    ...seedFiles.map(name => readFileSync(join(root, "scripts", name))),
    ...schemaFiles.map(rel => readFileSync(join(root, rel))),
  ];
  if (process.env.TRUNK_CACHE_BINARY === "1") keyParts.push(readFileSync(bin));
  const key = hash(...keyParts).slice(0, 28);
  const base = "trunk_base_" + owner + "_" + key;
  const dir = join(cache, key);
  await withBaselineLock(key, async () => {
    const marker = join(dir, "ready.json");
    let valid = false;
    if (existsSync(marker) && existsSync(join(dir, "files")) && existsSync(join(dir, "tenants.json")) && existsDB(pg, base)) {
      const meta = JSON.parse(readFileSync(marker, "utf8"));
      valid = meta.pg === pg.id && meta.files === treeDigest(join(dir, "files")) &&
        sql(pg, "select datallowconn from pg_database where datname='" + base + "'") === "f";
    }
    if (!valid) {
      note("基准缓存未命中 " + key);
      if (existsDB(pg, base)) dropDB(pg, base);
      rmSync(dir, { recursive: true, force: true });
      mkdirSync(dir, { recursive: true, mode: 0o700 });
      createDB(pg, base);
      try {
        await measure("构建初始基准", () => seed(dir, bin, pg, base, port, plan));
        sql(pg, "alter database " + base + " with allow_connections false");
        writeFileSync(marker, JSON.stringify({ pg: pg.id, files: treeDigest(join(dir, "files")) }), { mode: 0o600 });
        console.log("trunk: created baseline " + key);
      } catch (err) { dropDB(pg, base); rmSync(dir, { recursive: true, force: true }); throw err; }
    } else note("基准缓存命中 " + key);
    createDB(pg, db, base);
    measure("克隆文件与租户配置", () => {
      cpSync(join(dir, "files"), join(work, "files"), { recursive: true });
      cpSync(join(dir, "tenants.json"), join(work, "tenants.json"));
    });
  });
}

async function runBrowserSpec(pg, bin, baseArgs, spec, jsonOut) {
  const runNonce = randomBytes(6).toString("hex");
  const db = "trunk_run_" + runNonce;
  const restore = spec === "official" ? "trunk_restore_" + runNonce : "";
  const work = mkdtempSync(join(build, "trunk-run-"));
  const plan = seedPlan([spec + ".spec.ts"], false);
  const started = performance.now();
  let success = false;
  try {
    const [seedPort, hostPort, controlPort, pagePort] = await measure(spec + " · 分配隔离端口", () => freePorts(4));
    await measure(spec + " · 数据库与文件准备", async () => {
      if (plan.cache && process.env.TRUNK_FRESH !== "1") await cloneBaseline(pg, work, bin, db, seedPort, plan);
      else { createDB(pg, db); await seed(work, bin, pg, db, seedPort, plan); }
    });
    if (restore) createDB(pg, restore);
    const specArgs = specSelectors(baseArgs, spec);
    const reporter = join(root, "frontend/e2e/timing-reporter.ts");
    const reporterIndex = specArgs.findIndex(arg => arg === "--reporter" || arg.startsWith("--reporter="));
    if (reporterIndex < 0) specArgs.push("--reporter=list," + reporter);
    else if (specArgs[reporterIndex] === "--reporter") specArgs[reporterIndex + 1] += "," + reporter;
    else specArgs[reporterIndex] += "," + reporter;
    const env = {
      ...process.env,
      PLATFORM_PACKAGES_DIR: "",
      PLATFORM_TRUNK_BIN: bin,
      PLATFORM_TIMING_SCOPE: spec,
      TRUNK_DATABASE: pg.url(db),
      TRUNK_RESTORE_DATABASE: restore ? pg.url(restore) : "",
      PLATFORM_TRUNK_HOST_PORT: String(hostPort),
      PLATFORM_TRUNK_CONTROL_PORT: String(controlPort),
      PLATFORM_TRUNK_PAGE_PORT: String(pagePort),
      PLATFORM_TRUNK_WORK: work,
      PLATFORM_TRUNK_SPECS: JSON.stringify([spec]),
      PLATFORM_TRUNK_FILES: pathToFileURL(join(work, "files")).href,
      TRUNK_SPLIT_OFFICIAL: "0",
      ...(jsonOut ? { PLAYWRIGHT_JSON_OUTPUT_FILE: jsonOut } : {}),
    };
    await measure(spec + " · 浏览器启动与测试", () => checked(process.execPath, [join(root, "scripts/connector-trunk.mjs"), ...specArgs], env, join(root, "frontend")));
    const count = Number(sql(pg, "select count(*) from journal", db));
    if (!(count > 0)) throw Error("Trunk PostgreSQL journal is empty for " + spec);
    console.log(`[trunk] ✓ ${spec} (${((performance.now() - started) / 1000).toFixed(3)}s, ${count} journal)`);
    success = !stopped;
  } finally {
    measure(spec + " · 清理隔离数据库", () => {
      if (restore && existsDB(pg, restore)) dropDB(pg, restore);
      if (existsDB(pg, db)) dropDB(pg, db);
    });
    if (success && process.env.TRUNK_KEEP_EVIDENCE !== "1") rmSync(work, { recursive: true, force: true });
    else console.error("trunk evidence retained at " + work);
  }
}

function mergePlaywrightJSON(files, target) {
  const merged = { config: null, suites: [], errors: [], stats: { startTime: "", duration: 0, expected: 0, skipped: 0, unexpected: 0, flaky: 0 } };
  let first = Infinity, last = -Infinity;
  for (const file of files) {
    if (!existsSync(file)) throw Error("Missing Playwright report: " + file);
    const part = JSON.parse(readFileSync(file, "utf8"));
    if (!part.stats || !Array.isArray(part.suites)) throw Error("Invalid Playwright report: " + file);
    if (!merged.config && part.config) merged.config = part.config;
    merged.suites.push(...part.suites);
    if (Array.isArray(part.errors)) merged.errors.push(...part.errors);
    const started = Date.parse(part.stats.startTime);
    if (!Number.isFinite(started) || !Number.isFinite(part.stats.duration) || part.stats.duration < 0) throw Error("Invalid Playwright report timing: " + file);
    first = Math.min(first, started);
    last = Math.max(last, started + part.stats.duration);
    for (const key of ["expected", "skipped", "unexpected", "flaky"]) {
      const count = part.stats[key];
      if (!Number.isSafeInteger(count) || count < 0) throw Error("Invalid Playwright report count: " + file);
      merged.stats[key] += count;
    }
  }
  if (!files.length) throw Error("No Playwright reports to merge");
  merged.stats.startTime = new Date(first).toISOString();
  merged.stats.duration = last - first;
  writeFileSync(target, JSON.stringify(merged, null, 2));
  for (const file of files) rmSync(file, { force: true });
}

const args = process.argv.slice(2);
const mode = args.shift();
if (mode === "--browser" && args[0] === "--layer") {
  args.shift();
  const layer = args.shift();
  if (!Object.hasOwn(layers, layer)) throw Error("Unknown LAYER: " + layer);
  if (args.some(arg => /\.spec\.ts(?::\d+)*$/.test(arg))) throw Error("Choose SPEC or LAYER, not both");
  args.push(...layers[layer].map(name => name + ".spec.ts"));
}
if (mode === "--browser" && args.includes("--list")) {
  await measure("收集浏览器用例（不启动宿主）", () => checked(args[0], playwrightSelectors(args.slice(1)), process.env, join(root, "frontend")));
  process.exit(0);
}
if (!["--browser", "--host", "--backend", "--security", "--clean"].includes(mode)) throw Error("Specify --browser, --host, --backend, --security or --clean");
const plan = mode === "--browser" ? seedPlan(args, false) : null;
const jobs = plan ? Math.min(countOption("TRUNK_JOBS", Math.min(plan.specs.length, 3), Object.values(layers).flat().length), plan.specs.length) : 0;
const pattern = args[0] ?? "";
const shardCount = mode === "--backend" ? countOption("PLATFORM_PG_SHARDS", pattern ? 1 : Math.min(availableParallelism(), 4), 6) : 0;
const pg = await measure("检查 PostgreSQL 测试池", pool);
if (mode === "--clean") {
  await withCacheLock(async () => {
    const active = readdirSync(cache).filter(name => /^lock-[a-f0-9]{28}$/.test(name));
    if (active.length) throw Error("Trunk baselines are in use; confirm no active runs before cleanup: " + active.join(", "));
    const names = sql(pg, "select datname from pg_database where datname like 'trunk_base_" + owner + "_%'").split("\n");
    for (const name of names) if (new RegExp("^trunk_base_" + owner + "_[a-f0-9]{28}$").test(name)) dropDB(pg, name);
    for (const name of readdirSync(cache)) if (/^[a-f0-9]{28}$/.test(name)) rmSync(join(cache, name), { recursive: true, force: true });
  });
} else if (mode === "--backend") {
  const dbs = Array.from({ length: shardCount }, (_, idx) => `trunk_bench_${nonce}_s${idx}`);
  try {
    for (const db of dbs) createDB(pg, db);
    const urls = dbs.map(db => pg.url(db));
    await checked(process.execPath, [join(root, "scripts/check-invariants.mjs"), pattern, "--require-no-skip"], {
      ...process.env,
      PLATFORM_BENCH_DATABASE: urls[0],
      PLATFORM_BENCH_DATABASES: urls.join(","),
    });
  } finally {
    for (const db of dbs) if (existsDB(pg, db)) dropDB(pg, db);
  }
} else if (mode === "--browser") {
  mkdirSync(build, { recursive: true });
  const bin = buildServerBinary();
  const jsonTarget = process.env.PLAYWRIGHT_JSON_OUTPUT_FILE || "";
  const partFiles = [];
  const queue = [...plan.specs];
  const failures = [];
  if (jsonTarget) rmSync(jsonTarget, { force: true });
  note("调度 " + plan.specs.length + " 个 spec / " + jobs + " 路独立宿主");
  await Promise.all(Array.from({ length: jobs }, async () => {
    while (queue.length > 0 && !stopped) {
      const spec = queue.shift();
      const partOut = jsonTarget ? join(build, "trunk", `part-${spec}-${nonce}.json`) : "";
      if (partOut) partFiles.push(partOut);
      try {
        await runBrowserSpec(pg, bin, args, spec, partOut);
        if (partOut && !existsSync(partOut)) throw Error("Missing Playwright report");
      } catch (err) {
        failures.push(`${spec}: ${err.message}`);
      }
    }
  }));
  if (jsonTarget && partFiles.length === plan.specs.length && partFiles.every(existsSync)) {
    measure("合并浏览器验收报告", () => mergePlaywrightJSON(partFiles, jsonTarget));
  }
  if (stopped || queue.length || failures.length) throw Error("Trunk failures:\n" + failures.join("\n"));
  if (jsonTarget && !existsSync(jsonTarget)) throw Error("Incomplete Playwright reports");
} else {
  const db = "trunk_run_" + nonce;
  mkdirSync(build, { recursive: true });
  const bin = buildServerBinary();
  const work = mkdtempSync(join(build, "trunk-run-"));
  let success = false;
  try {
    const [seedPort, hostPort] = await freePorts(2);
    const plan = mode === "--security" ? securityAPIPlan : seedPlan(args, true);
    if (plan.cache && process.env.TRUNK_FRESH !== "1") await cloneBaseline(pg, work, bin, db, seedPort, plan);
    else { createDB(pg, db); await seed(work, bin, pg, db, seedPort, plan); }
    if (mode === "--security") {
      const proc = start(bin, hostArgs(work, pg, db, hostPort), { ...process.env, PLATFORM_PACKAGES_DIR: "" }, join(root, "capabilities/server"));
      try {
        await measure("安全验收宿主启动", () => ready(proc, hostPort));
        await measure("HTTP 租户隔离验收", () => checked(process.execPath, ["--test", join(root, "scripts/pg-security.test.mjs")], { ...process.env, PLATFORM_SECURITY_TEST_HOST: "http://127.0.0.1:" + hostPort }));
        const count = Number(sql(pg, "select count(*) from journal", db));
        if (!(count > 0)) throw Error("Security PostgreSQL journal is empty");
        console.log("security: " + count + " journal entries in PostgreSQL");
      } finally { await stop(proc); }
    } else {
      const port = 18511;
      console.log("frontend-host: PostgreSQL at http://127.0.0.1:" + port);
      const proc = start(bin, hostArgs(work, pg, db, port), { ...process.env, PLATFORM_PACKAGES_DIR: "" }, join(root, "capabilities/server"));
      try {
        await measure("开发夹具宿主启动", () => ready(proc, port));
        await proc.done;
        if (!stopped && proc.child.exitCode !== 0) throw Error("Frontend host failed");
      } finally {
        if (proc.child.exitCode === null && proc.child.signalCode === null) await stop(proc);
      }
    }
    success = !stopped || mode === "--host";
  } finally {
    interrupt("SIGTERM");
    if (existsDB(pg, db)) dropDB(pg, db);
    if (success && process.env.TRUNK_KEEP_EVIDENCE !== "1") rmSync(work, { recursive: true, force: true });
    else console.error("trunk evidence retained at " + work);
  }
}
