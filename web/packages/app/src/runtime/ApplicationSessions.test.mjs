import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
const {ApplicationSessionHub}=await import("./ApplicationSessions.ts");

test("application state follows one identity and instance, and retired handles cannot write a reopened instance", () => {
  const variables = { condition: { scope: "application", type: "boolean", mode: "state", initial: false }, label: { scope: "application", type: "string", mode: "constant", initial: "read only" } };
  const hub = new ApplicationSessionHub(), first = hub.get("member:app:v1:one", variables), second = hub.get("member:app:v1:two", variables);
  const a = Symbol(), b = Symbol(); let closed = 0;
  first.attach(a, () => closed++); first.attach(b, () => closed++); second.attach(Symbol(), () => {});
  first.set("condition", true);
  assert.equal(hub.get("member:app:v1:one", variables), first);
  assert.deepEqual(second.snapshot(), {});
  assert.deepEqual(hub.get("member:app:v2:one", variables).snapshot(), {});
  assert.deepEqual(hub.get("other:app:v1:one", variables).snapshot(), {});
  first.set("condition", "wrong type"); first.set("label", "write constant");
  assert.deepEqual(first.snapshot(), { condition: true });
  first.detach(a); assert.equal(first.snapshot().condition, true);
  first.close(); assert.equal(closed, 1); assert.deepEqual(first.snapshot(), {});
  const reopened = hub.get("member:app:v1:one", variables); reopened.attach(Symbol(), () => {});
  first.set("condition", true); assert.deepEqual(reopened.snapshot(), {});
  second.detach(Symbol()); // Removing an unrelated owner does not retire a live instance.
  assert.equal(second.retired, false);
});

const tick=()=>new Promise(resolve=>setImmediate(resolve));
test("pages share one application read while instances and retired requests remain isolated",async()=>{
 const pending=[];const source={scope:"member:v1",entity:()=>({fields:[]}),get:async()=>({record:{id:"one",revision:1}}),list:()=>new Promise(resolve=>pending.push(resolve))};
 const variables={window:{scope:"application",type:"object-set",mode:"resource",source:{kind:"plan",query:"read"}}},options={source,queries:{read:{object:{app:"sample",kind:"object",name:"sample.note"},limit:10}}};
 const hub=new ApplicationSessionHub(),first=hub.get("one",variables,options),a=Symbol(),b=Symbol();first.attach(a,()=>{});first.attach(b,()=>{});
 const read=first.reads.querySource("plan/read"),old=read.list("sample.note",{limit:10});assert.equal(old,hub.get("one",variables,options).reads.querySource("plan/read").list("sample.note",{limit:10}));await tick();assert.equal(pending.length,1);
 const second=hub.get("two",variables,options);second.attach(Symbol(),()=>{});const other=second.reads.querySource("plan/read").list("sample.note",{limit:10});await tick();assert.equal(pending.length,2);
 first.detach(a);assert.equal(first.retired,false);first.close();assert.equal(first.retired,true);
 const reopened=hub.get("one",variables,options);reopened.attach(Symbol(),()=>{});const current=reopened.reads.querySource("plan/read").list("sample.note",{limit:10});await tick();assert.equal(pending.length,3);
 pending[0]({records:[{id:"old",revision:1}],total:1});await old;assert.deepEqual(first.reads.snapshot().queries,{});assert.equal(reopened.reads.snapshot().queries["plan/read"].status,"pending");
 pending[1]({records:[{id:"other",revision:1}],total:1});await other;pending[2]({records:[{id:"new",revision:1}],total:1});await current;
 assert.equal(second.reads.snapshot().queries["plan/read"].value.records[0].id,"other");assert.equal(reopened.reads.snapshot().queries["plan/read"].value.records[0].id,"new");
 reopened.set("window","mutable rows");assert.deepEqual(reopened.snapshot(),{});
});

test("application record references require authorized reads, preserve producer ownership and retire late replies",async()=>{
 const variables={selected:{scope:"application",type:"record",mode:"resource",source:{kind:"record",object:{app:"sample",kind:"object",name:"sample.note"}}}},pending=[];
 const source={scope:"member:v1",entity:()=>({fields:[]}),get:(_,id)=>new Promise((resolve,reject)=>pending.push({id,resolve,reject})),list:async()=>({records:[],total:0})};
 const hub=new ApplicationSessionHub(),options={source,queries:{}},first=hub.get("one",variables,options),second=hub.get("two",variables,options),a=Symbol(),b=Symbol();first.attach(a,()=>{});first.attach(b,()=>{});second.attach(Symbol(),()=>{});
 first.select("selected",{object:"sample.other",id:"wrong"},a);assert.deepEqual(first.reads.snapshot().records,{});
 first.select("selected",{object:"sample.note",id:"old"},a);await tick();assert.equal(first.reads.snapshot().records.selected.status,"pending");
 first.select("selected",{object:"sample.note",id:"new"},b);await tick();pending[0].resolve({record:{id:"old",revision:1}});await tick();assert.equal(first.reads.snapshot().records.selected.status,"pending");pending[1].resolve({record:{id:"new",revision:1,private:"never copied to variable"}});await tick();
 assert.deepEqual(first.reads.snapshot().records.selected.value,{object:"sample.note",id:"new"});assert.deepEqual(second.reads.snapshot().records,{});
 first.select("selected",undefined,a,true);assert.equal(first.reads.snapshot().records.selected.value.id,"new");first.detach(a);assert.equal(first.reads.snapshot().records.selected.value.id,"new");
 first.select("selected",undefined,b,true);assert.equal(first.reads.snapshot().records.selected.status,"empty");
 first.select("selected",{object:"sample.note",id:"denied"},b);await tick();pending[2].reject(new Error("Denied"));await tick();assert.equal(first.reads.snapshot().records.selected.status,"error");assert.equal(first.reads.selected("selected"),undefined);
 first.select("selected",{object:"sample.note",id:"late"},b);await tick();first.close();const reopened=hub.get("one",variables,options);reopened.attach(Symbol(),()=>{});pending[3].resolve({record:{id:"late",revision:1}});await tick();assert.deepEqual(reopened.reads.snapshot().records,{});assert.deepEqual(first.reads.snapshot().records,{});
 first.select("selected",{object:"sample.note",id:"resurrect"},b);assert.deepEqual(first.reads.snapshot().records,{});
});
