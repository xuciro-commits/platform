import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
// The bundler resolves extensionless source imports; mirror that resolution in
// this source-level Node test while executing the actual production module.
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
const {PageSessionStore}=await import("./Session.ts");
const {overlaySessionScopes,selectionSlot,recordSlot,resourceVariables,recordOutputSlot,recordResourceSlot}=await import("./resources.ts");
const page={object:{app:"sample",kind:"object",name:"sample.note"},sections:[{id:"main",widget:"table"},{id:"local",widget:"table",collectionVariable:"window"},{id:"detail",widget:"detail"},{id:"other",widget:"table"}],document:{uiProfile:"platform.page.v2.11",root:"root",nodes:{root:{kind:"rows",children:["main"]},main:{kind:"widget",section:"main"},panel:{kind:"rows",children:["local","detail","cards"]},local:{kind:"widget",section:"local"},detail:{kind:"widget",section:"detail"},cards:{kind:"loop",children:[]},otherPanel:{kind:"rows",children:["other"]},other:{kind:"widget",section:"other"}},overlays:{picker:{root:"panel"},other:{root:"otherPanel"}},queries:{read:{owner:"picker"}},variables:{selected:{scope:"overlay",owner:"picker",type:"record",mode:"resource",source:{kind:"record",section:"local"}},window:{scope:"overlay",owner:"picker",type:"object-set",mode:"resource",source:{kind:"plan",query:"read"}}}}};
const tick=()=>new Promise((resolve)=>setImmediate(resolve));
const deferred=()=>{let resolve;const promise=new Promise((r)=>resolve=r);return {promise,resolve};};
test("overlay selections belong to their producer root, with explicit page fallback when no local producer exists",()=>{
 assert.equal(selectionSlot(page,page.sections[0]),"object/sample.note");assert.equal(selectionSlot(page,page.sections[1]),"overlay:picker/object/sample.note");assert.equal(selectionSlot(page,page.sections[2]),selectionSlot(page,page.sections[1]));assert.notEqual(selectionSlot(page,page.sections[3]),selectionSlot(page,page.sections[1]));
 assert.equal(selectionSlot({...page,document:{...page.document,uiProfile:"platform.page.v2.10"}},page.sections[1]),selectionSlot(page,page.sections[0]));
 const without={...page,sections:page.sections.filter((s)=>s.id!=="local")};assert.equal(selectionSlot(without,page.sections[2]),selectionSlot(page,page.sections[0]));
});
test("closing an overlay clears its windows, views, selections and item state while old identical requests cannot revive them",async()=>{
 const pending=[],root=selectionSlot(page,page.sections[0]),local=selectionSlot(page,page.sections[1]),other=selectionSlot(page,page.sections[3]);
 const source={entity:()=>({fields:[]}),get:async(_,id)=>({record:{id,revision:1}}),list:(_,query)=>{if(query.search==="root")return Promise.resolve({records:[{id:"root",revision:1}],total:1});const d=deferred();pending.push(d);return d.promise;}};
 const store=new PageSessionStore(source,{objects:new Map([[root,"sample.note"],[local,"sample.note"],[other,"sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["plan/read",new Set([local])]]),overlayScopes:overlaySessionScopes(page)});
 for(const [slot,id] of [[root,"root"],[local,"selected"],[other,"other"]])store.select(slot,{id,revision:1});await tick();
 await store.querySource("plan/main").list("sample.note",{search:"root",limit:10});
 store.setQueryView("plan/read","base",{offset:20});store.setItemScalar("cards","item","note","local draft");
 const first=store.querySource("plan/read").list("sample.note",{search:"same",limit:10});await tick();const epoch=store.overlayEpoch("picker");
 store.endOverlay("picker");assert.equal(store.overlayEpoch("picker"),epoch+1);assert.equal(store.snapshot().views["plan/read"],undefined);assert.equal(store.snapshot().queries["plan/read"],undefined);assert.equal(store.selected(local),undefined);assert.deepEqual(store.snapshot().items,{});assert.equal(store.selected(root).id,"root");assert.equal(store.selected(other).id,"other");assert.equal(store.snapshot().queries["plan/main"].value.records[0].id,"root");assert.equal(resourceVariables(page,store.snapshot()).selected.status,"empty");
 const reopened=store.querySource("plan/read").list("sample.note",{search:"same",limit:10});await tick();assert.equal(pending.length,2);
 pending[0].resolve({records:[{id:"obsolete",revision:1}],total:1});await first;assert.equal(store.snapshot().queries["plan/read"].status,"pending");
 pending[1].resolve({records:[{id:"current",revision:1}],total:1});await reopened;assert.equal(store.snapshot().queries["plan/read"].value.records[0].id,"current");
 store.dispose();
});

test("local filter resources use their owning root and retiring the root preserves page filters and plan windows",async()=>{
 const {filterSessionBindings,filterSlot,filterOwner,filtersForOwner}=await import("./resources.ts");
 const doc=structuredClone(page);doc.document.uiProfile="platform.page.v2.13";doc.sections.push({id:"pageFilter",widget:"filter"},{id:"localFilter",widget:"filter"});doc.document.nodes.pageFilter={kind:"widget",section:"pageFilter"};doc.document.nodes.root.children.push("pageFilter");doc.document.nodes.localFilter={kind:"widget",section:"localFilter"};doc.document.nodes.panel.children.unshift("localFilter");doc.sections.find(s=>s.id==="local").collectionVariable=undefined;
 doc.document.variables.localFilter={scope:"overlay",owner:"picker",type:"filter",mode:"resource",source:{kind:"filter",section:"localFilter"}};
 const root=selectionSlot(doc,doc.sections[0]),local=selectionSlot(doc,doc.sections[1]),other=selectionSlot(doc,doc.sections[3]),pending=[];
 const source={entity:()=>({fields:[{name:"active",type:"boolean"},{name:"state",type:"choice",choices:["open","done"]},{name:"hidden",type:"text"}]}),get:async(_,id)=>({record:{id,revision:1}}),list:()=>{const read=deferred();pending.push(read);return read.promise;}};
 const store=new PageSessionStore(source,{objects:new Map([[root,"sample.note"],[local,"sample.note"],[other,"sample.note"]]),children:new Map(),queryParents:new Map(),overlayScopes:overlaySessionScopes(doc),...filterSessionBindings(doc)});
 for(const [slot,id] of [[root,"page"],[local,"local"],[other,"other"]])store.select(slot,{id,revision:1});await tick();
 const owned=store.querySource("local").list("sample.note",{limit:10}),planRead=store.querySource("plan/read").list("sample.note",{limit:10});await tick();
 pending[1].resolve({records:[{id:"fixed",revision:1}],total:1});await planRead;
 store.filter("sample.note","active",false);store.select(root,{id:"page",revision:1});await tick();
 store.filter("sample.note","active",true,"picker");assert.equal(store.selected(root).id,"page");assert.equal(store.selected(other).id,"other");assert.equal(store.selected(local),undefined);assert.equal(store.snapshot().queries["plan/read"].value.records[0].id,"fixed");
 assert.deepEqual(filtersForOwner(store.snapshot().filters),{"sample.note":{active:false}});assert.deepEqual(filtersForOwner(store.snapshot().filters,"picker"),{"sample.note":{active:true}});assert.equal(filterSlot("sample.note",filterOwner(doc,doc.sections[1])),"overlay:picker/sample.note");
 assert.deepEqual(resourceVariables(doc,store.snapshot()).localFilter.value.fields,{active:true});
 store.filter("sample.note","state","undeclared","picker");store.filter("sample.note","hidden","private","picker");store.filter("sample.note","active",false,"unknown");assert.deepEqual(filtersForOwner(store.snapshot().filters,"picker"),{"sample.note":{active:true}});
 store.endOverlay("picker");assert.deepEqual(filtersForOwner(store.snapshot().filters,"picker"),{});assert.equal(store.selected(root).id,"page");assert.deepEqual(filtersForOwner(store.snapshot().filters),{"sample.note":{active:false}});
 pending[0].resolve({records:[{id:"obsolete",revision:1}],total:1});await owned;assert.equal(store.snapshot().queries.local,undefined);
 assert.equal(filterOwner({...doc,document:{...doc.document,uiProfile:"platform.page.v2.12"}},doc.sections[1]),undefined);
 store.dispose();
});

test("a shared-filter consumer's selection survives unrelated windows and is cleared by its own changed query",async()=>{
 const slot="object/sample.note",source={entity:()=>({fields:[]}),get:async(_,id)=>({record:{id,revision:1}}),list:async(_,q)=>({records:[{id:q.search,revision:1}],total:1})};
 const store=new PageSessionStore(source,{objects:new Map([[slot,"sample.note"]]),children:new Map(),queryParents:new Map(),querySelections:new Map([["shared",new Set([slot])],["fixed",new Set([slot])]])});
 await store.querySource("shared").list("sample.note",{search:"selected",limit:10});store.select(slot,{id:"selected",revision:1},"shared");await tick();
 await store.querySource("fixed").list("sample.note",{search:"other",limit:10});assert.equal(store.selected(slot).id,"selected");store.setQueryView("fixed","base",{offset:10});assert.equal(store.selected(slot).id,"selected");store.resetQueries(["fixed"]);assert.equal(store.selected(slot).id,"selected");
 await store.querySource("shared").list("sample.note",{search:"changed",limit:10});assert.equal(store.selected(slot),undefined);store.dispose();
});

test("property cache exposes only the last successful member read and clears with its source scope",async()=>{
 const ref={object:"sample.note",id:"one"};const source={scope:"member:v1",entity:()=>({fields:[{name:"active",type:"boolean"},{name:"count",type:"integer"}]}),get:async()=>({record:{id:"one",revision:1},values:{active:true,count:{kind:"decimal",value:"9007199254740993"}}}),list:async()=>({records:[],total:0})};
 const store=new PageSessionStore(source,{objects:new Map([["selected","sample.note"]]),children:new Map(),queryParents:new Map()});store.selectReference("selected",ref);await tick();assert.equal(store.property(ref,"active","boolean").value,true);assert.equal(store.property(ref,"count","decimal").value.value,"9007199254740993");assert.equal(store.property(ref,"hidden","boolean").status,"error");store.updateSource({...source,scope:"member:v2"});assert.equal(store.property(ref,"active","boolean").status,"error");store.dispose();
});

test("nested sessions isolate parent paths and retire pending windows when their parent leaves",async()=>{
 const pending=[],source={scope:"one",entity:()=>({fields:[]}),get:async()=>({record:{id:"child",revision:1}}),list:()=>{const request=deferred();pending.push(request);return request.promise;}};
 const root=new PageSessionStore(source,{objects:new Map(),children:new Map(),queryParents:new Map()});root.reconcileLoop("parents","same",["A","B"]);
 const a=root.childSession("parents","A"),b=root.childSession("parents","B");a.reconcileLoop("children","a",["shared-child"]);b.reconcileLoop("children","b",["shared-child"]);a.setItemScalar("children","shared-child","note","A draft");assert.equal(b.snapshot().items["shared-child"],undefined);assert.equal(root.childSession("parents","A"),a);
 const old=a.querySource("read").list("sample.child",{limit:3});await tick();root.reconcileLoop("parents","same",["B"]);pending[0].resolve({records:[{id:"obsolete",revision:1}],total:1});await old;assert.deepEqual(a.snapshot().queries,{});assert.equal(root.childSession("parents","B"),b);assert.notEqual(root.childSession("parents","A"),a);root.dispose();
});

test("child item evaluation inherits current outer values without sharing sibling item state",async()=>{
 const {evaluateVariables}=await import("./variables.ts");const {readFileSync}=await import("node:fs");const pageUIManifest=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url),"utf8"));
 const variables={global:{scope:"page",type:"boolean",mode:"state",initial:false},shared:{scope:"application",type:"string",mode:"shared",source:{kind:"application",variable:"label"}},local:{scope:"loop-item",owner:"children",type:"boolean",mode:"state",initial:false}};
 const values=evaluateVariables(variables,{local:true},pageUIManifest.runtime,{},"children",undefined,undefined,{global:{status:"value",value:true},shared:{status:"value",value:"current"},local:{status:"value",value:false}});
 assert.deepEqual(values.global,{status:"value",value:true});assert.deepEqual(values.shared,{status:"value",value:"current"});assert.deepEqual(values.local,{status:"value",value:true});
});

test("typed graph ports keep same IDs in different objects separate and root/overlay retirement clears original consumers",async()=>{
 const graph={id:"graph",widget:"graph-explorer",recordVariable:"selected",graphExplorer:{outputs:[{id:"sensor",object:{app:"sample",kind:"object",name:"sample.sensor"},variable:"sensorPort"},{id:"alert",object:{app:"sample",kind:"object",name:"sample.alert"},variable:"alertPort"}]}},p={...page,sections:[...page.sections,graph],document:{...page.document,variables:{...page.document.variables,sensorPort:{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:"graph",port:"sensor"}},alertPort:{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:"graph",port:"alert"}}},nodes:{...page.document.nodes,graph:{kind:"widget",section:"graph"}}}};
 const s=recordOutputSlot(p,graph,"sensor"),a=recordOutputSlot(p,graph,"alert"),parent="object/sample.note",source={scope:"member",get:async(object,id)=>({record:{id,revision:1,name:object}})},session=new PageSessionStore(source,{objects:new Map([[parent,"sample.note"],[s,"sample.sensor"],[a,"sample.alert"]]),children:new Map([[parent,new Set([s,a])]]),queryParents:new Map()});
 assert.notEqual(s,a);assert.equal(recordResourceSlot(p,"sensorPort"),s);assert.equal(recordResourceSlot(p,"alertPort"),a);await session.selectReference(s,{object:"sample.sensor",id:"SAME"});await session.selectReference(a,{object:"sample.alert",id:"SAME"});
 const resources=resourceVariables(p,session.snapshot());assert.equal(resources.sensorPort.value.reference.object,"sample.sensor");assert.equal(resources.alertPort.value.reference.object,"sample.alert");assert.equal(session.confirmedSelected(s).name,"sample.sensor");assert.equal(session.confirmedSelected(a).name,"sample.alert");
 session.select(parent,undefined);assert.equal(resourceVariables(p,session.snapshot()).sensorPort.status,"empty");assert.equal(resourceVariables(p,session.snapshot()).alertPort.status,"empty");
 const local={...p,document:{...p.document,nodes:{...p.document.nodes,panel:{kind:"rows",children:["local","graph"]}},overlays:{picker:{root:"panel"}}}};assert.ok(recordOutputSlot(local,graph,"sensor").startsWith("overlay:picker/"));assert.ok(overlaySessionScopes(local).get("picker").selections.has(recordOutputSlot(local,graph,"alert")));
});
test("observation row and asset ports retain separate typed identities and retire together with their overlay",async()=>{
 const table={id:"local",widget:"observation",collectionVariable:"samples",observation:{kind:"table",rowOutput:"row",assetOutput:"asset",assetObject:{app:"sample",kind:"object",name:"sample.asset"}}},p={...page,sections:[table],document:{...page.document,queries:{samples:{object:{app:"sample",kind:"object",name:"sample.observation"}}},variables:{samples:{scope:"overlay",owner:"picker",type:"object-set",mode:"resource",source:{kind:"plan",query:"samples"}},...Object.fromEntries(["row","asset"].map(port=>[port,{scope:"overlay",owner:"picker",type:"record",mode:"resource",source:{kind:"record",section:"local",port}}]))}}},row=recordOutputSlot(p,table,"row"),asset=recordOutputSlot(p,table,"asset"),source={scope:"member",get:async(object,id)=>({record:{id,revision:1,name:object}})},session=new PageSessionStore(source,{objects:new Map([[row,"sample.observation"],[asset,"sample.asset"]]),children:new Map([[row,new Set([asset])]]),queryParents:new Map(),overlayScopes:overlaySessionScopes(p)});
 await session.selectReference(row,{object:"sample.observation",id:"SAME"});await session.selectReference(asset,{object:"sample.asset",id:"SAME"});const values=resourceVariables(p,session.snapshot());assert.equal(values.row.value.reference.object,"sample.observation");assert.equal(values.asset.value.reference.object,"sample.asset");assert.equal(recordResourceSlot(p,"row"),row);assert.equal(recordResourceSlot(p,"asset"),asset);assert.ok(overlaySessionScopes(p).get("picker").selections.has(asset));session.endOverlay("picker");assert.equal(session.confirmedSelected(row),undefined);assert.equal(session.confirmedSelected(asset),undefined);assert.equal(resourceVariables(p,session.snapshot()).asset.status,"empty");session.dispose();
});

test("application producers have distinct confirmation slots in the multi-producer profile while legacy slots remain stable",()=>{
 const scatter={id:"scatter",widget:"record-scatter",configVersion:1,selectionVariable:"selected"},rank={id:"rank",widget:"record-leaderboard",configVersion:1,selectionVariable:"selected"};
 const page={object:{app:"sample",kind:"object",name:"sample.note"},sections:[scatter,rank],document:{formatVersion:2,uiProfile:"platform.page.v2.95",root:"root",nodes:{root:{kind:"rows",children:["scatter","rank"]},scatter:{kind:"widget",section:"scatter"},rank:{kind:"widget",section:"rank"}},variables:{local:{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:"scatter"}}}}};
 assert.notEqual(selectionSlot(page,scatter),selectionSlot(page,rank));assert.equal(recordResourceSlot(page,"local"),selectionSlot(page,scatter));assert.equal(selectionSlot(page,scatter,true),recordSlot("sample.note"));
 page.document.uiProfile="platform.page.v2.94";assert.equal(selectionSlot(page,scatter),selectionSlot(page,rank));assert.equal(selectionSlot(page,scatter),recordSlot("sample.note"));
});
