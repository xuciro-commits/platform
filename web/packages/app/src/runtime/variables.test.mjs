import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { compileVariables, evaluateVariables } from "./variables.ts";
const root = new URL("../../../../../capabilities/server/platform/pageui/", import.meta.url);
const contract = JSON.parse(readFileSync(new URL("widgets.json", root))).runtime;
const vectors = JSON.parse(readFileSync(new URL("variables.vectors.json", root)));
for (const vector of vectors) test(vector.name, () => {
  assert.equal(compileVariables(vector.variables, contract).issues.length === 0, vector.valid);
  if (!vector.valid) return;
  const values = (state) => Object.fromEntries(Object.entries(evaluateVariables(vector.variables, state, contract, vector.resources, vector.owner)).map(([id, result]) => {
    assert.equal(result.status, "value"); return [id, result.value];
  }));
  assert.deepEqual(values({}), vector.values);
  if (vector.state) assert.deepEqual(values(vector.state), vector.updated);
});
test("runtime rejects wrong state types and propagates bounded result errors", () => {
  const variables = vectors[0].variables;
  assert.equal(evaluateVariables(variables, { tab: true }, contract).tab.status, "error");
  const result = evaluateVariables(variables, { tab: "a".repeat(4096) }, contract);
  assert.equal(result.tab.status, "value");
  assert.equal(result.label.status, "error");
  // Evaluation never mutates the document or shares a state snapshot.
  assert.equal(evaluateVariables(variables, {}, contract).tab.value, "first");
});

test("resource presence preserves pending, empty and refusal without coercing record objects", () => {
  const variables = vectors.find((vector) => vector.name === "typed record output").variables;
  assert.deepEqual(evaluateVariables(variables, {}, contract, { selected: { status: "empty" } }).hasRecord, { status: "value", value: false });
  assert.equal(evaluateVariables(variables, {}, contract, { selected: { status: "pending" } }).hasRecord.status, "pending");
  assert.equal(evaluateVariables(variables, {}, contract, { selected: { status: "error", code: "Denied" } }).hasRecord.status, "error");
  assert.equal(evaluateVariables(variables, {}, contract, { selected: { status: "value", value: "untyped record" } }).hasRecord.status, "error");
});
