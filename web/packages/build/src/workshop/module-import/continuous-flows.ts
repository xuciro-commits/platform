import {schemaIssue} from "../../automate/workflow-schema";
import {parameterSchema} from "../../automate/workflow-model";
import type {Capability,WorkflowDraft,WorkflowStep} from "../../automate/workflow-model";
import type {Api} from "@platform/kernel";
import type {FlowBindings,FlowCapabilityBinding,FlowImportReport,FlowIntakeBinding,FlowIssue,SourceFlow,SourceFlowEdge,SourceFlowNode} from "./flows";

type Target={name:string;capabilities:Capability[];definitions:Api.Definition[];sources?:Api.Source[]};
type ObjectValue=Record<string,unknown>;
const pointer=(value:string)=>value.replaceAll("~","~0").replaceAll("/","~1");
const identifier=(value:unknown):value is string=>typeof value==="string"&&/^[A-Za-z_][A-Za-z0-9_]{0,127}$/.test(value);
const nativeName=(value:string)=>{const clean=value.toLowerCase().replace(/[^a-z0-9]/g,"").slice(0,48)||"output";return /^[a-z]/.test(clean)?clean:`output${clean}`;};
const CONTINUOUS_TYPES=["telemetrySource","window","aggregate","threshold","alertOutput","variableOutput","deadLetter"] as const;
type ContinuousType=typeof CONTINUOUS_TYPES[number];
const expectedEdges:Record<string,[string,string,string,string]>={
 "telemetrySource:batch:window:in":["telemetrySource","batch","window","in"],
 "window:window:aggregate:in":["window","window","aggregate","in"],
 "aggregate:alerts:threshold:in":["aggregate","alerts","threshold","in"],
 "threshold:triggered:alertOutput:in":["threshold","triggered","alertOutput","in"],
 "aggregate:stats:variableOutput:in":["aggregate","stats","variableOutput","in"],
 "window:late:deadLetter:in":["window","late","deadLetter","in"],
};
const path=(root:string,node:SourceFlowNode,suffix="")=>`${root}/nodes/${pointer(node.id)}${suffix}`;
const configKeys:Record<ContinuousType,string[]>={
 telemetrySource:["plants","batchSize","partitionBy","offset"],
 window:["windowMs","slideMs","watermarkMs","lateEvents","windowKind","maxRecords"],
 aggregate:["signal","threshold","kernel","groupBy"],
 threshold:["field","high","low","debounceMs","severity"],
 alertOutput:["severity","actionType"],
 variableOutput:["variableId","mode"],
 deadLetter:["maxRecords","retry","ttlMs"],
};
const readNumber=(value:unknown)=>typeof value==="number"&&Number.isFinite(value);
const readString=(value:unknown)=>typeof value==="string"&&value.length>0;
const dataBinding=(step:string,key:string[]):Api.Binding=>({source:"step",step,path:key});
const inputBinding=(...key:string[]):Api.Binding=>({source:"input",path:key});
const retryFields=(node:SourceFlowNode)=>node.retry?{retryAttempts:node.retry.attempts,retryBackoffMs:node.retry.backoffMs}:{};
const isActionBinding=(binding:FlowBindings[string]|undefined):binding is FlowCapabilityBinding=>!!binding&&binding.kind!=="source";
const validItemBinding=(binding:Api.Binding)=>binding.source==="item"&&Array.isArray(binding.path)&&binding.path.length>0&&binding.path.every(part=>typeof part==="string"&&part.length>0)||binding.source==="literal";

/** Compile the seven-node event-time telemetry graph into the platform's
 * continuous Flow contract plus its original retained Compute/Action owners.
 * No browser executor or alternate stream runner is produced. */
