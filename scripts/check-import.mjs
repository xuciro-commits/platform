#!/usr/bin/env node
// CSV 导入压测：对已灌好数据的压测租户，按 PLATFORM_LOAD_IMPORT_ROWS 给出的每个行数
// 各生成一份 CSV，先试算（?preview=true）再导入，记录耗时、每秒行数与逐行结果。
// 由 deploy/dev/sql-load.sh 在重启后的宿主上调用；结果写入 $PLATFORM_LOAD_OUTPUT/import.json。
import http from "node:http";
import https from "node:https";
import os from "node:os";
import { execFileSync } from "node:child_process";
import { createGunzip } from "node:zlib";
import { readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { performance } from "node:perf_hooks";

const base = new URL(process.env.PLATFORM_LOAD_BASE ?? "http://127.0.0.1:20296");
const tenant = process.env.PLATFORM_LOAD_TENANT;
const directory = resolve(process.env.PLATFORM_LOAD_OUTPUT ?? ".build/load/latest");
const sizes = (process.env.PLATFORM_LOAD_IMPORT_ROWS ?? "1000,10000").split(",").map(Number);
const subject = process.env.PLATFORM_LOAD_IMPORT_SUBJECT ?? "loadadmin";
const timeout = Number(process.env.PLATFORM_LOAD_IMPORT_TIMEOUT ?? 3600000);
const skipPreview = process.env.PLATFORM_LOAD_IMPORT_PREVIEW === "0";
const database = process.env.PLATFORM_LOAD_DATABASE ?? "";
if (!tenant || !sizes.length || sizes.some(n => !Number.isInteger(n) || n < 1 || n > 200000)) {
  throw Error("Set PLATFORM_LOAD_TENANT and PLATFORM_LOAD_IMPORT_ROWS (comma-separated, 1–200000)");
}
const transport = base.protocol === "https:" ? https : http;

function call(method, path, body, contentType) {
  const started = performance.now();
  return new Promise((resolvePromise, reject) => {
    const headers = { Authorization: `Bearer ${subject}`, "Platform-Tenant": tenant, "Accept-Language": "en", "Accept-Encoding": "gzip" };
    if (body !== undefined) Object.assign(headers, { "Content-Type": contentType, "Content-Length": Buffer.byteLength(body) });
    const req = transport.request(new URL(path, base), { method, headers, agent: false }, res => {
      const chunks = [], stream = res.headers["content-encoding"] === "gzip" ? res.pipe(createGunzip()) : res;
      stream.on("data", chunk => chunks.push(chunk));
      stream.on("error", reject);
      stream.on("end", () => {
        const elapsedMs = performance.now() - started;
        const raw = Buffer.concat(chunks).toString();
        let value;
        try { value = JSON.parse(raw); } catch { reject(Error(`${path}: HTTP ${res.statusCode}, invalid JSON: ${raw.slice(0, 300)}`)); return; }
        if (res.statusCode < 200 || res.statusCode >= 300 || value?.error) {
          reject(Error(`${path}: HTTP ${res.statusCode}: ${JSON.stringify(value?.error ?? value).slice(0, 400)}`));
          return;
        }
        resolvePromise({ value, elapsedMs, bytes: raw.length });
      });
    });
    req.setTimeout(timeout, () => req.destroy(Error(`request timeout after ${timeout} ms`)));
    req.on("error", reject);
    req.end(body);
  });
}

// 每张表累计的扫描次数与读出行数（pg_stat_user_tables）。导入前后各取一次，差值除以行数，
// 就是每导入一行 PostgreSQL 在这张表上读了多少行；它随表变大而变大，说明有查询没走索引。
// 统计由各连接空闲时上报（至多约 1 秒延迟），所以取之前先等一下。
async function tableReads() {
  if (!database) return null;
  await new Promise(r => setTimeout(r, 1500));
  // 表的键是表名；索引的键是"表名.索引名"（只有扫描次数和读出的索引项数），用来看是哪条索引在读整段。
  const query = "select json_build_object("
    + "'tables', (select coalesce(json_object_agg(relname, json_build_array(seq_scan, seq_tup_read, coalesce(idx_scan,0), coalesce(idx_tup_fetch,0), n_live_tup, analyze_count + autoanalyze_count)), '{}') from pg_stat_user_tables), "
    + "'indexes', (select coalesce(json_object_agg(relname || '.' || indexrelname, json_build_array(0, 0, idx_scan, idx_tup_read, 0, 0)), '{}') from pg_stat_user_indexes))";
  try {
    const raw = execFileSync("docker", ["exec", "platform-postgres-1", "sh", "-ec", 'psql -U "$POSTGRES_USER" -d "$1" -tAc "$2"', "_", database, query], { encoding: "utf8" });
    const { tables, indexes } = JSON.parse(raw.trim() || "{}");
    return { ...tables, ...indexes };
  } catch (err) {
    process.stdout.write(`(无法读取 PostgreSQL 表统计：${String(err.message).split("\n")[0]})\n`);
    return null;
  }
}

function readsPerRow(before, after, rows) {
  if (!before || !after) return null;
  const out = {};
  for (const [table, [seq, seqRows, idx, idxRows, live, analyzed]] of Object.entries(after)) {
    const [seq0, seqRows0, idx0, idxRows0] = before[table] ?? [0, 0, 0, 0];
    const r = n => Math.round((n / rows) * 10) / 10;
    const perRow = { seqScans: r(seq - seq0), seqRowsRead: r(seqRows - seqRows0), indexScans: r(idx - idx0), indexRowsRead: r(idxRows - idxRows0), liveRows: live, analyzedBefore: (before[table] ?? [])[5] ?? 0, analyzedAfter: analyzed };
    if (perRow.seqScans || perRow.seqRowsRead || perRow.indexScans || perRow.indexRowsRead) out[table] = perRow;
  }
  return out;
}

// 每行都带 id：分支 3acb603 之前的导入要求表头里有 id 列；之后的版本两种都接受。
// id 按本次运行与行号生成，目标记录都不存在，所以每行都是新建。
function csvFor(size, tag) {
  const lines = ["id,note,value,group"];
  for (let i = 0; i < size; i++) lines.push(`imp-${tag}-${String(i).padStart(6, "0")},Import ${tag} row ${i},${i},Group ${i % 20}`);
  return lines.join("\n") + "\n";
}

function summarize(rows, elapsedMs, size) {
  const outcomes = {}, actions = {}, samples = {};
  for (const row of rows) {
    outcomes[row.outcome] = (outcomes[row.outcome] ?? 0) + 1;
    actions[row.action ?? ""] = (actions[row.action ?? ""] ?? 0) + 1;
    if (row.outcome !== "ok" && !samples[row.outcome]) samples[row.outcome] = { row: row.row, error: row.error };
  }
  const seconds = elapsedMs / 1000;
  return {
    rows: rows.length, expected: size, elapsedSeconds: Math.round(seconds * 100) / 100,
    rowsPerSecond: Math.round((rows.length / seconds) * 10) / 10,
    msPerRow: Math.round((elapsedMs / Math.max(rows.length, 1)) * 100) / 100,
    outcomes, actions, failureSamples: samples,
  };
}

const fixture = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
const before = (await call("GET", `/v1/records/${fixture.object}?limit=1`)).value.total;
const report = {
  at: new Date().toISOString(), base: base.origin, object: fixture.object, seededRows: fixture.rows, recordsBefore: before, subject,
  hardware: { platform: os.platform(), arch: os.arch(), cpus: os.cpus().length, cpu: os.cpus()[0]?.model, node: process.version },
  boundaries: ["One loopback host and one tenant; client and server share this machine", "One synchronous HTTP request per file, as the list's import dialog sends it", "Every row carries a fresh id, so each row is a create"],
  runs: [],
};
let failed = false;
for (const [index, size] of sizes.entries()) {
  const tag = `${Date.now()}-${index}`;
  const body = csvFor(size, tag);
  const run = { size, csvBytes: Buffer.byteLength(body) };
  const path = `/v1/import/${encodeURIComponent(fixture.object)}`;
  if (!skipPreview) {
    process.stdout.write(`import ${size} rows: preview…\n`);
    const preview = await call("POST", `${path}?preview=true`, body, "text/csv");
    run.preview = summarize(preview.value, preview.elapsedMs, size);
    process.stdout.write(`import ${size} rows: preview ${run.preview.elapsedSeconds}s, ${run.preview.rowsPerSecond} rows/s, ${run.preview.msPerRow} ms/row, outcomes ${JSON.stringify(run.preview.outcomes)}\n`);
  }
  process.stdout.write(`import ${size} rows: apply…\n`);
  const statsBefore = await tableReads();
  const applied = await call("POST", path, body, "text/csv");
  run.apply = summarize(applied.value, applied.elapsedMs, size);
  run.sqlReadsPerRow = readsPerRow(statsBefore, await tableReads(), size);
  const after = (await call("GET", `/v1/records/${fixture.object}?limit=1`)).value.total;
  run.recordsAfter = after;
  process.stdout.write(`import ${size} rows: apply ${run.apply.elapsedSeconds}s, ${run.apply.rowsPerSecond} rows/s, ${run.apply.msPerRow} ms/row, outcomes ${JSON.stringify(run.apply.outcomes)}, records ${after}\n`);
  if (run.sqlReadsPerRow) {
    const entries = Object.entries(run.sqlReadsPerRow);
    const top = entries.filter(([name]) => !name.includes(".")).sort((a, b) => (b[1].seqRowsRead + b[1].indexRowsRead) - (a[1].seqRowsRead + a[1].indexRowsRead)).slice(0, 5);
    for (const [table, v] of top) {
      process.stdout.write(`import ${size} rows: sql ${table} 每行 顺序扫描 ${v.seqScans} 次/读 ${v.seqRowsRead} 行，索引扫描 ${v.indexScans} 次/读 ${v.indexRowsRead} 行（表内 ${v.liveRows} 行，导入前已统计 ${v.analyzedBefore} 次/后 ${v.analyzedAfter} 次）\n`);
    }
    for (const [index, v] of entries.filter(([name]) => name.startsWith("runtime_changes.")).sort((a, b) => b[1].indexRowsRead - a[1].indexRowsRead)) {
      process.stdout.write(`import ${size} rows: sql 索引 ${index} 每行 扫描 ${v.indexScans} 次/读 ${v.indexRowsRead} 项\n`);
    }
  }
  if ((run.apply.outcomes.ok ?? 0) !== size || run.apply.rows !== size) failed = true;
  report.runs.push(run);
  await writeFile(join(directory, "import.json"), JSON.stringify(report, null, 2));
}
if (report.runs.length > 1) {
  const first = report.runs[0].apply, last = report.runs.at(-1).apply;
  report.scaling = { firstSize: report.runs[0].size, lastSize: report.runs.at(-1).size, msPerRowRatio: Math.round((last.msPerRow / first.msPerRow) * 100) / 100 };
  process.stdout.write(`import scaling: ms/row ${first.msPerRow} → ${last.msPerRow} (×${report.scaling.msPerRowRatio})\n`);
  await writeFile(join(directory, "import.json"), JSON.stringify(report, null, 2));
}
if (failed) process.exitCode = 1;
