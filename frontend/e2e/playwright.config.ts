import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

const root = fileURLToPath(new URL("../..", import.meta.url));
const offset = Number(process.env.TRUNK_PORT_OFFSET ?? 0);
const hostPort = Number(process.env.PLATFORM_TRUNK_HOST_PORT ?? (18521 + offset));
const controlPort = Number(process.env.PLATFORM_TRUNK_CONTROL_PORT ?? (19521 + offset));
const pagePort = Number(process.env.PLATFORM_TRUNK_PAGE_PORT ?? (8497 + offset));
const work = process.env.PLATFORM_TRUNK_WORK ?? `${root}.build/trunk`;
const listOnly = process.argv.includes("--list");
if (!listOnly && (!process.env.TRUNK_DATABASE || !process.env.PLATFORM_TRUNK_WORK)) throw Error("Use make trunk (PostgreSQL required)");
const host = `http://127.0.0.1:${hostPort}`;
const page = `http://localhost:${pagePort}`;

if (process.env.TEST_WORKER_INDEX === undefined && !listOnly) {
  if (!process.env.PLATFORM_TRUNK_FILES) throw Error("Missing isolated trunk file store");
  writeFileSync(`${work}/host.log`, "");
  writeFileSync(
  `${work}/supervisor.mjs`,
  `import { execFileSync, spawn } from "node:child_process";
import { once } from "node:events";
import { closeSync, existsSync, openSync, readFileSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { measure } from ${JSON.stringify(`${root}scripts/timing.mjs`)};

const bin = ${JSON.stringify(process.env.PLATFORM_TRUNK_BIN ?? `${work}/platform-server`)};
const cwd = ${JSON.stringify(`${root}capabilities/server`)};
const tenantsFile = ${JSON.stringify(`${work}/tenants.json`)};
const logFile = ${JSON.stringify(`${work}/host.log`)};
const backupDir = ${JSON.stringify(work)};

let currentDatabase = process.env.TRUNK_DATABASE;
let child = null;
let stopping = false;
let restarting = false;
let queue = Promise.resolve();

function startHost(snapshotEvery) {
  return measure("测试宿主启动与健康检查", () => startHostProcess(snapshotEvery));
}

async function startHostProcess(snapshotEvery) {
  const from = existsSync(logFile) ? statSync(logFile).size : 0;
  const args = [
    "-addr", "127.0.0.1:${hostPort}",
    "-tenants", tenantsFile,
    "-host-admins", "admin,root",
    "-allow-loopback-packages",
    "-database", currentDatabase,
    "-files", process.env.PLATFORM_TRUNK_FILES,
    ...(snapshotEvery === undefined ? [] : ["-snapshot-every", String(snapshotEvery)]),
  ];
  const fd = openSync(logFile, "a");
  const proc = spawn(bin, args, { cwd, stdio: ["ignore", fd, fd] });
  closeSync(fd);
  child = proc;
  proc.on("exit", (code, signal) => {
    if (!stopping && !restarting && child === proc) {
      process.stderr.write(readFileSync(logFile, "utf8").slice(-4000));
      process.exit(code || (signal ? 1 : 0));
    }
  });
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (proc.exitCode !== null || proc.signalCode !== null) {
      throw new Error(readFileSync(logFile, "utf8").slice(from));
    }
    const ok = await fetch("${host}/healthz", { signal: AbortSignal.timeout(2000) }).then(async (r) => { if (!r.ok) return false; const h = await r.json(); if (h.quarantined) throw Error("Quarantined tenants: " + h.quarantined); return true; }, () => false);
    if (ok) return readFileSync(logFile, "utf8").slice(from);
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("host healthz timed out:\\n" + readFileSync(logFile, "utf8").slice(from));
}

async function stopHost() {
  const proc = child;
  if (!proc || proc.exitCode !== null || proc.signalCode !== null) return;
  restarting = true;
  try {
    const exited = once(proc, "exit");
    proc.kill("SIGTERM");
    const [code, signal] = await exited;
    if (code !== 0) {
      throw new Error(\`host exited with code \${code ?? signal}:\\n\${readFileSync(logFile, "utf8").slice(-4000)}\`);
    }
  } finally {
    restarting = false;
  }
}

const control = createServer((req, res) => {
  if (req.method !== "POST" || (req.url !== "/restart" && req.url !== "/restore-backup")) {
    res.writeHead(404).end();
    return;
  }
  let raw = "";
  req.setEncoding("utf8");
  req.on("data", (chunk) => { raw += chunk; });
  req.on("end", () => {
    queue = queue.then(async () => {
      const body = raw ? JSON.parse(raw) : {};
      if (req.url === "/restart") {
        await stopHost();
        const log = await startHost(body.snapshotEvery);
        res.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify({ log }));
        return;
      }
      const restoreDatabase = process.env.TRUNK_RESTORE_DATABASE ?? "";
      if (!currentDatabase || !restoreDatabase) {
        throw new Error("TRUNK_DATABASE and TRUNK_RESTORE_DATABASE are required for backup restore");
      }
      const tenant = String(body.tenant ?? "");
      const backupFile = \`\${backupDir}/\${tenant}.backup.gz\`;
      await stopHost();
      execFileSync(bin, ["-database", currentDatabase, "-tenants", tenantsFile, "-export-tenant", tenant, "-backup", backupFile], { cwd });
      if ((statSync(backupFile).mode & 0o777) !== 0o600) {
        throw new Error("backup file mode is not 0600");
      }
      execFileSync(bin, ["-database", restoreDatabase, "-import-tenant", tenant, "-backup", backupFile], { cwd });
      currentDatabase = restoreDatabase;
      const log = await startHost(10000);
      res.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify({ log }));
    }).catch((err) => {
      res.writeHead(500, { "content-type": "application/json" }).end(JSON.stringify({ error: String(err?.message ?? err) }));
    });
  });
});

for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, () => {
    if (stopping) return;
    stopping = true;
    control.close();
    stopHost().finally(() => process.exit(0));
  });
}

await new Promise((resolve) => control.listen(${controlPort}, "127.0.0.1", resolve));
await startHost();
`,
);
}

