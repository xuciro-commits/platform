import assert from "node:assert/strict";
import { test } from "node:test";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { classifySpeeds, extractTimings, filterSmallTests, selectTests, shardTests, summarizeGo, testGate } from "./test-results.mjs";

import { testPurpose } from "./test-purposes.mjs";
import { createTestProgress } from "./test-progress.mjs";

const report = (events, expected = []) => summarizeGo(events.map(([Action, Test, Package = "pkg"]) => JSON.stringify({ Action, Test, Package })).join("\n"), expected);

test("selection includes non-invariant tests and zero matches are empty", () => {
  assert.deepEqual(selectTests("TestInvariantK1\nTestOther\nBenchmarkX\n", ""), ["TestInvariantK1", "TestOther"]);
  assert.deepEqual(selectTests("TestInvariantK1\n", "K99"), []);
});
test("complete parent and children count once at each level", () => {
  const r = report([["run", "TestA"], ["run", "TestA/file"], ["pass", "TestA/file"], ["pass", "TestA"], ["pass"]], ["TestA"]);
  assert.equal(r.passed, 1);
  assert.equal(r.leaves.pass, 1);
  assert.equal(testGate(r), true);
});
test("skipped child means partial, not passed; strict gate fails", () => {
  const r = report([["run", "TestA"], ["pass", "TestA/file"], ["skip", "TestA/pg"], ["pass", "TestA"], ["pass"]], ["TestA"]);
  assert.equal(r.passed, 0);
  assert.equal(r.partial, 1);
  assert.equal(r.leaves.skip, 1);
  assert.equal(testGate(r), true);
  assert.equal(testGate(r, { requireNoSkip: true }), false);
});
test("all skipped or zero matches fail, including exit-zero no-test package", () => {
  const r = report([["run", "TestA"], ["skip", "TestA"], ["pass"]], ["TestA"]);
  assert.equal(r.skipped, 1);
  assert.equal(testGate(r), false);
  assert.equal(testGate(report([["pass"]], ["TestMissing"])), false);
  assert.equal(testGate(report([["skip"]])), false);
  const partial = report([["run", "TestA"], ["skip", "TestA/pg"], ["pass", "TestA"]]);
  assert.equal(testGate(partial), false);
  assert.equal(testGate(partial, { allowSkippedReport: true }), true);
  assert.equal(testGate(partial, { allowSkippedReport: true, requireNoSkip: true }), false);
});
test("failed, interrupted and build failures are not passed", () => {
  const r = report([["run", "TestA"], ["fail", "TestA"], ["run", "TestB"], ["fail"]], ["TestA", "TestB", "TestC"]);
  assert.equal(r.failed, 1);
  assert.equal(r.notRun, 2);
  assert.equal(r.packageFailures, 1);
  assert.equal(testGate(r), false);
});
test("identical names in different packages stay separate", () => {
  const r = report([["pass", "TestA", "a"], ["pass", "TestA", "b"], ["skip", undefined, "no-tests"]]);
  assert.equal(r.passed, 2);
  assert.equal(r.skipped, 0);
});
test("sharding balances test durations, filters small tier and classifies S/M/L", () => {
  const timings = { TestSlow: 6000, TestMed1: 2500, TestMed2: 2000, TestFast1: 200, TestFast2: 100 };
  const shards = shardTests(Object.keys(timings), timings, 2);
  assert.equal(shards.length, 2);
  assert.deepEqual(shards[0], ["TestSlow"]);
  assert.deepEqual(shards[1], ["TestMed1", "TestMed2", "TestFast1", "TestFast2"]);
  assert.deepEqual(filterSmallTests(["TestSlow", "TestFast1", "TestInvariantK1ImportBatchThousandReplay", "TestNewKernelRule"], timings), ["TestFast1", "TestNewKernelRule"]);
  const extracted = extractTimings([
    JSON.stringify({ Action: "pass", Test: "TestFast1/sub", Elapsed: 0.1 }),
    JSON.stringify({ Action: "pass", Test: "TestFast1", Elapsed: 0.25 }),
  ].join("\n"));
  assert.deepEqual(extracted, { TestFast1: 250 });
  const tiers = classifySpeeds(timings);
  assert.equal(tiers.small, 2);
  assert.equal(tiers.medium, 2);
  assert.deepEqual(tiers.slow, [["TestSlow", 6000]]);
});

