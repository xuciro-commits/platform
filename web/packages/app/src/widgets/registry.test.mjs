import {registerHooks} from "node:module";
import {readFileSync} from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";
const manifest=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
// The registry is pure; use the canonical descriptor without executing React.
registerHooks({resolve(s,c,n){if(s==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)}`),shortCircuit:true};return n(s,c)}});
const {createWidgetRegistry,widgetContracts}=await import("./registry.ts");
test("registration rejects missing or undeclared implementations and never resolves an unknown version",()=>{
 const renderer=()=>null,implementations=Object.fromEntries(widgetContracts.map(c=>[c.componentID,renderer]));
 const registry=createWidgetRegistry(implementations);
 for(const id of ["table","button"]){const d=registry.resolveDefinition(id,1);assert.equal(d.Renderer,renderer);assert.equal(d.contract,widgetContracts.find(c=>c.componentID===id));assert.equal(registry.resolve(id,2),undefined);}
 assert.equal(registry.resolve("iframe",1),undefined);
 const incomplete={...implementations};delete incomplete.table;assert.throws(()=>createWidgetRegistry(incomplete),/Missing widget implementation/);
 assert.throws(()=>createWidgetRegistry({...implementations,iframe:renderer}),/Undeclared widget implementation/);
});
