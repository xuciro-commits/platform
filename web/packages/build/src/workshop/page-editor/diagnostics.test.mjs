import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { registerHooks } from "node:module";
import test from "node:test";

const manifest = JSON.parse(readFileSync(new URL("../../../../app/src/pageui/widgets.json", import.meta.url)));
const app = new URL("../../../../app/src/", import.meta.url);
const ui = new URL("../../../../ui/src/", import.meta.url);
const runtime = `export {pageUIProfile,supportsPageUIProfile,widgetContract} from ${JSON.stringify(new URL("widgets/registry.ts", app).href)};
export {pageLayoutDiagnostics} from ${JSON.stringify(new URL("pages/layout.ts", app).href)};
export {parseDecimal as parsePageDecimal} from ${JSON.stringify(new URL("runtime/decimal.ts", app).href)};
export {interfaceQueryForSection} from ${JSON.stringify(new URL("runtime/ontology-interface.ts", app).href)};
import {compileVariables} from ${JSON.stringify(new URL("runtime/variables.ts", app).href)};
export const pageVariableContract=${JSON.stringify(manifest.runtime)};
export const pageVariableDiagnostics=variables=>compileVariables(variables,pageVariableContract).issues;`;
registerHooks({ resolve(specifier, context, next) {
  if (specifier === "@platform/kernel") return { url: `data:text/javascript,${encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`)}`, shortCircuit: true };
  if (specifier === "@platform/app") return { url: `data:text/javascript,${encodeURIComponent(runtime)}`, shortCircuit: true };
  // These cases exercise binding and layout ownership; rendering-only helpers
  // must not accidentally run. Their original boundary tests stay with UI.
  if (specifier === "@platform/ui") return { url: `data:text/javascript,${encodeURIComponent(`
export {validTimestampOffset,validCivilDate} from ${JSON.stringify(new URL("components/date.ts", ui).href)};
export {rangeGrid} from ${JSON.stringify(new URL("components/range.ts", ui).href)};
export {validExternalFrame} from ${JSON.stringify(new URL("components/external-frame.ts", ui).href)};
export {validStaticImage} from ${JSON.stringify(new URL("components/static-image.ts", ui).href)};
export {validSceneConfig} from ${JSON.stringify(new URL("spatial/scene-model.ts", ui).href)};
export {validChoiceInput} from ${JSON.stringify(new URL("components/choice.ts", ui).href)};
export const t=(s,v={})=>s.replace(/\\{(\\w+)\\}/g,(_,k)=>v[k]??k);
const rendering=()=>{throw Error("Unexpected rendering calculation in diagnostic case");};
export const gaugeModel=rendering,progressRatio=rendering,ganttRange=rendering;`)}`, shortCircuit: true };
  try { return next(specifier, context); }
  catch (error) {
    if (specifier.startsWith("./") || specifier.startsWith("../")) return next(`${specifier}.ts`, context);
    throw error;
  }
} });
const { pageDraftDiagnostics } = await import("./diagnostics.ts");
const context = { object: "build.order", entity: () => undefined, catalog: [], definitions: [] };
const section = (id, widget, values = {}) => ({ id, widget, configVersion: manifest.widgets.find(item => item.componentID === widget).configVersion, ...values });
const draft = sections => ({ title: "Orders", description: "", selections: [], sections, document: {
  formatVersion: 2, uiProfile: manifest.uiProfile, root: "root",
  nodes: { root: { kind: "rows", children: sections.map(item => item.id) }, ...Object.fromEntries(sections.map(item => [item.id, { kind: "widget", section: item.id }])) },
} });

test("one diagnostic source keeps valid drafts unchanged and locates previously unexplained save blockers", () => {
  const valid = draft([section("copy", "text", { text: "Orders" })]);
  assert.deepEqual(pageDraftDiagnostics(valid, context), []);
  const missing = ["external-frame", "embedded-page", "histogram", "term-counts", "tag-counts"];
  const value = draft(missing.map((widget, index) => section(`bad-${index}`, widget)));
  const before = structuredClone(value), issues = pageDraftDiagnostics(value, context);
  for (const item of value.sections) assert.ok(issues.some(issue => issue.target.kind === "widget" && issue.target.id === item.id), item.widget);
  assert.deepEqual(value, before);
});

test("each invalid group retains its own widget location instead of one page-wide flag", () => {
  const value = draft([section("first", "button-group", { buttons: [{ id: "go", title: "Go" }] }), section("second", "button-group", { buttons: [{ id: "go", title: "" }] })]);
  const groups = pageDraftDiagnostics(value, context).filter(issue => issue.code.startsWith("Give every group button"));
  assert.deepEqual(groups.map(issue => issue.target), [{ kind: "widget", id: "first" }, { kind: "widget", id: "second" }]);
});

test("scoped state checks preserve the original page or overlay owner", () => {
  const value = draft([section("check", "boolean-input", { booleanVariable: "flag" })]);
  value.document.variables = { flag: { scope: "page", type: "boolean", mode: "state", initial: false } };
  assert.deepEqual(pageDraftDiagnostics(value, context), []);
  value.document.nodes.root.children = ["modal"];
  value.document.nodes.modal = { kind: "rows", children: ["check"] };
  value.document.overlays = { dialog: { kind: "modal", root: "modal", title: "Dialog", openVariable: "open" } };
  value.document.variables.open = { scope: "page", type: "boolean", mode: "state", initial: false };
  value.document.variables.flag = { scope: "overlay", owner: "dialog", type: "boolean", mode: "state", initial: false };
  assert.ok(!pageDraftDiagnostics(value, context).some(issue => issue.code.startsWith("Bind a boolean state")));
  value.document.variables.flag.owner = "another-dialog";
  assert.ok(pageDraftDiagnostics(value, context).some(issue => issue.code.startsWith("Bind a boolean state") && issue.target.id === "check"));
});

test("shared scope checking does not erase the different input type and mutability rules", () => {
  const cases = [
    ["date-input", { dateVariable: "value" }, { type: "string", initial: "" }, "Bind the date input"],
    ["boolean-input", { booleanVariable: "value" }, { type: "boolean", initial: false }, "Bind a boolean state"],
    ["choice-input", { choiceVariable: "value", choiceInput: { variant: "select", options: ["one", "two"] } }, { type: "string", initial: "one" }, "Bind a scoped text state"],
    ["alert-banner", { alertValueVariable: "value", alertBanner: { threshold: "1", tone: "warning", message: "Check value" } }, { type: "decimal", initial: "0" }, "Bind an original decimal value"],
  ];
  for (const [widget, config, state, message] of cases) {
    const value = draft([section("input", widget, config)]);
    value.document.variables = { value: { scope: "page", mode: "state", ...state } };
    assert.ok(!pageDraftDiagnostics(value, context).some(issue => issue.code.startsWith(message)), widget);
    value.document.variables.value.mode = "constant";
    assert.equal(pageDraftDiagnostics(value, context).some(issue => issue.code.startsWith(message)), widget !== "alert-banner", widget);
    value.document.variables.value = { scope: "page", mode: "state", type: "number", initial: 0 };
    assert.ok(pageDraftDiagnostics(value, context).some(issue => issue.code.startsWith(message)), widget);
  }
});

test("profile, query, variables and selection diagnostics keep their original correction destinations", () => {
  const value = draft([section("orders", "text", { text: "Orders", selection: "missing" })]);
  value.document.uiProfile = "unknown-profile";
  value.document.queries = { invalid: { object: { app: "build", kind: "object", name: "build.order" }, limit: 0, conditions: [{ field: "", op: "=", value: "x" }] } };
  const issues = pageDraftDiagnostics(value, context);
  assert.equal(issues.filter(issue => issue.target.kind === "compatibility").length, 1);
  assert.ok(issues.some(issue => issue.target.kind === "queries"));
  assert.ok(issues.some(issue => issue.code.startsWith("A widget references") && issue.target.id === "orders"));
  value.selections = [{ name: "same", object: { name: "build.order" } }, { name: "same", object: { name: "build.order" } }];
  assert.ok(pageDraftDiagnostics(value, context).some(issue => issue.code.startsWith("Selection names") && issue.target.kind === "page"));
});

test("original widget owners still reject foreign metadata and shared outputs retain component locations", () => {
  const value = draft([section("foreign", "text", { text: "Orders", actionTable: { parameters: {} } }), section("selection", "record-list", { selectionSetVariable: "selected" })]);
  value.document.variables = { selected: { type: "record-set", scope: "application", mode: "shared", writable: true, source: { kind: "application", object: { app: "build", kind: "object", name: "build.order" } } } };
  const issues = pageDraftDiagnostics(value, context);
  assert.ok(issues.some(issue => issue.target.id === "foreign" && issue.code.startsWith("Bind the original row action")));
  assert.ok(issues.some(issue => issue.target.id === "selection" && issue.code.startsWith("Bind shared record-set output")));
});

test("inline editing uses current model defaults and still rejects fields outside explicit display bindings", () => {
  const value = draft([section("items", "table", { model: { type: "build.order" }, inlineEdit: { action: "build.order.edit", fields: ["name", "note"] } })]);
  const info = { type: "build.order", fields: [{ name: "name", type: "text" }, { name: "note", type: "text" }] };
  const current = { ...context, entity: type => type === info.type ? info : undefined };
  const editingIssue = () => pageDraftDiagnostics(value, current).some(issue => issue.code.startsWith("Inline table editing"));
  assert.equal(editingIssue(), false);
  value.sections[0].model.fields = [{ name: "name" }];
  assert.equal(editingIssue(), true);
});
