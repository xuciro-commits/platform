import assert from "node:assert/strict";
import test from "node:test";
import { renameTransactionStep, transactionIssues } from "./transaction-edit.ts";

const expr = ref => ({ op: "", kind: "text", ref });
const txn = () => ({ subject: "build.order", message: "step.number", steps: [
  { name: "number", kind: "sequence", sequence: { Name: "documents", Pattern: "step.number", Yearly: false } },
  { name: "create", kind: "create", target: { type: "build.line" }, changes: [{ field: "number", from: "step.number" }, { field: "note", expr: { op: "", kind: "text", lit: { value: "step.number" } } }] },
  { name: "change", kind: "update", target: { type: "build.line", id: "step.create" }, changes: [{ field: "number", expr: { op: "concat", kind: "text", args: [expr("step.number"), { op: "", kind: "text", lit: { value: "step.number" } }] } }], when: { op: "=", kind: "boolean", args: [expr("step.number"), expr("input.number")] } },
  { name: "loop", kind: "for-each", forEach: { set: "lines", steps: [
    { name: "child", kind: "call", call: { ref: { app: "build", kind: "query", name: "step.number" }, version: 2, view: "snapshot", inputs: [{ field: "number", expr: expr("step.number") }], resultType: { kind: "text" } } },
    { name: "childwrite", kind: "update", target: { type: "build.line", id: "$item" }, changes: [{ field: "number", expr: expr("step.child") }] },
  ] } },
] });

test("rename changes typed result references throughout the original transaction without changing literal data", () => {
  const before = txn(), result = renameTransactionStep(before, [0], "document");
  assert.equal(result.steps[0].name, "document");
  assert.equal(result.steps[1].changes[0].from, "step.document");
  assert.equal(result.steps[2].changes[0].expr.args[0].ref, "step.document");
  assert.equal(result.steps[2].when.args[0].ref, "step.document");
  assert.equal(result.steps[3].forEach.steps[0].call.inputs[0].expr.ref, "step.document");
  assert.equal(result.steps[1].changes[1].expr.lit.value, "step.number");
  assert.equal(result.steps[2].changes[0].expr.args[1].lit.value, "step.number");
  assert.equal(result.steps[0].sequence.Pattern, "step.number");
  assert.equal(result.steps[3].forEach.steps[0].call.ref.name, "step.number");
  assert.equal(result.message, "step.number");
  assert.equal(before.steps[0].name, "number");
  const created = renameTransactionStep(before, [1], "item");
  assert.equal(created.steps[2].target.id, "step.item");
  const nested = renameTransactionStep(before, [3, 0], "read");
  assert.equal(nested.steps[3].forEach.steps[1].changes[0].expr.ref, "step.read");
  assert.deepEqual(transactionIssues(nested), []);
});

test("moving or removing a producer leaves original values intact and diagnoses their dependent steps", () => {
  const before = txn();
  assert.deepEqual(transactionIssues(before), []);
  const moved = { ...before, steps: [before.steps[2], before.steps[0], before.steps[1], before.steps[3]] };
  const issues = transactionIssues(moved);
  assert.ok(issues.some(issue => issue.path.join(".") === "0" && issue.ref === "step.create" && issue.reason === "forward"));
  assert.ok(issues.some(issue => issue.ref === "step.number" && issue.reason === "forward"));
  const removed = { ...before, steps: before.steps.slice(1) };
  assert.ok(transactionIssues(removed).some(issue => issue.ref === "step.number" && issue.reason === "missing"));
  assert.equal(removed.steps[0].changes[0].from, "step.number");
  assert.deepEqual(before, txn());
});

test("a child result stays within its iteration and non-result steps cannot be consumed", () => {
  const value = txn();
  value.steps.push({ name: "after", kind: "update", target: { type: "build.order", id: "$subject" }, changes: [
    { field: "a", expr: expr("step.child") }, { field: "b", expr: expr("step.change") },
  ] });
  assert.deepEqual(transactionIssues(value).filter(issue => issue.step === "after").map(issue => [issue.ref, issue.reason]), [["step.child", "forward"], ["step.change", "unavailable"]]);
});

test("fixing a duplicate name changes only the chosen step without guessing ambiguous references", () => {
  const value = txn(); value.steps[1].name = "number";
  assert.equal(transactionIssues(value).filter(issue => issue.reason === "name").length, 2);
  const changed = renameTransactionStep(value, [1], "item");
  assert.equal(changed.steps[0].name, "number");
  assert.equal(changed.steps[1].name, "item");
  assert.equal(changed.steps[1].changes[0].from, "step.number");
});

test("lowercase Unicode names retain the Build owner's accepted naming rule", () => {
  const value = { subject: "build.order", steps: [{ name: "étape١", kind: "sequence" }] };
  assert.deepEqual(transactionIssues(value), []);
  value.steps[0].name = "Étape";
  assert.equal(transactionIssues(value)[0].reason, "name");
});

test("refusal routes and audit expressions retain their references across rename", () => {
  const value = txn();
  value.steps[2].refusals = [{ code: "check", level: "flag", message: "step.create", when: { op: "=", kind: "boolean", args: [expr("step.number"), expr("input.number")] }, route: { type: "build.line", id: "step.create", action: "repair" }, record: { name: "audit", kind: "create", target: { type: "build.audit" }, changes: [{ field: "number", expr: expr("step.number") }] } }];
  const renamed = renameTransactionStep(value, [0], "document");
  assert.equal(renamed.steps[2].refusals[0].when.args[0].ref, "step.document");
  assert.equal(renamed.steps[2].refusals[0].record.changes[0].expr.ref, "step.document");
  assert.equal(renamed.steps[2].refusals[0].message, "step.create");
  assert.equal(renameTransactionStep(value, [1], "line").steps[2].refusals[0].route.id, "step.line");
});

test("renaming a query keeps interval bounds and conditional accounting lines connected", () => {
  const bound = { op: "", kind: "integer", ref: "step.quantity" };
  const condition = { op: ">", kind: "boolean", args: [bound, { op: "", kind: "integer", lit: { value: 0 } }] };
  const value = { subject: "build.order", steps: [
    { name: "quantity", kind: "call", call: { ref: { app: "build", kind: "query", name: "quantity" }, version: 1, view: "snapshot", inputs: [], resultType: { kind: "integer" } } },
    { name: "items", kind: "for-each", forEach: { range: { from: { op: "", kind: "integer", lit: { value: 0 } }, until: bound, unit: "item", step: 1, limit: 20 }, steps: [] } },
    { name: "books", kind: "journal", journal: { lines: [{ when: condition, account: { op: "", kind: "text", lit: { value: "cash" } }, debit: bound }] } },
  ] };
  const renamed = renameTransactionStep(value, [0], "count");
  assert.equal(renamed.steps[1].forEach.range.until.ref, "step.count");
  assert.equal(renamed.steps[2].journal.lines[0].when.args[0].ref, "step.count");
  assert.deepEqual(transactionIssues(renamed), []);
  assert.equal(value.steps[1].forEach.range.until.ref, "step.quantity");
  const removed = { ...value, steps: value.steps.slice(1) };
  assert.ok(transactionIssues(removed).some(issue => issue.step === "items" && issue.reason === "missing"));
  assert.ok(transactionIssues(removed).some(issue => issue.step === "books" && issue.reason === "missing"));
});
