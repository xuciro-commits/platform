import {registerHooks} from "node:module";
registerHooks({resolve(s,c,next){try{return next(s,c)}catch(e){if(s.startsWith("./")||s.startsWith("../"))return next(`${s}.ts`,c);throw e;}}});
import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
import {createHash} from "node:crypto";
const {compileWorkshopModule,parseWorkshopModule}=await import("./compile.ts");
const {workshopMigrationCatalog}=await import("./catalog.ts");
const nativeRegistry=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url))),profile=nativeRegistry.uiProfile;
const bindings={objects:{Asset:"sample.note"},fields:{Asset:{id:"id",name:"note"}},actions:{finishAsset:"sample.note.close"},queries:{}};
const target={object:"sample.note",profile,entities:[{app:"sample",type:"sample.note",fields:[{name:"note",type:"text"}]}],actions:[{schema:"sample.note.close",target:"sample.note"}]};
test("table editing requires explicit original edit and displayed payload fields without rewriting source configuration",()=>{
 const m=sourceModule();m.widgets.table.config.enableInlineEdit=true;const source=JSON.stringify(m),action={schema:"sample.note.edit",target:"sample.note",payload:[{name:"note"}]},mapped={...bindings,edits:{table:{action:action.schema,fields:["note"]}}},editing={...target,actions:[...target.actions,action]};
 assert.equal(compileWorkshopModule(source,"page",bindings,editing).draft,undefined);const result=compileWorkshopModule(source,"page",mapped,editing);assert.equal(result.source,source);assert.deepEqual(result.draft.sections.find(s=>s.widget==="table").inlineEdit,{action:action.schema,fields:["note"]});
 for(const edit of [{action:"sample.note.close",fields:["note"]},{action:action.schema,fields:["hidden"]},{action:action.schema,fields:[]}])assert.equal(compileWorkshopModule(source,"page",{...mapped,edits:{table:edit}},editing).draft,undefined);
 assert.equal(compileWorkshopModule(source,"page",mapped,{...editing,actions:[{...action,needsApproval:true}]}).draft,undefined);
});
const sourceModule=()=>JSON.parse(readFileSync(new URL("./sample.workshop.json",import.meta.url),"utf8"));
test("migration inventory tracks every pinned source type once and cannot confer runtime eligibility",()=>{
 assert.equal(workshopMigrationCatalog.entries.length,92);assert.equal(new Set(workshopMigrationCatalog.entries.map(e=>e.sourceType)).size,92);assert.equal(workshopMigrationCatalog.entries.filter(e=>e.status==="profile").length,8);assert.equal(workshopMigrationCatalog.entries.find(e=>e.sourceType==="Scene3D").status,"planned");
 // Pinned from the actual WidgetType union, independently of migration entries.
 assert.equal(createHash("sha256").update(workshopMigrationCatalog.entries.map(e=>e.sourceType).sort().join("\n")).digest("hex"),"fd2ad8319d16fbe084db00d4635ee9bd30c99d718d012f4e8c61f01df9d958e0");
 for(const entry of workshopMigrationCatalog.entries.filter(e=>e.status==="profile"))assert.ok(nativeRegistry.widgets.some(w=>w.componentID===entry.target),entry.sourceType);
});
test("the original ModuleDef compiles explicit object fields, selection resources and original action references into one native draft",()=>{
 const source=JSON.stringify(sourceModule(),null,2),result=compileWorkshopModule(source,"page",bindings,target);assert.ok(result.draft,JSON.stringify(result.diagnostics));assert.equal(result.source,source);const draft=result.draft,table=draft.sections.find(s=>s.widget==="table"),detail=draft.sections.find(s=>s.widget==="detail"),action=draft.sections.find(s=>s.widget==="inline-action");assert.deepEqual(table.fields,["note"]);assert.deepEqual(action.actions,["sample.note.close"]);assert.equal(action.recordVariable,detail.recordVariable);assert.equal(draft.document.variables[detail.recordVariable].source.section,table.id);assert.equal(draft.document.queries[draft.document.variables[table.collectionVariable].source.query].object.name,"sample.note");assert.equal(draft.document.nodes[draft.document.root].kind,"columns");
 const reordered=sourceModule();reordered.sections.root.children.reverse();assert.ok(compileWorkshopModule(JSON.stringify(reordered),"page",bindings,target).draft);
});
test("unsupported source keys, unknown widgets, multi-selection and executable definitions keep source bytes and block application",()=>{
 for(const mutate of [m=>m.widgets.table.config.selectedVarId="many",m=>m.widgets.markdown.type="Unregistered",m=>m.variables[0].definitionKind="sqlQuery",m=>m.widgets.table.config.enableInlineEdit=true,m=>{m.variables[2].type="boolean";m.variables[2].staticValue=true;},m=>m.widgets.action.config.extra={doNotLose:42}]){const module=sourceModule();mutate(module);const source=JSON.stringify(module),result=compileWorkshopModule(source,"page",bindings,target);assert.equal(result.source,source);assert.equal(result.draft,undefined);assert.ok(result.diagnostics.some(d=>d.blocking));}
 const script=sourceModule();script.widgets.markdown={id:"markdown",type:"SingleButton",name:"Bad expression",config:{label:"Bad",eventActions:[{kind:"setVariable",variableId:"scratch",valueExpr:"globalThis.importExecuted=true"}]}};assert.equal(compileWorkshopModule(JSON.stringify(script),"page",bindings,target).draft,undefined);assert.equal(globalThis.importExecuted,undefined);
});
test("whole overlays rewrite opened identities and local resources while every source configuration stays in the report",()=>{
 const m=sourceModule();m.sections.root.children=[{kind:"widget",id:"markdown"}];m.widgets.markdown={id:"markdown",type:"SingleButton",name:"Open",config:{label:"Open imported picker",eventActions:[{kind:"openOverlay",overlayId:"picker"}]}};m.overlays=[{id:"picker",name:"Picker",kind:"drawer",rootSectionId:"side",openVariableId:"opened"}];m.variables.push({id:"opened",name:"Opened",type:"boolean",definitionKind:"static",staticValue:false});m.sections.side.children=[{kind:"widget",id:"table"},{kind:"widget",id:"detail"},{kind:"widget",id:"action"},{kind:"widget",id:"input"}];const result=compileWorkshopModule(JSON.stringify(m),"page",bindings,target);assert.ok(result.draft,JSON.stringify(result.diagnostics));const doc=result.draft.document,overlay=Object.values(doc.overlays)[0],event=doc.events[0];assert.equal(event.target,overlay.openVariable);assert.equal(doc.variables[overlay.openVariable].initial,false);assert.equal(overlay.kind,"drawer");const input=result.draft.sections.find(s=>s.widget==="input"),v=doc.variables[doc.nodes[result.ids.nodes.input].valueVariable];assert.equal(v.scope,"overlay");assert.equal(v.owner,Object.keys(doc.overlays)[0]);assert.ok(input);
});
test("malformed and oversized data, layout cycles and missing native bindings produce diagnostics rather than a partial draft",()=>{
 assert.equal(parseWorkshopModule('{"id":').module,undefined);assert.equal(parseWorkshopModule("x".repeat(1_048_577)).diagnostics[0].code,"source-size");const m=sourceModule();m.sections.side.children.push({kind:"section",id:"root"});assert.equal(compileWorkshopModule(JSON.stringify(m),"page",bindings,target).draft,undefined);assert.equal(compileWorkshopModule(JSON.stringify(sourceModule()),"page",{objects:{},fields:{},actions:{},queries:{}},target).draft,undefined);
});
test("query bindings require the exact retained version, matching object and no unresolved parent input",()=>{
 const ref={app:"sample",kind:"query",name:"all"},query={name:"all",title:"All",description:"",object:"sample.note"},definition={ref,version:"v2",query,queryVersions:{v1:query}};
 const mapped={...bindings,queries:{notes:{ref,sourceVersion:"v1"}}},source=JSON.stringify(sourceModule());
 assert.ok(compileWorkshopModule(source,"page",mapped,{...target,definitions:[definition]}).draft);
 for(const definitions of [[],[{...definition,queryVersions:{v1:{...query,object:"other"}}}],[{...definition,queryVersions:{v1:{...query,by:"parent"}}}]])assert.equal(compileWorkshopModule(source,"page",mapped,{...target,definitions}).draft,undefined);
});
test("page selection retains its producer scope when an overlay consumes it",()=>{
 const m=sourceModule();m.sections.side.children=m.sections.side.children.filter(c=>c.id!=="detail");m.sections.panel={id:"panel",name:"Panel",layout:"rows",children:[{kind:"widget",id:"detail"}]};m.overlays=[{id:"panel",name:"Panel",kind:"modal",rootSectionId:"panel"}];
 const report=compileWorkshopModule(JSON.stringify(m),"page",bindings,target);assert.ok(report.draft,JSON.stringify(report.diagnostics));assert.equal(report.draft.document.variables[report.ids.variables.selected].scope,"page");
 m.sections.root.children=[{kind:"section",id:"side"}];m.sections.panel.children=[{kind:"widget",id:"table"},{kind:"widget",id:"detail"}];assert.equal(compileWorkshopModule(JSON.stringify(m),"page",bindings,target).draft,undefined);
});
test("diagnostics use JSON pointers with array indices and escaped source keys",()=>{
 const m=sourceModule();m.variables[0].definitionKind="sqlQuery";m.widgets.markdown.config["raw/key~"]={retained:true};
 const report=compileWorkshopModule(JSON.stringify(m),"page",bindings,target);assert.equal(report.draft,undefined);assert.ok(report.diagnostics.some(d=>d.path==="/variables/0"&&d.blocking));assert.ok(report.diagnostics.some(d=>d.path==="/widgets/markdown/config/raw~1key~0"&&d.blocking));
 for(const diagnostic of report.diagnostics.filter(d=>d.blocking)){let value=m;for(const key of diagnostic.path.slice(1).split("/"))value=value[key.replaceAll("~1","/").replaceAll("~0","~")];assert.notEqual(value,undefined,diagnostic.path);}
});

