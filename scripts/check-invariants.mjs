#!/usr/bin/env node
import { execFile, execFileSync } from "node:child_process";
import { appendFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { availableParallelism } from "node:os";
import { fileURLToPath } from "node:url";
import { begin, measure, note } from "./timing.mjs";
import { testPurpose } from "./test-purposes.mjs";
import { createTestProgress } from "./test-progress.mjs";
import { classifySpeeds, extractTimings, filterSmallTests, selectTests, shardTests, summarizeGo, testGate } from "./test-results.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const server = root + "capabilities/server";
const pkg = server + "/host";

const timings = root + ".build/check/invariant-timings.json";
const latestEvidence = root + ".build/check/test-results.json";
const started = performance.now();
const go = process.env.GO || "go";
const rawArgs = process.argv.slice(2);
const flags = new Set(rawArgs.filter(arg => arg.startsWith("--")));
const pattern = rawArgs.find(arg => !arg.startsWith("--")) || "";
const strict = flags.has("--require-no-skip");
const smallOnly = flags.has("--tier=small");
const benchDatabases = (process.env.PLATFORM_BENCH_DATABASES || "").split(",").map(s => s.trim()).filter(Boolean);
const workers = benchDatabases.length ? benchDatabases.length : (process.env.PLATFORM_BENCH_DATABASE ? 1 : Math.min(availableParallelism(), 6));

mkdirSync(root + ".build/check/runs", { recursive: true });
const runDir = mkdtempSync(root + ".build/check/runs/" + new Date().toISOString().replace(/[:.]/g, "-") + "-");
const binary = runDir + "/host.test";
process.once("exit", () => rmSync(binary, { force: true }));
const evidence = runDir + "/test-results.json";
const saveEvidence = report => {
  const json = JSON.stringify({ ...report, runDir }, null, 2);
  writeFileSync(evidence, json);
  writeFileSync(latestEvidence, json);
};
const emit = message => process.env.PLATFORM_TIMING_FILE ? note(message) : console.error(new Date().toISOString() + " " + message);
saveEvidence({ status: "incomplete" });
emit("验收日志：" + runDir);
emit("模式：" + (strict ? "PostgreSQL 严格验收（禁止跳过）" : smallOnly ? "Small：宿主轻量用例 + 非宿主测试包（未配置 PG 时有明确跳过）" : "宿主不变量验收（未配置 PG 时有明确跳过）"));

measure("编译后端测试二进制", () => execFileSync(go, ["test", "-c", "-o", binary, "."], { cwd: pkg, stdio: "inherit" }));
const rawKnown = existsSync(timings) ? JSON.parse(readFileSync(timings, "utf8")) : {};
const listed = measure("收集后端不变量", () => execFileSync(binary, ["-test.list", "^Test"], { cwd: pkg, encoding: "utf8" }));
const existing = new Set(selectTests(listed, ""));
const known = Object.fromEntries(Object.entries(rawKnown).filter(([name]) => existing.has(name)));
const matched = selectTests(listed, pattern);
const tests = smallOnly ? filterSmallTests(matched, known) : matched;
for (const name of existing) testPurpose(name);
const selected = new Set(tests);
const excluded = matched.filter(name => !selected.has(name)).map(name => ({
  name, purpose: testPurpose(name),
  reason: known[name] !== undefined ? "历史耗时 " + (known[name] / 1000).toFixed(3) + "s 超过 Small 1s 预算" : "无历史耗时且属预设重型场景",
}));
for (const item of excluded) emit("⊘ [未选入] " + item.purpose + " (" + item.name + ") · " + item.reason);
emit("宿主共 " + existing.size + " 项；匹配 " + matched.length + "；本次选入 " + tests.length + "；Small 排除 " + excluded.length + "；RUN 未匹配 " + (existing.size - matched.length));

if (!tests.length) {
  saveEvidence({ status: "failed", pattern, tier: smallOnly ? "small" : "all", reason: "zero matches", excluded, totals: { passed: 0, partial: 0, skipped: 0, failed: 0, notRun: 0 } });
  console.error(`tests: no host tests match ${pattern || "--tier=small"}`);
  process.exit(1);
}
const reports = [];
const measured = {};
const run = (name, args, cwd, expected = [], collectTimings = false, env = process.env) => new Promise(resolve => {
  const end = begin(name + " · 执行 " + (expected.length || "子包") + " 项");
  const log = runDir + "/" + name.replace(/[^a-zA-Z0-9]+/g, "-") + ".jsonl";
  const errLog = log + ".stderr";
  const progress = createTestProgress(name, emit, expected);
  let pending = "", progressError;
  function consume(line) {
    if (!line.trim()) return;
    try { progress.accept(JSON.parse(line)); }
    catch (error) { progressError ??= error; emit("✗ [" + name + "] 报告事件异常：" + error.message); }
  }
  const child = execFile(go, args, { cwd, env, maxBuffer: 64 << 20 }, (error, stdout, stderr) => {
    if (pending.trim()) consume(pending);
    let result;
    try {
      result = summarizeGo(stdout, expected);
      if (collectTimings) Object.assign(measured, extractTimings(stdout));
    } catch (parseError) {
      emit("✗ [" + name + "] 无法解析 Go 事件：" + parseError.message);
      result = { passed: 0, partial: 0, skipped: 0, failed: 0, notRun: expected.length, packageFailures: 1, leaves: {}, skippedTests: [] };
    }
    const details = progress.details();
    const actual = new Set(details.map(row => row.name));
    for (const test of expected.filter(test => !actual.has(test))) emit("✗ [未执行] " + testPurpose(test) + " (" + test + ")");
    for (const row of details.filter(row => row.status === "not-run")) emit("✗ [未完成] " + row.purpose + " (" + row.name + ")");
    const unexplained = details.filter(row => row.status === "skip" && !row.reasonProvided);
    for (const row of unexplained) emit("✗ 跳过缺少原因：" + row.name);
    const ok = !error && !progressError && !unexplained.length && testGate(result, { requireNoSkip: strict, allowSkippedReport: true });
    if (!ok) {
      emit("✗ [" + name + "] 失败证据：" + log);
      if (stderr.trim()) emit(stderr.trim().slice(0, 4000));
      if (strict && result.skippedTests.length) emit("严格 PG 验收不允许跳过：" + result.skippedTests.join(", "));
    }
    reports.push({ name, tests: expected, ok, exitCode: error?.code ?? 0, log, errLog, details, ...result });
    saveEvidence({ status: "incomplete", pattern, strict, tier: smallOnly ? "small" : "all", selectedHostTests: tests, excluded, reports });
    end(ok);
    resolve();
  });
  child.stdout.on("data", chunk => {
    appendFileSync(log, chunk);
    pending += chunk;
    const lines = pending.split("\n");
    pending = lines.pop() ?? "";
    for (const line of lines) consume(line);
  });
  child.stderr.on("data", chunk => appendFileSync(errLog, chunk));
});
const shards = shardTests(tests, known, workers);
emit("选择 " + tests.length + " 项不变量 / " + shards.length + " 个分片");
await Promise.all(shards.map((batch, idx) => {
  const shardEnv = benchDatabases[idx] ? { ...process.env, PLATFORM_BENCH_DATABASE: benchDatabases[idx] } : process.env;
  return run(`host shard ${idx + 1}/${shards.length}`, ["tool", "test2json", "-t", "-p", "platformserver/host", binary, "-test.v", "-test.run", `^(${batch.join("|")})$`, "-test.count", "1", "-test.timeout", "10m"], pkg, batch, true, shardEnv);
}));
if (!pattern) {
  const packages = measure("收集非宿主 Go 测试包", () => execFileSync(go, ["list", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}", "./..."], { cwd: server, encoding: "utf8" }))
    .split("\n").filter(name => name.trim() && name !== "platformserver/host");
  emit("非宿主测试包：" + packages.length + " 个（全部执行，不受 Small 耗时过滤）");
  if (packages.length) await run("other server packages", ["test", "-json", "-count=1", ...packages], server);
}
const mergedTimings = { ...known, ...measured };
writeFileSync(timings, JSON.stringify(mergedTimings));
const tiers = classifySpeeds(mergedTimings, tests);
const totals = Object.fromEntries(["passed", "partial", "skipped", "failed", "notRun", "packageFailures"].map(key => [key, reports.reduce((sum, report) => sum + report[key], 0)]));
const leaves = Object.fromEntries(["pass", "fail", "skip", "not-run"].map(key => [key, reports.reduce((sum, report) => sum + (report.leaves[key] ?? 0), 0)]));
const ok = reports.length > 0 && reports.every(report => report.ok) && testGate({ ...totals, leaves }, { requireNoSkip: strict });
saveEvidence({ status: ok ? "completed" : "failed", pattern, strict, tier: smallOnly ? "small" : "all", tiers, totals, leaves, selectedHostTests: tests, excluded, reports });
for (const report of reports) for (const row of report.details.filter(row => row.status === "skip" || row.status === "fail")) {
  emit((row.status === "skip" ? "↷ 跳过汇总：" : "✗ 失败汇总：") + row.purpose + " (" + row.name + ")" + (row.reason ? " · " + row.reason : ""));
}

for (const [name, ms] of Object.entries(measured).sort((a, b) => b[1] - a[1]).slice(0, 5)) console.log(`耗时 TOP ${ms}ms · ${testPurpose(name)} (${name})`);
console.log(`speed: ${tiers.small} small (<=1s), ${tiers.medium} medium (<=5s), ${tiers.slow.length} slow (>5s) across ${shards.length} shard(s)`);
console.log(`tests: ${totals.passed} passed, ${totals.partial} partial, ${totals.skipped} skipped, ${totals.failed} failed, ${totals.notRun} not run, ${totals.packageFailures} package failures; leaf cases ${leaves.pass} passed / ${leaves.fail} failed / ${leaves.skip} skipped / ${leaves["not-run"]} not run in ${((performance.now() - started) / 1000).toFixed(3)}s`);
emit("结果报告：" + evidence);
if (!ok) process.exitCode = 1;
