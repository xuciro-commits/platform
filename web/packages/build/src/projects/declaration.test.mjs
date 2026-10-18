import assert from "node:assert/strict";
import test from "node:test";
import { declarationValue, replaceDeclaration, declarationChanges } from "../editor/declaration.ts";

const fields = [
  {name: "id", type: "text", readOnly: true},
  {name: "revision", type: "integer", readOnly: true},
  {name: "name", type: "text"},
  {name: "title", type: "text"},
  {name: "numbering", type: "json"},
  {name: "fields", type: "lines"},
];

test("source replacement retains record identity and every declared advanced value", () => {
  const current = {id: "O", revision: 7, name: "orders", title: "Orders", numbering: {field: "number", prefix: "NO-"}, fields: [{name: "ready", type: "boolean", required: false}]};
  const value = declarationValue(current, fields);
  assert.deepEqual(Object.keys(value), ["name", "title", "numbering", "fields"]);
  const next = replaceDeclaration(current, {...value, title: "Orders renamed"}, fields);
  assert.equal(next.id, "O");
  assert.equal(next.revision, 7);
  assert.deepEqual(next.numbering, current.numbering);
  assert.equal(next.fields[0].required, false);
  assert.deepEqual(declarationChanges(declarationValue(next, fields), value), {title: "Orders renamed"});
});

test("source omission clears an optional owner field rather than restoring the old value", () => {
  const before = {id: "O", revision: 2, name: "orders", title: "Orders", numbering: {field: "number"}, fields: []};
  const next = replaceDeclaration(before, {name: "orders", title: "Orders", fields: []}, fields);
  assert.equal(Object.hasOwn(next, "numbering"), false);
  assert.deepEqual(declarationChanges(declarationValue(next, fields), declarationValue(before, fields)), {numbering: null});
  assert.deepEqual(declarationChanges({active: false, count: 0, title: ""}, {active: true, count: 1, title: "Previous"}), {active: false, count: 0, title: ""});
});
