import {schemaIssue} from "../../automate/workflow-schema";
import type {Api} from "@platform/kernel";
import type {Capability,WorkflowDraft,WorkflowStep} from "../../automate/workflow-model";
import {compileContinuousGraph,isContinuousNode,type ContinuousOutputs} from "./continuous";

type ObjectValue=Record<string,unknown>;
export type SourceFlowNode={id:string;type:string;name:string;x:number;y:number;config:ObjectValue;disabled?:boolean;retry?:{attempts:number;backoffMs:number};lastStatus?:string};
export type SourceFlowEdge={id:string;source:string;sourcePort:string;target:string;targetPort:string;enabled?:boolean;label?:string};
export type SourceFlow={id:string;name:string;description:string;enabled:boolean;version:number;nodes:SourceFlowNode[];edges:SourceFlowEdge[];execution:{mode:string;maxConcurrency:number;checkpointEvery:number;errorPolicy:string;scheduleMs?:number}};
export type FlowNodeBinding={app:string;kind:"query"|"compute"|"action";name:string;version:number;sourceVersion:string;object?:string;inputs?:Record<string,Api.Binding>};
export type FlowBindings=Record<string,FlowNodeBinding>;
export type FlowIssue={path:string;code:string};
export type FlowImportReport={formatVersion:1;source:string;flow:string;bindings:FlowBindings;diagnostics:FlowIssue[];ids:Record<string,string>;draft?:WorkflowDraft;continuous?:Api.Continuous;outputs?:ContinuousOutputs};
export const flowPorts:Record<string,{inputs:string[];outputs:string[];owner:"query"|"compute"|"runtime"}>={
 objectSource:{inputs:[],outputs:["objects"],owner:"query"},
 filter:{inputs:["in"],outputs:["out","rejected"],owner:"compute"},
 formula:{inputs:["in"],outputs:["out"],owner:"compute"},
 branch:{inputs:["in"],outputs:["match","else"],owner:"compute"},
 union:{inputs:["a","b"],outputs:["out"],owner:"compute"},
 debug:{inputs:["in"],outputs:["out"],owner:"compute"},
 telemetrySource:{inputs:[],outputs:["batch"],owner:"runtime"},window:{inputs:["in"],outputs:["window","late"],owner:"runtime"},aggregate:{inputs:["in"],outputs:["stats","alerts"],owner:"runtime"},join:{inputs:["stream","objects"],outputs:["joined","unmatched"],owner:"runtime"},enrich:{inputs:["in","objects"],outputs:["enriched","unresolved"],owner:"runtime"},threshold:{inputs:["in"],outputs:["triggered","normal"],owner:"runtime"},rateLimit:{inputs:["in"],outputs:["allowed","limited"],owner:"runtime"},delay:{inputs:["in"],outputs:["out","overflow"],owner:"runtime"},deduplicate:{inputs:["in"],outputs:["unique","duplicates"],owner:"runtime"},sample:{inputs:["in"],outputs:["sampled"],owner:"runtime"},pivotTo:{inputs:["objects"],outputs:["linked"],owner:"runtime"},action:{inputs:["in"],outputs:["result","errors"],owner:"runtime"},variableOutput:{inputs:["in"],outputs:["done"],owner:"runtime"},alertOutput:{inputs:["in"],outputs:["done","errors"],owner:"runtime"},deadLetter:{inputs:["in"],outputs:["queue"],owner:"runtime"},
};
const ports=(type:string)=>Object.hasOwn(flowPorts,type)?flowPorts[type]:undefined;
const object=(v:unknown):v is ObjectValue=>!!v&&typeof v==="object"&&!Array.isArray(v);
const identifier=(v:unknown):v is string=>typeof v==="string"&&/^[A-Za-z][A-Za-z0-9_.:-]{0,127}$/.test(v)&&!["constructor","prototype"].includes(v);
const pointer=(s:string)=>s.replaceAll("~","~0").replaceAll("/","~1");

