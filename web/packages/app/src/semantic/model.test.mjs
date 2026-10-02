import assert from "node:assert/strict";
import test from "node:test";
import { semanticModelView } from "./model.ts";

const object = (name, fields = []) => ({ ref: { app: "sample", kind: "object", name: `sample.${name}` }, source: "code", version: "1", contractVersion: 1,
  requires: [], entity: { type: `sample.${name}`, title: name, plural: name, app: "sample", display: "title", fields, standard: [] } });

test("semantic projection omits undiscoverable targets and never mutates owner descriptors", () => {
  const parent = object("parent");
  const child = object("child", [
    { name: "parent", title: "Parent", type: "reference", ref: parent.ref.name, inverse: "children" },
    { name: "private", title: "Private", type: "reference", ref: "sample.hidden" },
  ]);
  const definitions = [parent, child], original = JSON.stringify(definitions);
  const view = semanticModelView(definitions);
  assert.equal(view.relations.length, 1);
  assert.deepEqual(view.relations[0].ref, { kind: "reference", object: child.ref, field: "parent", direction: "outbound" });
  assert.equal(view.relations[0].target.name, parent.ref.name);
  assert.equal(view.relations[0].inverse, "children");
  assert.equal(JSON.stringify(definitions), original);
});

test("usage includes exact section and selection bindings while excluding hidden refs and free text", () => {
  const parent = object("parent"), child = object("child");
  const page = { ref: { app: "sample", kind: "page", name: "desk" }, source: "tenant", version: "1", contractVersion: 1,
    requires: [parent.ref, { app: "sample", kind: "object", name: "sample.hidden" }],
    page: { name: "desk", object: parent.ref, title: "sample.unrelated", layout: "composed", listFields: [], detailFields: [], actions: [],
      sections: [{ widget: "table", object: child.ref, text: "sample.hidden" }], selections: [{ name: "line", object: child.ref }] } };
  const usages = semanticModelView([parent, child, page]).usages.filter((usage) => usage.owner.ref.kind === "page");
  assert.deepEqual(usages.map((usage) => usage.resource.name), [parent.ref.name, child.ref.name]);
});

test("registered LinkTypes replace their raw reference edge while preserving typed source identity",()=>{
 const parent={ref:{app:"sample",kind:"object",name:"sample.parent"},source:"code",entity:{fields:[]}},child={ref:{app:"sample",kind:"object",name:"sample.child"},source:"code",entity:{fields:[{name:"parent",title:"Parent",type:"reference",ref:"sample.parent",inverse:"children"}]}};
 const relation={ref:{app:"sample",kind:"link-type",name:"children"},source:"code",version:"1",linkType:{name:"children",parent:parent.ref,child:child.ref,via:"parent",forward:"declaredchildren",reverse:"declaredparent"}};
 const model=semanticModelView([parent,child,relation]);assert.equal(model.relations.length,1);assert.equal(model.relations[0].ref.kind,"link-type");assert.equal(model.relations[0].ref.binding.sourceVersion,"1");assert.equal(model.relations[0].inverse,"declaredchildren");assert.equal(semanticModelView([child,relation]).relations.length,0);
});
