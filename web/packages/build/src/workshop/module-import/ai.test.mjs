import {registerHooks} from "node:module";
import {readFileSync} from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";
const manifest=JSON.parse(readFileSync(new URL("../../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
registerHooks({resolve(s,c,next){if(s==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)};`),shortCircuit:true};try{return next(s,c)}catch(e){if(s.startsWith("./")||s.startsWith("../"))return next(s+".ts",c);throw e;}}});
const {compileWorkshopModule}=await import("./compile.ts");
function fixture(){
 const module=JSON.parse(readFileSync(new URL("./sample.workshop.json",import.meta.url))),bindings={objects:{Asset:"build.note"},fields:{Asset:{id:"id",name:"note"}},actions:{finishAsset:"build.note.close"},queries:{}};
 for(const [id,type,config]of [["analyst","AIPAnalyst",{contextVarId:"selected",outputVarId:"notes"}],["generated","AIPGenerated",{objectVarId:"selected"}],["chat","AIPChatbot",{historyVarId:"history",inputVarId:"question",suggestions:["Summarise this record"]}]]){
  module.widgets[id]={id,type,name:id,config};module.sections.root.children.push({kind:"widget",id});
 }
 module.variables.push({id:"question",name:"Question",type:"string",definitionKind:"static",staticValue:""},{id:"history",name:"History",type:"struct",definitionKind:"static",staticValue:[]});
 const fn=(name,conversation)=>({ref:{app:"build",kind:"function",name},version:"1.function-1",function:{object:"build.note",conversation,fields:["note"],output:[{name:"summary",type:"string",required:true}]}}),definitions=[fn("advice",false),fn("chat",true)],functionBinding=d=>({ref:d.ref,sourceVersion:d.version});
 bindings.ai={analyst:{migration:"record-scoped-functions",function:functionBinding(definitions[0]),outputMigration:"display-only"},generated:{migration:"record-scoped-functions",function:functionBinding(definitions[0])},chat:{migration:"record-scoped-functions",function:functionBinding(definitions[1]),recordVarId:"selected",replyField:"summary",historyMigration:"original-call-history"}};
 return {module,bindings,target:{object:"build.note",profile:manifest.uiProfile,entities:[{app:"build",type:"build.note",fields:[{name:"note",type:"text"}]}],actions:[{schema:"build.note.close",target:"build.note"}],definitions}};
}
const compile=f=>compileWorkshopModule(JSON.stringify(f.module),"page",f.bindings,f.target);
test("three AI source presentations share original confirmed context and explicit function contracts",()=>{
 const f=fixture(),r=compile(f);assert.ok(r.draft,JSON.stringify(r.diagnostics));const ai=r.draft.sections.filter(s=>s.ai);assert.equal(ai.length,3);assert.equal(ai[0].recordVariable,ai[1].recordVariable);assert.equal(ai[1].recordVariable,ai[2].recordVariable);assert.deepEqual(ai[2].ai.suggestions,["Summarise this record"]);assert.equal(r.draft.document.variables[ai[2].ai.questionVariable].mode,"state");assert.equal(r.source,JSON.stringify(f.module));assert.ok(r.diagnostics.some(d=>d.code==="native-original-ai"&&!d.blocking));
});
test("mock histories, unreviewed output changes, wrong owners, modes, versions and source execution refuse together",()=>{
 for(const mutate of [f=>delete f.bindings.ai,f=>delete f.bindings.ai.chat.function,f=>f.bindings.ai.chat.function.sourceVersion=123,f=>f.bindings.ai.analyst.outputMigration=undefined,f=>f.target.definitions[1].function.conversation=false,f=>f.bindings.ai.chat.replyField="absent",f=>f.bindings.ai.chat.function.sourceVersion="1.function-2",f=>f.target.definitions[1].ref.app="foreign",f=>f.module.variables.find(v=>v.id==="history").staticValue=[{role:"assistant",text:"fake"}],f=>f.module.widgets.generated.config.execute="javascript",f=>f.target.profile="platform.page.v2.85"]){const f=fixture();mutate(f);assert.equal(compile(f).draft,undefined,String(mutate));}
});
