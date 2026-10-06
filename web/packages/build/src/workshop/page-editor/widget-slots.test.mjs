import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
import {registerHooks} from "node:module";
const manifest=JSON.parse(readFileSync(new URL("../../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
registerHooks({resolve(s,c,next){if(s==="@platform/app")return {url:"data:text/javascript,"+encodeURIComponent(`const manifest=${JSON.stringify(manifest)};export const pageUIProfile=manifest.uiProfile,pageVariableContract=manifest.runtime,widgetContract=id=>manifest.widgets.find(w=>w.componentID===id);`),shortCircuit:true};if(s==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`),shortCircuit:true};try{return next(s,c);}catch(e){if(s.startsWith("./")||s.startsWith("../"))return next(s+".ts",c);throw e;}}});
const {addWidgetSlot}=await import("./widget-slots.ts");
const {appendWidget,removeWidget,stashWidget,restoreWidget,widgetSubtreeSections}=await import("../page-layout.ts");
const {copyLayout,pasteLayout}=await import("./clipboard.ts");
const {pageLayoutDiagnostics}=await import("../../../../app/src/layout.ts");
const fixture=()=>({title:"Original",description:"",selections:[],sections:[{id:"table",widget:"table",configVersion:1},{id:"label",widget:"text",configVersion:1,text:"Original child"}],document:{formatVersion:2,uiProfile:manifest.uiProfile,root:"root",nodes:{root:{kind:"rows",children:["table"]},table:{kind:"widget",section:"table",children:["footer"]},footer:{kind:"flow",slot:"footer",children:["label"]},label:{kind:"widget",section:"label"}}}});
const limits={...manifest.runtime,selectionWriters:[],selectionWidgets:[],references:{}};
test("all three slot owners consume declared names and old profiles never silently upgrade",()=>{
 for(const [widget,slot] of [["table","toolbar"],["detail","actions"],["record-card","actions"]]){const d={formatVersion:2,uiProfile:manifest.uiProfile,root:"root",nodes:{root:{kind:"rows",children:["owner"]},owner:{kind:"widget",section:"owner"}}},before=JSON.stringify(d),added=addWidgetSlot(d,"owner",widget,slot);assert.ok(added.id);assert.equal(added.document.nodes[added.id].slot,slot);assert.equal(JSON.stringify(d),before);assert.equal(addWidgetSlot(added.document,"owner",widget,slot).id,added.id);d.uiProfile="platform.page.v2.103";assert.equal(addWidgetSlot(d,"owner",widget,slot).id,undefined);}
});
test("a composite copies every owned slot and section with fresh identities while a detached slot cannot be copied",()=>{
 const f=fixture(),clip=copyLayout(f,"table","original.asset");assert.ok(clip.value);assert.equal(copyLayout(f,"footer","original.asset").issue,"scope");
 const pasted=pasteLayout(f,clip.value,"root","original.asset",limits);assert.ok(pasted.value,JSON.stringify(pasted));
 const {draft,root}=pasted.value;assert.equal(draft.sections.length,4);assert.notEqual(root,"table");const footer=draft.document.nodes[root].children[0];assert.notEqual(footer,"footer");assert.equal(draft.document.nodes[footer].slot,"footer");assert.notEqual(draft.document.nodes[footer].children[0],"label");assert.deepEqual(pageLayoutDiagnostics(draft.document,draft.sections),[]);
});
test("stash and restore preserve the whole component; deleting it removes owned nodes and sections without changing the original",()=>{
 const f=fixture(),before=JSON.stringify(f),stored=stashWidget(f.document,"table");assert.equal(stored.nodes.footer.slot,"footer");assert.equal(stored.nodes.label.section,"label");assert.ok(stored.unusedWidgets.some(e=>e.node==="table"));
 const restored=restoreWidget(stored,"table","root");assert.deepEqual(restored.nodes.root.children,["table"]);
 assert.deepEqual([...widgetSubtreeSections(f.document,"table")].sort(),["label","table"]);const removed=removeWidget(f.document,"table");for(const id of ["table","footer","label"])assert.equal(removed.nodes[id],undefined);assert.equal(JSON.stringify(f),before);
 const onlyChild=removeWidget(f.document,"label");assert.equal(onlyChild.nodes.footer.slot,"footer");assert.deepEqual(onlyChild.nodes.footer.children,[]);
});
test("diagnostics reject undeclared, duplicate and detached slots on the same native tree",()=>{
 for(const change of [f=>f.document.uiProfile="platform.page.v2.103",f=>f.document.nodes.footer.slot="missing",f=>f.document.nodes.table.children.push("footer"),f=>f.document.nodes.root.children.push("footer")]){const f=fixture();change(f);assert.ok(pageLayoutDiagnostics(f.document,f.sections).length);}
});
