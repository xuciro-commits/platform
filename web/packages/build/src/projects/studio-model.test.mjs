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
const { assetRoute, resourceInArea, studioActionGroups, studioApplicationID, studioAreaForRoute, studioContext, studioResourceGroups, studioRoute } = await import("./studio-model.ts");
const { refKey, resourceKinds, resourceRef } = await import("./resources.ts");

const objectKind = resourceKinds.find(kind => kind.kind === "object");
const flowKind = resourceKinds.find(kind => kind.kind === "flow");
const pageKind = resourceKinds.find(kind => kind.kind === "page");
const object = { kind: objectKind, record: { id: "SAME", name: "order", title: "Order" }, ref: resourceRef(objectKind, "order"), title: "Order", installed: true, missing: false };
const flow = { kind: flowKind, record: { id: "SAME", name: "delivery", title: "Delivery" }, ref: resourceRef(flowKind, "delivery"), title: "Delivery", installed: true, missing: false };
const page = { kind: pageKind, record: { id: "PAGE", name: "orders", title: "Orders" }, ref: resourceRef(pageKind, "orders"), title: "Orders", installed: true, missing: false };
const resources = [flow, object, page];

test("editor view distinguishes object and flow records with the same ID", () => {
  const model = studioContext({ view: "object-type", params: { id: "SAME" } }, resources, []);
  assert.equal(model.selected, object);
  assert.deepEqual(model.ref, object.ref);
  assert.equal(model.area, "model");
  const assembly = studioContext({ view: "flow", params: { id: "SAME" } }, [object, flow, page], []);
  assert.equal(assembly.selected, flow);
  assert.deepEqual(assembly.ref, flow.ref);
  assert.equal(assembly.area, "assemble");
});

test("action editor selects its owning object and derives the original action reference", () => {
  const context = studioContext({ view: "action-type", params: { id: "SAME", action: "approve", application: "APP" } }, resources, []);
  assert.equal(context.selected, object);
  assert.deepEqual(context.ref, { app: "build", kind: "action", name: "build.order.approve" });
  assert.equal(context.area, "behavior");
});

test("automation keeps its asset in both process editors and a flow release review", () => {
  const automation = { ...flow, kind: resourceKinds.find(kind => kind.kind === "automation"), record: { ...flow.record, kind: "automation" } };
  for (const view of ["flow", "automation"]) {
    const context = studioContext({ view, params: { id: "SAME", application: "APP" } }, [object, automation], []);
    assert.equal(context.selected, automation);
    assert.deepEqual(context.ref, automation.ref);
    assert.equal(context.area, "assemble");
  }
  const review = studioContext({ view: "release-review", params: { id: "SAME", kind: "flow", application: "APP" } }, [object, automation], []);
  assert.equal(review.selected, automation);
  assert.deepEqual(studioRoute("APP", review.area, refKey(review.selected.ref)), { view: "project", params: { id: "APP", area: "verify", asset: refKey(automation.ref) } });
});

test("definition and page preview resolve the same complete owner reference", () => {
  const definition = { ref: page.ref, page: { title: "Orders" } };
  const otherOwner = { ref: { ...page.ref, app: "other" }, page: { title: "Other orders" } };
  for (const view of ["definition", "page-preview"]) {
    const context = studioContext({ view, params: { ...page.ref, application: "APP" } }, resources, [otherOwner, definition]);
    assert.equal(context.selected, page);
    assert.equal(context.ref, definition.ref);
    assert.equal(context.area, "interface");
  }
});

test("project asset selection retains its explicit workspace area", () => {
  const route = studioRoute("APP", "behavior", refKey(object.ref));
  const context = studioContext(route, resources, []);
  assert.equal(studioApplicationID(route), "APP");
  assert.equal(context.selected, object);
  assert.deepEqual(context.ref, object.ref);
  assert.equal(context.area, "behavior");
});

test("release review returns the original asset to Verify instead of its editing area", () => {
  const route = { view: "release-review", params: { id: "SAME", kind: "object", application: "APP" } };
  const context = studioContext(route, resources, []);
  assert.equal(context.selected, object);
  assert.equal(context.area, "verify");
  const back = studioRoute(studioApplicationID(route), context.area, refKey(context.selected.ref));
  assert.deepEqual(back, { view: "project", params: { id: "APP", area: "verify", asset: refKey(object.ref) } });
  const returned = studioContext(back, resources, []);
  assert.equal(returned.selected, object);
  assert.equal(returned.area, "verify");
  const history = studioContext({ view: "release-history", params: { application: "APP" } }, resources, []);
  assert.equal(history.selected, undefined);
  assert.deepEqual(studioRoute("APP", history.area), { view: "project", params: { id: "APP", area: "verify" } });
});

test("decision table release review resolves its canonical compute reference", () => {
  const kind = resourceKinds.find(kind => kind.kind === "table");
  const table = { kind, record: { id: "SAME", name: "prices" }, ref: resourceRef(kind, "prices"), title: "Prices", installed: true, missing: false };
  const context = studioContext({ view: "release-review", params: { id: "SAME", kind: "compute", application: "APP" } }, [object, table], []);
  assert.equal(context.selected, table);
  assert.deepEqual(context.ref, table.ref);
  assert.equal(context.area, "verify");
});

test("application test-plan route retains its selected asset in Verify", () => {
  const kind = resourceKinds.find(kind => kind.kind === "test-plan");
  const plan = { kind, record: { id: "PLAN", name: "shipping" }, ref: resourceRef(kind, "shipping"), title: "Shipping", installed: false, missing: false };
  const route = assetRoute(plan.ref, [plan], [], "APP");
  assert.deepEqual(route, { view: "project", params: { id: "APP", area: "verify", plan: "PLAN" } });
  const context = studioContext(route, [object, plan], []);
  assert.equal(context.selected, plan);
  assert.deepEqual(context.ref, plan.ref);
  assert.equal(context.area, "verify");
});

