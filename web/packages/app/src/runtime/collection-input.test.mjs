import {registerHooks} from "node:module";
registerHooks({resolve(s,c,n){try{return n(s,c)}catch(e){if(s.startsWith("./")&&!s.endsWith(".ts"))return n(s+".ts",c);throw e;}}});
import assert from "node:assert/strict";
import test from "node:test";
import {pageUIManifest} from "@platform/kernel/host-contract";
const {collectionInput,validCollectionInput,collectionQuery}=await import("./collection-input.ts");
const {checkPortValues,navigationValues}=await import("./page-values.ts");
const {compileQueryPlans,queryView}=await import("./query-plans.ts");
const object={app:"sample",kind:"object",name:"sample.note"},info={type:object.name,fields:[{name:"bucket",type:"text"},{name:"active",type:"boolean"}]},binding={ref:{app:"sample",kind:"query",name:"source"},sourceVersion:"q1"},definition={ref:binding.ref,version:"q2",query:{object:object.name,domain:[["active","=",false]]},queryVersions:{q1:{object:object.name,domain:[["active","=",true]],sort:["-bucket"]}}};
const input=()=>collectionInput(object,{domain:[["active","=",true],["bucket","=","A"]],sort:["-bucket"],offset:40,limit:2},[binding],true),port={set:{type:"object-set",variable:"objects",object,required:true}};
test("collection calls transfer predicates and retained identities while omitting rows and pagination",()=>{
 const value=input();assert.equal(validCollectionInput(value),true);assert.equal(checkPortValues(port,{set:value}),undefined);assert.equal(value.predicate.limit,undefined);assert.equal(value.predicate.offset,undefined);assert.equal(value.records,undefined);
 assert.deepEqual(navigationValues({set:{variable:"window"}},{window:{status:"value",value}}),{set:value});assert.equal(checkPortValues(port,{set:{...value,records:[{id:"A"}]}}),"Page value type does not match the interface.");assert.ok(checkPortValues({...port,set:{...port.set,object:{...object,app:"foreign"}}},{set:value}));
});
test("the receiver keeps complete fixed membership and its own window, current fields and version checks",()=>{
 const plan={object,input:"objects",limit:5,offset:10,conditions:[{field:"bucket",op:"!=",value:{literal:"B"}}]},variables={objects:{scope:"page",mode:"input",type:"object-set"}},values={objects:{status:"value",value:input()}},compile=(values,entity=info,named=definition)=>Object.fromEntries(compileQueryPlans({read:plan},variables,()=>values,()=>entity,()=>named,pageUIManifest.runtime.query)).read;
 const result=compile(values);assert.equal(result.status,"value");assert.equal(result.query.limit,5);assert.equal(result.query.offset,10);assert.deepEqual(result.query.sort,["-bucket"]);assert.deepEqual(result.query.domain,[["active","=",true],["bucket","=","A"],["bucket","!=","B"]]);assert.equal(queryView(plan,result,{sort:["id"]},info,undefined,pageUIManifest.runtime.query).status,"error");
 assert.equal(compile(values,{...info,fields:[{name:"bucket",type:"text"}]}).status,"error");assert.equal(compile(values,info,{...definition,queryVersions:{}}).status,"error");for(const status of ["empty","pending","error"])assert.equal(compile({objects:{status,code:"Denied"}}).status,status);
 const searched=compile({objects:{status:"value",value:{...input(),predicate:{...input().predicate,search:"parent search"}}}}),view=queryView(plan,searched,{search:"child search",offset:20},info,undefined,pageUIManifest.runtime.query);
 assert.equal(view.status,"value");assert.equal(view.query.offset,20);assert.equal(view.query.set.inputs[0].search,"parent search");assert.equal(view.query.set.inputs[1].search,"child search");
});
test("independent searches intersect and invalid or oversized predicates refuse without a broader fallback",()=>{
 const value={...input(),predicate:{...input().predicate,search:"parent"}},q=collectionQuery(value,{domain:[["bucket","=","B"]],search:"child",limit:5});assert.equal(q.set.op,"intersect");assert.equal(q.set.inputs[0].search,"parent");assert.equal(q.set.inputs[1].search,"child");assert.equal(collectionQuery(input(),{limit:5},["id"]),undefined);
 for(const patch of [{predicate:{domain:[["hidden","=",1]],set:{op:"execute",inputs:[]}}},{bindings:[{...binding,sourceVersion:""}]},{predicate:{search:"x".repeat(4097)}},{predicate:{records:[{id:"A"}]}},{sort:["id","hidden","third","fourth","fifth"]}])assert.equal(validCollectionInput({...input(),...patch}),false);
});
