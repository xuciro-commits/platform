import type {Api} from "@platform/kernel";
import type {FlowBindings,FlowIssue,SourceFlow,SourceFlowNode} from "./flows";

/** The runtime nodes of a continuous graph, folded by the Flow owner itself
 * (ADR-0047 §13.1/§13.3). Every other node type needs an owner the platform
 * does not have yet, so it is refused rather than approximated. */
export const continuousNodeTypes=["telemetrySource","window","aggregate","threshold","alertOutput","variableOutput","deadLetter"] as const;
export type ContinuousNodeType=(typeof continuousNodeTypes)[number];
export const isContinuousNode=(type:string):type is ContinuousNodeType=>(continuousNodeTypes as readonly string[]).includes(type);

/** What the graph binds outside the declaration: the read-only asset its
 * statistics feed, and the action its alerts are delivered to. */
export type ContinuousOutputs={stats?:string;acknowledge?:string};
export type ContinuousCompilation={continuous?:Api.Continuous;outputs:ContinuousOutputs;diagnostics:FlowIssue[]};

const measures=["count","sum","mean","min","max"];
/** The host pulls continuous sources on its own one-second work tick
 * (platformserver.runWorkFrom); the graph may name that rate, not another. */
const streamTickMs=1000;
const alertFields=["group","node","field","severity","state","batch"];
/** The keys the Flow settings own: the real Source record and its columns. The
 * graph owns the runtime chain below, so a settings copy of it is refused. */
const settingsKeys=["Source","State","FrameBytes","Intake"] as const;
const graphKeys=["Batch","Window","Aggregate","Threshold","Effects","DeadLetter","CheckpointEvery"] as const;
const bounded=(v:unknown,low:number,high:number)=>Number.isSafeInteger(v)&&(v as number)>=low&&(v as number)<=high;
const count=(v:unknown):v is number=>Number.isFinite(v);

/** compileContinuousGraph derives the platform's continuous declaration from a
 * graph's runtime nodes, their ports and the node bindings the importer chose —
 * the only place the two vocabularies meet, so a field/port mismatch (the
 * imported graph's mean→reading) becomes a refusal here and at save/install,
 * never a silent never-firing node. The alert's action is a tenant binding like
 * any other node's capability: the graph names the intent, the binding names the
 * exact declared action. Keys the graph does not own stay exactly as the Flow
 * settings bound them. */