test("published observation opens and returns with its complete owner reference", () => {
  const kind = resourceKinds.find(kind => kind.kind === "observation");
  const observation = { kind, ref: { app: "other", kind: "observation", name: "temperature" }, title: "Temperature", installed: true, missing: false };
  const wrongOwner = { ref: { ...observation.ref, app: "build" } };
  const definition = { ref: observation.ref };
  const route = assetRoute(observation.ref, [observation], [wrongOwner, definition], "APP");
  const context = studioContext(route, [observation], [wrongOwner, definition]);
  assert.equal(context.selected, observation);
  assert.equal(context.ref, definition.ref);
  assert.equal(context.area, "assemble");
  assert.deepEqual(studioRoute("APP", context.area, refKey(context.selected.ref)), { view: "project", params: { id: "APP", area: "assemble", asset: "other/observation/temperature" } });
});

test("workspace groups use business categories and preserve custom concept titles", () => {
  const queryKind = resourceKinds.find(kind => kind.kind === "query");
  const query = { kind: queryKind, ref: resourceRef(queryKind, "openorders"), title: "Open orders", installed: true, missing: false };
  const otherObject = { ...object, ref: { ...object.ref, name: "build.customer" }, title: "Customer" };
  const application = { name: "delivery", title: "Delivery", concepts: [{ id: "fulfilment", title: "订单交付 / Fulfilment", assets: [object.ref, page.ref] }] };
  const all = [flow, page, query, object, otherObject];
  const modelGroups = studioResourceGroups(application, "model", all.filter(item => resourceInArea(item, "model")));
  assert.deepEqual(modelGroups.map(group => [group.id, group.title, group.items.map(item => item.ref.name)]), [
    ["concept:fulfilment", "订单交付 / Fulfilment", [object.ref.name]],
    ["business-model", "Business model", [otherObject.ref.name]],
    ["queries", "Queries", [query.ref.name]],
  ]);
  const interfaceGroups = studioResourceGroups(application, "interface", all.filter(item => resourceInArea(item, "interface")));
  assert.deepEqual(interfaceGroups.map(group => [group.id, group.title, group.items]), [["concept:fulfilment", "订单交付 / Fulfilment", [page]]]);
  const assemblyGroups = studioResourceGroups(application, "assemble", all.filter(item => resourceInArea(item, "assemble")));
  assert.deepEqual(assemblyGroups.map(group => [group.id, group.items]), [["automation", [flow]]]);
});

test("actions group by declared target and complete owner while unmatched actions stay visible", () => {
  const nativeObject = { ...object, record: undefined, ref: { app: "mes", kind: "object", name: "mes.order" } };
  const sameNameOtherOwner = { ...nativeObject, ref: { ...nativeObject.ref, app: "external" }, title: "External order" };
  const taskObject = { ...nativeObject, ref: { ...nativeObject.ref, name: "mes.task" }, title: "Task" };
  const action = (app, name) => ({ kind: { kind: "action" }, ref: { app, kind: "action", name }, title: name, installed: true, missing: false });
  const release = action("mes", "release");
  const misleadingPrefix = action("mes", "mes.order.route");
  const otherOwner = action("external", "release");
  const hiddenTarget = action("mes", "finish");
  const noTarget = action("mes", "refresh");
  const unreadable = action("mes", "unknown");
  const definitions = [
    { ref: release.ref, action: { target: nativeObject.ref.name } },
    { ref: misleadingPrefix.ref, action: { target: taskObject.ref.name } },
    { ref: otherOwner.ref, action: { target: sameNameOtherOwner.ref.name } },
    { ref: hiddenTarget.ref, action: { target: "mes.unavailable" } },
    { ref: noTarget.ref, action: {} },
  ];
  const grouped = studioActionGroups([nativeObject, sameNameOtherOwner, taskObject, release, misleadingPrefix, otherOwner, hiddenTarget, noTarget, unreadable], definitions);
  assert.deepEqual(grouped.groups.map(group => [refKey(group.object.ref), group.actions.map(item => refKey(item.ref))]), [
    ["mes/object/mes.order", ["mes/action/release"]],
    ["external/object/mes.order", ["external/action/release"]],
    ["mes/object/mes.task", ["mes/action/mes.order.route"]],
  ]);
  assert.deepEqual(grouped.other, [hiddenTarget, noTarget, unreadable]);
});

test("editor and read-only routes choose the right workspace without a selected draft", () => {
  for (const [route, area] of [
    [{ view: "project", params: { area: "verify" } }, "verify"],
    [{ view: "project", params: { area: "unknown" } }, "model"],
    [{ view: "module" }, "interface"],
    [{ view: "page-preview" }, "interface"],
    [{ view: "definition", params: { kind: "page" } }, "interface"],
    [{ view: "definition", params: { kind: "query" } }, "model"],
    [{ view: "definition", params: { kind: "action" } }, "behavior"],
    [{ view: "definition", params: { kind: "compute" } }, "behavior"],
    [{ view: "definition", params: { kind: "protocol" } }, "assemble"],
    [{ view: "action-type" }, "behavior"],
    [{ view: "decision-table" }, "behavior"],
    [{ view: "link-type" }, "model"],
    [{ view: "observations" }, "assemble"],
    [{ view: "flow" }, "assemble"],
    [{ view: "release-review", params: { kind: "object" } }, "verify"],
    [{ view: "release-history" }, "verify"],
    [{ view: "test-plan" }, "verify"],
    [undefined, "model"],
  ]) assert.equal(studioAreaForRoute(route), area, JSON.stringify(route));
});
