import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {registerHooks} from 'node:module';
import test from 'node:test';

const manifest=JSON.parse(readFileSync(new URL('../../../../../../capabilities/server/platform/pageui/widgets.json',import.meta.url)));
registerHooks({resolve(specifier,context,next){
 if(specifier==='@platform/kernel')return {url:'data:text/javascript,'+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`),shortCircuit:true};
 if(specifier==='@platform/ui')return {url:'data:text/javascript,'+encodeURIComponent('export const t=(s,values={})=>s.replace(/\\{(\\w+)\\}/g,(_,key)=>values[key]??key);'),shortCircuit:true};
 try{return next(specifier,context);}catch(error){
  if(specifier.startsWith('./')||specifier.startsWith('../'))return next(specifier+'.ts',context);
  throw error;
 }
}});
const {widgetDeclarationIssues}=await import('./widget-declaration.ts');
const section=(widget,values={})=>({widget,configVersion:1,...values});
const issues=(...sections)=>widgetDeclarationIssues({sections});

test('a native scene section uses the closed widget manifest schema without changing its business declaration',()=>{
 const scene=section('scene-3d',{
  id:'scene',object:'build.asset',recordVariable:'active',fileVariable:'modelFile',scenePartVariable:'part',sceneSampleCollectionVariable:'samples',
  scene:{background:'dark',showGrid:true,quality:'balanced',sampleTimeField:'at',sampleAssetField:'asset',layers:[{id:'equipment',name:'Equipment',nodes:['Pump'],visible:true,opacity:1,wireframe:false,color:'#334455'}],mappings:[{id:'temperature',node:'Pump',source:'sample',field:'temperature',mode:'position',axis:'x',inputMin:0,inputMax:100,outputMin:0,outputMax:1,enabled:true}]},
 });
 const before=structuredClone(scene);
 assert.ok(scene.scene.layers.length>0&&scene.scene.mappings.length>0);
 assert.deepEqual(issues(scene),[]);
 assert.deepEqual(scene,before);
 const invalid=structuredClone(scene);invalid.scene.layers[0].unregistered=true;
 assert.match(issues(invalid)[0].message,/Unknown field: unregistered/);
});

test('manifest property and port fields are scoped by widget without altering original business declarations',()=>{
 const retained=section('notice',{id:'instructions',notice:{tone:'info',message:'Read <b>literally</b>',title:''},model:{type:'build.note'}});
 const before=structuredClone(retained);
 assert.deepEqual(issues(retained),[]);
 assert.deepEqual(retained,before);
 assert.match(issues(section('heading',{notice:{tone:'info',message:'wrong widget'}}))[0].message,/Unknown field: notice/);
 assert.deepEqual(issues(section('table',{collectionVariable:'original_query',selectionVariable:'selected_record'})),[]);
 assert.deepEqual(issues(section('record-list',{recordList:{layout:'tiles'},collectionVariable:'original_query'})),[]);
 assert.match(issues(section('record-list',{recordList:{layout:'invented'},collectionVariable:'original_query'}))[0].message,/declared option/);
 assert.deepEqual(issues(section('button',{collectionVariable:'original_query'}))[0].path,['sections','0','collectionVariable']);
 assert.deepEqual(issues(section('heading',{avatar:{contextVariable:'record'}}))[0].path,['sections','0','avatar','contextVariable']);
 assert.match(issues(section('table',{collectionVariable:42}))[0].message,/Expected text/);
 assert.match(issues({...retained,configVersion:2})[0].message,/Unsupported widget configuration/);
});

test('source uses the same byte limits, numeric bounds and explicit empty values as ordinary property editors',()=>{
 assert.deepEqual(issues(section('spacer',{spacer:{size:0}}),section('spacer',{spacer:{size:0.5}}),section('separator',{separator:{label:''}})),[]);
 for(const size of [-1,4097])assert.match(issues(section('spacer',{spacer:{size}}))[0].message,/allowed range/);
 assert.deepEqual(issues(section('notice',{notice:{tone:'info',message:'中'.repeat(1365)}})),[]);
 assert.match(issues(section('notice',{notice:{tone:'info',message:'中'.repeat(1366)}}))[0].message,/text exceeds/);
 assert.match(issues(section('notice',{notice:{tone:'unavailable',message:''}}))[0].message,/declared option/);
});
