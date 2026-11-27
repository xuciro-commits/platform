#!/usr/bin/env node
import { execFile, execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { availableParallelism } from "node:os";
import { fileURLToPath } from "node:url";
import { selectTests, summarizeGo, testGate } from "./test-results.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const server = root + "capabilities/server";
const pkg = server + "/host";
const binary = root + ".build/check/host.test";
const timings = root + ".build/check/invariant-timings.json";
const evidence = root + ".build/check/test-results.json";
const started = Date.now();
const go = process.env.GO || "go";
const pattern = process.argv[2] || "";
const strict = process.argv.includes("--require-no-skip");
const workers = process.env.PLATFORM_BENCH_DATABASE ? 1 : Math.min(availableParallelism(), 6);

mkdirSync(root + ".build/check", { recursive: true });
writeFileSync(evidence, JSON.stringify({ status: "incomplete" }));
execFileSync(go, ["test", "-c", "-o", binary, "."], { cwd: pkg, stdio: "inherit" });
const known = existsSync(timings) ? JSON.parse(readFileSync(timings, "utf8")) : {};
const tests = selectTests(execFileSync(binary, ["-test.list", "^Test"], { cwd: pkg, encoding: "utf8" }), pattern)
  .sort((a, b) => (known[b] ?? Infinity) - (known[a] ?? Infinity));
if (pattern && !tests.length) {
  writeFileSync(evidence, JSON.stringify({ status: "failed", pattern, reason: "zero matches", totals: { passed: 0, partial: 0, skipped: 0, failed: 0, notRun: 0 } }));
  console.error(`tests: no host tests match ${pattern}`);
  process.exit(1);
}
const reports = [];
const slow = [];
let next = 0;
const run = (name, args, cwd, expected = []) => new Promise(resolve =>
  execFile(go, args, { cwd, maxBuffer: 64 << 20 }, (error, stdout, stderr) => {
    let result;
    try {
      result = summarizeGo(stdout, expected);
    } catch (parseError) {
      console.error(name, parseError.message);
      result = { passed: 0, partial: 0, skipped: 0, failed: 0, notRun: expected.length, packageFailures: 1, leaves: {}, skippedTests: [] };
    }
    const ok = !error && testGate(result, { requireNoSkip: strict, allowSkippedReport: true });
    if (!ok) console.error(`--- ${name}\n${(stdout + stderr).slice(-6000)}`);
    reports.push({ name, ok, exitCode: error?.code ?? 0, ...result });
    writeFileSync(evidence, JSON.stringify({ status: "incomplete", pattern, strict, selectedHostTests: tests, reports }, null, 2));
    resolve();
  }));
const worker = async () => {
  while (next < tests.length) {
    const name = tests[next++];
    const begin = Date.now();
    await run(name, ["tool", "test2json", "-t", "-p", "platformserver/host", binary, "-test.v=test2json", "-test.run", `^${name}$`, "-test.count", "1", "-test.timeout", "10m"], pkg, [name]);
    slow.push([Date.now() - begin, name]);
  }
};
await Promise.all(Array.from({ length: Math.min(workers, tests.length) }, worker));
if (!pattern) {
  const packages = execFileSync(go, ["list", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}", "./..."], { cwd: server, encoding: "utf8" })
    .split("\n").filter(name => name.trim() && name !== "platformserver/host");
  if (packages.length) await run("other server packages", ["test", "-json", "-count=1", ...packages], server);
}
slow.sort((a, b) => b[0] - a[0]);
writeFileSync(timings, JSON.stringify({ ...known, ...Object.fromEntries(slow.map(([ms, name]) => [name, ms])) }));
const totals = Object.fromEntries(["passed", "partial", "skipped", "failed", "notRun", "packageFailures"].map(key => [key, reports.reduce((sum, report) => sum + report[key], 0)]));
const leaves = Object.fromEntries(["pass", "fail", "skip", "not-run"].map(key => [key, reports.reduce((sum, report) => sum + (report.leaves[key] ?? 0), 0)]));
const ok = reports.length > 0 && reports.every(report => report.ok) && testGate({ ...totals, leaves }, { requireNoSkip: strict });
writeFileSync(evidence, JSON.stringify({ status: ok ? "completed" : "failed", pattern, strict, totals, leaves, reports }, null, 2));
for (const report of reports) for (const name of report.skippedTests) console.log("SKIP " + name);
console.log(`tests: ${totals.passed} passed, ${totals.partial} partial, ${totals.skipped} skipped, ${totals.failed} failed, ${totals.notRun} not run, ${totals.packageFailures} package failures; leaf cases ${leaves.pass} passed / ${leaves.fail} failed / ${leaves.skip} skipped / ${leaves["not-run"]} not run in ${((Date.now() - started) / 1000).toFixed(1)}s`);
if (!ok) process.exitCode = 1;
