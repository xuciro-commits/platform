import assert from "node:assert/strict";
import test from "node:test";
import { retireLimit, retirementBatch, retirementEdits, retirementIntent } from "./retirement.ts";

const item = (name, id = name) => ({ kind: { type: "build.object" }, ref: { app: "build", kind: "object", name }, record: { id }, title: name, installed: false, missing: false });
const keyOf = (item) => `${item.ref.app}/${item.ref.kind}/${item.ref.name}`;

test("a retirement batch is exactly the selected retireable items: 64 pass, 65 are refused up front, nothing is truncated", () => {
  const items = Array.from({ length: 65 }, (_, index) => item(`asset-${index}`));
  const picked = new Set(items.map(keyOf));
  assert.equal(retirementBatch(items, picked, keyOf).length, 65);
  assert.equal(retireLimit, 64);
  // The caller refuses above the limit before any composite is created; the
  // edit list it does submit always matches the reviewed list one to one.
  const batch64 = retirementBatch(items.slice(0, 64), picked, keyOf);
  const edits = retirementEdits(batch64);
  assert.equal(edits.length, 64);
  assert.deepEqual(edits[0], { schema: "build.object.archive", target: { type: "build.object", id: "asset-0" }, payload: {} });
  assert.deepEqual(edits[63], { schema: "build.object.archive", target: { type: "build.object", id: "asset-63" }, payload: {} });
});

test("selection changes after the review opens do not alter a frozen batch, and a retry keeps the original composite key", () => {
  const items = [item("a"), item("b"), item("c")];
  const picked = new Set(["build/object/a", "build/object/b"]);
  const frozen = retirementIntent("KEY-1", retirementBatch(items, picked, keyOf));
  // The member changes their selection while the dialog is open.
  picked.add("build/object/c"); picked.delete("build/object/a");
  items[0].record.id = "changed-after-review";
  items[0].title = "Changed title";
  assert.deepEqual(frozen.items.map(keyOf), ["build/object/a", "build/object/b"]);
  assert.equal(frozen.items[0].title, "a");
  assert.deepEqual(frozen.edits.map((edit) => edit.target.id), ["a", "b"]);
  assert.equal(frozen.key, "KEY-1");
});

test("unselected or record-less rows never enter the batch", () => {
  const items = [item("a"), { ...item("b", "b"), record: undefined }, item("c")];
  const picked = new Set(["build/object/a", "build/object/b", "build/object/c"]);
  const batch = retirementBatch(items, picked, keyOf);
  assert.deepEqual(batch.map((item) => item.record.id), ["a", "c"]);
});
