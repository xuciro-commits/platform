import {registerHooks} from 'node:module';import {readFileSync} from 'node:fs';import assert from 'node:assert/strict';import test from 'node:test';
import {defaultConsumerGroup} from './default-consumers.fixture.mjs';
const manifest=JSON.parse(readFileSync(new URL('../../../../../capabilities/server/platform/pageui/widgets.json',import.meta.url)));
registerHooks({resolve(s,c,next){if(s==='@platform/kernel')return {url:'data:text/javascript,'+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`),shortCircuit:true};try{return next(s,c)}catch(e){if(s.startsWith('./')||s.startsWith('../'))return next(s+'.ts',c);throw e;}}});
const {compileWorkshopModule}=await import('./compile.ts');
function fixture(){
 const object={app:'build',kind:'object',name:'build.asset'},states=['Active','Warning','Maintenance','Offline'],app={ref:{app:'build',kind:'app',name:'desk'},sourceVersion:'1.app-1'},groups=['sensor','alert','work'];
 const entity={app:'build',type:object.name,fields:[{name:'name',type:'text'},{name:'state',type:'choice',choices:states.map(s=>s.toLowerCase())},{name:'priority',type:'choice',choices:['High','Low']},{name:'owner',type:'text'},{name:'pressure',type:'integer'}],lifecycle:{field:'state',states:states.map(s=>({name:s.toLowerCase(),title:s}))}};
 const links=groups.map(name=>({object:{app:'build',kind:'object',name:`build.${name}`},field:'asset'}));
 const bindings={objects:{Asset:object.name},fields:{Asset:{name:'name',status:'state',priority:'priority',owner:'owner',pressure:'pressure'}},queries:{},actions:{},links:{wLinks1:links,'wLinks1~9':links},states:{wStatusTracker:Object.fromEntries(states.map(s=>[s,s.toLowerCase()]))},application:{binding:app,ports:{selectedAsset:{variable:'record',writable:true}}}};
 const target={object:object.name,profile:manifest.uiProfile,entities:[entity,...groups.map(name=>({app:'build',type:`build.${name}`,fields:[{name:'asset',type:'reference',ref:object.name,inverse:name}]}))],actions:[],definitions:[{ref:app.ref,version:app.sourceVersion,application:{name:'desk',title:'Desk',pages:['page'],uiProfile:manifest.uiProfile,variables:{record:{scope:'application',type:'record',mode:'resource',source:{kind:'record',object}}}}}]};return {module:defaultConsumerGroup(),bindings,target};
}
const compile=f=>compileWorkshopModule(JSON.stringify(f.module),'pOperations',f.bindings,f.target);
test('five original title links and status configs share one page collection and confirmed application record across the drawer',()=>{
 const f=fixture(),r=compile(f);assert.ok(r.draft,JSON.stringify(r.diagnostics));const titles=r.draft.sections.filter(s=>s.widget==='collection-title');assert.equal(titles.length,2);assert.equal(titles[0].collectionVariable,titles[1].collectionVariable);assert.equal(titles[0].countVariable,titles[1].countVariable);
 const v=r.draft.document.variables,set=v[titles[0].collectionVariable],count=v[titles[0].countVariable];assert.equal(set.scope,'page');assert.equal(count.scope,set.scope);assert.equal(count.owner,set.owner);assert.equal(count.source.query,set.source.query);assert.equal(Object.values(v).filter(v=>v.mode==='aggregate').length,1);assert.equal(r.draft.document.queries[set.source.query].owner,undefined);
 const related=r.draft.sections.filter(s=>s.widget==='record-links');assert.equal(related.length,2);assert.deepEqual(related[0].recordLinks,related[1].recordLinks);assert.deepEqual(related[0].recordLinks.map(g=>g.title),['Sensors','Alerts','Work Orders']);assert.equal(v[related[0].recordVariable].scope,'application');assert.equal(related[0].recordVariable,related[1].recordVariable);
 assert.deepEqual(r.draft.sections.find(s=>s.widget==='status-tracker').statusTracker,{field:'state',stages:['active','warning','maintenance','offline']});assert.equal(r.source,JSON.stringify(f.module));assert.ok(r.diagnostics.some(d=>d.code==='native-page-variable-reader'&&!d.blocking));
});
test('a sibling overlay still cannot adopt another overlay local collection or a mismapped original reference',()=>{
 const f=fixture();f.module.sections.group.children=f.module.sections.group.children.filter(c=>!['wObjectTable1','wObjectSetTitle1','wSearch1'].includes(c.id));delete f.module.moduleInterface;
 const first=f.module.overlays[0],second={...structuredClone(first),id:'second',rootSectionId:'secondRoot',openVariableId:undefined};f.module.overlays.push(second);f.module.sections[first.rootSectionId].children=[{kind:'widget',id:'wObjectSetTitle1~7'}];f.module.sections.secondRoot={id:'secondRoot',layout:'rows',children:[{kind:'widget',id:'wObjectSetTitle1'}]};delete f.bindings.application;
 // The remaining root status/links would declare the producer's page collection: remove those test consumers too.
 f.module.sections.group.children=f.module.sections.group.children.filter(c=>c.id==='open');const r=compile(f);assert.equal(r.draft,undefined);assert.ok(r.diagnostics.some(d=>d.code==='variable-scope'&&d.blocking));
 const bad=fixture();bad.target.entities[1].fields[0].ref='build.other';assert.equal(compile(bad).draft,undefined);
});