/** Read a FlowDef or the complete ModuleDef; preserve its exact original bytes. */
export function parseFlowSource(source:string):{flows:SourceFlow[];diagnostics:FlowIssue[]}{
 if(new TextEncoder().encode(source).length>1_048_576)return {flows:[],diagnostics:[{path:"/",code:"source-size"}]};
 try{
  const value:unknown=JSON.parse(source),flows=object(value)&&Array.isArray(value.flows)?value.flows:[value];
  if(flows.length>32)throw Error();
  const used=new Set<string>();
  for(const f of flows){
   if(!object(f)||!identifier(f.id)||used.has(f.id)||typeof f.name!=="string"||typeof f.description!=="string"||typeof f.enabled!=="boolean"||!Number.isSafeInteger(f.version)||Number(f.version)<1||!Array.isArray(f.nodes)||f.nodes.length>128||!Array.isArray(f.edges)||f.edges.length>512||!object(f.execution))throw Error();
   used.add(f.id);const nodes=new Set<string>(),edges=new Set<string>();
   for(const n of f.nodes){if(!object(n)||!identifier(n.id)||nodes.has(n.id)||typeof n.type!=="string"||typeof n.name!=="string"||!Number.isFinite(n.x)||!Number.isFinite(n.y)||!object(n.config)||n.retry!==undefined&&(!object(n.retry)||!Number.isSafeInteger(n.retry.attempts)||!Number.isFinite(n.retry.backoffMs)))throw Error();nodes.add(n.id);}
   for(const e of f.edges){if(!object(e)||!identifier(e.id)||edges.has(e.id)||!identifier(e.source)||!identifier(e.target)||typeof e.sourcePort!=="string"||typeof e.targetPort!=="string")throw Error();edges.add(e.id);}
  }
  return {flows:flows as SourceFlow[],diagnostics:[]};
 }catch{return {flows:[],diagnostics:[{path:"/flows",code:"flow-shape"}]};}
}

/** A bounded, serial, stateless DAG uses original native requests. Unsupported
 * scheduling/state/effects never become an approximate manual workflow. */
