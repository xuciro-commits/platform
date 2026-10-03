import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
import test from "node:test";
import assert from "node:assert/strict";
const {aggregateQuery,aggregateValue,aggregateBudget}=await import("./aggregates.ts");
const {PageSessionStore}=await import("./Session.ts");
const data=count=>({columns:[{name:"count",kind:"measure",type:"quantitative"}],rows:count===undefined?[]:[{count}]});
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject};};
const tick=()=>new Promise(resolve=>setImmediate(resolve));
test("aggregate scalar field identity preserves a colon in a declared original field name",()=>{assert.deepEqual(aggregateValue({columns:[{name:"avg:reading:raw",kind:"measure",type:"quantitative",field:"reading:raw"}],rows:[{"avg:reading:raw":1.5}]},"avg:reading:raw"),{status:"value",value:{kind:"number",value:1.5}});});
test("numeric aggregate answers preserve original host precision and empty values without claiming exact decimal semantics",()=>{const reply=(value)=>({columns:[{name:"avg:availability",kind:"measure",type:"quantitative",field:"availability"}],rows:value===undefined?[]:[{"avg:availability":value}]});assert.deepEqual(aggregateValue(reply(0.1),"avg:availability"),{status:"value",value:{kind:"number",value:0.1}});assert.equal(aggregateValue(reply(),"avg:availability").status,"empty");assert.equal(aggregateValue(reply(null),"avg:availability").status,"empty");for(const value of [NaN,Infinity,"0.1",{}])assert.equal(aggregateValue(reply(value),"avg:availability").status,"error");assert.equal(aggregateValue({...reply(1),columns:[{...reply(1).columns[0],money:true}]},"avg:availability").status,"error");});
test("different measures of one query use independent reads and a closed scope retires all of them",async()=>{const pending=[],source={scope:"member:1",entity:()=>({fields:[]}),get:async()=>({record:{id:"one"}}),list:async()=>({records:[],total:0}),aggregate:(_,q)=>{const p=deferred();pending.push({p,q});return p.promise;}};const store=new PageSessionStore(source,{objects:new Map(),children:new Map(),queryParents:new Map(),overlayScopes:new Map([["panel",{queries:new Set(["plan/read"]),selections:new Set()}]])});const count=store.aggregateScalar("plan/read","sample.note",{measures:["count"]}),avg=store.aggregateScalar("plan/read","sample.note",{measures:["avg:availability"]});await tick();assert.equal(pending.length,2);pending[0].p.resolve(data(2));await count;assert.equal(store.aggregateResource("plan/read","sample.note",{measures:["count"]}).value.kind,"decimal");assert.equal(store.aggregateResource("plan/read","sample.note",{measures:["avg:availability"]}).status,"pending");store.endOverlay("panel");pending[1].p.resolve({columns:[{name:"avg:availability",kind:"measure",type:"quantitative",field:"availability"}],rows:[{"avg:availability":90}]});await avg;assert.deepEqual(store.snapshot().aggregates,{});});
test("count output preserves exact safe integers and rejects ambiguous, fractional or truncated replies",()=>{
 assert.deepEqual(aggregateValue(data()),{status:"value",value:{kind:"decimal",value:"0"}});
 assert.equal(aggregateValue(data(Number.MAX_SAFE_INTEGER)).value.value,"9007199254740991");
 for(const reply of [null,{},data(-1),data(1.5),data(Number.MAX_SAFE_INTEGER+1),data("2"),{...data(1),rows:[null]},{...data(1),rows:[{count:1},{count:2}]},{...data(1),rows:[{count:1,bucket:"A"}]}])assert.equal(aggregateValue(reply).status,"error");
 const full=aggregateQuery({domain:[["active","=",true]],set:{op:"union",inputs:[{},{}]},search:"A",sort:["id"],offset:100,limit:1});assert.deepEqual(full.measures,["count"]);assert.equal(full.limit,undefined);assert.equal(full.offset,undefined);assert.equal(full.sort,undefined);assert.equal(full.set.op,"union");
});
test("count reads share one request, discard switched and retired results, clear denial and observe data scope",async()=>{
 const pending=[],source={scope:"member:1",entity:()=>({fields:[]}),get:async()=>({record:{id:"one"}}),list:async()=>({records:[],total:0}),aggregate:(_,q)=>{const p=deferred();pending.push(p);return p.promise;}};
 const plan={objects:new Map(),children:new Map(),queryParents:new Map(),overlayScopes:new Map([["picker",{queries:new Set(["plan/read"]),selections:new Set()}]])},store=new PageSessionStore(source,plan),a={measures:["count"],search:"A"},b={measures:["count"],search:"B"};
 const first=store.aggregateScalar("plan/read","sample.note",a),alias=store.aggregateScalar("plan/read","sample.note",a);assert.equal(first,alias);await tick();assert.equal(pending.length,1);
 const second=store.aggregateScalar("plan/read","sample.note",b);await tick();pending[0].resolve(data(99));await first;assert.equal(store.aggregateResource("plan/read","sample.note",b).status,"pending");pending[1].resolve(data(2));await second;assert.equal(store.aggregateResource("plan/read","sample.note",b).value.value,"2");assert.equal(store.aggregateResource("plan/read","sample.note",a).status,"pending");
 const denied=store.aggregateScalar("plan/read","sample.note",a);await tick();pending[2].reject(new Error("Denied"));await denied;assert.equal(store.aggregateResource("plan/read","sample.note",a).status,"error");
 store.resetQueries(["plan/read"]);const old=store.aggregateScalar("plan/read","sample.note",a);await tick();store.endOverlay("picker");pending[3].resolve(data(4));await old;assert.deepEqual(store.snapshot().aggregates,{});
 const fresh=store.aggregateScalar("plan/read","sample.note",a);await tick();store.updateSource({...source,scope:"member:2"});pending[4].resolve(data(8));await fresh;assert.deepEqual(store.snapshot().aggregates,{});store.dispose();
});


