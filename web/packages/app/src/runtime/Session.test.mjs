import assert from "node:assert/strict";
import test from "node:test";
import { PageSessionStore } from "./Session.ts";
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };
const record = (id) => ({ id, revision: 1, note: id });
const plan = () => ({ objects: new Map([["parent", "sample.parent"], ["child", "sample.child"]]), children: new Map([["parent", new Set(["child"])]]), queryParents: new Map([["children", "parent"]]) });
const tick = () => new Promise((resolve) => setImmediate(resolve));

test("changing a parent invalidates descendant references, cached fields and late reads", async () => {
  const reads = new Map();
  const store = new PageSessionStore({ entity: () => ({ fields: [{name:"active",type:"boolean"}] }), list: async () => ({ records: [], total: 0 }), get: (object, id) => {
    const read = deferred(); reads.set(id, read); return read.promise;
  } }, plan());
  store.select("parent", record("A")); store.select("child", record("A-child"));
  store.select("parent", record("B"));
  reads.get("B").resolve({ record: record("B") });
  reads.get("A").resolve({ record: record("A") }); reads.get("A-child").resolve({ record: record("A-child") });
  await tick();
  assert.deepEqual(store.snapshot().records.parent, { status: "value", value: { object: "sample.parent", id: "B" } });
  assert.equal(store.snapshot().records.child.status, "empty");
  assert.equal(store.selected("child"), undefined);
  assert.equal(store.selected("parent").id, "B");
  assert.equal(store.snapshot().records.parent.value.note, undefined);
  store.dispose();
  assert.deepEqual(store.snapshot().records, {});
});

test("query windows coalesce in flight and cannot republish an obsolete parameter set", async () => {
  const requests = [];
  const store = new PageSessionStore({ entity: () => ({ fields: [{name:"active",type:"boolean"}] }), get: async (_, id) => ({ record: record(id) }), list: (_, query) => {
    const read = deferred(); requests.push({ ...read, query }); return read.promise;
  } }, plan());
  const source = store.querySource("children");
  const first = source.list("sample.child", { search: "old", offset: 0, limit: 2 });
  assert.equal(first, source.list("sample.child", { search: "old", offset: 0, limit: 2 }));
  const second = source.list("sample.child", { search: "new", offset: 0, limit: 2 });
  await tick(); assert.equal(requests.length, 2);
  requests[1].resolve({ records: [record("new")], total: 10 }); await second;
  requests[0].resolve({ records: [record("old")], total: 1 }); await first;
  assert.equal(store.snapshot().queries.children.value.records[0].id, "new");
  assert.equal(store.snapshot().queries.children.value.complete, false);
  assert.equal(store.snapshot().queries.children.value.total, 10);
  assert.deepEqual(store.snapshot().queries.children.value.query, { search: "new", offset: 0, limit: 2 });
});

test("filters invalidate reads immediately and scope caches to their page session", async () => {
  const read = deferred(), source = { entity: () => ({ fields: [{name:"active",type:"boolean"}] }), get: async (_, id) => ({ record: record(id) }), list: () => read.promise };
  const one = new PageSessionStore(source, plan()), two = new PageSessionStore(source, plan());
  one.select("parent", record("A")); one.select("child", record("C")); await tick();
  const query = one.querySource("children").list("sample.child", { domain: [["parent", "=", "A"]] });
  one.filter("sample.parent", "active", true);
  assert.equal(one.snapshot().records.child.status, "empty");
  assert.equal(one.snapshot().queries.children.status, "empty");
  read.resolve({ records: [record("C")], total: 1 }); await query;
  assert.equal(one.snapshot().queries.children.status, "empty");
  assert.deepEqual(two.snapshot().filters, {});
  one.filter("sample.parent", "active", undefined);
  assert.deepEqual(one.snapshot().filters["sample.parent"], {});
});

test("read denial clears dependent records and source revisions invalidate cached query windows", async () => {
  const source = { revision: 1, entity: () => ({ fields: [{name:"active",type:"boolean"}] }), get: async (_, id) => ({ record: record(id) }), list: async () => ({ records: [], total: 0 }) };
  const store = new PageSessionStore(source, plan());
  store.select("parent", record("A")); store.select("child", record("C")); await tick();
  await store.querySource("children").list("sample.child", { limit: 100 });
  assert.equal(store.snapshot().queries.children.value.complete, true);
  source.revision = 2;
  source.get = async () => { throw new Error("Denied"); };
  store.updateSource(source); await tick();
  assert.equal(store.snapshot().records.parent.status, "error");
  assert.equal(store.selected("child"), undefined);
  assert.equal(store.snapshot().queries.children.status, "empty");
});

test("a changed member/definition scope clears values and discards the previous scope's responses", async () => {
  const read = deferred();
  const source = { scope: "member-a", entity: () => ({ fields: [{ name: "active", type: "boolean" }] }), get: () => read.promise, list: async () => ({ records: [], total: 0 }) };
  const store = new PageSessionStore(source, plan());
  store.setScalar("tab", "private-tab"); store.filter("sample.parent", "active", true); store.select("parent", record("A"));
  source.scope = "member-b"; store.updateSource(source);
  read.resolve({ record: record("A") }); await tick();
  assert.equal(store.selected("parent"), undefined);
  assert.equal(store.snapshot().records.parent.status, "empty");
  assert.deepEqual(store.snapshot().scalars, {}); assert.deepEqual(store.snapshot().filters, {});
});
