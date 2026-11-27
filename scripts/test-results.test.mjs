import assert from "node:assert/strict";
import { test } from "node:test";
import { classifySpeeds, extractTimings, filterSmallTests, selectTests, shardTests, summarizeGo, testGate } from "./test-results.mjs";

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
