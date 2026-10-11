import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync} from 'node:fs';
import {registerHooks} from 'node:module';

const uiStub='export const t=(key,values={})=>String(key).replace(/\\{(\\w+)\\}/g,(_,name)=>String(values[name]??""));';
registerHooks({resolve(specifier,context,nextResolve){
 if(specifier==='@platform/ui')return {url:`data:text/javascript,${encodeURIComponent(uiStub)}`,shortCircuit:true};
 try{return nextResolve(specifier,context);}catch(error){if(specifier.startsWith('./')||specifier.startsWith('../'))return nextResolve(`${specifier}.ts`,context);throw error;}
}});
const {compileFlowSource,parseFlowSource}=await import('./flows.ts');
const moduleFixture=JSON.parse(readFileSync(new URL('./default.workshop.json',import.meta.url),'utf8'));
const originalFlow=moduleFixture.flows.find(flow=>flow.id==='flow-telemetry');
const aggregateSchema={type:'object',properties:{signal:{type:'string'},threshold:{type:'number'},kernel:{type:'string'},groupBy:{type:'array',items:{type:'string'},maxItems:8}},required:['signal','threshold','kernel','groupBy']};
const thresholdSchema={type:'object',properties:{field:{type:'string'},high:{type:'number'},low:{type:'number'},debounceMs:{type:'integer'},severity:{type:'string'}},required:['field','high','low','debounceMs','severity']};
const configSchema=(config)=>({type:'object',properties:{config, in:{type:'array',items:{type:'object'}}},required:['config','in']});
const outputSchema=(ports)=>({type:'object',properties:Object.fromEntries(ports.map(name=>[name,{type:'array',items:{type:'object'}}])),required:ports});
const capability=(kind,name,version,revision,input,output,target)=>({kind,ref:{app:kind==='action'?'alerts':'analytics',kind:kind==='action'?'action':'operation',name},title:name,description:'',group:'',icon:'',tone:'neutral',source:'application',version,revision,target,input,output,effects:[],ports:[]});
const operationDefinition=(name,version,input,output)=>({ref:{app:'analytics',kind:'operation',name},source:'application',version,contractVersion:1,requires:[],operation:{name,title:name,description:'',input,output,roles:[],binding:{kind:'wasm',module:'stats',abi:'1'},limits:{timeoutMillis:1000,memoryPages:64,maxInputBytes:1_048_576,maxOutputBytes:48*1024}}});
function fixture({source=originalFlow,concurrency=1,deadLetterRetry=false,preserveDefaults=false}={}){
 const flow=structuredClone(source);flow.execution.maxConcurrency=concurrency;flow.nodes.find(node=>node.type==='deadLetter').config.retry=deadLetterRetry;
 if(!preserveDefaults){flow.execution.checkpointEvery=0;flow.execution.errorPolicy='stop';for(const type of ['telemetrySource','window','deadLetter']){const node=flow.nodes.find(item=>item.type===type);if(node.retry)node.retry={attempts:0,backoffMs:0};}}
 const aggregateName='windowStats',thresholdName='alertThreshold',actionName='acknowledgeAlert';
 const aggregateInput=configSchema(aggregateSchema),aggregateOutput=outputSchema(['stats','alerts']),thresholdInput=configSchema(thresholdSchema),thresholdOutput=outputSchema(['triggered','normal']);
 const aggregateOperation={type:'object',properties:aggregateInput.properties,required:aggregateInput.required},thresholdOperation={type:'object',properties:thresholdInput.properties,required:thresholdInput.required};
 const action={schema:'alerts.acknowledgeAlert',target:'alerts.alert',capability:'alerts',title:'Acknowledge alert',description:'',payload:[{name:'reading',type:'decimal',required:true,description:'Reading'},{name:'severity',type:'text',required:true,description:'Severity'}],needsApproval:false,automation:false,new:false};
 const capabilities=[capability('compute',aggregateName,'3.2',7,aggregateInput,aggregateOutput),capability('compute',thresholdName,'2.1',4,thresholdInput,thresholdOutput),capability('action',actionName,'1.4',2,undefined,undefined,action.target)];
 const definitions=[operationDefinition(aggregateName,'3.2',aggregateOperation,aggregateOutput),operationDefinition(thresholdName,'2.1',thresholdOperation,thresholdOutput),{ref:{app:'alerts',kind:'action',name:actionName},source:'application',version:'1.4',contractVersion:1,requires:[],action}];
 const sources=[{id:'source-telemetry',revision:1,name:'planttelemetry',title:'Plant telemetry',stream:true,state:'published',profile:'table',since:'sequence'}];
 const bindings={
  'flow-stream':{kind:'source',sourceRecord:'source-telemetry',key:'eventId',partition:['plantId','deviceId'],eventTime:'eventTime',value:'vibration',offset:'latest'},
  'flow-wasm':{kind:'compute',app:'analytics',name:aggregateName,version:7,sourceVersion:'3.2'},
  'flow-threshold':{kind:'compute',app:'analytics',name:thresholdName,version:4,sourceVersion:'2.1'},
  'flow-alert':{kind:'action',app:'alerts',name:actionName,version:2,sourceVersion:'1.4',target:{source:'item',path:['alertId']},inputs:{reading:{source:'item',path:['reading']},severity:{source:'literal',value:'High'}}},
 };
 return {flow,source:JSON.stringify(flow),target:{name:'criticalvibration',capabilities,definitions,sources},bindings};
}
const blocking=(report)=>report.diagnostics.filter(issue=>issue.blocking!==false);