export function compileContinuousGraph(flow:SourceFlow,settings:Partial<Api.Continuous>,bindings:FlowBindings={}):ContinuousCompilation{
 const diagnostics:FlowIssue[]=[],outputs:ContinuousOutputs={},root=`/flows/${flow.id}`;
 const issue=(path:string,code:string)=>{diagnostics.push({path,code});};
 for(const key of graphKeys)if(settings[key]!==undefined)issue(`${root}/continuous/${key}`,"flow-continuous-owner");
 for(const node of flow.nodes)if(!isContinuousNode(node.type))issue(`${root}/nodes/${node.id}`,"flow-node-owner");
 const nodes=flow.nodes.filter(n=>isContinuousNode(n.type));
 const one=(type:ContinuousNodeType,required=true)=>{const found=nodes.filter(n=>n.type===type);if(found.length>1||(required&&found.length!==1))issue(`${root}/nodes`,"flow-continuous-shape");return found[0];};
 const source=one("telemetrySource"),window=one("window"),aggregate=one("aggregate"),threshold=one("threshold"),dlq=one("deadLetter"),output=one("variableOutput",false),alert=one("alertOutput",false);
 const path=(node:SourceFlowNode|undefined)=>node?`${root}/nodes/${node.id}`:root;
 const config=(node:SourceFlowNode,allowed:string[]):Record<string,unknown>=>{
  for(const key of Object.keys(node.config))if(!allowed.includes(key))issue(`${path(node)}/config/${key}`,"unsupported-setting");
  return node.config;
 };
 const wired=(from:SourceFlowNode|undefined,fromPort:string,to:SourceFlowNode|undefined,toPort:string)=>{
  if(!from||!to)return false;
  const matched=flow.edges.filter(e=>e.source===from.id&&e.sourcePort===fromPort);
  if(matched.length!==1||matched[0]!.target!==to.id||matched[0]!.targetPort!==toPort){issue(`${root}/edges`,"flow-continuous-edge");return false;}
  return true;
 };
 const unused=flow.edges.filter(e=>{
  const from=flow.nodes.find(n=>n.id===e.source),to=flow.nodes.find(n=>n.id===e.target);
  return !from||!to||!isContinuousNode(from.type)||!isContinuousNode(to.type)||
   !wired(from,e.sourcePort,to,e.targetPort);
 });
 if(unused.length)issue(`${root}/edges`,"flow-continuous-edge");
 // The source node owns its intake budget; the Source record, its columns and
 // the state/frame budgets stay with the Flow settings.
 let batch=0;
 if(source){
  const c=config(source,["batchSize"]);
  if(!bounded(c.batchSize,1,1000000))issue(`${path(source)}/config/batchSize`,"flow-continuous-config");
  else batch=c.batchSize as number;
 }
 if(settings.Source===""||settings.Source===undefined)issue(`${root}/continuous/Source`,"flow-continuous-source");
 // window
 if(window){
  const c=config(window,["windowMs","slideMs","watermarkMs","maxRecords","lateEvents","windowKind"]);
  if(c.windowKind!==undefined&&c.windowKind!=="sliding"&&c.windowKind!=="tumbling")issue(`${path(window)}/config/windowKind`,"flow-continuous-config");
  if(c.windowKind==="tumbling"&&c.slideMs!==c.windowMs)issue(`${path(window)}/config/slideMs`,"flow-continuous-config");
  if(!count(c.windowMs)||!count(c.slideMs)||!count(c.watermarkMs)||!bounded(c.maxRecords,1,1000000)||!["sideOutput","accept","reject"].includes(c.lateEvents as string))issue(`${path(window)}/config`,"flow-continuous-config");
 }
 // aggregate: the declared measure names are the only field names it produces.
 let measuresDeclared:string[]=[];
 if(aggregate){
  const c=config(aggregate,["signal","groupBy","measures","kernel"]);
  const list=Array.isArray(c.measures)?c.measures.filter((m):m is string=>typeof m==="string"):[];
  if(!list.length||list.length>measures.length||new Set(list).size!==list.length||list.some(m=>!measures.includes(m)))issue(`${path(aggregate)}/config/measures`,"flow-aggregate-measures");
  else measuresDeclared=list;
  if(c.groupBy!==undefined&&(!Array.isArray(c.groupBy)||c.groupBy.length>8||c.groupBy.some(g=>typeof g!=="string")))issue(`${path(aggregate)}/config/groupBy`,"flow-continuous-config");
  if(c.signal!==undefined&&typeof c.signal!=="string")issue(`${path(aggregate)}/config/signal`,"flow-continuous-config");
  if(c.kernel!==undefined&&c.kernel!=="native")issue(`${path(aggregate)}/config/kernel`,"flow-aggregate-kernel");
 }
 // threshold: its field must be one of the aggregate's measure names.
 if(threshold){
  const c=config(threshold,["field","high","low","debounceMs","severity"]);
  if(typeof c.field!=="string"||!measuresDeclared.includes(c.field))issue(`${path(threshold)}/config/field`,"flow-threshold-field");
  if(!count(c.high)||!count(c.low)||!count(c.debounceMs)||(c.low as number)>=(c.high as number)||!bounded(c.debounceMs,0,600000))issue(`${path(threshold)}/config`,"flow-continuous-config");
  if(c.severity!==undefined&&typeof c.severity!=="string")issue(`${path(threshold)}/config/severity`,"flow-continuous-config");
 }
 // the alert output is the action binding: one effect per threshold node, and
 // the bound action is the exact schema the declaring app must own.
 let effects:Api.StreamEffect[]|undefined;
 if(alert){
  const c=config(alert,["actionType","targetField","states","severity"]);
  const bound=bindings[alert.id],action=bound?.kind==="action"?bound.name:"";
  if(!action)issue(`${path(alert)}/binding`,"flow-effect-action");
  if(bound&&Object.keys(bound).some(k=>!["app","kind","name","version","sourceVersion"].includes(k)))issue(`${path(alert)}/binding`,"flow-effect-action");
  if(c.targetField!==undefined&&!alertFields.includes(c.targetField as string))issue(`${path(alert)}/config/targetField`,"flow-effect-action");
  const states=Array.isArray(c.states)?c.states.filter((s):s is string=>typeof s==="string"):[];
  if(c.states!==undefined&&(!states.length||states.some(s=>s!=="triggered"&&s!=="cleared")))issue(`${path(alert)}/config/states`,"flow-effect-action");
  if(action){effects=[{node:threshold?.id??"",action,...(c.targetField?{target:c.targetField as string}:{}),...(states.length?{states}: {})}];outputs.acknowledge=action;}
 }
 // the statistics output is the read-only asset the Flow publishes.
 if(output){
  const c=config(output,["variableId","mode"]);
  if(typeof c.variableId!=="string"||!c.variableId||c.mode!=="replace")issue(`${path(output)}/config`,"flow-continuous-output");
  else outputs.stats=c.variableId;
 }
 // the dead-letter asset is required and bounded; letters are kept for replay.
 if(dlq){
  const c=config(dlq,["maxRecords","ttlMs","retry"]);
  if(!bounded(c.maxRecords,1,1000000)||c.ttlMs!==undefined&&!bounded(c.ttlMs,0,30*24*60*60*1000)||c.retry!==true)issue(`${path(dlq)}/config`,"flow-continuous-config");
  wired(window,"late",dlq,"in");
 }
 // the operator chain is these ports, and nothing else.
 wired(source,"batch",window,"in");
 wired(window,"window",aggregate,"in");
 wired(aggregate,"alerts",threshold,"in");
 if(output)wired(aggregate,"stats",output,"in");
 if(alert)wired(threshold,"triggered",alert,"in");
 const execution=flow.execution;
 if(execution.mode!=="stream")issue(`${root}/execution/mode`,"flow-mode-owner");
 if(execution.maxConcurrency!==1)issue(`${root}/execution/maxConcurrency`,"flow-concurrency-owner");
 if(execution.errorPolicy!=="deadLetter")issue(`${root}/execution/errorPolicy`,"flow-error-owner");
 // The stream is pulled by the host's own one-second tick; a graph asking for
 // another rate has no owner for it here.
 if(execution.scheduleMs!==undefined&&execution.scheduleMs!==streamTickMs)issue(`${root}/execution/scheduleMs`,"flow-schedule-owner");
 if(!bounded(execution.checkpointEvery,0,1000000))issue(`${root}/execution/checkpointEvery`,"flow-checkpoint-owner");
 if(diagnostics.length)return {outputs,diagnostics};
 const declared:Api.Continuous={...settings,Source:settings.Source!,Batch:batch,State:settings.State??0,FrameBytes:settings.FrameBytes??0,
  Window:{node:window!.id,windowMs:window!.config.windowMs as number,slideMs:window!.config.slideMs as number,watermarkMs:window!.config.watermarkMs as number,maxRecords:window!.config.maxRecords as number,lateEvents:window!.config.lateEvents as Api.StreamWindow["lateEvents"]},
  Aggregate:{node:aggregate!.id,signal:(aggregate!.config.signal as string|undefined)||undefined,group:(aggregate!.config.groupBy as string[]|undefined)??[],measures:measuresDeclared},
  Threshold:{node:threshold!.id,field:threshold!.config.field as string,high:threshold!.config.high as number,low:threshold!.config.low as number,debounceMs:threshold!.config.debounceMs as number,...(threshold!.config.severity?{severity:threshold!.config.severity as string}:{})},
  DeadLetter:{node:dlq!.id,maxRecords:dlq!.config.maxRecords as number,...(dlq!.config.ttlMs!==undefined?{ttlMs:dlq!.config.ttlMs as number}:{})}};
 if(execution.checkpointEvery>0)declared.CheckpointEvery=execution.checkpointEvery;
 if(effects)declared.Effects=effects;
 return {continuous:declared,outputs,diagnostics};
}

/** The settings a continuous draft must carry: every key the graph does not
 * own. Kept beside the compiler so both the dialog and the server check name
 * the same split. */
export const continuousSettingsKeys=settingsKeys;

/** The Flow settings inside a saved draft's continuous declaration. A saved
 * draft holds the whole last derivation, so re-importing a graph must hand the
 * compiler only these keys: the graph re-derives the rest, and a caller that
 * hands over the declaration itself is refused (flow-continuous-owner) instead
 * of being silently trusted. */
export const continuousSettings=(c?:Partial<Api.Continuous>):Partial<Api.Continuous>=>c?Object.fromEntries(Object.entries(c).filter(([k])=>(settingsKeys as readonly string[]).includes(k))) as Partial<Api.Continuous>:{};