test("timed command records actual elapsed time and preserves failure exit code", () => {
  const dir = mkdtempSync(join(tmpdir(), "platform-timing-test-"));
  const file = join(dir, "phases.jsonl");
  try {
    writeFileSync(file, "");
    const env = { ...process.env, PLATFORM_TIMING_FILE: file, PLATFORM_TIMING_START: String(Date.now()), PLATFORM_TIMING_SCOPE: "" };
    const script = fileURLToPath(new URL("./timing.mjs", import.meta.url));
    const result = spawnSync(process.execPath, [script, "run", "diagnostic", process.execPath, "-e",
      "setTimeout(() => { console.error('expected failure'); process.exit(7); }, 120)"], { env, encoding: "utf8", timeout: 10_000 });
    assert.equal(result.status, 7);
    assert.match(result.stderr, /expected failure/);
    assert.match(result.stderr, /→ diagnostic/);
    assert.match(result.stderr, /✗ diagnostic/);
    const [phase] = readFileSync(file, "utf8").trim().split("\n").map(line => JSON.parse(line));
    assert.equal(phase.name, "diagnostic");
    assert.equal(phase.ok, false);
    assert.ok(phase.ms >= 100, "timer lost actual wait: " + phase.ms);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("all Go tests carry a concise human-readable purpose", () => {
  const files = execFileSync("git", ["ls-files", "capabilities/server/*_test.go"], { encoding: "utf8" }).trim().split("\n");
  for (const file of files) {
    const source = readFileSync(new URL("../" + file, import.meta.url), "utf8");
    for (const [, name] of source.matchAll(/^func (Test\w+)\(/gm)) assert.match(testPurpose(name), /\p{Script=Han}/u, name);
  }
  assert.throws(() => testPurpose("TestMissingPurpose"), /缺少用途说明/);
});

test("test progress shows the original failing assertion and skip reason, not a trailing successful event", () => {
  const lines = [];
  const progress = createTestProgress("shard 1", line => lines.push(line));
  const send = (Action, Test, Output, Elapsed) => progress.accept({ Action, Test, Output, Elapsed, Package: "platformserver/host" });
  const name = "TestInvariantK1ReplayIsByteIdentical";
  send("run", name);
  send("run", name + "/postgres");
  send("output", name + "/postgres", "    invariants_test.go:88: requires an isolated PostgreSQL database\n");
  send("skip", name + "/postgres", undefined, 0);
  send("pass", name, undefined, 0.2);
  send("run", "TestInvariantK2SameKeyReturnsSameAnswer");
  send("output", "TestInvariantK2SameKeyReturnsSameAnswer", "    invariants_test.go:220: duplicate key changed the result\n");
  send("fail", "TestInvariantK2SameKeyReturnsSameAnswer", undefined, 0.1);
  send("run", "TestInvariantK3StaleRevisionIsRefusedAndSealed");
  send("pass", "TestInvariantK3StaleRevisionIsRefusedAndSealed", undefined, 0.1);
  assert.match(lines.join("\n"), /→ .*重放后的接受映像字节一致/);
  assert.match(lines.join("\n"), /需要独立 PostgreSQL 数据库/);
  assert.match(lines.join("\n"), /部分通过/);
  assert.match(lines.join("\n"), /invariants_test.go:220: duplicate key changed the result/);
  assert.equal(progress.details().find(row => row.name === name).status, "partial");
  assert.equal(progress.details().filter(row => row.status === "fail").length, 1);
});
