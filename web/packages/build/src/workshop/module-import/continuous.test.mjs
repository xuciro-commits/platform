import {registerHooks} from "node:module";
import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
registerHooks({resolve(s,c,next){if(s==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent("export const pageUIManifest={};"),shortCircuit:true};if(s==="@platform/ui")return {url:"data:text/javascript,"+encodeURIComponent("export const t=(text)=>text;"),shortCircuit:true};try{return next(s,c)}catch(e){if(s.startsWith("./")||s.startsWith("../"))return next(`${s}.ts`,c);throw e;}}});
const {compileFlowSource}=await import("./flows.ts");
const {continuousSettings}=await import("./continuous.ts");
const module=JSON.parse(readFileSync(new URL("./default.workshop.json",import.meta.url),"utf8"));
const graph=()=>structuredClone(module.flows[0]);
const settings={Source:"telemetry",State:2097152,FrameBytes:4194304,Intake:{sourceRecord:"STREAM-SOURCE",key:"event_id",partition:["plant","device"],eventTime:"at",value:"reading"}};
const bindings={"flow-alert":{app:"build",kind:"action",name:"build.alert.acknowledge",version:0,sourceVersion:"1.action-1"}};
const compile=(flow,extra={})=>compileFlowSource(JSON.stringify({flows:[flow]}),flow.id,bindings,{name:"telemetry",capabilities:[],definitions:[],continuous:settings,...extra});
const codes=report=>report.diagnostics.map(issue=>issue.code).sort();

test("the default continuous graph declares its own window, operators, effect and dead letters",()=>{
 const report=compile(graph());
 assert.deepEqual(report.diagnostics,[],JSON.stringify(report.diagnostics));
 assert.deepEqual(report.outputs,{stats:"liveTelemetryStats",acknowledge:"build.alert.acknowledge"});
 assert.deepEqual(report.continuous,{Source:"telemetry",State:2097152,FrameBytes:4194304,Intake:settings.Intake,Batch:512,
  Window:{node:"flow-window",windowMs:30000,slideMs:5000,watermarkMs:2000,maxRecords:50000,lateEvents:"sideOutput"},
  Aggregate:{node:"flow-wasm",signal:"vibration",group:["plantId","deviceId"],measures:["count","mean"]},
  Threshold:{node:"flow-threshold",field:"mean",high:11.5,low:9.5,debounceMs:3000,severity:"High"},
  DeadLetter:{node:"flow-dlq",maxRecords:10000,ttlMs:86400000},CheckpointEvery:5000,
  Effects:[{node:"flow-threshold",action:"build.alert.acknowledge"}]});
 const {continuous,steps,object,when,manual}=report.draft;
 assert.equal(object,"");
 assert.equal(when,"");
 assert.equal(manual,true);
 assert.deepEqual(steps.map(s=>s.kind),["wait"]);
 assert.equal(continuous.Source,"telemetry");
});

test("a graph's measure names are the only fields its threshold may read",()=>{
 const unnamed=graph();
 unnamed.nodes.find(n=>n.type==="aggregate").config.measures=undefined;
 assert.deepEqual(codes(compile(unnamed)),["flow-aggregate-measures","flow-threshold-field"]);
 const aliased=graph();
 aliased.nodes.find(n=>n.type==="threshold").config.field="reading";
 assert.deepEqual(codes(compile(aliased)),["flow-threshold-field"]);
 const foreignKernel=graph();
 foreignKernel.nodes.find(n=>n.type==="aggregate").config.kernel="wasm:f32-window-stats";
 assert.deepEqual(codes(compile(foreignKernel)),["flow-aggregate-kernel"]);
});

test("the alert's action is a tenant binding, and the settings never repeat the graph's chain",()=>{
 const noBinding=compileFlowSource(JSON.stringify({flows:[graph()]}),"flow-telemetry",{},{name:"telemetry",capabilities:[],definitions:[],continuous:settings});
 assert.deepEqual(codes(noBinding),["flow-effect-action"]);
 const settingsRepeat=compile(graph(),{continuous:{...settings,Window:{node:"w",windowMs:30000,slideMs:5000,watermarkMs:0,maxRecords:1,lateEvents:"sideOutput"}}});
 assert.deepEqual(codes(settingsRepeat),["flow-continuous-owner"]);
});

test("ports, bounds and owners are refused instead of approximated",()=>{
 const split=graph();
 split.edges.find(e=>e.id==="fe3").target="flow-alert";
 assert.deepEqual(codes(compile(split)),["flow-continuous-edge"]);
 const unowned=graph();
 unowned.nodes.push({id:"flow-join",type:"join",name:"Join",x:0,y:0,config:{}});
 assert.deepEqual(codes(compile(unowned)),["flow-node-owner"]);
 const noLetters=graph();
 noLetters.nodes=noLetters.nodes.filter(n=>n.type!=="deadLetter");
 noLetters.edges=noLetters.edges.filter(e=>e.target!=="flow-dlq");
 assert.deepEqual(codes(compile(noLetters)),["flow-continuous-shape"]);
 const tooManyRows=graph();
 tooManyRows.nodes.find(n=>n.type==="window").config.maxRecords=1000001;
 assert.deepEqual(codes(compile(tooManyRows)),["flow-continuous-config"]);
 const parallel=graph();
 parallel.execution.maxConcurrency=8;
 assert.deepEqual(codes(compile(parallel)),["flow-concurrency-owner"]);
 const scheduled=graph();
 scheduled.execution.scheduleMs=250;
 assert.deepEqual(codes(compile(scheduled)),["flow-schedule-owner"]);
 const otherPolicy=graph();
 otherPolicy.execution.errorPolicy="stop";
 assert.deepEqual(codes(compile(otherPolicy)),["flow-error-owner"]);
});

test("a graph whose source binding is unset asks for it before it is applied",()=>{
 const report=compileFlowSource(JSON.stringify({flows:[graph()]}),"flow-telemetry",bindings,{name:"telemetry",capabilities:[],definitions:[]});
 assert.deepEqual(codes(report),["flow-continuous-source"]);
 assert.equal(report.draft,undefined);
});

test("re-importing a saved draft re-reads only the settings and derives the same declaration",()=>{
 const saved=compile(graph()).continuous;
 assert.deepEqual(Object.keys(continuousSettings(saved)).sort(),["FrameBytes","Intake","Source","State"]);
 const again=compileFlowSource(JSON.stringify({flows:[graph()]}),"flow-telemetry",bindings,{name:"telemetry",capabilities:[],definitions:[],continuous:continuousSettings(saved)});
 assert.deepEqual(codes(again),[]);
 assert.deepEqual(again.continuous,saved);
});
