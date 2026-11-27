import { createHash, randomBytes } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { layers, seedPlan, securityAPIPlan } from "./trunk-seeds.mjs";

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
  // The caller awaits done, but the readiness loop can observe an early failure first.
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
  const name = "platform-test-pool-" + hash(imageID).slice(0, 12);
  const inspect = () => {
    try { return JSON.parse(execFileSync("docker", ["inspect", name], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }))[0]; }
    catch { return undefined; }
  };
  if (!inspect()) {
    try {
      docker("run", "-d", "--name", name, "--label", label + "=1", "-e", "POSTGRES_HOST_AUTH_METHOD=trust",
        "-p", "127.0.0.1::5432", imageID);
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
const createDB = (pg, db, template) => docker("exec", pg.name, "createdb", "-U", "postgres", ...(template ? ["-T", template] : []), db);
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
  const deadline = Date.now() + 60_000;
  while (!stopped && Date.now() < deadline) {
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
  const proc = start(bin, hostArgs(work, pg, db, port), { ...process.env, PLATFORM_PACKAGES_DIR: "" }, join(root, "capabilities/server"));
  try {
    await ready(proc, port);
    await checked(process.execPath, [join(root, "scripts/frontend-seed.mjs")], { ...process.env,
      PLATFORM_HOST: "http://127.0.0.1:" + port,
      PLATFORM_SEED_TENANTS: plan.seed.length ? plan.seed.join(",") : "none",
      PLATFORM_ACCEPTANCE_CRM: plan.acceptanceCrm ? "1" : "0" });
    await ready(proc, port);
  } finally { await stop(proc); }
}
async function withCacheLock(fn) {
  mkdirSync(cache, { recursive: true, mode: 0o700 });
  const lock = join(cache, "lock");
  const deadline = Date.now() + 360_000;
  while (true) {
    if (stopped) throw Error("Interrupted");
    try { mkdirSync(lock); break; } catch (err) { if (err.code !== "EEXIST" || Date.now() >= deadline) throw Error("Trunk cache busy; check active runs before removing " + lock); }
    await sleep(250);
  }
  try { return await fn(); } finally { rmSync(lock, { recursive: true, force: true }); }
}
function treeDigest(dir) {
  const entries = readdirSync(dir, { recursive: true, withFileTypes: true }).filter(e => e.isFile())
    .map(e => join(e.parentPath, e.name)).sort();
  return hash(...entries.flatMap(file => [file.slice(dir.length), readFileSync(file)]));
}
async function cloneBaseline(pg, work, bin, db, port, plan) {
  const inputs = readFileSync(join(process.env.PLATFORM_TEST_PACKAGES_DIR, "inputs.json"));
  const seedFiles = ["frontend-seed.mjs", "frontend-seed.json", "frontend-tenants.json", "frontend-security-tenants.json", "demo-roster.mjs", "trunk-seeds.mjs", "trunk-postgres.mjs"];
  const key = hash(pg.id, readFileSync(bin), inputs, JSON.stringify([plan.tenants, plan.seed, plan.acceptanceCrm]),
    new Date().toISOString().slice(0, 10), ...seedFiles.map(name => readFileSync(join(root, "scripts", name)))).slice(0, 28);
  const base = "trunk_base_" + owner + "_" + key;
  const dir = join(cache, key);
  await withCacheLock(async () => {
    const marker = join(dir, "ready.json");
    let valid = false;
    if (existsSync(marker) && existsSync(join(dir, "files")) && existsSync(join(dir, "tenants.json")) && existsDB(pg, base)) {
      const meta = JSON.parse(readFileSync(marker, "utf8"));
      valid = meta.pg === pg.id && meta.files === treeDigest(join(dir, "files")) &&
        sql(pg, "select datallowconn from pg_database where datname='" + base + "'") === "f";
    }
    if (!valid) {
      if (existsDB(pg, base)) dropDB(pg, base);
      rmSync(dir, { recursive: true, force: true });
      mkdirSync(dir, { recursive: true, mode: 0o700 });
      createDB(pg, base);
      try {
        await seed(dir, bin, pg, base, port, plan);
        sql(pg, "alter database " + base + " with allow_connections false");
        writeFileSync(marker, JSON.stringify({ pg: pg.id, files: treeDigest(join(dir, "files")) }), { mode: 0o600 });
        console.log("trunk: created baseline " + key);
      } catch (err) { dropDB(pg, base); rmSync(dir, { recursive: true, force: true }); throw err; }
    } else console.log("trunk: reused baseline " + key);
    createDB(pg, db, base);
    cpSync(join(dir, "files"), join(work, "files"), { recursive: true });
    cpSync(join(dir, "tenants.json"), join(work, "tenants.json"));
  });
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
  await checked(args[0], args.slice(1), process.env, join(root, "frontend"));
  process.exit(0);
}
if (!["--browser", "--host", "--backend", "--security", "--clean"].includes(mode)) throw Error("Specify --browser, --host, --backend, --security or --clean");
const pg = await pool();
if (mode === "--clean") {
  await withCacheLock(async () => {
    const names = sql(pg, "select datname from pg_database where datname like 'trunk_base_" + owner + "_%'").split("\n");
    for (const name of names) if (new RegExp("^trunk_base_" + owner + "_[a-f0-9]{28}$").test(name)) dropDB(pg, name);
    for (const name of readdirSync(cache)) if (/^[a-f0-9]{28}$/.test(name)) rmSync(join(cache, name), { recursive: true, force: true });
  });
} else if (mode === "--backend") {
  const db = "trunk_bench_" + nonce;
  createDB(pg, db);
  try { await checked(process.execPath, [join(root, "scripts/check-invariants.mjs"), args[0] ?? "", "--require-no-skip"], { ...process.env, PLATFORM_BENCH_DATABASE: pg.url(db) }); }
  finally { dropDB(pg, db); }
} else {
  const db = "trunk_run_" + nonce, restore = "trunk_restore_" + nonce;
  mkdirSync(build, { recursive: true });
  const work = mkdtempSync(join(build, "trunk-run-"));
  const bin = join(work, "platform-server");
  let success = false;
  try {
    run(process.env.GO || "go", ["build", "-trimpath", "-o", bin, "./cmd/platform-server"], join(root, "capabilities/server"));
    const [seedPort, hostPort, controlPort, pagePort] = await freePorts(4);
    const plan = mode === "--security" ? securityAPIPlan : seedPlan(args, mode === "--host");
    if (plan.cache && process.env.TRUNK_FRESH !== "1") await cloneBaseline(pg, work, bin, db, seedPort, plan);
    else { createDB(pg, db); await seed(work, bin, pg, db, seedPort, plan); }
    if (mode === "--security") {
      const proc = start(bin, hostArgs(work, pg, db, hostPort), { ...process.env, PLATFORM_PACKAGES_DIR: "" }, join(root, "capabilities/server"));
      try {
        await ready(proc, hostPort);
        await checked(process.execPath, ["--test", join(root, "scripts/pg-security.test.mjs")], { ...process.env, PLATFORM_SECURITY_TEST_HOST: "http://127.0.0.1:" + hostPort });
        const count = Number(sql(pg, "select count(*) from journal", db));
        if (!(count > 0)) throw Error("Security PostgreSQL journal is empty");
        console.log("security: " + count + " journal entries in PostgreSQL");
      } finally { await stop(proc); }
    } else if (mode === "--host") {
      const port = 18511;
      console.log("frontend-host: PostgreSQL at http://127.0.0.1:" + port);
      const proc = start(bin, hostArgs(work, pg, db, port), { ...process.env, PLATFORM_PACKAGES_DIR: "" }, join(root, "capabilities/server"));
      await proc.done;
      if (!stopped && proc.child.exitCode !== 0) throw Error("Frontend host failed");
    } else {
      createDB(pg, restore);
      const env = { ...process.env, PLATFORM_PACKAGES_DIR: "", TRUNK_DATABASE: pg.url(db), TRUNK_RESTORE_DATABASE: pg.url(restore),
        PLATFORM_TRUNK_HOST_PORT: String(hostPort), PLATFORM_TRUNK_CONTROL_PORT: String(controlPort), PLATFORM_TRUNK_PAGE_PORT: String(pagePort),
        PLATFORM_TRUNK_WORK: work, PLATFORM_TRUNK_SPECS: JSON.stringify(plan.specs), PLATFORM_TRUNK_FILES: pathToFileURL(join(work, "files")).href,
        TRUNK_SPLIT_OFFICIAL: plan.specs.length === 0 || (plan.specs.includes("official") && plan.specs.length > 1) ? "1" : "0" };
      await checked(process.execPath, [join(root, "scripts/connector-trunk.mjs"), ...args], env, join(root, "frontend"));
      const count = Number(sql(pg, "select count(*) from journal", db));
      if (!(count > 0)) throw Error("Trunk PostgreSQL journal is empty");
      console.log("trunk: " + count + " journal entries in PostgreSQL");
    }
    success = !stopped || mode === "--host";
  } finally {
    interrupt("SIGTERM");
    if (existsDB(pg, restore)) dropDB(pg, restore);
    if (existsDB(pg, db)) dropDB(pg, db);
    if (success) rmSync(work, { recursive: true, force: true });
    else console.error("trunk evidence retained at " + work);
  }
}
