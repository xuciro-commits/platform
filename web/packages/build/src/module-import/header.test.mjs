import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
import {registerHooks} from "node:module";
const manifest=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
registerHooks({resolve(s,c,next){if(s==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`),shortCircuit:true};if(s==="@platform/ui/static-image")return {url:new URL("../../../ui/src/components/static-image.ts",import.meta.url).href,shortCircuit:true};return next(s,c);}});
const {importApplicationHeader}=await import("./header.ts");
const original=JSON.parse(readFileSync(new URL("./default.workshop.json",import.meta.url),"utf8")),destinations=original.pages.map((p,i)=>({sourcePage:p.id,name:`page${i}`}));
test("complete original header preserves seven-page tab order and native refresh/theme actions",()=>{
 const bytes=JSON.stringify(original.header),header=importApplicationHeader(original.header,destinations);assert.ok(header);assert.equal(header.title,original.header.title);assert.deepEqual(header.items.find(i=>i.kind==="tabs").pages,original.header.items.find(i=>i.kind==="tabs").pageIds.map(id=>destinations.find(d=>d.sourcePage===id).name));assert.deepEqual(header.items.filter(i=>i.kind==="button").map(i=>i.action),["refresh","theme"]);assert.equal(JSON.stringify(original.header),bytes);
});
test("vertical collapse is explicit and unknown pages, scripts or event chains do not become shell commands",()=>{
 const vertical={...original.header,variant:"vertical",collapsed:true};assert.equal(importApplicationHeader(vertical,destinations).collapsed,true);
 for(const change of [h=>h.logo="javascript:alert(1)",h=>h.items.find(i=>i.kind==="tabs").pageIds.push("missing"),h=>h.items.find(i=>i.kind==="button").eventActions.push({kind:"runAction"}),h=>h.items.find(i=>i.kind==="button").eventActions[0].kind="runAction",h=>h.inject="script",h=>h.collapsed=true]){const h=structuredClone(original.header);change(h);assert.equal(importApplicationHeader(h,destinations),undefined);}
});