test('compiles the original seven-node stream with f32 kernel, eight workers and retained output',()=>{
 const input=fixture({concurrency:8,deadLetterRetry:true,preserveDefaults:true});
 delete input.bindings['flow-wasm'];delete input.bindings['flow-threshold'];
 const report=compileFlowSource(input.source,input.flow.id,input.bindings,input.target);
 assert.equal(report.source,input.source);assert.equal(report.ready,true,JSON.stringify(report.diagnostics));
 const c=report.draft.continuous;
 assert.equal(report.draft.manual,false);assert.equal(c.entry,'alertsloop');assert.equal(c.concurrency,8);
 assert.equal(c.checkpointEvery,5000);assert.equal(c.Batch,512);assert.equal(c.Window.windowMs,30000);
 assert.equal(c.Window.slideMs,5000);assert.equal(c.Window.watermarkMs,2000);
 assert.equal(c.aggregate.kernel,'wasm:f32-window-stats');assert.equal(c.aggregate.above,11.5);
 assert.deepEqual(c.aggregate.group,['plantId','deviceId']);assert.ok(c.aggregate.measures.includes('above'));
 assert.equal(c.threshold.field,'mean');assert.equal(c.threshold.high,11.5);assert.equal(c.threshold.low,9.5);
 assert.equal(c.threshold.debounceMs,3000);assert.equal(c.output.name,'liveTelemetryStats');
 assert.equal(c.DeadLetter,true);assert.equal(c.deadLetterMaxRecords,10000);assert.equal(c.deadLetterTtlMs,86400000);
 const steps=new Map(report.draft.steps.map(step=>[step.name,step]));
 assert.deepEqual(steps.get('alertsloop').collection,{source:'input',path:['triggered']});
 assert.equal(steps.get('emitalert').act,'alerts.acknowledgeAlert');
 assert.deepEqual(steps.get('emitalert').target,{source:'item',path:['alertId']});
 assert.ok(report.diagnostics.some(issue=>issue.code==='flow-threshold-mean-reading'&&issue.blocking===false));
});

function nativeFixture(){const input=fixture();delete input.bindings['flow-wasm'];delete input.bindings['flow-threshold'];return input;}

test('refuses private source, wrong kernel, stale action and incomplete action payload',()=>{
 const input=nativeFixture();
 const stale=structuredClone(input.target);stale.sources[0].state='draft';
 let report=compileFlowSource(input.source,input.flow.id,input.bindings,stale);
 assert.equal(report.ready,false);assert.ok(blocking(report).some(issue=>issue.code==='flow-source-record'));
 const invalid=structuredClone(input.flow);invalid.nodes.find(n=>n.type==='aggregate').config.kernel='javascript:approximate';
 report=compileFlowSource(JSON.stringify(invalid),invalid.id,input.bindings,input.target);
 assert.ok(blocking(report).some(issue=>issue.code==='flow-aggregate-kernel'));
 const changed=structuredClone(input.bindings);changed['flow-alert'].sourceVersion='old';
 report=compileFlowSource(input.source,input.flow.id,changed,input.target);assert.equal(report.ready,false);
 const missing=structuredClone(input.bindings);delete missing['flow-alert'].inputs.severity;
 report=compileFlowSource(input.source,input.flow.id,missing,input.target);assert.ok(blocking(report).some(issue=>issue.code==='flow-action-input-required'));
});

test('rejects old Compute bindings rather than silently substituting a native operator',()=>{
 const input=fixture(),report=compileFlowSource(input.source,input.flow.id,input.bindings,input.target);
 assert.equal(report.ready,false);assert.equal(blocking(report).filter(i=>i.code==='flow-native-kernel-binding').length,2);
});

test('refuses broken topology, field mismatch and excessive concurrency before applying',()=>{
 const input=nativeFixture();
 const invalid=structuredClone(input.flow);invalid.edges.pop();invalid.execution.maxConcurrency=9;
 invalid.nodes.find(n=>n.type==='threshold').config.field='nonexistent';
 const report=compileFlowSource(JSON.stringify(invalid),invalid.id,input.bindings,input.target);
 assert.equal(report.ready,false);assert.ok(blocking(report).some(i=>i.code==='flow-continuous-topology'));
 assert.ok(blocking(report).some(i=>i.code==='flow-concurrency-invalid'));
 assert.ok(blocking(report).some(i=>i.code==='flow-threshold-field'));
});