test("preview count budgets include parent multiplication and reject cyclic ancestry before reading",()=>{
 const variables={count:{mode:"aggregate",source:{query:"read"}}},plans={read:{itemOwner:"parents"}},contract={maxVariables:8,maxExpandedReads:32};
 assert.equal(aggregateBudget(variables,plans,{parents:{loop:{limit:32}}},contract),true);assert.equal(aggregateBudget(variables,plans,{parents:{loop:{limit:33}}},contract),false);
 assert.equal(aggregateBudget(variables,plans,{parents:{children:["parents"],loop:{limit:1}}},contract),false);
 assert.equal(aggregateBudget(Object.fromEntries(Array.from({length:9},(_,i)=>[i,variables.count])),plans,{parents:{loop:{limit:1}}},contract),false);
});
test("aggregate budgets distinguish measures while identical scalar aliases share one expanded read",()=>{const plans={read:{itemOwner:"parents"}},nodes={parents:{loop:{limit:17}}},contract={maxVariables:8,maxExpandedReads:32},avg={mode:"aggregate",source:{kind:"aggregate",query:"read",measure:"avg:availability"}},max={mode:"aggregate",source:{kind:"aggregate",query:"read",measure:"max:availability"}};assert.equal(aggregateBudget({avg,alias:avg},plans,nodes,contract),true);assert.equal(aggregateBudget({avg,max},plans,nodes,contract),false);});

test("a synchronous pending subscriber shares the in-flight count instead of starting another read",async()=>{
 let reads=0,alias,observed=false;
 const source={scope:"member:1",entity:()=>({fields:[]}),get:async()=>({record:{id:"one"}}),list:async()=>({records:[],total:0}),aggregate:async()=>{reads++;return data(2);}};
 const store=new PageSessionStore(source,{objects:new Map(),children:new Map(),queryParents:new Map()}),query={measures:["count"]};
 store.subscribe(()=>{if(!observed&&store.aggregateResource("plan/read","sample.note",query).status==="pending"){observed=true;alias=store.aggregateScalar("plan/read","sample.note",query);}});
 const original=store.aggregateScalar("plan/read","sample.note",query);await original;await alias;
 assert.equal(reads,1);assert.equal(original,alias);assert.equal(store.aggregateResource("plan/read","sample.note",query).value.value,"2");
});
