import {registerHooks} from 'node:module';
import {readFileSync} from 'node:fs';
import assert from 'node:assert/strict';
import test from 'node:test';
const manifest=JSON.parse(readFileSync(new URL('../../../../../capabilities/server/platform/pageui/widgets.json',import.meta.url)));
registerHooks({resolve(s,c,next){if(s==='@platform/kernel')return {url:'data:text/javascript,'+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)}`),shortCircuit:true};if(s==='@platform/ui')return {url:'data:text/javascript,export const t=s=>s;',shortCircuit:true};try{return next(s,c)}catch(e){if(s.startsWith('./')||s.startsWith('../'))return next(s+'.ts',c);throw e;}}});
const {defineWidgetPlugin}=await import('./plugin.ts');
const {createWidgetDefinitions,createWidgetRegistry,widgetContracts}=await import('./registry.ts');
const {inputPlugins}=await import('./input-plugins.ts');
const {contentPlugins}=await import('./content-plugins.ts');

test('runtime and authoring reject missing, undeclared or incompatible registrations and keep definitions immutable',()=>{
 const definitions=Object.fromEntries(widgetContracts.map(c=>[c.componentID,{configVersion:1,owner:'original'}]));
 const registry=createWidgetDefinitions(definitions);assert.equal(registry.resolve('input',1).owner,'original');assert.equal(registry.resolve('input',2),undefined);assert.equal(registry.resolve('unknown',1),undefined);assert.ok(Object.isFrozen(registry.entries));assert.ok(Object.isFrozen(registry.resolve('input',1)));
 const missing={...definitions};delete missing.text;assert.throws(()=>createWidgetDefinitions(missing),/Missing widget implementation/);assert.throws(()=>createWidgetDefinitions({...definitions,custom:{configVersion:1}}),/Undeclared widget implementation/);assert.throws(()=>createWidgetDefinitions({...definitions,text:{configVersion:2}}),/Unsupported widget configuration/);
 const renderer=()=>null,implementations=Object.fromEntries(widgetContracts.map(c=>[c.componentID,renderer]));assert.throws(()=>createWidgetRegistry({...implementations,text:inputPlugins.input}),/Mismatched widget plugin/);assert.throws(()=>createWidgetRegistry({...implementations,text:undefined}),/Missing widget implementation/);
 const current=createWidgetRegistry({...implementations,...inputPlugins,...contentPlugins});for(const [id,plugin]of Object.entries({...inputPlugins,...contentPlugins})){assert.equal(current.resolve(id,1),plugin.Renderer);assert.equal(current.resolve(id,2),undefined);}
});
test('the plugin adapter projects only declared props and never loads a renderer during registration or binding',()=>{
 let loads=0;const define=defineWidgetPlugin(),plugin=define('input',1,context=>({value:context.value}),async()=>{loads++;return ()=>null;});const context={value:'kept',privateSession:'must not escape'};
 const element=plugin.Renderer(context);assert.deepEqual(element.props,{value:'kept'});assert.equal(loads,0);assert.ok(Object.isFrozen(plugin));assert.throws(()=>define('input',2,()=>({}),async()=>()=>null),/Unsupported widget plugin/);
});
test('five input and five content plugins preserve ports and explicit empty/zero values without receiving host or session state',()=>{
 const callback=()=>{},value={status:'value',value:'draft'};
 const section={title:'Original title',text:'Original text',inputKind:'search',booleanLabel:'',booleanVariant:'checkbox',choiceInput:{variant:'multiple',options:['a']},dateKind:'datetime',dateOffset:'+08:00',dateLabel:'',rangeInput:{min:'0',max:'100',step:'1'},headingLevel:'h3',spacer:{size:0},separator:{label:''},notice:{title:'',message:'Original note',tone:'info'}};
 const context={section,value:'typed',numeric:true,valueError:'invalid-decimal',inputScopes:['original'],onValue:callback,enabled:false,dateValue:value,onDate:callback,choiceValue:value,choiceSetValue:{status:'value',value:{kind:'string-set',values:['a']}},onChoice:callback,onChoiceSet:callback,booleanInput:{status:'value',value:false},onBoolean:callback,rangeLower:value,rangeUpper:value,onRange:callback};
 for(const key of ['page','session','readSource','window'])Object.defineProperty(context,key,{get:()=>{throw Error(`forbidden context ${key}`);}});
 for(const plugin of Object.values({...inputPlugins,...contentPlugins})){const props=plugin.Renderer(context).props;for(const forbidden of ['page','section','session','readSource','window'])assert.equal(Object.hasOwn(props,forbidden),false);}
 assert.equal(inputPlugins.input.bind(context).onChange,callback);assert.equal(inputPlugins.input.bind(context).error,'invalid-decimal');assert.equal(inputPlugins['boolean-input'].bind(context).value.value,false);assert.equal(inputPlugins['boolean-input'].bind(context).label,'');assert.equal(inputPlugins['boolean-input'].bind(context).enabled,false);assert.equal(inputPlugins['date-input'].bind(context).offset,'+08:00');assert.equal(inputPlugins['choice-input'].bind(context).onSet,callback);assert.equal(inputPlugins['range-input'].bind(context).lower,value);assert.equal(contentPlugins.spacer.bind(context).config.size,0);assert.equal(contentPlugins.separator.bind(context).config.label,'');assert.equal(contentPlugins.notice.bind(context).config.title,'');assert.equal(contentPlugins.heading.bind(context).level,3);
});
