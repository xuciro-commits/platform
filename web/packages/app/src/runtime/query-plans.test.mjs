import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
const { compileQueryPlan, queryView }=await import("./query-plans.ts");
const {compileQueryPlans}=await import("./query-plans.ts");
const contract = JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url))).runtime.query;
const info={type:"sample.note",fields:[{name:"bucket",type:"text"},{name:"active",type:"boolean"},{name:"count",type:"integer"}]};
const variables={bucket:{scope:"page",type:"string",mode:"state",initial:"A"}};
const plan={object:{app:"sample",kind:"object",name:info.type},limit:20,conditions:[{field:"bucket",op:"=",value:{variable:"bucket"}}]};
test("optional typed facets retain fixed conditions and reject malformed or failed input instead of broadening reads",()=>{
 const variables={picked:{scope:"page",type:"string-set",mode:"state",initial:{kind:"string-set",values:[]}},minimum:{scope:"page",type:"string",mode:"state",initial:""}},q={...plan,conditions:[{field:"active",op:"=",value:{literal:true}},{field:"bucket",op:"in",value:{variable:"picked"},optional:true},{field:"count",op:">=",value:{variable:"minimum"},optional:true,asDecimal:true}]};
 const compile=(picked,minimum)=>compileQueryPlan(q,variables,{picked,minimum},info,undefined,contract),value=v=>({status:"value",value:v});
 assert.deepEqual(compile(value({kind:"string-set",values:[]}),value("")).query.domain,[["active","=",true]]);
 assert.deepEqual(compile(value({kind:"string-set",values:["A","B"]}),value("1.20")).query.domain,[["active","=",true],["bucket","in",["A","B"]],["count",">=",{kind:"decimal",value:"1.2"}]]);
 for(const status of ["empty","pending","error"])assert.equal(compile({status,code:"Denied"},value("")).status,status);
 for(const raw of [{kind:"string-set",values:["A","A"]},{kind:"string-set",values:[1]},[]])assert.equal(compile(value(raw),value("")).status,"error");
 assert.equal(compile(value({kind:"string-set",values:[]}),value("1e3")).status,"error");
 assert.equal(compile(value({kind:"string-set",values:Array.from({length:65},(_,i)=>String(i))}),value("")).status,"error");
 for(const field of ["hidden","active"])assert.equal(compileQueryPlan({...q,conditions:[{field,op:"in",optional:true,value:{variable:"picked"}}]},variables,{picked:value({kind:"string-set",values:[]})},info,undefined,contract).status,"error");
});
test("query compilation preserves empty/pending/error inputs and rejects hidden fields and result dependencies",()=>{
  for(const status of ["empty","pending","error"]){const result=compileQueryPlan(plan,variables,{bucket:status==="error"?{status,code:"Denied"}:{status}},info,undefined,contract);assert.equal(result.status,status);}
  const values={bucket:{status:"value",value:"A"}};
  assert.deepEqual(compileQueryPlan(plan,variables,values,info,undefined,contract).query,{domain:[["bucket","=","A"]],sort:["id"],offset:0,limit:20});
  assert.equal(compileQueryPlan(plan,variables,values,{...info,fields:[]},undefined,contract).status,"error");
  const cycle={...variables,window:{scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query:"read"}},bucket:{scope:"page",type:"boolean",mode:"derived",expression:{op:"present",args:[{variable:"window"}]}}};
  assert.equal(compileQueryPlan(plan,cycle,values,info,undefined,contract).status,"error");
});