const splitOfficial = process.env.TRUNK_SPLIT_OFFICIAL === "1";
const selectedSpecs: string[] = JSON.parse(process.env.PLATFORM_TRUNK_SPECS ?? "[]");
const selectedNonOfficial = selectedSpecs.filter(name => name !== "official").map(name => `**/${name}.spec.ts`);

export default defineConfig({
  testDir: ".",
  outputDir: `${work}/results`,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: true,
  workers: 1,
  reporter: [["list"]],
  ...(process.env.TRUNK_SPECIAL === "performance" ? { grep: /@performance/ } : { grepInvert: /@performance/ }),
  projects: splitOfficial
    ? [
        { name: "trunk", testMatch: selectedNonOfficial, testIgnore: /official\.spec\.ts/ },
        { name: "official", testMatch: /official\.spec\.ts/, dependencies: ["trunk"] },
      ]
    : undefined,
  use: { baseURL: page, locale: "en-US", channel: "chrome", trace: "retain-on-failure", actionTimeout: 10_000 },
  webServer: [
    {
      command: `node "${work}/supervisor.mjs"`,
      cwd: `${root}capabilities/server`,
      url: `${host}/healthz`,
      timeout: 360_000,
      stderr: "pipe",
      reuseExistingServer: false,
      env: {
        GOPROXY: "https://goproxy.cn,direct",
        ...(process.env.TRUNK_DATABASE ? { TRUNK_DATABASE: process.env.TRUNK_DATABASE } : {}),
        ...(process.env.TRUNK_RESTORE_DATABASE ? { TRUNK_RESTORE_DATABASE: process.env.TRUNK_RESTORE_DATABASE } : {}),
      },
    },
    {
      command: `pnpm exec vite --mode trunk --port ${pagePort} --strictPort`,
      cwd: `${root}frontend`,
      url: page,
      timeout: 360_000,
      stderr: "pipe",
      reuseExistingServer: false,
      env: {
        PLATFORM_HOST: host,
      },
    },
  ],
});
