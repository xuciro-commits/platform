import assert from "node:assert/strict";
import { test } from "node:test";
import { selectTests, summarizeGo, testGate } from "./test-results.mjs";

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