export function compileFlowSource(source:string,flowID:string,bindings:FlowBindings,target:{name:string;capabilities:Capability[];definitions:Api.Definition[];continuous?:Partial<Api.Continuous>}):FlowImportReport{
 const parsed=parseFlowSource(source),report:FlowImportReport={formatVersion:1,source,flow:flowID,bindings:structuredClone(bindings),diagnostics:[...parsed.diagnostics],ids:{}};
 const issue=(path:string,code:string)=>report.diagnostics.push({path,code});
 const flow=parsed.flows.find(f=>f.id===flowID);if(!flow){issue("/flows","flow-required");return report;}
 const root=`/flows/${pointer(flow.id)}`;
 const keys=(v:object,allowed:string[],path:string)=>Object.keys(v).filter(k=>!allowed.includes(k)).forEach(k=>issue(`${path}/${pointer(k)}`,"unsupported-setting"));
 keys(flow,["id","name","description","enabled","version","nodes","edges","execution"],root);
 // A source graph with runtime nodes is the continuous route: the declared
 // window/aggregate/threshold/effects are derived from its nodes and ports
 // (ADR-0047 §13.1), and the Flow settings keep only the real source binding.
 if(flow.nodes.some(n=>isContinuousNode(n.type))){
  keys(flow.execution,["mode","maxConcurrency","checkpointEvery","errorPolicy","scheduleMs"],`${root}/execution`);
  if(!flow.enabled||!flow.name.trim()||! /^[a-z][a-z0-9]{0,63}$/.test(target.name))issue(root,"flow-destination");
  const derived=compileContinuousGraph(flow,target.continuous??{},bindings);
  report.diagnostics.push(...derived.diagnostics);
  report.outputs=derived.outputs;
  report.continuous=derived.continuous;
  if(!report.diagnostics.length)report.draft={id:"",revision:0,name:target.name,title:flow.name,object:"",when:"",manual:true,input:{},inputSchema:{type:"object",properties:{}},continuous:derived.continuous,steps:[{name:"intake",kind:"wait",condition:{op:"eq",left:{source:"literal",value:false},right:{source:"literal",value:true}}}],layout:{}};
  return report;
 }
 keys(flow.execution,["mode","maxConcurrency","checkpointEvery","errorPolicy","scheduleMs"],`${root}/execution`);
 if(flow.execution.mode!=="onDemand")issue(`${root}/execution/mode`,"flow-mode-owner");
 if(flow.execution.maxConcurrency!==1)issue(`${root}/execution/maxConcurrency`,"flow-concurrency-owner");
 if(flow.execution.checkpointEvery!==0||flow.execution.scheduleMs!==undefined)issue(`${root}/execution`,"flow-checkpoint-owner");
 if(flow.execution.errorPolicy!=="stop")issue(`${root}/execution/errorPolicy`,"flow-error-owner");
 if(!flow.enabled||flow.nodes.length===0||!flow.name.trim()||! /^[a-z][a-z0-9]{0,63}$/.test(target.name))issue(root,"flow-destination");
 const nodes=new Map(flow.nodes.map(n=>[n.id,n])),incoming=new Map<string,SourceFlowEdge[]>();
 for(const edge of flow.edges){
  const path=`${root}/edges/${pointer(edge.id)}`;keys(edge,["id","source","sourcePort","target","targetPort","enabled","label"],path);
  if(edge.enabled!==undefined&&typeof edge.enabled!=="boolean"||edge.label!==undefined&&typeof edge.label!=="string")issue(path,"flow-shape");
  const from=nodes.get(edge.source),to=nodes.get(edge.target);
  if(!from||!to||!ports(from.type)?.outputs.includes(edge.sourcePort)||!ports(to.type)?.inputs.includes(edge.targetPort))issue(path,"flow-port");
  if(edge.enabled===false)continue;
  const list=incoming.get(edge.target)??[];if(list.some(e=>e.targetPort===edge.targetPort))issue(path,"flow-input-writer");list.push(edge);incoming.set(edge.target,list);
 }
 const order:SourceFlowNode[]=[],remaining=new Set(nodes.keys());
 while(remaining.size){const ready=flow.nodes.filter(n=>remaining.has(n.id)&&(incoming.get(n.id)??[]).every(e=>!remaining.has(e.source)));if(!ready.length){issue(`${root}/edges`,"flow-cycle");break;}for(const n of ready){order.push(n);remaining.delete(n.id);}}
 order.forEach((node,i)=>report.ids[node.id]=`source${i+1}`);
 const steps:WorkflowStep[]=[],layout:NonNullable<WorkflowDraft["layout"]>={};
 for(const [i,node] of order.entries()){
  const path=`${root}/nodes/${pointer(node.id)}`,spec=ports(node.type),binding=Object.hasOwn(bindings,node.id)?bindings[node.id]:undefined,name=report.ids[node.id]!,next=i+1<order.length?`gate${i+2}`:"completed";
  keys(node,["id","type","name","x","y","config","disabled","retry","lastStatus"],path);
  if(!spec||spec.owner==="runtime"){issue(path,"flow-node-owner");continue;}
  if(node.disabled!==undefined&&node.disabled!==false)issue(`${path}/disabled`,"flow-disabled-owner");
  if(node.retry){keys(node.retry,["attempts","backoffMs"],`${path}/retry`);if(node.retry.attempts!==0||!Number.isFinite(node.retry.backoffMs)||node.retry.backoffMs<0)issue(`${path}/retry`,"flow-retry-owner");}
  if(spec.inputs.some(port=>!(incoming.get(node.id)??[]).some(e=>e.targetPort===port)))issue(path,"flow-port-required");
  const cap=binding&&target.capabilities.find(c=>c.ref.app===binding.app&&c.ref.name===binding.name&&c.kind===binding.kind&&c.version===binding.sourceVersion&&(c.revision??0)===binding.version);
  if(!binding||!object(binding)||typeof binding.sourceVersion!=="string"||!Number.isSafeInteger(binding.version)||binding.version<0||binding.inputs!==undefined&&!object(binding.inputs)||binding.kind!==spec.owner||!cap){issue(`${path}/binding`,"flow-capability-version");continue;}
  keys(binding,["app","kind","name","version","sourceVersion","object","inputs"],`${path}/binding`);
  const inputs:Record<string,Api.Binding>=Object.create(null);
  for(const edge of incoming.get(node.id)??[]){const producer=nodes.get(edge.source);inputs[edge.targetPort]={source:"step",step:report.ids[edge.source],path:producer?.type==="objectSource"?["records"]:[edge.sourcePort]};}
  const step:WorkflowStep={name,title:node.name,kind:spec.owner,next,error:"failed"};
  if(spec.owner==="query"){
   keys(node.config,["objectType","limit"],`${path}/config`);
   const definition=target.definitions.find(d=>d.ref.app===binding.app&&d.ref.kind==="query"&&d.ref.name===binding.name),query=definition?.queryVersions?.[binding.sourceVersion]??(definition?.version===binding.sourceVersion?definition.query:undefined);
   if(!query||query.object!==binding.object||typeof node.config.objectType!=="string"||!binding.object||!Number.isSafeInteger(node.config.limit)||Number(node.config.limit)<1||query.limit!==node.config.limit||Object.keys(cap.input?.properties??{}).length||Object.keys(binding.inputs??{}).length)issue(`${path}/binding`,"flow-query-scope");
   Object.assign(step,{app:binding.app,query:binding.name,queryVersion:binding.version});
  }else{
   if(cap.input?.type!=="object"||!cap.input.properties?.config||cap.output?.type!=="object"||spec.inputs.some(p=>cap.input?.properties?.[p]?.type!=="array"||cap.input.properties[p]?.nullable)||spec.outputs.some(p=>cap.output?.properties?.[p]?.type!=="array"||cap.output.properties[p]?.nullable||!cap.output.required?.includes(p))||cap.input.required?.some(p=>p!=="config"&&!spec.inputs.includes(p)))issue(`${path}/binding`,"flow-compute-ports");
   if(cap.input?.properties?.config&&schemaIssue(cap.input.properties.config,node.config))issue(`${path}/config`,"flow-config-schema");
   if(Object.keys(binding.inputs??{}).length)issue(`${path}/binding`,"flow-input-override");
   inputs.config={source:"literal",value:structuredClone(node.config)};
   Object.assign(step,{operation:{app:binding.app,name:binding.name,version:binding.version},inputs});
  }
  const gate:WorkflowStep={name:`gate${i+1}`,kind:"branch",condition:spec.inputs.length?{op:"all",terms:Object.entries(inputs).filter(([key])=>key!=="config").map(([,left])=>({op:"exists",left}))}:{op:"eq",left:{source:"literal",value:true},right:{source:"literal",value:true}},cases:{true:name,false:next}};
  steps.push(gate,step);layout[name]={x:node.x,y:node.y};layout[gate.name]={x:node.x-110,y:node.y};
 }
 steps.push({name:"completed",kind:"end",value:{source:"literal",value:{sourceFlow:flow.id}}},{name:"failed",kind:"fail",value:{source:"literal",value:"Imported flow request failed"}});
 if(steps.length>128)issue(root,"flow-native-budget");
 if(!report.diagnostics.length)report.draft={id:"",revision:0,name:target.name,title:flow.name,object:"",when:"",manual:true,input:{},inputSchema:{type:"object",properties:{}},steps,layout};
 return report;
}
