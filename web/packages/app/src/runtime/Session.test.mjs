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

test("closing an overlay discards its pending query and preserves the page selection", async () => {
  const read = deferred();
  const store = new PageSessionStore({ entity: () => ({ fields: [] }), get: async (_, id) => ({ record: record(id) }), list: () => read.promise }, plan());
  store.select("parent", record("A")); await tick();
  const pending = store.querySource("overlay-list").list("sample.child", { limit: 20 });
  store.resetQueries(["overlay-list"]);
  read.resolve({ records: [record("old")], total: 1 }); await pending;
  assert.equal(store.snapshot().queries["overlay-list"], undefined);
  assert.equal(store.selected("parent").id, "A");
  const changes = [];
  store.subscribe(() => changes.push(store.snapshot().scalars));
  store.setScalars({ first: false, second: true });
  assert.deepEqual(changes, [{ first: false, second: true }]);
});

test("loop item states follow identity through reorder and clear on removal, query or scope changes", () => {
  const source = { scope:"one", entity:()=>({fields:[]}), get:async(_,id)=>({record:record(id)}), list:async()=>({records:[],total:0}) };
  const store = new PageSessionStore(source, plan());
  store.reconcileLoop("loop", "query-a", ["A", "B"]);
  store.setItemScalar("loop", "A", "open", true);
  store.reconcileLoop("loop", "query-a", ["B", "A"]);
  assert.equal(store.itemValues("loop", "query-a", "A").open, true);
  assert.equal(store.itemValues("loop", "query-a", "B"), undefined);
  assert.equal(store.itemValues("loop", "query-b", "A"), undefined);
  store.reconcileLoop("loop", "query-a", ["B"]);
  assert.equal(store.snapshot().items.A, undefined);
  store.setItemScalar("loop", "B", "open", true);
  store.reconcileLoop("loop", "query-b", ["B"]);
  assert.equal(store.snapshot().items.B, undefined);
  store.setItemScalar("loop", "B", "open", true);
  source.scope="two";store.updateSource(source);
  assert.deepEqual(store.snapshot().items, {});
});

test("loop record readers share an authorized read and reject old-scope completion", async () => {
  const first=deferred(), second=deferred();let count=0;
  const source={scope:"one",entity:()=>({fields:[]}),list:async()=>({records:[],total:0}),get:()=>++count===1?first.promise:second.promise};
  const store=new PageSessionStore(source,plan()), reader=store.readSource();
  const old=reader.get("sample.parent","A");assert.equal(old,reader.get("sample.parent","A"));await tick();assert.equal(count,1);
  source.scope="two";store.updateSource(source);
  const current=reader.get("sample.parent","A");await tick();assert.equal(count,2);
  first.resolve({record:record("A")});await assert.rejects(old,/Obsolete/);
  second.resolve({record:record("A")});assert.equal((await current).record.id,"A");
  store.dispose();await assert.rejects(reader.get("sample.parent","B"),/ended/);
});

test("ending an overlay clears only its states atomically and invalidates the old opening", () => {
  const source = { scope: "one", entity: () => ({ fields: [] }), list: async () => ({ records: [], total: 0 }), get: async (_, id) => ({ record: record(id) }) };
  const store = new PageSessionStore(source, plan()), other = new PageSessionStore(source, plan());
  store.setScalars({ page: "shared", first: "private-a", second: "private-b", openA: true });
  const opening = store.overlayEpoch("A"), changes = [];
  store.subscribe(() => changes.push(store.snapshot().scalars));
  store.endOverlay("A"); store.setScalars({ openA: false, openB: true }, ["first"]);
  assert.deepEqual(changes, [{ page: "shared", second: "private-b", openA: false, openB: true }]);
  assert.notEqual(store.overlayEpoch("A"), opening);
  assert.equal(store.overlayEpoch("B"), 0);
  assert.deepEqual(other.snapshot().scalars, {});
  source.scope = "different-member-or-version"; store.updateSource(source);
  assert.deepEqual(store.snapshot().scalars, {});
});

test("shared plan windows own table selections and fields while views and obsolete replies are isolated",async()=>{
 const old=deferred(),source={scope:"one",entity:()=>({fields:[]}),get:async(_,id)=>({record:record(id)}),list:async(_,q)=>q.offset?{records:[record("B")],total:2}:old.promise};
 const selections=plan();selections.querySelections=new Map([["plan/read",new Set(["parent"])]]);
 const store=new PageSessionStore(source,selections);
 store.select("parent",record("A"));await tick();
 const first=store.querySource("plan/read").list("sample.parent",{offset:0,limit:1});
 store.setQueryView("plan/read","base",{offset:1});
 assert.equal(store.selected("parent"),undefined);
 const current=store.querySource("plan/read").list("sample.parent",{offset:1,limit:1});await current;
 old.resolve({records:[record("A")],total:2});await first;
 const signature=JSON.stringify(["sample.parent",{offset:1,limit:1}]);
 assert.equal(store.queryPage("plan/read",signature).records[0].id,"B");
 assert.equal(store.queryPage("plan/read",JSON.stringify(["sample.parent",{offset:0,limit:1}])),undefined);
 store.select("parent",record("B"));await tick();source.scope="two";store.updateSource(source);
 assert.deepEqual(store.snapshot().views,{});assert.equal(store.selected("parent"),undefined);
});
