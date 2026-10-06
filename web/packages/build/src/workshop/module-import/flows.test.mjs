import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
import {registerHooks} from "node:module";
registerHooks({resolve(s,c,next){if(s==="@platform/ui")return {url:"data:text/javascript,export const t=s=>s;",shortCircuit:true};try{return next(s,c);}catch(error){if(s.startsWith("./")||s.startsWith("../"))return next(s+".ts",c);throw error;}}});
const {compileFlowSource,parseFlowSource,flowPorts}=await import("./flows.ts");
const fixture=()=>{
 const flow={id:"original",name:"Original pipeline",description:"Typed native import",enabled:true,version:1,execution:{mode:"onDemand",maxConcurrency:1,checkpointEvery:0,errorPolicy:"stop"},nodes:[{id:"objects",type:"objectSource",name:"Assets",x:20,y:40,config:{objectType:"Asset",limit:3}},{id:"select",type:"filter",name:"Select",x:280,y:40,config:{field:"pressure",value:50}},{id:"formula",type:"formula",name:"Formula",x:400,y:40,config:{expression:"pressure * 1.05"}},{id:"route",type:"branch",name:"Route",x:520,y:40,config:{field:"status",value:"Warning"}},{id:"inspect",type:"debug",name:"Inspect",x:800,y:40,config:{}},{id:"merge",type:"union",name:"Merge",x:1050,y:40,config:{preserveOrder:true}}],edges:[{id:"a",source:"objects",sourcePort:"objects",target:"select",targetPort:"in"},{id:"b",source:"select",sourcePort:"out",target:"formula",targetPort:"in"},{id:"formula-route",source:"formula",sourcePort:"out",target:"route",targetPort:"in"},{id:"c",source:"route",sourcePort:"match",target:"inspect",targetPort:"in"},{id:"union-a",source:"inspect",sourcePort:"out",target:"merge",targetPort:"a"},{id:"union-b",source:"select",sourcePort:"rejected",target:"merge",targetPort:"b"}]};
 const query={ref:{app:"build",kind:"query",name:"assets"},kind:"query",source:"tenant",revision:2,version:"1.query-2",input:{type:"object",properties:{}},output:{type:"object",properties:{records:{type:"array",items:{type:"object",properties:{}}}}}};
 const capabilities=[query],bindings={objects:{app:"build",kind:"query",name:"assets",version:2,sourceVersion:query.version,object:"build.asset"}};
 for(const node of flow.nodes.slice(1)){const ports=flowPorts[node.type],config={type:"object",properties:Object.fromEntries(Object.entries(node.config).map(([k,v])=>[k,{type:typeof v}]))};const cap={ref:{app:"build",kind:"compute",name:node.id},kind:"compute",source:"tenant",revision:3,version:"1.compute-3",input:{type:"object",properties:{config,...Object.fromEntries(ports.inputs.map(p=>[p,{type:"array"}]))},required:["config",...ports.inputs]},output:{type:"object",required:ports.outputs,properties:Object.fromEntries(ports.outputs.map(p=>[p,{type:"array"}]))}};capabilities.push(cap);bindings[node.id]={app:"build",kind:"compute",name:node.id,version:3,sourceVersion:cap.version};}
 return {flow,bindings,target:{name:"imported",capabilities,definitions:[{ref:query.ref,version:query.version,query:{name:"assets",object:"build.asset",limit:3},queryVersions:{[query.version]:{name:"assets",object:"build.asset",limit:3}}}]}};
};
const compile=f=>compileFlowSource(JSON.stringify(f.flow),f.flow.id,f.bindings,f.target);
test("the whole manual pipeline retains source configuration, exact query/function versions and every data edge",()=>{
 const f=fixture(),source=JSON.stringify(f.flow),report=compile(f);assert.ok(report.draft,JSON.stringify(report.diagnostics));assert.equal(report.source,source);assert.equal(JSON.stringify(f.flow),source);
 assert.equal(Object.keys(report.ids).length,6);assert.equal(report.draft.steps.length,14);
 const query=report.draft.steps.find(s=>s.kind==="query"),select=report.draft.steps.find(s=>s.name===report.ids.select),route=report.draft.steps.find(s=>s.name===report.ids.route),inspect=report.draft.steps.find(s=>s.name===report.ids.inspect);
 assert.equal(query.queryVersion,2);assert.equal(select.operation.version,3);assert.deepEqual(select.inputs.config.value,f.flow.nodes[1].config);assert.deepEqual(select.inputs.in,{source:"step",step:report.ids.objects,path:["records"]});assert.equal(route.inputs.in.step,report.ids.formula);assert.deepEqual(inspect.inputs.in.path,["match"]);assert.equal(report.draft.layout[inspect.name].x,800);
 const gate=report.draft.steps.find(s=>s.name==="gate5");assert.equal(gate.condition.op,"all");assert.equal(gate.condition.terms[0].op,"exists");assert.equal(gate.cases.false,"gate6");assert.equal(inspect.error,"failed");
});
test("actual complete default Module flows remain intact and cannot silently become manual or stateless",()=>{
 const source=readFileSync(new URL("./default.workshop.json",import.meta.url),"utf8"),parsed=parseFlowSource(source);assert.equal(parsed.diagnostics.length,0);assert.equal(parsed.flows.length,1);
 const f=parsed.flows[0],report=compileFlowSource(source,f.id,{},fixture().target);assert.equal(report.source,source);assert.equal(f.nodes.length,7);assert.equal(f.edges.length,6);assert.equal(report.draft,undefined);assert.ok(report.diagnostics.some(d=>d.code==="flow-mode-owner"));assert.ok(report.diagnostics.some(d=>d.code==="flow-node-owner"));
 assert.equal(Object.keys(flowPorts).length,21);
});
test("unknown settings, scheduling, effects, retries, wrong schemas and missing retained versions reject the complete import",()=>{
 for(const change of [f=>f.flow.execution.mode="stream",f=>f.flow.execution.maxConcurrency=4,f=>f.flow.execution.checkpointEvery=10,f=>f.flow.execution.errorPolicy="continue",f=>f.flow.nodes[1].retry={attempts:3,backoffMs:250},f=>f.flow.nodes[1].disabled=true,f=>f.flow.nodes[1].type="constructor",f=>f.flow.nodes[1].type="action",f=>f.flow.nodes[1].extra="unsupported",f=>f.bindings.select.sourceVersion="1.compute-2",f=>f.bindings.objects.object="other.asset",f=>f.flow.nodes[0].config.limit=2,f=>f.target.capabilities[1].input.properties.config.properties.value.type="string",f=>f.flow.nodes[1].config.constructor=12,f=>delete f.target.capabilities[1].output.properties.out]){
  const f=fixture();change(f);const original=JSON.stringify(f.flow),report=compile(f);assert.equal(report.source,original);assert.equal(report.draft,undefined,String(change));assert.equal(JSON.stringify(f.flow),original);
 }
});
test("cycles, missing required inputs, duplicate writers and dangling connections cannot create partial drafts",()=>{
 for(const change of [f=>f.flow.edges.pop(),f=>f.flow.edges.push({...f.flow.edges[1],id:"duplicate"}),f=>f.flow.edges[0].source="absent",f=>f.flow.edges.push({id:"cycle",source:"inspect",sourcePort:"out",target:"select",targetPort:"in"})]){const f=fixture();change(f);assert.equal(compile(f).draft,undefined);}
});