export function compileContinuousFlow(source:string,flow:SourceFlow,bindings:FlowBindings,target:Target,prior:FlowIssue[]=[]):FlowImportReport{
 const report:FlowImportReport={formatVersion:1,source,flow:flow.id,bindings:structuredClone(bindings),diagnostics:[...prior],ids:{},ready:false,outputs:{}};
 const issue=(at:string,code:string)=>report.diagnostics.push({path:at,code});
 const note=(at:string,code:string)=>report.diagnostics.push({path:at,code,blocking:false});
 const root=`/flows/${pointer(flow.id)}`;
 const keys=(value:object,allowed:string[],at:string)=>Object.keys(value).filter(key=>!allowed.includes(key)).forEach(key=>issue(`${at}/${pointer(key)}`,"unsupported-setting"));
 keys(flow,["id","name","description","enabled","version","nodes","edges","execution"],root);
 keys(flow.execution,["mode","maxConcurrency","checkpointEvery","errorPolicy","scheduleMs"],`${root}/execution`);
 if(flow.execution.mode!=="stream")issue(`${root}/execution/mode`,"flow-continuous-mode");
 if(!Number.isSafeInteger(flow.execution.maxConcurrency)||flow.execution.maxConcurrency<1||flow.execution.maxConcurrency>32)issue(`${root}/execution/maxConcurrency`,`flow-concurrency-invalid`);
 else if(flow.execution.maxConcurrency!==1)issue(`${root}/execution/maxConcurrency`,"flow-concurrency-owner");
 if(!Number.isSafeInteger(flow.execution.checkpointEvery)||flow.execution.checkpointEvery<0||flow.execution.checkpointEvery>1_000_000)issue(`${root}/execution/checkpointEvery`,"flow-checkpoint-invalid");
 if(flow.execution.errorPolicy!=="stop")issue(`${root}/execution/errorPolicy`,"flow-error-owner");
 if(flow.execution.scheduleMs!==1000)issue(`${root}/execution/scheduleMs`,"flow-schedule-owner");
 if(!flow.enabled||flow.nodes.length!==CONTINUOUS_TYPES.length||!flow.name.trim()||! /^[a-z][a-z0-9]{0,63}$/.test(target.name))issue(root,"flow-destination");
 const byType=new Map<ContinuousType,SourceFlowNode>();
 for(const node of flow.nodes){
  const at=path(root,node);
  keys(node,["id","type","name","x","y","config","disabled","retry","lastStatus"],at);
  if(!CONTINUOUS_TYPES.includes(node.type as ContinuousType)){issue(at,"flow-continuous-node");continue;}
  const type=node.type as ContinuousType;
  if(byType.has(type))issue(at,"flow-continuous-duplicate-node");else byType.set(type,node);
  keys(node.config,configKeys[type],`${at}/config`);
  if(node.disabled!==undefined&&node.disabled!==false)issue(`${at}/disabled`,"flow-disabled-owner");
  if(node.retry){keys(node.retry,["attempts","backoffMs"],`${at}/retry`);if(!Number.isSafeInteger(node.retry.attempts)||node.retry.attempts<0||node.retry.attempts>20||!Number.isSafeInteger(node.retry.backoffMs)||node.retry.backoffMs<0||node.retry.backoffMs>300000)issue(`${at}/retry`,"flow-retry-invalid");
   else if((type==="telemetrySource"||type==="window"||type==="deadLetter")&&(node.retry.attempts>0||node.retry.backoffMs>0))issue(`${at}/retry`,"flow-retry-owner");}
  if(node.lastStatus!==undefined)note(`${at}/lastStatus`,"flow-status-not-imported");
 }
 for(const type of CONTINUOUS_TYPES)if(!byType.has(type))issue(root,"flow-continuous-topology");
 const nodeFor=(type:ContinuousType)=>byType.get(type)??({id:`missing-${type}`,type,name:type,x:0,y:0,config:{}} as SourceFlowNode);
 const topology=(edge:SourceFlowEdge)=>{
  const from=flow.nodes.find(node=>node.id===edge.source),to=flow.nodes.find(node=>node.id===edge.target);
  return from&&to?`${from.type}:${edge.sourcePort}:${to.type}:${edge.targetPort}`:"";
 };
 const found=new Set<string>();
 for(const edge of flow.edges){
  const at=`${root}/edges/${pointer(edge.id)}`;
  keys(edge,["id","source","sourcePort","target","targetPort","enabled","label"],at);
  if(edge.enabled===false){issue(at,"flow-edge-disabled");continue;}
  const key=topology(edge);
  if(!Object.hasOwn(expectedEdges,key)||found.has(key)){issue(at,"flow-continuous-topology");continue;}
  found.add(key);
 }
 if(found.size!==Object.keys(expectedEdges).length||flow.edges.length!==Object.keys(expectedEdges).length)issue(`${root}/edges`,`flow-continuous-topology`);
 const stream=nodeFor("telemetrySource"),window=nodeFor("window"),aggregate=nodeFor("aggregate"),threshold=nodeFor("threshold"),alert=nodeFor("alertOutput"),output=nodeFor("variableOutput"),dead=nodeFor("deadLetter");
 if(output)issue(path(root,output),"flow-output-owner");
 if(dead)issue(path(root,dead),"flow-deadletter-owner");
 const sourceConfig=stream.config,windowConfig=window.config,aggregateConfig=aggregate.config,thresholdConfig=threshold.config,alertConfig=alert.config,outputConfig=output.config,deadConfig=dead.config;
 const numeric=(node:SourceFlowNode,key:string,min:number,max:number,at=path(root,node,`/config/${key}`))=>{const value=node.config[key];if(!Number.isSafeInteger(value)||Number(value)<min||Number(value)>max)issue(at,"flow-config-range");return Number(value);};
 const text=(node:SourceFlowNode,key:string,at=path(root,node,`/config/${key}`))=>{const value=node.config[key];if(!readString(value))issue(at,"flow-config-value");return typeof value==="string"?value:"";};
 const deadLetterMaxRecords=numeric(dead,"maxRecords",1,1_000_000);
 const windowMaxRecords=windowConfig.maxRecords===undefined?deadLetterMaxRecords:numeric(window,"maxRecords",1,1_000_000);
 const batchSize=numeric(stream,"batchSize",1,100_000);
 const windowMs=numeric(window,"windowMs",100,3_600_000),slideMs=numeric(window,"slideMs",1,3_600_000),watermarkMs=numeric(window,"watermarkMs",0,600_000);
 if(slideMs>windowMs)issue(path(root,window,"/config/slideMs"),"flow-config-range");
 if(windowConfig.windowKind!==undefined&&windowConfig.windowKind!=="sliding")issue(path(root,window,"/config/windowKind"),"flow-window-kind");
 if(windowConfig.lateEvents!=="sideOutput")issue(path(root,window,"/config/lateEvents"),"flow-late-policy");
 const offsetValue=sourceConfig.offset===undefined?"latest":sourceConfig.offset;
 if(offsetValue!=="latest"&&offsetValue!=="earliest")issue(path(root,stream,"/config/offset"),"flow-offset-policy");
 const offset:"latest"|"earliest"=offsetValue==="earliest"?"earliest":"latest";
 if(sourceConfig.plants!==undefined&&(!Array.isArray(sourceConfig.plants)||sourceConfig.plants.length>0))issue(path(root,stream,"/config/plants"),"flow-source-filter");
 const partitionBy=sourceConfig.partitionBy;
 if(!(typeof partitionBy==="string"&&identifier(partitionBy)||Array.isArray(partitionBy)&&partitionBy.length>0&&partitionBy.every(identifier)))issue(path(root,stream,"/config/partitionBy"),"flow-intake-columns");
 const signal=text(aggregate,"signal"),kernel=text(aggregate,"kernel"),groupBy=aggregateConfig.groupBy;
 if(!readNumber(aggregateConfig.threshold)||Number(aggregateConfig.threshold)<0)issue(path(root,aggregate,"/config/threshold"),"flow-config-value");
 if(!Array.isArray(groupBy)||groupBy.length>8||groupBy.some(field=>!identifier(field)))issue(path(root,aggregate,"/config/groupBy"),"flow-intake-columns");
 const sourceThresholdField=text(threshold,"field"),high=thresholdConfig.high,low=thresholdConfig.low;
 if(!readNumber(high)||!readNumber(low)||Number(low)>Number(high))issue(path(root,threshold,"/config"),"flow-threshold-range");
 numeric(threshold,"debounceMs",0,3_600_000);
 const variableId=text(output,"variableId"),outputMode=outputConfig.mode??"replace";
 if(outputMode!=="replace")issue(path(root,output,"/config/mode"),"flow-output-mode");
 const ttlMs=numeric(dead,"ttlMs",0,366*24*60*60*1000);
 if(deadConfig.retry!==undefined&&typeof deadConfig.retry!=="boolean")issue(path(root,dead,"/config/retry"),"flow-config-value");
 if(deadConfig.retry===true)issue(path(root,dead,"/config/retry"),"flow-deadletter-replay");
 if(!readString(alertConfig.severity)||!readString(alertConfig.actionType))issue(path(root,alert,"/config"),"flow-config-value");
 if(alertConfig.severity!==undefined&&thresholdConfig.severity!==undefined&&alertConfig.severity!==thresholdConfig.severity)issue(path(root,alert,"/config/severity"),"flow-alert-severity");
 const sourceBinding=bindings[stream.id];
 if(!sourceBinding||sourceBinding.kind!=="source")issue(`${path(root,stream)}/binding`,"flow-source-binding");
 const intakeBinding=sourceBinding?.kind==="source"?sourceBinding as FlowIntakeBinding:undefined;
 let sourceRecord:Api.Source|undefined;
 if(intakeBinding){
  keys(intakeBinding,["kind","sourceRecord","key","partition","eventTime","value","offset"],`${path(root,stream)}/binding`);
  sourceRecord=target.sources?.find(record=>record.id===intakeBinding.sourceRecord);
  if(!sourceRecord||!sourceRecord.stream||sourceRecord.state!=="published"||sourceRecord.profile!=="table"||!sourceRecord.since)issue(`${path(root,stream)}/binding/sourceRecord`,"flow-source-record");
  if(!identifier(intakeBinding.key)||!identifier(intakeBinding.eventTime)||!identifier(intakeBinding.value)||!Array.isArray(intakeBinding.partition)||intakeBinding.partition.length<1||intakeBinding.partition.length>8||intakeBinding.partition.some(field=>!identifier(field)))issue(`${path(root,stream)}/binding`,"flow-intake-columns");
  const unique=[intakeBinding.key,intakeBinding.eventTime,intakeBinding.value,...(intakeBinding.partition??[])];
  if(new Set(unique).size!==unique.length)note(`${path(root,stream)}/binding`,`flow-intake-overlap`);
  if(intakeBinding.offset!==offset)issue(`${path(root,stream)}/binding/offset`,"flow-offset-mismatch");
  if(signal!==intakeBinding.value)issue(path(root,aggregate,"/config/signal"),"flow-signal-source-mismatch");
  if(typeof partitionBy==="string"&&!intakeBinding.partition.includes(partitionBy)||Array.isArray(partitionBy)&&partitionBy.some(field=>!intakeBinding.partition.includes(field)))issue(`${path(root,stream)}/binding/partition`,"flow-partition-mismatch");
  if(Array.isArray(groupBy)&&JSON.stringify(intakeBinding.partition)!==JSON.stringify(groupBy))issue(path(root,aggregate,"/config/groupBy"),"flow-partition-mismatch");
 }else if(sourceBinding)issue(`${path(root,stream)}/binding`,"flow-source-binding");
 for(const id of Object.keys(bindings))if(id!==stream.id&&!byTypeHasNode(flow.nodes,id))issue(`${root}/bindings/${pointer(id)}`,"flow-binding-unused");
 const sourceId=sourceRecord?.id??"",sourceName=sourceRecord?.name??"";
 const capabilityFor=(node:SourceFlowNode,kind:"compute"|"action")=>{
  const binding=bindings[node.id];
  if(!isActionBinding(binding)||binding.kind!==kind){issue(`${path(root,node)}/binding`,kind==="action"?"flow-action-binding":"flow-capability-version");return undefined;}
  const capability=target.capabilities.find(item=>item.kind===kind&&item.ref.app===binding.app&&item.ref.name===binding.name&&item.version===binding.sourceVersion&&(item.revision??0)===binding.version);
  const definition=target.definitions.find(item=>item.ref.app===binding.app&&item.ref.name===binding.name&&item.ref.kind===(kind==="action"?"action":"operation")&&item.version===binding.sourceVersion);
  if(!capability||!definition){issue(`${path(root,node)}/binding`,kind==="action"?"flow-action-version":"flow-capability-version");return undefined;}
  keys(binding,["app","kind","name","version","sourceVersion","object","inputs","target"],`${path(root,node)}/binding`);
  return {binding,capability,definition};
 };
 const aggregateTarget=capabilityFor(aggregate,"compute"),thresholdTarget=capabilityFor(threshold,"compute"),actionTarget=capabilityFor(alert,"action");
 const checkCompute=(node:SourceFlowNode,targetRef:ReturnType<typeof capabilityFor>,config:ObjectValue,inputs:string[],outputs:string[])=>{
  if(!targetRef)return;
  const {capability,definition,binding}=targetRef,operation=definition.operation;
  if(!operation){issue(`${path(root,node)}/binding`,"flow-compute-ports");return;}
  if(capability.input?.type!=="object"||capability.input.nullable||!capability.input.properties?.config||capability.input.properties.config.nullable||capability.output?.type!=="object"||capability.output.nullable||inputs.some(name=>capability.input?.properties?.[name]?.type!=="array"||capability.input.properties[name]?.nullable)||outputs.some(name=>capability.output?.properties?.[name]?.type!=="array"||capability.output.properties[name]?.nullable||!capability.output.required?.includes(name))||capability.input.required?.some(name=>name!=="config"&&!inputs.includes(name)))issue(`${path(root,node)}/binding`,"flow-compute-ports");
  if(capability.input?.properties?.config&&schemaIssue(capability.input.properties.config,config))issue(path(root,node,"/config"),"flow-config-schema");
  if(Object.keys(binding.inputs??{}).length)issue(`${path(root,node)}/binding/inputs`,"flow-input-override");
  if(operation.limits.maxInputBytes<1||operation.limits.maxOutputBytes<1||operation.limits.maxOutputBytes>48*1024)note(`${path(root,node)}/binding`,"flow-compute-output-budget");
  if(node===aggregate&&kernel!=="wasm:f32-window-stats")issue(path(root,node,"/config/kernel"),"flow-aggregate-kernel");
 };
 checkCompute(aggregate,aggregateTarget,aggregateConfig,["in"],["stats","alerts"]);
 const mappedThreshold=structuredClone(thresholdConfig);
 if(kernel==="wasm:f32-window-stats"&&sourceThresholdField==="mean"){
  mappedThreshold.field="reading";
  note(path(root,threshold,"/config/field"),"flow-threshold-mean-reading");
 }
 checkCompute(threshold,thresholdTarget,mappedThreshold,["in"],["triggered","normal"]);
 if(actionTarget){
  const {binding,capability,definition}=actionTarget,action=definition.action;
  if(!action||action.needsApproval||action.automation||action.new||capability.target!==action.target)issue(`${path(root,alert)}/binding`,"flow-action-owner");
  const targetBinding=binding.target;
  if(!targetBinding||!validItemBinding(targetBinding)||targetBinding.source==="literal"&&(typeof targetBinding.value!=="string"||targetBinding.value.length===0))issue(`${path(root,alert)}/binding/target`,"flow-action-target");
  const payload=action?.payload??[],mapped=binding.inputs??{};
  for(const field of payload){
   const value=mapped[field.name];
   if(field.required&&!value)issue(`${path(root,alert)}/binding/inputs/${pointer(field.name)}`,"flow-action-input-required");
   if(value&&!validItemBinding(value))issue(`${path(root,alert)}/binding/inputs/${pointer(field.name)}`,"flow-action-input-source");
   const schema=parameterSchema(field);
   if(value?.source==="literal"&&schema&&schemaIssue(schema,value.value))issue(`${path(root,alert)}/binding/inputs/${pointer(field.name)}`,"flow-action-input-schema");
  }
  for(const name of Object.keys(mapped))if(!payload.some(field=>field.name===name))issue(`${path(root,alert)}/binding/inputs/${pointer(name)}`,"flow-action-input-unknown");
 }
 const outputName=nativeName(`publish${variableId}`);
 report.ids[stream.id]="source";report.ids[window.id]="windowstate";report.ids[aggregate.id]="aggregate";report.ids[threshold.id]="threshold";report.ids[alert.id]="emitalert";report.ids[output.id]=outputName;report.ids[dead.id]="deadletter";
 if(variableId)report.outputs![variableId]=outputName;
 if(window&&windowMaxRecords>10_000)issue(path(root,window,"/config/maxRecords"),"flow-loop-budget");
 const operationRef=(ref:NonNullable<typeof aggregateTarget>)=>({app:ref.binding.app,name:ref.binding.name,version:ref.binding.version});
 const actionDef=actionTarget?.definition.action;
 const steps:WorkflowStep[]=[];
 if(aggregateTarget)steps.push({name:"aggregate",title:aggregate.name,kind:"compute",operation:operationRef(aggregateTarget),inputs:{config:{source:"literal",value:structuredClone(aggregateConfig)},in:inputBinding("signals")},next:"routeoutputs",error:"flowfailed",...retryFields(aggregate)});
 else steps.push({name:"aggregate",title:aggregate.name,kind:"compute",operation:{app:"",name:"",version:0},inputs:{config:{source:"literal",value:structuredClone(aggregateConfig)},in:inputBinding("signals")},next:"routeoutputs"});
 steps.push({name:"routeoutputs",title:"Publish statistics and evaluate alerts",kind:"fork",branches:[outputName,"threshold"],mode:"all",next:"completed"});
 steps.push({name:outputName,title:output.name,kind:"transform",value:dataBinding("aggregate",["stats"]),next:"statsdone",error:"flowfailed",...retryFields(output)});
 steps.push({name:"statsdone",kind:"end",value:dataBinding(outputName,[])});
 const thresholdInputs:Record<string,Api.Binding>={config:{source:"literal",value:mappedThreshold},in:dataBinding("aggregate",["alerts"])};
 if(thresholdTarget)steps.push({name:"threshold",title:threshold.name,kind:"compute",operation:operationRef(thresholdTarget),inputs:thresholdInputs,next:"alertsloop",error:"flowfailed",...retryFields(threshold)});
 else steps.push({name:"threshold",title:threshold.name,kind:"compute",operation:{app:"",name:"",version:0},inputs:thresholdInputs,next:"alertsloop"});
 const actionBinding=actionTarget?.binding;
 steps.push({name:"alertsloop",title:"Emit every triggered alert",kind:"foreach",collection:dataBinding("threshold",["triggered"]),body:"emitalert",next:"alertsdone",maxIterations:windowMaxRecords,concurrency:1});
 steps.push({name:"emitalert",title:alert.name,kind:"action",act:actionDef?.schema??"",target:actionBinding?.target,inputs:actionBinding?.inputs??{},next:"",error:"flowfailed",...retryFields(alert)});
 steps.push({name:"alertsdone",kind:"end"});
 steps.push({name:"completed",kind:"end",value:{source:"literal",value:{sourceFlow:flow.id}}});
 steps.push({name:"flowfailed",kind:"fail",title:"Imported stream step failed"});
 const layout:NonNullable<WorkflowDraft["layout"]>={aggregate:{x:420,y:160},routeoutputs:{x:700,y:160},[outputName]:{x:950,y:60},statsdone:{x:1180,y:60},threshold:{x:950,y:260},alertsloop:{x:1180,y:260},emitalert:{x:1420,y:260},alertsdone:{x:1640,y:260},completed:{x:1880,y:160},flowfailed:{x:1640,y:420}};
 const diagnostics=report.diagnostics,blocked=diagnostics.some(item=>item.blocking!==false);
 report.ready=!blocked;
 {
  const windowMax=Math.min(windowMaxRecords,1_000_000);
  if([stream,window,aggregate,threshold,alert,output,dead].every(Boolean)){
  const intake=intakeBinding??{sourceRecord:sourceId,key:"",partition:["deviceId"],eventTime:"",value:"",offset};
  report.draft={id:"",revision:0,name:target.name,title:flow.name,object:"",when:"",manual:false,input:{},inputSchema:{type:"object",properties:{}},steps,layout,
   continuous:{Source:sourceName,entry:"aggregate",Batch:batchSize,State:16*1024*1024,FrameBytes:64*1024*1024,checkpointEvery:flow.execution.checkpointEvery,
    Window:{node:"windowstate",windowMs,slideMs,watermarkMs,maxRecords:windowMax,lateEvents:"sideOutput"},
    Intake:{sourceRecord:sourceId,key:intake.key,partition:intake.partition,eventTime:intake.eventTime,value:intake.value,offset},DeadLetter:true,deadLetterMaxRecords,deadLetterTtlMs:ttlMs}};
 }
 }
 return report;
}
function byTypeHasNode(nodes:SourceFlowNode[],id:string){return nodes.some(node=>node.id===id);}