test("actual default FilterList and ObjectSet clauses import only after explicit unsupported table/detail repairs",()=>{
 const m=JSON.parse(readFileSync(new URL("./filters.workshop.json",import.meta.url),"utf8"));
 const fields=["id","name","status","priority","owner","pressure"],mapped={objects:{Asset:"sample.note"},fields:{Asset:Object.fromEntries(fields.map(f=>[f,f]))},actions:{updateAssetStatus:"sample.note.close"},queries:{}},target={object:"sample.note",profile,entities:[{app:"sample",type:"sample.note",fields:fields.filter(f=>f!=="id").map(name=>({name,type:name==="pressure"?"decimal":"text"}))}],actions:[{schema:"sample.note.close",target:"sample.note"}]};
 assert.equal(compileWorkshopModule(JSON.stringify(m),"filters",mapped,target).draft,undefined);
 m.widgets.wObjectTable1.config={objectSetVarId:"filteredAssets",activeVarId:"selectedAsset",columns:fields.map(key=>({key})),enableInlineEdit:false,selectionMode:"single"};m.widgets.wObjectTable1.events=[];m.widgets.wPropList1.config={objectVarId:"selectedAsset",properties:fields,hideNull:false};
 const report=compileWorkshopModule(JSON.stringify(m),"filters",mapped,target);assert.ok(report.draft,JSON.stringify(report.diagnostics));const filter=report.draft.sections.find(s=>s.widget==="filter"),doc=report.draft.document;assert.equal(filter.facets.length,3);assert.equal(doc.variables[filter.facets[0].variable].type,"string-set");const filtered=Object.values(doc.queries).find(q=>q.conditions.length);assert.equal(filtered.conditions.length,5);assert.ok(filtered.conditions.every(c=>c.optional));assert.equal(filtered.conditions.filter(c=>c.asDecimal).length,2);assert.ok(filtered.search.variable);assert.ok(report.diagnostics.some(d=>d.code==="native-search-scope"&&!d.blocking));
});