test("set plans compile complete fixed predicates, ignore operand windows and propagate unavailable inputs",()=>{
 const binding={ref:{app:"sample",kind:"query",name:"fixed"},sourceVersion:"q1"},named={ref:binding.ref,version:"q2",query:{object:info.type,domain:[["active","=",false]]},queryVersions:{q1:{object:info.type,domain:[["active","=",true]],limit:1,sort:["-bucket"]}}};
 const plans={left:{...plan,limit:1,offset:99,query:binding},right:{...plan,conditions:[{field:"count",op:">",value:{literal:0}}],limit:1},combined:{object:plan.object,limit:50,set:{op:"subtract",inputs:["left","right"]}}};
 const compile=(plans,values)=>Object.fromEntries(compileQueryPlans(plans,variables,()=>values,()=>info,()=>named,contract));
 const value=compile(plans,{bucket:{status:"value",value:"A"}}).combined;
 assert.equal(value.status,"value");assert.equal(value.query.limit,50);assert.deepEqual(value.query.sort,["id"]);
 assert.deepEqual(value.query.set.inputs[0].domain,[["active","=",true],["bucket","=","A"]]);
 assert.equal(value.query.set.inputs[0].limit,undefined);assert.equal(value.query.set.inputs[0].offset,undefined);assert.equal(value.query.set.inputs[0].sort,undefined);
 for(const status of ["empty","pending","error"])assert.equal(compile(plans,{bucket:{status,code:"Denied"}}).combined.status,status);
 assert.equal(compile({...plans,combined:{...plans.combined,set:{op:"union",inputs:["combined","right"]}}},{bucket:{status:"value",value:"A"}}).combined.status,"error");
 assert.equal(compile({...plans,right:{...plans.right,object:{...plan.object,name:"other"}}},{bucket:{status:"value",value:"A"}}).combined.status,"error");
 assert.equal(compile({...plans,right:{...plans.right,owner:"other"}},{bucket:{status:"value",value:"A"}}).combined.status,"error");
 assert.notEqual(compile(plans,{bucket:{status:"value",value:"B"}}).combined.signature,value.signature);
 const withSearch=Object.fromEntries(compileQueryPlans(plans,variables,()=>({bucket:{status:"value",value:"A"}}),()=>info,p=>p.query?named:undefined,contract,[],()=>true,id=>id==="right"?{search:"current search",offset:25}:undefined)).combined;
 assert.equal(withSearch.query.set.inputs[1].search,"current search");assert.equal(withSearch.query.set.inputs[1].offset,undefined);assert.notEqual(withSearch.signature,value.signature);
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

test("overlay query parameters accept their own locals and reject foreign roots, page leaks and selected plan results",()=>{
 const locals={local:{scope:"overlay",owner:"picker",type:"string",mode:"state",initial:"A"}},values={local:{status:"value",value:"A"}};
 const scoped={object:plan.object,owner:"picker",limit:20,search:{variable:"local"}};
 assert.equal(compileQueryPlan(scoped,locals,values,info,undefined,contract).status,"value");
 for(const owner of [undefined,"other"])assert.equal(compileQueryPlan({...scoped,owner},locals,values,info,undefined,contract).status,"error");
 const selected={local:{scope:"overlay",owner:"picker",type:"record",mode:"resource",source:{kind:"record",section:"table"}}};
 assert.equal(compileQueryPlan(scoped,selected,values,info,undefined,contract,[{id:"table",collectionVariable:"window"}]).status,"error");
});

test("decimal parameters retain exact tagged conditions and errors never fall back to an old threshold",()=>{
 const variables={threshold:{scope:"page",type:"decimal",mode:"state",initial:{kind:"decimal",value:"0"}}},plan={object:{app:"sample",kind:"object",name:"sample.note"},limit:10,conditions:[{field:"count",op:">",value:{variable:"threshold"}}]},value={kind:"decimal",value:"1.9999999999999999"};
 const result=compileQueryPlan(plan,variables,{threshold:{status:"value",value}},info,undefined,contract);assert.equal(result.status,"value");assert.deepEqual(result.query.domain,[["count",">",value]]);
 assert.equal(compileQueryPlan(plan,variables,{threshold:{status:"error",code:"Invalid numeric value."}},info,undefined,contract).status,"error");
});


test("item queries never broaden an unavailable parent into an object read and set leaves keep the same parent scope",()=>{
 const object={app:"sample",kind:"object",name:"sample.child"},binding={ref:{app:"sample",kind:"query",name:"children"},sourceVersion:"q1"};
 const info={type:object.name,fields:[{name:"parent",type:"reference",ref:"sample.parent"}]},variables={parent:{scope:"loop-item",owner:"parents",type:"record",mode:"resource",source:{kind:"item",node:"parents"}}};
 const named={ref:binding.ref,version:"q1",query:{object:object.name,by:"parent",limit:3}},plan={object,itemOwner:"parents",query:binding,for:{variable:"parent"},limit:3};
 const values={parent:{status:"value",value:{kind:"record",reference:{object:"sample.parent",id:"A"}}}};
 assert.deepEqual(compileQueryPlan(plan,variables,values,info,named,contract).query.domain,[["parent","=","A"]]);
 for(const status of ["empty","pending","error"])assert.equal(compileQueryPlan(plan,variables,{parent:{status,code:"Denied"}},info,named,contract).status,status);
 assert.equal(compileQueryPlan({...plan,query:undefined,for:undefined},variables,values,info,undefined,contract).status,"error");
 assert.equal(compileQueryPlan(plan,variables,{parent:{status:"value",value:{kind:"record",reference:{object:"sample.other",id:"A"}}}},info,named,contract).status,"error");
 const plans={left:plan,right:plan,combined:{object,itemOwner:"parents",limit:3,set:{op:"union",inputs:["left","right"]}}};
 const compile=plans=>Object.fromEntries(compileQueryPlans(plans,variables,()=>values,()=>info,()=>named,contract)).combined;
 assert.equal(compile(plans).status,"value");assert.equal(compile({...plans,right:{...plan,query:undefined,for:undefined}}).status,"error");assert.equal(compile({...plans,right:{...plan,itemOwner:"other"}}).status,"error");
});

test("relation plans keep retained versions and typed starts instead of broadening empty or refused input",()=>{
 const parent={app:"sample",kind:"object",name:"sample.parent"},child={app:"sample",kind:"object",name:"sample.child"},binding={ref:{app:"sample",kind:"link-type",name:"children"},sourceVersion:"l1"},link={name:"children",parent,child,via:"parent"},definition={ref:binding.ref,version:"l2",linkType:{...link,title:"new"},linkVersions:{l1:link}};
 const variables={start:{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:"parents"}}},values={start:{status:"value",value:{kind:"record",reference:{object:parent.name,id:"A"}}}},plan={object:child,query:binding,direction:"forward",for:{variable:"start"},limit:1},info={type:child.name,fields:[{name:"parent",type:"reference",ref:parent.name}]};
 const result=compileQueryPlan(plan,variables,values,info,definition,contract);assert.equal(result.status,"value");assert.deepEqual(result.query.traversal,{binding,direction:"forward",id:"A"});assert.deepEqual(result.query.domain,[]);
 for(const status of ["empty","pending","error"])assert.equal(compileQueryPlan(plan,variables,{start:{status,code:"Denied"}},info,definition,contract).status,status);
 assert.equal(compileQueryPlan(plan,variables,values,info,{...definition,linkVersions:{}},contract).status,"error");assert.equal(compileQueryPlan({...plan,object:parent},variables,values,{type:parent.name,fields:[]},definition,contract).status,"error");
});

test("civil date query text rejects normalization and clears invalid input instead of broadening conditions",()=>{
 const vars={day:{scope:"page",type:"string",mode:"state",initial:""}},info={type:"sample.note",fields:[{name:"due",type:"date"}]},q={object:{app:"sample",kind:"object",name:"sample.note"},limit:20,conditions:[{field:"due",op:">=",value:{variable:"day"},optional:true,asDate:true}]},compile=day=>compileQueryPlan(q,vars,{day:{status:"value",value:day}},info,undefined,contract);assert.deepEqual(compile("").query.domain,[]);assert.deepEqual(compile("2028-02-29").query.domain,[["due",">=","2028-02-29"]]);for(const date of ["2026-02-30","0000-01-01","2028-02-29T00:00:00Z"]){assert.equal(compile(date).status,"error");assert.equal(compile(date).code,"Invalid date value.");}
});

test("record picker search adds authorized title or ID predicates while retaining fixed filters and declared search",()=>{
 const info={type:"sample.note",fields:[{name:"title",type:"text"}]},q={object:{app:"sample",kind:"object",name:"sample.note"},limit:20,sort:["id"],search:{variable:"fixed"}},base={status:"value",object:"sample.note",query:{limit:20,sort:["id"],search:"fixed search",domain:[["bucket","=","A"]]},signature:"base"};const result=queryView(q,base,{search:"Needle"},info,undefined,contract,"title");assert.equal(result.status,"value");assert.equal(result.query.search,"fixed search");assert.deepEqual(result.query.domain,[["bucket","=","A"],"|",["title","like","Needle"],["id","like","Needle"]]);assert.deepEqual(queryView(q,base,{search:""},info,undefined,contract,"title").query.domain,base.query.domain);assert.equal(queryView(q,base,{search:"x"},info,undefined,contract,"hidden").status,"error");
});
