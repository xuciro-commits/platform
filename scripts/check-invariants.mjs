#!/usr/bin/env node
import { execFile, execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { availableParallelism } from "node:os";
import { fileURLToPath } from "node:url";
import { begin, measure, note } from "./timing.mjs";
import { classifySpeeds, extractTimings, filterSmallTests, selectTests, shardTests, summarizeGo, testGate } from "./test-results.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const server = root + "capabilities/server";
const pkg = server + "/host";
const binary = root + ".build/check/host.test";
const timings = root + ".build/check/invariant-timings.json";
const evidence = root + ".build/check/test-results.json";
const started = performance.now();
const go = process.env.GO || "go";
const rawArgs = process.argv.slice(2);
const flags = new Set(rawArgs.filter(arg => arg.startsWith("--")));
const pattern = rawArgs.find(arg => !arg.startsWith("--")) || "";
const strict = flags.has("--require-no-skip");
const smallOnly = flags.has("--tier=small");
const benchDatabases = (process.env.PLATFORM_BENCH_DATABASES || "").split(",").map(s => s.trim()).filter(Boolean);
const workers = benchDatabases.length ? benchDatabases.length : (process.env.PLATFORM_BENCH_DATABASE ? 1 : Math.min(availableParallelism(), 6));

mkdirSync(root + ".build/check", { recursive: true });
writeFileSync(evidence, JSON.stringify({ status: "incomplete" }));
measure("编译后端测试二进制", () => execFileSync(go, ["test", "-c", "-o", binary, "."], { cwd: pkg, stdio: "inherit" }));
const rawKnown = existsSync(timings) ? JSON.parse(readFileSync(timings, "utf8")) : {};
const listed = measure("收集后端不变量", () => execFileSync(binary, ["-test.list", "^Test"], { cwd: pkg, encoding: "utf8" }));
const existing = new Set(selectTests(listed, ""));
const known = Object.fromEntries(Object.entries(rawKnown).filter(([name]) => existing.has(name)));
const matched = selectTests(listed, pattern);
const tests = smallOnly ? filterSmallTests(matched, known) : matched;
if (!tests.length) {
  writeFileSync(evidence, JSON.stringify({ status: "failed", pattern, tier: smallOnly ? "small" : "all", reason: "zero matches", totals: { passed: 0, partial: 0, skipped: 0, failed: 0, notRun: 0 } }));
  console.error(`tests: no host tests match ${pattern || "--tier=small"}`);
  process.exit(1);
}
const reports = [];
const measured = {};
const run = (name, args, cwd, expected = [], collectTimings = false, env = process.env) => new Promise(resolve => {
  const end = begin(name + " · 执行 " + (expected.length || "子包") + " 项");
  const child = execFile(go, args, { cwd, env, maxBuffer: 64 << 20 }, (error, stdout, stderr) => {
    let result;
    try {
      result = summarizeGo(stdout, expected);
      if (collectTimings) Object.assign(measured, extractTimings(stdout));
    } catch (parseError) {
      console.error(name, parseError.message);
      result = { passed: 0, partial: 0, skipped: 0, failed: 0, notRun: expected.length, packageFailures: 1, leaves: {}, skippedTests: [] };
    }
    const ok = !error && testGate(result, { requireNoSkip: strict, allowSkippedReport: true });
    if (!ok) console.error(`--- ${name}\n${(stdout + stderr).slice(-6000)}`);
    reports.push({ name, tests: expected, ok, exitCode: error?.code ?? 0, ...result });
    writeFileSync(evidence, JSON.stringify({ status: "incomplete", pattern, strict, tier: smallOnly ? "small" : "all", selectedHostTests: tests, reports }, null, 2));
    end(ok);
    resolve();
  });
  let pending = "";
  child.stdout.on("data", chunk => {
    pending += chunk;
    const lines = pending.split("\n");
    pending = lines.pop() ?? "";
    for (const line of lines) {
      let event;
      try { event = JSON.parse(line); } catch { continue; }
      if (event.Test && !event.Test.includes("/") && ["pass", "fail", "skip"].includes(event.Action) && typeof event.Elapsed === "number") {
        note(name + " · " + event.Action.toUpperCase() + " " + event.Test + " (" + event.Elapsed.toFixed(3) + "s)");
      }
    }
  });
});
const shards = shardTests(tests, known, workers);
note("选择 " + tests.length + " 项不变量 / " + shards.length + " 个分片");
await Promise.all(shards.map((batch, idx) => {
  const shardEnv = benchDatabases[idx] ? { ...process.env, PLATFORM_BENCH_DATABASE: benchDatabases[idx] } : process.env;
  return run(`host shard ${idx + 1}/${shards.length}`, ["tool", "test2json", "-t", "-p", "platformserver/host", binary, "-test.v", "-test.run", `^(${batch.join("|")})$`, "-test.count", "1", "-test.timeout", "10m"], pkg, batch, true, shardEnv);
}));
if (!pattern) {
  const packages = measure("收集非宿主 Go 测试包", () => execFileSync(go, ["list", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}", "./..."], { cwd: server, encoding: "utf8" }))
    .split("\n").filter(name => name.trim() && name !== "platformserver/host");
  if (packages.length) await run("other server packages", ["test", "-json", "-count=1", ...packages], server);
}
const mergedTimings = { ...known, ...measured };
writeFileSync(timings, JSON.stringify(mergedTimings));
const tiers = classifySpeeds(mergedTimings, tests);
const totals = Object.fromEntries(["passed", "partial", "skipped", "failed", "notRun", "packageFailures"].map(key => [key, reports.reduce((sum, report) => sum + report[key], 0)]));
const leaves = Object.fromEntries(["pass", "fail", "skip", "not-run"].map(key => [key, reports.reduce((sum, report) => sum + (report.leaves[key] ?? 0), 0)]));
const ok = reports.length > 0 && reports.every(report => report.ok) && testGate({ ...totals, leaves }, { requireNoSkip: strict });
writeFileSync(evidence, JSON.stringify({ status: ok ? "completed" : "failed", pattern, strict, tier: smallOnly ? "small" : "all", tiers, totals, leaves, reports }, null, 2));
for (const report of reports) for (const name of report.skippedTests) console.log("SKIP " + name);
for (const [name, ms] of Object.entries(measured).sort((a, b) => b[1] - a[1]).slice(0, 5)) console.log(`SLOW ${ms}ms ${name}`);
console.log(`speed: ${tiers.small} small (<=1s), ${tiers.medium} medium (<=5s), ${tiers.slow.length} slow (>5s) across ${shards.length} shard(s)`);
console.log(`tests: ${totals.passed} passed, ${totals.partial} partial, ${totals.skipped} skipped, ${totals.failed} failed, ${totals.notRun} not run, ${totals.packageFailures} package failures; leaf cases ${leaves.pass} passed / ${leaves.fail} failed / ${leaves.skip} skipped / ${leaves["not-run"]} not run in ${((performance.now() - started) / 1000).toFixed(3)}s`);
if (!ok) process.exitCode = 1;
