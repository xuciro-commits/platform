import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
import assert from "node:assert/strict";
import test from "node:test";
const { PageSessionStore }=await import("./Session.ts");
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };
const record = (id) => ({ id, revision: 1, note: id });
const plan = () => ({ objects: new Map([["parent", "sample.parent"], ["child", "sample.child"]]), children: new Map([["parent", new Set(["child"])]]), queryParents: new Map([["children", "parent"]]) });
const tick = () => new Promise((resolve) => setImmediate(resolve));
test("record sets confirm authorized window members, preserve producer isolation and retire failed or obsolete reads",async()=>{
 const reads=new Map(),source={scope:"member",revision:1,entity:()=>({fields:[]}),list:async()=>({records:[record("A"),record("B")],total:2}),get:(_type,id)=>{const read=deferred();reads.set(id,read);return read.promise;}},p={objects:new Map([["set","sample.note"],["other","sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["read",new Set(["set"])],["other-read",new Set(["other"])]])},store=new PageSessionStore(source,p);
 await store.querySource("read").list("sample.note",{limit:2});await store.querySource("other-read").list("sample.note",{limit:2});store.selectSet("set",["hidden"],"read");assert.equal(store.snapshot().recordSets.set,undefined);
 store.selectSet("set",["A","B"],"read");assert.equal(store.snapshot().recordSets.set.status,"pending");assert.deepEqual(store.selectedSet("set"),[]);await tick();reads.get("A").resolve({record:record("A")});reads.get("B").resolve({record:record("B")});await tick();assert.equal(store.snapshot().recordSets.set.status,"value");assert.deepEqual(store.snapshot().recordSets.set.value,[{object:"sample.note",id:"A"},{object:"sample.note",id:"B"}]);assert.equal(store.snapshot().recordSets.set.value[0].note,undefined);
 store.selectSet("other",["A"],"other-read");await tick();store.setQueryView("read","base",{search:"new"});assert.equal(store.snapshot().recordSets.set.status,"empty");assert.equal(store.snapshot().recordSets.other.status,"value");
 store.updateSource({...source,revision:2});await tick();assert.equal(store.snapshot().recordSets.other.status,"pending");assert.deepEqual(store.selectedSet("other"),[]);store.resetQueries(["other-read"]);reads.get("A").resolve({record:{...record("A"),note:"late"}});await tick();assert.equal(store.snapshot().recordSets.other.status,"empty");
 await store.querySource("read").list("sample.note",{limit:2});store.selectSet("set",["B"],"read");await tick();reads.get("B").reject(Error("Denied"));await tick();assert.equal(store.snapshot().recordSets.set.status,"error");assert.deepEqual(store.selectedSet("set"),[]);
});
test("record sets enforce declared query ownership, budget, current membership and actor retirement",async()=>{
 const source={scope:"one",revision:1,entity:()=>({fields:[]}),get:async(_type,id)=>({record:record(id)}),list:async()=>({records:[record("A"),record("B")],total:2})},p={maxSelectionSetRecords:1,objects:new Map([["set","sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["read",new Set(["set"])]] )},store=new PageSessionStore(source,p);
 await store.querySource("read").list("sample.note",{limit:2});await store.querySource("wrong").list("sample.note",{limit:2});store.selectSet("set",["A"],"wrong");store.selectSet("set",["A","B"],"read");store.selectSet("set",["A","A"],"read");assert.equal(store.snapshot().recordSets.set,undefined);
 store.selectSet("set",["A"],"read");await tick();assert.equal(store.snapshot().recordSets.set.status,"value");store.updateSource({...source,scope:"two"});assert.equal(store.snapshot().recordSets.set.status,"empty");assert.deepEqual(store.selectedSet("set"),[]);store.selectSet("set",["A"],"read");assert.equal(store.snapshot().recordSets.set.status,"empty");
 store.reconcileExternalWindow("read","external",["A"]);store.selectSet("set",["A"],"read");await tick();assert.equal(store.snapshot().recordSets.set.status,"value");store.reconcileExternalWindow("read","external",undefined);assert.equal(store.snapshot().recordSets.set.status,"empty");assert.deepEqual(store.selectedSet("set"),[]);
});
test("disposing and reactivating a session cannot restore an earlier identical record-set read",async()=>{
 const old=deferred(),next=deferred();let calls=0;const source={scope:"one",entity:()=>({fields:[]}),get:()=>++calls===1?old.promise:next.promise,list:async()=>({records:[record("A")],total:1})},p={objects:new Map([["set","sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["read",new Set(["set"])]])},store=new PageSessionStore(source,p);
 await store.querySource("read").list("sample.note",{limit:1});store.selectSet("set",["A"],"read");await tick();store.dispose();store.activate();await store.querySource("read").list("sample.note",{limit:1});store.selectSet("set",["A"],"read");await tick();old.resolve({record:{...record("A"),note:"obsolete"}});await tick();assert.equal(store.snapshot().recordSets.set.status,"pending");next.resolve({record:{...record("A"),note:"current"}});await tick();assert.equal(store.selectedSet("set")[0].note,"current");
});

test("a revision during an uncached reference read retries that reference and rejects the older response",async()=>{
 const old=deferred(),current=deferred();let calls=0;
 const source={scope:"member-a",revision:1,entity:()=>({fields:[{name:"active",type:"boolean"}]}),get:()=>{calls++;return old.promise;},list:async()=>({records:[],total:0})};
 const store=new PageSessionStore(source,plan());
 store.selectReference("parent",{object:"sample.parent",id:"A"});assert.equal(store.snapshot().records.parent.status,"pending");
 store.updateSource({...source,revision:2,get:()=>{calls++;return current.promise;}});assert.equal(calls,2);
 old.resolve({record:record("A"),values:{active:false}});await tick();assert.equal(store.snapshot().records.parent.status,"pending");
 current.resolve({record:{...record("A"),revision:2},values:{active:true}});await tick();assert.equal(store.snapshot().records.parent.status,"value");assert.equal(store.property({object:"sample.parent",id:"A"},"active","boolean").value,true);
});

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

test("a synchronous window subscriber joins the installed query before pending is published",async()=>{
 let reads=0,alias,observed=false;
 const store=new PageSessionStore({scope:"member:1",entity:()=>({fields:[]}),get:async()=>({record:record("one")}),list:async()=>{reads++;return {records:[record("one")],total:1};}},plan()),source=store.querySource("window"),query={limit:1};
 store.subscribe(()=>{if(!observed&&store.snapshot().queries.window?.status==="pending"){observed=true;alias=source.list("sample.parent",query);}});
 const original=source.list("sample.parent",query);await original;await alias;
 assert.equal(reads,1);assert.equal(original,alias);assert.equal(store.snapshot().queries.window.value.total,1);
});

test("recreating a scoped source wrapper preserves one read until its revision changes",async()=>{
 let reads=0;
 const source={scope:"member-and-definitions:1",revision:0,entity:()=>({fields:[]}),get:async()=>({record:record("one")}),list:async()=>{reads++;return {records:[record("one")],total:1};}};
 const store=new PageSessionStore(source,plan()),reader=store.querySource("window"),query={limit:1};
 await reader.list("sample.parent",query);store.updateSource({...source});await reader.list("sample.parent",query);assert.equal(reads,1);
 store.updateSource({...source,revision:1});await reader.list("sample.parent",query);assert.equal(reads,2);
});

test("paired range Clear publishes one original snapshot and member replacement retires both drafts",()=>{
 const source={scope:"one",entity:()=>({fields:[]}),get:async(_type,id)=>({record:record(id)}),list:async()=>({records:[],total:0})},store=new PageSessionStore(source,plan());store.setScalars({lower:"10",upper:"20"});const snapshots=[];store.subscribe(()=>snapshots.push({...store.snapshot().scalars}));store.setScalars({lower:"",upper:""});assert.deepEqual(snapshots,[{lower:"",upper:""}]);store.setScalars({lower:"10",upper:"20"});store.updateSource({...source,scope:"two"});assert.equal(store.snapshot().scalars.lower,undefined);assert.equal(store.snapshot().scalars.upper,undefined);
});
test("stable picker IDs require a current window and confirmed authorized identity; obsolete confirmations never return",async()=>{
 const reads=new Map(),source={scope:"member",revision:1,entity:()=>({fields:[]}),list:async()=>({records:[record("A"),record("B")],total:2}),get:(_type,id)=>{const r=deferred();reads.set(id,r);return r.promise;}},p={objects:new Map([["pick","sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["read",new Set(["pick"])]] )},store=new PageSessionStore(source,p);
 assert.equal(await store.confirmSelection("pick",record("A"),"read"),undefined);await store.querySource("read").list("sample.note",{limit:20});assert.equal(await store.confirmSelection("pick",record("hidden"),"read"),undefined);assert.equal(await store.confirmSelection("pick",record("A"),"wrong"),undefined);
 const old=store.confirmSelection("pick",record("A"),"read");assert.equal(store.selected("pick"),undefined);assert.equal(store.snapshot().records.pick.status,"pending");const next=store.confirmSelection("pick",record("B"),"read");reads.get("A").resolve({record:record("A")});assert.equal(await old,undefined);reads.get("B").resolve({record:record("B")});assert.equal((await next).id,"B");assert.equal(store.selected("pick").id,"B");
 for(const result of [{record:record("wrong")},{record:{...record("A"),archived:true}},Error("Denied")]){const request=store.confirmSelection("pick",record("A"),"read");result instanceof Error?reads.get("A").reject(result):reads.get("A").resolve(result);assert.equal(await request,undefined);assert.equal(store.selected("pick"),undefined);assert.equal(store.snapshot().records.pick.status,"error");}
 const changed=store.confirmSelection("pick",record("A"),"read");store.setQueryView("read","base",{search:"different"});reads.get("A").resolve({record:record("A")});assert.equal(await changed,undefined);
 await store.querySource("read").list("sample.note",{limit:20});const scope=store.confirmSelection("pick",record("A"),"read");store.updateSource({...source,scope:"other"});reads.get("A").resolve({record:record("A")});assert.equal(await scope,undefined);assert.equal(store.selected("pick"),undefined);
});
test("closing a picker overlay retires its pending identity confirmation while scalar drafts remain independent",async()=>{
 const read=deferred(),source={entity:()=>({fields:[]}),get:()=>read.promise,list:async()=>({records:[record("A")],total:1})},p={objects:new Map([["pick","sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["read",new Set(["pick"])]]),overlayScopes:new Map([["panel",{queries:new Set(["read"]),selections:new Set(["pick"])}]])},store=new PageSessionStore(source,p);await store.querySource("read").list("sample.note",{limit:20});store.setScalar("personID","legacy name");const pending=store.confirmSelection("pick",record("A"),"read");store.endOverlay("panel");read.resolve({record:record("A")});assert.equal(await pending,undefined);assert.equal(store.selected("pick"),undefined);assert.equal(store.snapshot().scalars.personID,"legacy name");
});
test("confirmed record access excludes optimistic publications and is retired by failure, later selection and scope changes",async()=>{
 const reads=new Map(),source={scope:"one",entity:()=>({fields:[]}),get:(_type,id)=>{const d=deferred();reads.set(id,d);return d.promise;},list:async()=>({records:[],total:0})},store=new PageSessionStore(source,plan()),observed=[];
 store.subscribe(()=>{if(store.snapshot().records.parent?.status==="value")observed.push(store.confirmedSelected("parent")?.note);});store.select("parent",{...record("A"),note:"optimistic"});assert.equal(store.selected("parent").note,"optimistic");assert.equal(store.confirmedSelected("parent"),undefined);await tick();reads.get("A").resolve({record:{...record("A"),note:"authorized"}});await tick();assert.deepEqual(observed,[undefined,"authorized"]);assert.equal(store.confirmedSelected("parent").note,"authorized");
 store.select("parent",record("B"));assert.equal(store.confirmedSelected("parent"),undefined);await tick();reads.get("B").reject(new Error("Denied"));await tick();assert.equal(store.confirmedSelected("parent"),undefined);store.select("parent",record("A"));await tick();const old=reads.get("A");store.select("parent",undefined);old.resolve({record:{...record("A"),note:"obsolete"}});await tick();assert.equal(store.confirmedSelected("parent"),undefined);
 store.select("parent",record("C"));await tick();reads.get("C").resolve({record:record("C")});await tick();assert.equal(store.confirmedSelected("parent").id,"C");store.dispose();assert.equal(store.confirmedSelected("parent"),undefined);
});

test("confirmed record leases retire bound collaboration states on selection and query clearing while data refresh retains the captured target",async()=>{
 const source={scope:"one",revision:1,entity:()=>({fields:[]}),get:async(_type,id)=>({record:record(id)}),list:async()=>({records:[record("A"),record("B")],total:2})},p={objects:new Map([["chosen","sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["read",new Set(["chosen"])]]),recordScalars:new Map([["chosen",new Map([["draft",""],["file",""],["pdfPage","1"]])]])},store=new PageSessionStore(source,p);
 await store.querySource("read").list("sample.note",{limit:2});store.select("chosen",record("A"),"read");assert.equal(store.captureRecordLease("chosen"),undefined);await tick();const current=store.captureRecordLease("chosen");assert.ok(current?.());store.setScalars({draft:"kept",file:"FILE",pdfPage:"2"});store.updateSource({...source,revision:2});assert.equal(current(),true);await tick();assert.equal(current(),true);store.select("chosen",record("B"),"read");assert.equal(current(),false);assert.deepEqual(store.snapshot().scalars,{draft:"",file:"",pdfPage:"1"});await tick();const next=store.captureRecordLease("chosen");assert.ok(next?.());store.setScalars({draft:"obsolete",file:"OLD"});store.resetQueries(["read"]);assert.equal(next(),false);assert.equal(store.snapshot().scalars.draft,"");assert.equal(store.snapshot().scalars.file,"");store.dispose();store.activate();assert.equal(current(),false);
});
test("same-ID rebinding advances the collaboration epoch and retires the old lease while data revisions keep the attempt scope",async()=>{
 const source={scope:"member",revision:1,entity:()=>({fields:[]}),get:async(_type,id)=>({record:record(id)}),list:async()=>({records:[record("A")],total:1})},p={objects:new Map([["chosen","sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["read",new Set(["chosen"])]]),recordScalars:new Map([["chosen",new Map([["draft",""],["file",""],["pdfPage","1"]])]])},store=new PageSessionStore(source,p);
 assert.equal(store.recordBindingEpoch("chosen"),0);await store.querySource("read").list("sample.note",{limit:1});store.select("chosen",record("A"),"read");await tick();const previous=store.captureRecordLease("chosen"),epoch=store.recordBindingEpoch("chosen");assert.ok(previous?.());store.setScalars({draft:"Original draft",file:"FILE",pdfPage:"2"});
 store.updateSource({...source,revision:2});assert.equal(store.recordBindingEpoch("chosen"),epoch);assert.ok(previous());await tick();assert.equal(store.recordBindingEpoch("chosen"),epoch);assert.ok(previous());
 store.select("chosen",record("A"),"read");assert.ok(store.recordBindingEpoch("chosen")>epoch);assert.equal(previous(),false);assert.equal(store.confirmedSelected("chosen"),undefined);assert.deepEqual(store.snapshot().scalars,{draft:"",file:"",pdfPage:"1"});await tick();assert.equal(store.confirmedSelected("chosen").id,"A");const current=store.captureRecordLease("chosen"),rebound=store.recordBindingEpoch("chosen");assert.ok(current?.());store.resetQueries(["read"]);assert.ok(store.recordBindingEpoch("chosen")>rebound);assert.equal(current(),false);
});
