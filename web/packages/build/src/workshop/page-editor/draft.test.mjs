import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { previewSection } from "./draft.ts";

test("preview preserves every generated widget field without a second field list", () => {
  const source = readFileSync(new URL("../../../../kernel/src/gen/host.ts", import.meta.url), "utf8");
  const section = source.split("export type Section = {")[1].split("};")[0];
  const bindings = new Set(["object", "query", "actions", "function", "inlineEdit"]);
  const fields = [...section.matchAll(/^  (\w+)\??:/gm)].map(match => match[1]).filter(name => !bindings.has(name));
  assert.ok(fields.length > 100);
  const draft = Object.fromEntries(fields.map(name => [name, { original: name }]));
  const result = previewSection(draft);
  for (const field of fields) assert.equal(result[field], draft[field], field);
  for (const field of ["object", "query", "function", "inlineEdit"]) assert.equal(result[field], undefined);
  assert.equal(result.actions, undefined);
  assert.deepEqual(previewSection({widget:"table",actions:[]}).actions, []);
});

test("preview translates original owner bindings and keeps ordering without changing its draft", () => {
  const draft = { widget: "table", object: "crm.account", query: "crm.open.accounts",
    actions: ["crm.account.close", "work.task.complete"], function: { name: "advice", version: 3 },
    inlineEdit: { action: "crm.account.edit", fields: ["name"] } };
  const before = structuredClone(draft), result = previewSection(draft);
  assert.deepEqual(result.object, { app: "crm", kind: "object", name: "crm.account" });
  assert.deepEqual(result.query, { app: "crm", kind: "query", name: "open.accounts" });
  assert.deepEqual(result.actions, [
    { app: "crm", kind: "action", name: "crm.account.close" },
    { app: "work", kind: "action", name: "work.task.complete" },
  ]);
  assert.deepEqual(result.function, { ref: { app: "build", kind: "function", name: "advice" }, sourceVersion: "preview.function-3" });
  assert.deepEqual(result.inlineEdit, { action: { app: "crm", kind: "action", name: "crm.account.edit" }, fields: ["name"] });
  assert.deepEqual(draft, before);
});
