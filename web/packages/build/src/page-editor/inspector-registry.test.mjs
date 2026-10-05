import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
import {registerHooks} from "node:module";

const manifest=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
const loaded=[];
// Isolate React scheduling and inspector UI; exercise the production assembly,
// shared contract gate and lazy loader without evaluating editor dependencies.
registerHooks({resolve(specifier,context,next){
 if(specifier==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`),shortCircuit:true};
 if(specifier==="@platform/app")return {url:new URL("../../../app/src/widgets/registry.ts",import.meta.url).href,shortCircuit:true};
 if(specifier==="react")return {url:"data:text/javascript,export const lazy=load=>({load});",shortCircuit:true};
 if(specifier==="./TableInspector"){
  loaded.push(specifier);
  return {url:"data:text/javascript,export function TableInspector(){};export function TableSelectionInspector(){};",shortCircuit:true};
 }
 try{return next(specifier,context);}catch(error){
  if(specifier.startsWith("./")||specifier.startsWith("../"))return next(specifier+".ts",context);
  throw error;
 }
}});
const {widgetInspector,widgetInspectorStatus}=await import("./widgets/registry.ts");
const {lazyInspector,InspectorLoadError}=await import("./widgets/lazy-inspector.ts");

test("all native inspectors resolve exact versions without eagerly loading UI or treating common editors as missing",()=>{
 assert.deepEqual(loaded,[]);
 for(const contract of manifest.widgets){
  assert.ok(widgetInspector(contract.componentID,contract.configVersion));
  assert.equal(widgetInspectorStatus(contract.componentID,contract.configVersion+1),"unsupported");
  assert.equal(widgetInspector(contract.componentID,contract.configVersion+1),undefined);
 }
 assert.equal(widgetInspectorStatus("unregistered",1),"unsupported");
 assert.equal(widgetInspectorStatus("table",1),"specialized");
 assert.deepEqual(manifest.widgets.filter(c=>widgetInspectorStatus(c.componentID,c.configVersion)==="common").map(c=>c.componentID).sort(),["actions","compute","form","function","input","tasks","text"]);
 assert.deepEqual(loaded,[]);
});

test("bindings and events load their requested exports while preserving a distinguishable module failure",async()=>{
 const table=widgetInspector("table",1);
 const binding=await table.bindings.load();
 assert.equal(binding.default.name,"TableInspector");
 const event=await table.events.load();
 assert.equal(event.default.name,"TableSelectionInspector");
 assert.deepEqual(loaded,["./TableInspector","./TableInspector"]);
 const original=new Error("fixture load failure");
 const failing=lazyInspector(async()=>{throw original;});
 await assert.rejects(failing.load(),error=>error instanceof InspectorLoadError&&error.cause===original);
 assert.ok(widgetInspector("heading",1),"one module failure must not invalidate another registration");
});
