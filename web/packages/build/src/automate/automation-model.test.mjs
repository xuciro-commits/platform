import assert from "node:assert/strict";
import { registerHooks } from "node:module";
import test from "node:test";

registerHooks({ resolve(specifier, context, next) {
  try { return next(specifier, context); }
  catch (error) {
    if (specifier.startsWith("./") || specifier.startsWith("../")) return next(`${specifier}.ts`, context);
    throw error;
  }
} });
const { automationParts, relinkAutomation, removeAutomationStep } = await import("./automation-model.ts");
const { renameWorkflowStep } = await import("./workflow-edit.ts");
const { workflowInputs } = await import("./workflow-session.ts");

const draft = () => ({
  id: "process", revision: 4, name: "review", title: "Review", kind: "automation", object: "orders", when: "ready",
  lanes: [{ name: "staff", title: "Staff" }], layout: { compute: { x: 1, y: 2 } }, input: { complete: true },
  steps: [
    { name: "eligible", kind: "branch", condition: { op: "all", terms: [
      { op: "eq", left: { source: "subject", path: ["state"] }, right: { source: "literal", value: "ready" } },
      { op: "eq", left: { source: "input", path: ["complete"] }, right: { source: "literal", value: true } },
    ] }, cases: { true: "compute", false: "skipped" } },
    { name: "compute", kind: "compute", operation: { app: "build", name: "score", version: 2 }, lane: "staff", next: "notify", error: "failed", retryAttempts: 3, retryBackoffMs: 50, timeoutSeconds: 10, value: { source: "input" } },
    { name: "notify", kind: "action", act: "orders.notify", inputs: { score: { source: "step", step: "compute", path: ["score"] } } },
    { name: "skipped", kind: "end", value: { source: "literal", value: { status: "skipped" } } },
    { name: "failed", kind: "end", value: { source: "literal", value: { status: "failed" } } },
  ],
});

test("custom condition names, nested predicates, end values and advanced settings survive card edits", () => {
  const original = draft(), { condition, effects, editable } = automationParts(original);
  assert.equal(editable, true);
  assert.equal(condition.name, "eligible");
  const steps = relinkAutomation(original.steps, condition, effects.map(step => step.name === "notify" ? { ...step, title: "Send notification" } : step));
  assert.deepEqual(steps[0], original.steps[0]);
  assert.deepEqual(steps[1], original.steps[1]);
  assert.deepEqual(steps.slice(3), original.steps.slice(3));
  const inputs = workflowInputs({ ...original, steps });
  assert.deepEqual(inputs.lanes, original.lanes);
  assert.deepEqual(inputs.input, original.input);
  assert.deepEqual(inputs.layout, original.layout);
});

test("reordering and appending cards only change linear links", () => {
  const original = draft(), { condition, effects } = automationParts(original);
  const reordered = relinkAutomation(original.steps, condition, [...effects].reverse());
  assert.equal(reordered[0].cases.true, "notify");
  assert.equal(reordered[1].next, "compute");
  assert.equal(reordered[2].next, "");
  assert.deepEqual(reordered[1].inputs, effects[1].inputs);
  assert.equal(reordered[2].error, "failed");
  assert.deepEqual(reordered.slice(3), original.steps.slice(3));
  const appended = relinkAutomation(original.steps, condition, [...effects, { name: "calculate", kind: "compute", operation: { app: "build", name: "total", version: 7 } }]);
  assert.equal(appended[2].next, "calculate");
  assert.equal(appended[3].operation.version, 7);
});

test("unsupported graphs remain intact for the flow editor", () => {
  for (const patch of [
    { kind: "flow" }, { manual: true }, { continuous: { source: "events" } },
    { steps: [{ name: "first", kind: "query", next: "compute" }, ...draft().steps] },
    { steps: draft().steps.map(step => step.name === "compute" ? { ...step, next: "skipped" } : step) },
    { steps: [{ name: "first", kind: "end" }, ...draft().steps] },
  ]) {
    const value = { ...draft(), ...patch }, before = structuredClone(value);
    assert.equal(automationParts(value).editable, false);
    assert.deepEqual(value, before);
  }
  assert.equal(automationParts({ ...draft(), object: "", when: "", every: "1h" }).editable, true);
  assert.equal(automationParts({ ...draft(), steps: [] }).editable, true);
});

test("shared rename preserves control, nested data references, explicit returns and canvas positions", () => {
  const original = draft();
  original.steps[0].condition.terms.push({ op: "exists", left: { source: "step", step: "compute", path: ["score"] } });
  original.steps[3].value = { source: "step", step: "compute", path: ["score"] };
  const renamed = renameWorkflowStep(original, "compute", "score");
  assert.equal(renamed.steps[0].cases.true, "score");
  assert.equal(renamed.steps[0].condition.terms[2].left.step, "score");
  assert.equal(renamed.steps[1].name, "score");
  assert.equal(renamed.steps[2].inputs.score.step, "score");
  assert.equal(renamed.steps[3].value.step, "score");
  assert.deepEqual(renamed.layout, { score: { x: 1, y: 2 } });
  assert.equal(original.steps[1].name, "compute");
  assert.equal(renameWorkflowStep(original, "compute", "notify"), original);
});

test("explicit removal shares reference cleanup and drops only disconnected returns", () => {
  const original = draft(), removed = removeAutomationStep(original, "compute"), steps = removed.steps;
  assert.deepEqual(steps.map(step => step.name), ["eligible", "notify", "skipped"]);
  assert.equal(steps[0].cases.true, "notify");
  assert.deepEqual(steps[1].inputs, {});
  assert.deepEqual(removed.layout, {});
  assert.deepEqual(steps[2].value, original.steps[3].value);
});

test("removing a condition retains a return still used by an effect", () => {
  const original = draft(), steps = removeAutomationStep(original, "eligible").steps;
  assert.deepEqual(steps.map(step => step.name), ["compute", "notify", "failed"]);
  assert.deepEqual(steps[2].value, original.steps[4].value);
});
