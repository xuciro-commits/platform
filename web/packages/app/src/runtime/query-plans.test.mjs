import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { compileQueryPlan, queryView } from "./query-plans.ts";
const contract = JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url))).runtime.query;
const info={type:"sample.note",fields:[{name:"bucket",type:"text"},{name:"active",type:"boolean"},{name:"count",type:"integer"}]};
const variables={bucket:{scope:"page",type:"string",mode:"state",initial:"A"}};
const plan={object:{app:"sample",kind:"object",name:info.type},limit:20,conditions:[{field:"bucket",op:"=",value:{variable:"bucket"}}]};
test("query compilation preserves empty/pending/error inputs and rejects hidden fields and result dependencies",()=>{
  for(const status of ["empty","pending","error"]){const result=compileQueryPlan(plan,variables,{bucket:status==="error"?{status,code:"Denied"}:{status}},info,undefined,contract);assert.equal(result.status,status);}
  const values={bucket:{status:"value",value:"A"}};
  assert.deepEqual(compileQueryPlan(plan,variables,values,info,undefined,contract).query,{domain:[["bucket","=","A"]],sort:["id"],offset:0,limit:20});
  assert.equal(compileQueryPlan(plan,variables,values,{...info,fields:[]},undefined,contract).status,"error");
  const cycle={...variables,window:{scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query:"read"}},bucket:{scope:"page",type:"boolean",mode:"derived",expression:{op:"present",args:[{variable:"window"}]}}};
  assert.equal(compileQueryPlan(plan,cycle,values,info,undefined,contract).status,"error");
});
test("original named query conditions, ordering, limit and version constrain an independent plan",()=>{
  const binding={ref:{app:"sample",kind:"query",name:"notes"},sourceVersion:"q1"};
  const named={ref:binding.ref,version:"q1",query:{object:info.type,domain:[["active","=",true]],sort:["-bucket"],limit:5}};
  const bound={...plan,query:binding},values={bucket:{status:"value",value:"B"}};
  const result=compileQueryPlan(bound,variables,values,info,named,contract);
  assert.deepEqual(result.query,{domain:[["active","=",true],["bucket","=","B"]],sort:["-bucket"],offset:0,limit:5});
  assert.equal(compileQueryPlan(bound,variables,values,info,{...named,version:"q2"},contract).status,"error");
  assert.equal(compileQueryPlan({...plan,conditions:[{field:"count",op:">",value:{literal:4}}]},variables,{},info,undefined,contract).status,"value");
});

test("query views preserve domain and limit and reject locked or unauthorized overrides",()=>{
 const values={bucket:{status:"value",value:"A"}},base=compileQueryPlan(plan,variables,values,info,undefined,contract);
 const view=queryView(plan,base,{search:"note",offset:20,sort:["-bucket"]},info,undefined,contract);
 assert.deepEqual(view.query,{domain:[["bucket","=","A"]],sort:["-bucket"],offset:20,limit:20,search:"note"});
 assert.equal(queryView(plan,base,{sort:["private"]},info,undefined,contract).status,"error");
 assert.equal(queryView(plan,base,{offset:contract.maxOffset+1},info,undefined,contract).status,"error");
 assert.equal(queryView({...plan,search:{literal:"fixed"}},base,{search:"replace"},info,undefined,contract).status,"error");
 assert.equal(queryView(plan,base,{sort:["id"]},info,{query:{sort:["-bucket"]}},contract).status,"error");
});

test("a page binds its retained query while newer and unreadable latest versions cannot replace it",()=>{
 const ref={app:"sample",kind:"query",name:"notes"},query={object:info.type,domain:[["active","=",true]],sort:["-bucket"],limit:5};
 const binding={ref,sourceVersion:"q1"},bound={...plan,query:binding},values={bucket:{status:"value",value:"A"}};
 const definition={ref,version:"q2",query:{...query,domain:[["active","=",false]],sort:["bucket"],limit:1},queryVersions:{q1:query}};
 const compiled=compileQueryPlan(bound,variables,values,info,definition,contract);
 assert.equal(compiled.status,"value");assert.equal(compiled.query.limit,5);assert.deepEqual(compiled.query.domain,[["active","=",true],["bucket","=","A"]]);assert.deepEqual(compiled.query.sort,["-bucket"]);
 assert.equal(compileQueryPlan(bound,variables,values,info,{...definition,query:undefined},contract).status,"value");
 assert.equal(compileQueryPlan(bound,variables,values,info,{...definition,queryVersions:{}},contract).status,"error");
 assert.equal(queryView(bound,compiled,{sort:["id"]},info,definition,contract).status,"error");
});
