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

test('recognizes the full default seven-node stream graph without rewriting its source bytes',()=>{
 const text=JSON.stringify(originalFlow),parsed=parseFlowSource(text);
 assert.equal(parsed.diagnostics.length,0);assert.equal(parsed.flows.length,1);assert.equal(parsed.flows[0].nodes.length,7);assert.equal(parsed.flows[0].edges.length,6);
 const input=fixture({source:originalFlow,concurrency:8,deadLetterRetry:true,preserveDefaults:true}),report=compileFlowSource(input.source,input.flow.id,input.bindings,input.target);
 assert.equal(report.source,input.source);assert.ok(report.draft);assert.equal(report.ready,false);
 assert.ok(blocking(report).some(issue=>issue.code==='flow-concurrency-owner'));
 assert.ok(!report.diagnostics.some(issue=>issue.code==='flow-start-owner'));assert.ok(blocking(report).some(issue=>issue.code==='flow-output-owner'));assert.ok(blocking(report).some(issue=>issue.code==='flow-deadletter-owner'));
 assert.ok(blocking(report).some(issue=>issue.code==='flow-deadletter-replay'));
 assert.equal(report.draft.continuous.checkpointEvery,5000);assert.ok(!blocking(report).some(issue=>issue.code==='flow-checkpoint-owner'));assert.ok(blocking(report).some(issue=>issue.code==='flow-error-owner'));assert.ok(blocking(report).some(issue=>issue.code==='flow-retry-owner'));
 assert.ok(!blocking(report).some(issue=>issue.code==='flow-continuous-topology'));
});

test('previews the complete automatically started graph while blocking unsupported output and dead-letter owners',()=>{
 const input=fixture(),report=compileFlowSource(input.source,input.flow.id,input.bindings,input.target);
 assert.ok(report.draft,JSON.stringify(report.diagnostics));assert.equal(report.ready,false);assert.ok(!report.diagnostics.some(issue=>issue.code==='flow-start-owner'));assert.ok(blocking(report).some(issue=>issue.code==='flow-output-owner'));assert.ok(blocking(report).some(issue=>issue.code==='flow-deadletter-owner'));assert.ok(blocking(report).length>0);
 const draft=report.draft,continuous=draft.continuous,steps=new Map(draft.steps.map(step=>[step.name,step]));
 assert.equal(draft.manual,false);assert.equal(continuous.Source,'planttelemetry');assert.equal(continuous.entry,'aggregate');assert.equal(continuous.Batch,512);assert.equal(continuous.checkpointEvery,0);
 assert.equal(continuous.Window.windowMs,30000);assert.equal(continuous.Window.slideMs,5000);assert.equal(continuous.Window.watermarkMs,2000);assert.equal(continuous.Window.maxRecords,10000);assert.equal(continuous.Window.lateEvents,'sideOutput');
 assert.deepEqual(continuous.Intake,{sourceRecord:'source-telemetry',key:'eventId',partition:['plantId','deviceId'],eventTime:'eventTime',value:'vibration',offset:'latest'});
 assert.equal(continuous.DeadLetter,true);assert.equal(continuous.deadLetterMaxRecords,10000);assert.equal(continuous.deadLetterTtlMs,86400000);
 assert.equal(steps.get('aggregate').kind,'compute');assert.equal(steps.get('aggregate').operation.app,'analytics');assert.deepEqual(steps.get('aggregate').inputs.in,{source:'input',path:['signals']});assert.equal(steps.get('aggregate').retryAttempts,3);assert.equal(steps.get('aggregate').retryBackoffMs,250);
 assert.equal(steps.get('threshold').inputs.config.value.field,'reading');assert.deepEqual(steps.get('threshold').inputs.in,{source:'step',step:'aggregate',path:['alerts']});
 assert.equal(steps.get('routeoutputs').kind,'fork');assert.deepEqual(steps.get('routeoutputs').branches,['publishlivetelemetrystats','threshold']);
 assert.deepEqual(steps.get('publishlivetelemetrystats').value,{source:'step',step:'aggregate',path:['stats']});
 assert.equal(steps.get('alertsloop').kind,'foreach');assert.equal(steps.get('alertsloop').maxIterations,10000);assert.deepEqual(steps.get('alertsloop').collection,{source:'step',step:'threshold',path:['triggered']});
 assert.equal(steps.get('emitalert').act,'alerts.acknowledgeAlert');assert.deepEqual(steps.get('emitalert').target,{source:'item',path:['alertId']});assert.deepEqual(steps.get('emitalert').inputs.reading,{source:'item',path:['reading']});
 assert.equal(report.outputs.liveTelemetryStats,'publishlivetelemetrystats');
 assert.ok(report.diagnostics.some(issue=>issue.code==='flow-threshold-mean-reading'&&!issue.blocking));
});

test('rejects a stale or private intake source, cross-version Compute, and an incomplete action mapping',()=>{
 const input=fixture();
 const stale=structuredClone(input.target);stale.sources[0].state='draft';let report=compileFlowSource(input.source,input.flow.id,input.bindings,stale);assert.ok(report.draft);assert.equal(report.ready,false);assert.ok(blocking(report).some(issue=>issue.code==='flow-source-record'));
 const changed=structuredClone(input.bindings);changed['flow-wasm'].sourceVersion='3.1';report=compileFlowSource(input.source,input.flow.id,changed,input.target);assert.ok(report.draft);assert.equal(report.ready,false);assert.ok(blocking(report).some(issue=>issue.code==='flow-capability-version'));
 const missing=structuredClone(input.bindings);delete missing['flow-alert'].inputs.severity;report=compileFlowSource(input.source,input.flow.id,missing,input.target);assert.ok(report.draft);assert.equal(report.ready,false);assert.ok(blocking(report).some(issue=>issue.code==='flow-action-input-required'));
});

test('refuses topology, source-column, and graph-concurrency changes rather than silently dropping semantics',()=>{
 const input=fixture();
 const changed=structuredClone(input.flow);changed.edges.pop();let report=compileFlowSource(JSON.stringify(changed),changed.id,input.bindings,input.target);assert.ok(report.draft);assert.equal(report.ready,false);assert.ok(blocking(report).some(issue=>issue.code==='flow-continuous-topology'));
 const sourceMismatch=structuredClone(input.bindings);sourceMismatch['flow-stream'].value='temperature';report=compileFlowSource(input.source,input.flow.id,sourceMismatch,input.target);assert.ok(report.draft);assert.equal(report.ready,false);assert.ok(blocking(report).some(issue=>issue.code==='flow-signal-source-mismatch'));
 const parallel=fixture({concurrency:8});report=compileFlowSource(parallel.source,parallel.flow.id,parallel.bindings,parallel.target);assert.ok(report.draft);assert.equal(report.ready,false);assert.ok(blocking(report).some(issue=>issue.code==='flow-concurrency-owner'));
});
