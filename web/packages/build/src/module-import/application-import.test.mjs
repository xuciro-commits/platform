import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync} from 'node:fs';
import {registerHooks} from 'node:module';
import {defaultOverviewFixture} from './default-overview.fixture.mjs';
import {defaultAnalyticsCompleteFixture} from './default-analytics.fixture.mjs';
import {defaultMaintenanceFixture} from './default-maintenance.fixture.mjs';
import {defaultMapFixture} from './default-map.fixture.mjs';
import {defaultWorkflowFixture} from './default-workflow.fixture.mjs';
import {defaultTelemetryFixture} from './default-telemetry.fixture.mjs';
const manifest=JSON.parse(readFileSync(new URL('../../../../../capabilities/server/platform/pageui/widgets.json',import.meta.url)));
registerHooks({resolve(s,c,next){if(s==='@platform/kernel')return {url:'data:text/javascript,'+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)}`),shortCircuit:true};try{return next(s,c)}catch(e){if(s.startsWith('./')||s.startsWith('../'))return next(s+'.ts',c);throw e;}}});
const {compileWorkshopApplication,importDependencies,importedPageWrites,saveImportedPages}=await import('./application-import.ts');
const merge=(a,b)=>{if(!a||!b||Array.isArray(a)||Array.isArray(b)||typeof a!=='object'||typeof b!=='object')return structuredClone(b);const result=structuredClone(a);for(const [key,value] of Object.entries(b))result[key]=merge(result[key],value);return result;};
function complete(){
 const fixtures=[defaultOverviewFixture,defaultAnalyticsCompleteFixture,defaultMaintenanceFixture,defaultMapFixture,defaultWorkflowFixture,defaultTelemetryFixture].map(make=>make(manifest.uiProfile));
 const module=fixtures[0].module,entities=new Map(),definitions=new Map(),actions=new Map();let bindings={};
 for(const f of fixtures){bindings=merge(bindings,f.bindings);for(const e of f.target.entities){const previous=entities.get(e.type);entities.set(e.type,{...previous,...e,fields:[...new Map([...(previous?.fields??[]),...e.fields].map(field=>[field.name,field])).values()]});}for(const d of f.target.definitions){const key=JSON.stringify(d.ref);definitions.set(key,merge(definitions.get(key),d));}for(const a of f.target.actions)actions.set(a.schema,a);}
 bindings.application.recordSets.selectedAssets.writable=true;
 bindings.regions.sOperationsRoot={maxHeight:960};
 const destinations=module.pages.map((p,i)=>({sourcePage:p.id,id:`PAGE-${i}`,name:`import${i}`,object:fixtures[0].target.object}));
 return {source:JSON.stringify(module),bindings,destinations,target:{profile:manifest.uiProfile,entities:[...entities.values()],actions:[...actions.values()],definitions:[...definitions.values()]}};
}
test('one complete source and shared binding map compile all seven original pages, overlays and inventory',()=>{
 const f=complete(),before=JSON.stringify(f),r=compileWorkshopApplication(f.source,f.bindings,f.destinations,f.target);
 assert.equal(r.ready,true,JSON.stringify(r.diagnostics.filter(d=>d.blocking)));assert.equal(r.source,f.source);assert.equal(r.pages.length,7);assert.deepEqual(r.pages.map(p=>p.report.draft.sections.length),[24,17,30,17,21,21,15]);
 for(const p of r.pages){assert.equal(p.report.source,f.source);assert.equal(Object.keys(p.report.draft.document.overlays).length,4);assert.equal(p.report.draft.document.unusedWidgets.length,2);}assert.equal(JSON.stringify(f),before);
 const writes=importedPageWrites(r);r.pages[0].report.draft.title='Later edit';assert.notEqual(writes[0].payload.title,'Later edit');
 const missing=compileWorkshopApplication(f.source,f.bindings,f.destinations.slice(1),f.target);assert.equal(missing.ready,false);assert.ok(missing.diagnostics.some(d=>d.page===f.destinations[0].sourcePage&&d.code==='application-page-destination'));
 const duplicate=structuredClone(f.destinations);duplicate[1].id=duplicate[0].id;assert.equal(compileWorkshopApplication(f.source,f.bindings,duplicate,f.target).ready,false);
 const bad=JSON.parse(f.source);bad.widgets.wMediaUpload.config.execute='unsafe';const rejected=compileWorkshopApplication(JSON.stringify(bad),f.bindings,f.destinations,f.target);assert.equal(rejected.ready,false);assert.ok(rejected.diagnostics.some(d=>d.page==='pWorkflow'&&d.path.includes('wMediaUpload')&&d.blocking));
});
const payload=name=>({name,object:'build.asset',title:name,description:'',sections:[],selections:[],document:{formatVersion:2,uiProfile:manifest.uiProfile,root:'root',nodes:{root:{kind:'rows',children:[]}}}});
const writes=['one','two'].map((name,i)=>({destination:{sourcePage:name,id:`PAGE-${i}`,name,object:'build.asset'},payload:payload(name)}));
function io(directInstall=true){const records=new Map(),calls=[],progress=[];let pending=false,reject='';return {records,calls,progress,directInstall,setPending:v=>pending=v,setReject:v=>reject=v,active:()=>true,read:async id=>records.get(id),pending:()=>pending,decide:async(schema,id,value,revision)=>{calls.push({schema,id,revision});if(schema===reject)return false;if(schema==='build.page.create')records.set(id,{...structuredClone(value),id,revision:1});else if(schema==='build.page.publish'){const r=records.get(id);assert.equal(revision,r.revision);r.published=JSON.stringify(r);r.revision++;}else throw Error('unexpected edit');return true;},progress:value=>progress.push(value)};}
test('the imported pages report their saved object drafts as candidate inputs (ADR-0048 D6)',()=>{
 const destinations=[{sourcePage:'one',id:'PAGE-1',name:'import1',object:'stock.item'},{sourcePage:'two',id:'PAGE-2',name:'import2',object:'sales.order'},{sourcePage:'three',id:'PAGE-3',name:'import3',object:'stock.item'}];
 const drafts=[{id:'OBJ-1',name:'stock.item',title:'Items'},{id:'OBJ-2',name:'unused.object',title:'Unused'}];
 assert.deepEqual(importDependencies(destinations,drafts),[ {kind:'object',id:'OBJ-1',name:'stock.item',title:'Items'} ]);
 assert.deepEqual(importDependencies(destinations,[]),[]);
 assert.deepEqual(importDependencies([{sourcePage:'one',id:'PAGE-1',name:'import1',object:'unused.object'}],drafts),[ {kind:'object',id:'OBJ-2',name:'unused.object',title:'Unused'} ]);
 assert.deepEqual(importDependencies([{sourcePage:'one',id:'PAGE-1',name:'import1',object:'sales.order'}],drafts),[]);
 assert.deepEqual(importDependencies(destinations,[{id:'OBJ-1',name:'stock.item'}]),[ {kind:'object',id:'OBJ-1',name:'stock.item'} ]);
});
test('a delivery tenant saves the imported pages as drafts and publishes nothing (ADR-0048 D6)',async()=>{
 const state=io(false),seen=[];
 await saveImportedPages(writes,{...state,progress:value=>seen.push(value)});
 assert.deepEqual(state.calls.map(c=>c.schema),['build.page.create','build.page.create']);
 assert.deepEqual(seen,[{page:'one',state:'saved'},{page:'two',state:'saved'},{page:'one',state:'draft'},{page:'two',state:'draft'}]);
 for(const record of state.records.values())assert.equal(record.published,undefined);
 state.calls.length=0;await saveImportedPages(writes,{...state,progress:value=>seen.push(value)});
 assert.equal(state.calls.length,0);assert.equal(state.records.size,2);
});
test('save all drafts before publishing, retain partial results, and resume only confirmed matching bytes',async()=>{
 const state=io();state.setReject('build.page.publish');await assert.rejects(saveImportedPages(writes,state),/application-import-publish-refused/);assert.deepEqual(state.calls.map(c=>c.schema),['build.page.create','build.page.create','build.page.publish']);assert.equal(state.records.size,2);
 state.setReject('');state.calls.length=0;await saveImportedPages(writes,state);assert.deepEqual(state.calls.map(c=>c.schema),['build.page.publish','build.page.publish']);state.calls.length=0;await saveImportedPages(writes,state);assert.equal(state.calls.length,0);
 state.records.get('PAGE-1').title='Concurrent edit';await assert.rejects(saveImportedPages(writes,state),/application-import-conflict/);assert.equal(state.calls.length,0);
});
test('unknown submissions, lost original revisions and retired membership stop writes',async()=>{
 const state=io();state.setPending(true);await assert.rejects(saveImportedPages(writes,state),/application-import-pending/);assert.equal(state.calls.length,0);state.setPending(false);
 await assert.rejects(saveImportedPages([{...writes[0],destination:{...writes[0].destination,revision:5}}],state),/application-import-conflict/);
 await assert.rejects(saveImportedPages(writes,{...state,active:()=>false}),/application-import-scope-changed/);assert.equal(state.calls.length,0);
 const active={value:true};await assert.rejects(saveImportedPages(writes,{...state,active:()=>active.value,read:async()=>{active.value=false;return undefined;}}),/application-import-scope-changed/);assert.equal(state.calls.length,0);
});
test('Go omitted layout flags and empty query windows compare equally, while true flags and meaningful zeros stay distinct',async()=>{
 const state=io(),write=structuredClone(writes[0]);write.payload.document.nodes.root.presentation={showHeader:false,border:false,collapsible:false,defaultCollapsed:false,padding:0};write.payload.document.queries={q:{offset:0,conditions:[]}};write.payload.sections=[{id:'child',widget:'embedding',embedding:{inputs:{},results:{},contentVersion:'page.sha256.original'}}];
 state.records.set(write.destination.id,{...structuredClone(write.payload),id:write.destination.id,revision:3});const stored=state.records.get(write.destination.id);stored.document.nodes.root.presentation={padding:0};stored.document.queries={q:{}};delete stored.sections[0].embedding.inputs;delete stored.sections[0].embedding.results;
 await saveImportedPages([write],state);assert.deepEqual(state.calls.map(c=>c.schema),['build.page.publish']);state.calls.length=0;
 stored.document.nodes.root.presentation.showHeader=true;await assert.rejects(saveImportedPages([write],state),/application-import-conflict/);delete stored.document.nodes.root.presentation.showHeader;delete stored.document.nodes.root.presentation.padding;await assert.rejects(saveImportedPages([write],state),/application-import-conflict/);assert.equal(state.calls.length,0);
});
