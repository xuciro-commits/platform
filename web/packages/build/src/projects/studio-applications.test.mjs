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
const { applicationViewOptions, installedStudioID, installedStudioRef, pageDraftFromDefinition, studioApplications } = await import("./studio-applications.ts");

test("installed applications and drafts retain complete identities without creating native drafts", () => {
  const native = { ref: { app: "mes", kind: "app", name: "mes" }, source: "code", version: "1", application: { name: "mes", title: "MES", pages: ["shop-orders"] } };
  const published = { ref: { app: "build", kind: "app", name: "mes" }, source: "tenant", version: "app-1", application: { name: "mes", title: "My MES", pages: [] } };
  const draft = { id: "DRAFT", name: "mes", title: "My MES draft", state: "draft", pages: [] };
  const rows = studioApplications([{ id: "mes", title: "MES", version: "1" }, { id: "hidden", title: "Restricted", version: "1" }], [native, published], [draft]);
  assert.equal(rows.length, 3);
  const original = rows.find(row => row.ref.app === "mes");
  assert.deepEqual(installedStudioRef(original.id), native.ref);
  assert.equal(original.project, undefined);
  assert.equal(rows.find(row => row.ref.app === "build").id, "DRAFT");
  assert.equal(rows.find(row => row.ref.app === "hidden").definition, undefined);
});

test("published applications without local drafts remain discoverable", () => {
  const ref = { app: "build", kind: "app", name: "portable" };
  const [row] = studioApplications([], [{ ref, source: "tenant", version: "app-2", application: { name: "portable", title: "Installed", pages: [] } }], []);
  assert.equal(row.id, installedStudioID(ref));
  assert.equal(installedStudioRef("existing-project-id"), undefined);
});

test("archiving an editable draft does not hide its installed publication", () => {
  const ref = { app: "build", kind: "app", name: "portable" };
  const [row] = studioApplications([], [{ ref, source: "tenant", version: "app-2", application: { name: "portable", title: "Installed", pages: [] } }], [{ id: "DRAFT", name: "portable", archived: true }]);
  assert.equal(row.id, installedStudioID(ref));
  assert.equal(row.project, undefined);
});

test("replaceable views include declared details and retired navigation slots", () => {
  const options = applicationViewOptions({ views: [{ id: "detail", title: "Order detail" }], sections: [{ items: [{ label: "Orders", route: { view: "orders" } }] }], home: { view: "orders" }, opens: { "mes.order": "detail" } });
  assert.deepEqual(options.map(item => item.id).sort(), ["detail", "orders"]);
});

test("native page conversion preserves external owners and a separate editable snapshot", () => {
  const page = { name: "shop-orders", title: "Shop orders", layout: "list-detail", object: { app: "mes", kind: "object", name: "mes.order" }, listFields: ["product", "quantity"], detailFields: ["sfcs"], actions: [{ app: "mes", kind: "action", name: "mes.order.release" }] };
  const draft = pageDraftFromDefinition(page);
  assert.equal(draft.object, "mes.order");
  assert.deepEqual(draft.actions, ["mes.order.release"]);
  draft.list.push("status");
  assert.deepEqual(page.listFields, ["product", "quantity"]);
});

test("page conversion refuses bindings the authoring format would lose", () => {
  const page = { name: "custom", title: "Custom", layout: "composed", object: { app: "mes", kind: "object", name: "mes.order" }, sections: [{ widget: "ai-function", function: { ref: { app: "mes", kind: "function", name: "advice" }, sourceVersion: "1" } }] };
  assert.throws(() => pageDraftFromDefinition(page), /page-function-binding/);
});

test("composed conversion retains document bindings and normalizes empty optional refs", () => {
  const page = { name: "orders", title: "Orders", layout: "composed", object: { app: "mes", kind: "object", name: "mes.order" }, sections: [{ id: "orders", widget: "table", object: { app: "", kind: "", name: "" }, query: { app: "", kind: "", name: "" }, actions: [], operation: { ref: { app: "mes", kind: "compute", name: "availability" }, sourceVersion: "1" } }], document: { root: "root", nodes: { root: { kind: "rows", children: ["table"] }, table: { kind: "widget", section: "orders" } } } };
  const draft = pageDraftFromDefinition(page);
  assert.equal(draft.sections[0].object, undefined);
  assert.equal(draft.sections[0].query, undefined);
  assert.deepEqual(draft.sections[0].operation, page.sections[0].operation);
  draft.document.nodes.root.children.push("new");
  assert.deepEqual(page.document.nodes.root.children, ["table"]);
});

test("AI ordinal authoring never silently discards an exact source revision", () => {
  const page = { name: "advice", title: "Advice", layout: "composed", object: { app: "mes", kind: "object", name: "mes.order" }, sections: [{ widget: "ai-function", function: { ref: { app: "build", kind: "function", name: "advice" }, sourceVersion: "function-1" } }] };
  assert.throws(() => pageDraftFromDefinition(page), /page-function-binding/);
});
