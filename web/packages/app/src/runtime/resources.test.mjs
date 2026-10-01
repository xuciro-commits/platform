import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
// The bundler resolves extensionless source imports; mirror that resolution in
// this source-level Node test while executing the actual production module.
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
const {PageSessionStore}=await import("./Session.ts");
const {overlaySessionScopes,selectionSlot,resourceVariables}=await import("./resources.ts");
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
