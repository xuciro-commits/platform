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
const { studioApplicationID, studioContext, studioRoute } = await import("./studio-model.ts");
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
