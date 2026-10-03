import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if((specifier.startsWith("./")||specifier.startsWith("../"))&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
const {PageSessionStore}=await import("../runtime/Session.ts");
const {originalContextSlot,confirmedContext,avatarCollectionVariable,currentContextRead}=await import("./context-views.ts");
const section={id:"trail",widget:"breadcrumb",recordVariable:"selected"};
const page={object:{app:"sample",kind:"object",name:"sample.note"},sections:[{id:"producer",widget:"table",selection:"original"},section],document:{uiProfile:"platform.page.v2.78",root:"root",nodes:{root:{kind:"rows",children:["table","trail"]},table:{kind:"widget",section:"producer"},trail:{kind:"widget",section:"trail"}},variables:{selected:{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:"producer"}}}}};
const defer=()=>{let resolve;const promise=new Promise(r=>resolve=r);return{promise,resolve}};
const tick=()=>new Promise(r=>setImmediate(r));
test("scope changes suppress the prior confirmed source frame before passive session cleanup",async()=>{
 const slot=originalContextSlot(page,section,"selected"),source={scope:"old-permissions",get:async()=>({record:{id:"A",revision:1,note:"Old private frame"}})},session=new PageSessionStore(source,{objects:new Map([[slot,"sample.note"]]),children:new Map(),queryParents:new Map()});
 session.select(slot,{id:"A",revision:1});await tick();assert.equal(session.confirmedSelected(slot)?.note,"Old private frame");
 const next={...source,scope:"new-permissions"};assert.equal(currentContextRead(next.scope,session.snapshotScope()),false);
 session.updateSource(next);assert.equal(currentContextRead(next.scope,session.snapshotScope()),true);assert.equal(session.confirmedSelected(slot),undefined);
});
test("a mutable source scope also suppresses cached fields until the session admits its new scope",async()=>{
 const slot=originalContextSlot(page,section,"selected"),source={scope:"old",get:async()=>({record:{id:"A",revision:1,note:"Old frame"}})},session=new PageSessionStore(source,{objects:new Map([[slot,"sample.note"]]),children:new Map(),queryParents:new Map()});session.select(slot,{id:"A",revision:1});await tick();source.scope="new";
 assert.equal(session.readSource().scope,"new");assert.equal(currentContextRead(source.scope,session.snapshotScope()),false);session.updateSource(source);assert.equal(currentContextRead(source.scope,session.snapshotScope()),true);assert.equal(session.confirmedSelected(slot),undefined);
});
test("breadcrumb clears the original confirmed producer and retires late record confirmation",async()=>{
 const read=defer(),slot=originalContextSlot(page,section,"selected"),session=new PageSessionStore({scope:"owner",get:()=>read.promise},{objects:new Map([[slot,"sample.note"]]),children:new Map(),queryParents:new Map()});
 session.select(slot,{id:"A",revision:3,note:"optimistic title"});
 assert.equal(confirmedContext(page,section,"selected",session,session.snapshot()).record,undefined);
 assert.equal(confirmedContext(page,section,"selected",session,session.snapshot()).status,"pending");
 session.select(slot,undefined);read.resolve({record:{id:"A",revision:3,note:"confirmed"}});await tick();
 assert.equal(confirmedContext(page,section,"selected",session,session.snapshot()).status,"empty");
 assert.equal(session.selected(slot),undefined);
});
test("context switches original queries only on confirmed value or explicit empty and never falls back on failure",()=>{
 const config={labelField:"name",contextVariable:"selected",contextCollectionVariable:"related"};
 assert.equal(avatarCollectionVariable(config,"value","all"),"related");assert.equal(avatarCollectionVariable(config,"empty","all"),"all");
 for(const status of [undefined,"pending","error"])assert.equal(avatarCollectionVariable(config,status,"all"),undefined);
 assert.equal(avatarCollectionVariable({labelField:"name"},undefined,"all"),"all");
});
test("clearing the original parent retires its named personnel query and late window answer",async()=>{
 const read=defer(),slot=originalContextSlot(page,section,"selected"),key="plan/related",session=new PageSessionStore({scope:"owner",list:()=>read.promise},{objects:new Map([[slot,"sample.note"]]),children:new Map(),queryParents:new Map([[key,slot]])});
 const answer=session.querySource(key).list("sample.person",{limit:6,sort:["id"],domain:[["asset","=","A"]]});
 session.select(slot,undefined);read.resolve({records:[{id:"old-person",revision:1,name:"Old context"}],total:1});await answer;
 assert.equal(session.querySignature(key),undefined);assert.equal(session.snapshot().queries[key]?.status,"empty");
});
test("record scope and actual producer are required rather than state, copied resource or another Overlay",()=>{
 assert.equal(originalContextSlot(page,section,"selected"),"selection:original/sample.note");
 for(const variable of [{scope:"page",type:"record",mode:"state"},{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:"missing"}}])assert.equal(originalContextSlot({...page,document:{...page.document,variables:{selected:variable}}},section,"selected"),undefined);
 const separate={...page,document:{...page.document,overlays:{panel:{root:"trail"}}}};
 assert.equal(originalContextSlot(separate,section,"selected"),undefined);
});
