import assert from "node:assert/strict";
import { registerHooks } from "node:module";
import test from "node:test";

registerHooks({ resolve(specifier, context, next) {
  if (specifier === "@platform/ui") return { url: `data:text/javascript,${encodeURIComponent('export const t = (key, values = {}) => key.replace(/\\{(\\w+)\\}/g, (_, name) => String(values[name] ?? ""));')}`, shortCircuit: true };
  try { return next(specifier, context); }
  catch (error) {
    if (specifier.startsWith("./") || specifier.startsWith("../")) return next(`${specifier}.ts`, context);
    throw error;
  }
} });
const { bindingPortLabel, dataEdges, dataPort, workflowDiagnostics } = await import("./workflow-model.ts");
const { removeWorkflowSteps, renameWorkflowStep, workflowBindings, withWorkflowBinding } = await import("./workflow-edit.ts");

const result = (path) => ({ source: "step", step: "calculate", path });
const draft = () => ({ id: "flow", revision: 2, name: "review", title: "Review", object: "", when: "", manual: true,
  steps: [
    { name: "calculate", kind: "compute", next: "eligible", operation: { app: "build", name: "score", version: 2 } },
    { name: "eligible", kind: "branch", cases: { true: "done", false: "done" }, condition: { op: "all", terms: [
      { op: "eq", left: result(["deep", "score"]), right: { source: "literal", value: 3 } },
      { op: "not", terms: [{ op: "exists", left: result(["flags", "blocked"]) }] },
    ] } },
    { name: "done", kind: "end", value: result([]) },
  ],
});

test("condition data edges keep each nested operand and upstream field path", () => {
  const value = draft(), before = structuredClone(value), edges = dataEdges(value);
  assert.deepEqual(edges.map(edge => [edge.source, edge.sourcePort, edge.target, edge.targetPort]), [
    ["calculate", dataPort(["deep", "score"]), "eligible", "binding:condition.terms.0.left"],
    ["calculate", dataPort(["flags", "blocked"]), "eligible", "binding:condition.terms.1.terms.0.left"],
    ["calculate", dataPort([]), "done", "binding:value"],
  ]);
  assert.equal(bindingPortLabel(edges[0].targetPort), "Condition 1 · Left value");
  assert.equal(bindingPortLabel(edges[1].targetPort), "Condition 2.1 · Left value");
  assert.deepEqual(value, before);
});

test("graph rebind and disconnect edit one nested operand while preserving the original predicate", () => {
  const original = draft().steps[1], before = structuredClone(original), port = "binding:condition.terms.1.terms.0.left";
  const next = withWorkflowBinding(original, port, { source: "input", path: ["blocked"] });
  assert.deepEqual(next.condition.terms[1].terms[0].left, { source: "input", path: ["blocked"] });
  assert.deepEqual(next.condition.terms[0], original.condition.terms[0]);
  assert.deepEqual(next.cases, original.cases);
  assert.equal(Object.hasOwn(next, "condition.terms.1.terms.0.left"), false);
  const disconnected = withWorkflowBinding(next, port, undefined);
  assert.equal(disconnected.condition.terms[1].terms[0].left, undefined);
  assert.ok(workflowBindings(disconnected).some(item => item.port === port));
  assert.deepEqual(original, before);
  assert.equal(withWorkflowBinding(original, "binding:condition.terms.8.left", result([])), original);
  assert.equal(withWorkflowBinding(original, "binding:condition.constructor.left", result([])), original);
});

test("root predicates and existing input sockets use the same binding edit command", () => {
  const original = { name: "gate", kind: "branch", condition: { op: "eq", left: result(["value"]) }, inputs: { "a.b": result(["nested"]) }, target: result(["id"]), collection: result(["items"]) };
  assert.deepEqual(workflowBindings(original).map(item => item.port), ["input:a.b", "binding:target", "binding:collection", "binding:condition.left", "binding:condition.right"]);
  const connected = withWorkflowBinding(original, "binding:condition.right", result(["threshold"]));
  assert.deepEqual(connected.condition.right, result(["threshold"]));
  assert.equal(withWorkflowBinding(connected, "input:a.b", undefined).inputs["a.b"], undefined);
  assert.deepEqual(withWorkflowBinding(original, "binding:collection", { source: "input" }).collection, { source: "input" });
});

test("shared rename and deletion update every nested condition reference and preserve graph structure", () => {
  const original = draft(), renamed = renameWorkflowStep(original, "calculate", "score");
  const nested = workflowBindings(renamed.steps[1]).filter(item => item.binding?.source === "step");
  assert.ok(nested.every(item => item.binding.step === "score"));
  assert.deepEqual(renamed.steps[1].cases, original.steps[1].cases);
  assert.deepEqual(renamed.steps[1].condition.terms[0].right, original.steps[1].condition.terms[0].right);
  const removed = removeWorkflowSteps(renamed, ["score"]);
  assert.ok(workflowBindings(removed.steps[0]).every(item => item.binding?.source !== "step"));
  assert.equal(removed.steps[0].condition.terms[1].terms[0].op, "exists");
  assert.deepEqual(removed.steps[0].cases, original.steps[1].cases);
});

test("local and server diagnostics share node locations while process issues stay unassigned", () => {
  const value = draft(); value.steps[1].cases.false = "missing";
  const before = structuredClone(value);
  const issues = workflowDiagnostics(value, { issues: [{ node: "eligible", message: "Owner rejected the condition" }, { message: "Process publication needs review" }] });
  assert.ok(issues.eligible.some(issue => issue.message === "A connected step is missing."));
  assert.ok(issues.eligible.some(issue => issue.message === "Owner rejected the condition" && issue.severity === "error"));
  assert.deepEqual(issues[""], [{ message: "Process publication needs review", severity: "error" }]);
  assert.deepEqual(value, before);
  assert.deepEqual(workflowDiagnostics(draft()), {});
});
