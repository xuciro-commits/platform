import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync} from 'node:fs';
import {registerHooks} from 'node:module';
const manifest=JSON.parse(readFileSync(new URL('../../../../../capabilities/server/platform/pageui/widgets.json',import.meta.url)));
registerHooks({resolve(s,c,next){
 if(s==='@platform/ui/canvas-snapping')return {url:new URL('../../../ui/src/layout/canvas-snapping.ts',import.meta.url).href,shortCircuit:true};
 if(s==='@platform/kernel')return {url:'data:text/javascript,'+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`),shortCircuit:true};
 if(s==='@platform/app')return {url:'data:text/javascript,'+encodeURIComponent(`export const pageUIProfile=${JSON.stringify(manifest.uiProfile)},pageVariableContract=${JSON.stringify(manifest.runtime)};export {pageLayoutDiagnostics} from ${JSON.stringify(new URL('../../../app/src/layout.ts',import.meta.url).href)};`),shortCircuit:true};
 try{return next(s,c);}catch(e){if(s.startsWith('./')||s.startsWith('../'))return next(s+'.ts',c);throw e;}
}});
const {canvasDrop,canvasMove,canvasResize,canvasResetSize}=await import('./canvas-layout.ts');
const {reduceDraft}=await import('../session/draft-history.ts');
const sections=['a','b','c'].map(id=>({id,widget:'text',configVersion:1,title:id}));
const fixture=()=>({formatVersion:2,uiProfile:manifest.uiProfile,root:'root',nodes:{root:{kind:'rows',children:['a','b','c']},...Object.fromEntries(sections.map(s=>[s.id,{kind:'widget',section:s.id}]))}});
test('insert both before and after siblings uses original stable identities and one reversible document command',()=>{
 const d=fixture(),before=JSON.stringify(d),next=canvasDrop(d,'c',{kind:'insert',parent:'root',index:0},sections);
 assert.deepEqual(next.nodes.root.children,['c','a','b']);assert.equal(JSON.stringify(d),before);
 const state={draft:{document:d},saved:'',past:[],future:[]},edited=reduceDraft(state,{type:'edit',edit:{document:next}});
 assert.equal(edited.past.length,1);assert.deepEqual(reduceDraft(edited,{type:'undo'}).draft.document,d);
 assert.deepEqual(canvasMove(next,'c',1,sections).nodes.root.children,['a','c','b']);
 assert.deepEqual(canvasDrop(d,'a',{kind:'insert',parent:'root',index:3},sections).nodes.root.children,['b','c','a']);
});
test('perpendicular edge placement creates an ordered native group; moving a group into its own descendant is refused',()=>{
 const d=fixture(),n=canvasDrop(d,'c',{kind:'wrap',target:'a',side:'left'},sections),group=n.nodes.root.children[0];
 assert.equal(n.nodes[group].kind,'columns');assert.deepEqual(n.nodes[group].children,['c','a']);assert.equal(n.nodes.a.section,'a');
 assert.equal(canvasDrop(n,group,{kind:'into',parent:'c'},sections),undefined);
 assert.equal(canvasDrop(d,'root',{kind:'into',parent:'a'},sections),undefined);
});
test('overlay and loop fragments cannot escape their resource owner and slot roots cannot be detached',()=>{
 const d=fixture();d.nodes.modal={kind:'rows',children:['b']};d.nodes.root.children=['a','c'];d.overlays={modal:{kind:'modal',title:'Modal',root:'modal',openVariable:'open'}};
 assert.equal(canvasDrop(d,'b',{kind:'swap',target:'a'},sections),undefined);
 assert.equal(canvasDrop(d,'b',{kind:'into',parent:'root'},sections),undefined);
 d.nodes.b.slot='actions';assert.equal(canvasMove(d,'b',1,sections),undefined);
});
test('resize preserves the measured sibling pair, enforces both bounds and replaces conflicting weights',()=>{
 const d=fixture();d.nodes.root.kind='columns';d.nodes.a.size={weight:2,minWidth:60,maxWidth:200};d.nodes.b.size={weight:1,minWidth:80,maxWidth:240};
 const r={a:{x:0,y:0,width:150,height:100},b:{x:162,y:0,width:150,height:100}};
 const n=canvasResize(d,'a','width',260,r);assert.equal(n.a.width,200);assert.equal(n.b.width,100);assert.equal(n.a.weight,undefined);assert.equal(n.b.weight,undefined);
 assert.equal(canvasResize(d,'a','width',10,r).a.width,64);
 d.nodes.root.kind='rows';assert.equal(canvasResize(d,'a','height',120,r).a.weight,undefined);assert.equal(canvasResize(d,'a','height',120,r).a.scroll,'auto');
 assert.equal(canvasResize(d,'a','height',NaN,r),undefined);
 d.nodes.a.size={minWidth:500,maxWidth:100};assert.equal(canvasResize(d,'a','width',200,r),undefined);
});
test('double-click reset restores natural height and equal column widths without changing other configuration',()=>{
 const d=fixture();d.nodes.root.kind='columns';d.nodes.a.size={width:100,height:150,scroll:'auto',minWidth:60};d.nodes.b.size={width:200};
 const widths=canvasResetSize(d,'a','width');assert.equal(widths.a.width,undefined);assert.equal(widths.b.width,undefined);assert.equal(widths.a.weight,1);assert.equal(widths.b.weight,1);
 const height=canvasResetSize(d,'a','height');assert.equal(height.a.height,undefined);assert.equal(height.a.scroll,undefined);assert.equal(height.a.minWidth,60);
});

test('grouping transfers occupied sizing while tabs receive a private selector and row equalization needs bounded space',async()=>{
 const {canvasGroup,canvasEqualize}=await import('./canvas-layout.ts');
 const d=fixture();d.nodes.a.size={height:160};const g=canvasGroup(d,'a','columns',sections);
 assert.equal(g.document.nodes[g.id].size.height,160);assert.equal(g.document.nodes.a.size,undefined);
 assert.deepEqual(g.document.nodes[g.id].children,['a']);assert.equal(d.nodes.a.size.height,160);
 const tabs=canvasGroup(d,'a','tabs',sections),selector=tabs.document.nodes[tabs.id].activeVariable;
 assert.equal(tabs.document.variables[selector].initial,'a');assert.equal(tabs.document.variables[selector].mode,'state');
 assert.equal(canvasEqualize(d,'root',sections),undefined);
 d.nodes.root.size={height:400};const equal=canvasEqualize(d,'root',sections);assert.equal(equal.nodes.a.size.height,undefined);assert.equal(equal.nodes.a.size.weight,1);
});

test('deleting a group removes active and dormant descendants and undo restores the exact fragment',async()=>{
 const {canvasRemove}=await import('./canvas-layout.ts');
 const d=fixture();d.nodes.root.children=['group','c'];d.nodes.group={kind:'columns',children:['a']};d.unusedWidgets=[{node:'b',parent:'group'}];d.events=[{source:'a',event:'click',target:'state'},{source:'c',event:'click',target:'state'}];
 const before=JSON.stringify(d),result=canvasRemove(d,'group');assert.deepEqual([...result.sections].sort(),['a','b']);assert.deepEqual(Object.keys(result.document.nodes).sort(),['c','root']);assert.deepEqual(result.document.unusedWidgets,[]);assert.equal(result.document.events[0].source,'c');assert.equal(JSON.stringify(d),before);
 const state={draft:{document:d},saved:'',past:[],future:[]},edited=reduceDraft(state,{type:'edit',edit:{document:result.document}});assert.deepEqual(reduceDraft(edited,{type:'undo'}).draft.document,d);
 assert.equal(canvasRemove(d,'root'),undefined);d.nodes.group.slot='footer';assert.equal(canvasRemove(d,'group'),undefined);
});
