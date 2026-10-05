import assert from "node:assert/strict";
import test from "node:test";
import {reduceDraft} from "../session/draft-history.ts";
import {workflowInputs,workflowRunMatches} from "../workflow-session.ts";

const workflow=()=>({id:"original",revision:4,name:"example",title:"Example",object:"build.asset",when:"",manual:true,input:{},inputSchema:{type:"object",properties:{}},steps:[{name:"result",kind:"end",value:{source:"literal",value:"original"}}],layout:{result:{x:100,y:200}},version:1,published:"unchanged"});
const history=draft=>({draft,saved:JSON.stringify(draft),past:[],future:[]});
test("a confirmed save retains undo/redo and a later acknowledgement cannot replace newer edits",()=>{
 const initial=workflow();let state=reduceDraft(history(initial),{type:"edit",edit:{title:"Saved title"}}),submitted=state.draft;
 state=reduceDraft(state,{type:"saved",draft:submitted,normalized:{...submitted,revision:5}});
 assert.equal(state.past.length,1);assert.equal(state.saved,JSON.stringify(state.draft));
 state=reduceDraft(state,{type:"undo"});assert.equal(state.draft.title,"Example");assert.notEqual(JSON.stringify(state.draft),state.saved);
 state=reduceDraft(state,{type:"redo"});assert.equal(state.draft.revision,5);assert.equal(JSON.stringify(state.draft),state.saved);
 state=reduceDraft(state,{type:"edit",edit:{title:"Later title"}});
 state=reduceDraft(state,{type:"saved",draft:submitted,normalized:{...submitted,revision:5}});
 assert.equal(state.draft.title,"Later title");assert.notEqual(JSON.stringify(state.draft),state.saved);
});
test("saving then installing acknowledges each captured snapshot without clearing history",()=>{
 const initial=workflow();let state=reduceDraft(history(initial),{type:"edit",edit:{title:"New title"}});
 const submitted=state.draft,confirmed={...submitted,revision:5};
 state=reduceDraft(state,{type:"saved",draft:submitted,normalized:confirmed});
 const published={...confirmed,revision:6,version:2,published:"exact-installed-bytes"};
 state=reduceDraft(state,{type:"saved",draft:confirmed,normalized:published});
 assert.equal(state.draft.version,2);assert.equal(state.past.length,1);assert.equal(JSON.stringify(state.draft),state.saved);
 const inputs=workflowInputs(state.draft);for(const field of ["revision","id","version","published"])assert.equal(field in inputs,false);
});
test("run decoration and outputs require the exact installed definition and original flow identity",()=>{
 const installed=workflow(),run={flow:"build.example",version:1};
 assert.equal(workflowRunMatches(installed,installed,run,false),true);
 const reordered={...installed,inputSchema:{properties:{},type:"object"}};assert.equal(workflowRunMatches(reordered,installed,run,false),true);
 const savedDraft={...installed,revision:5,steps:[{...installed.steps[0],value:{source:"literal",value:"new draft"}}]};
 assert.equal(workflowRunMatches(savedDraft,installed,run,false),false,"a saved draft with the old version stamp is not the installed definition");
 for(const change of [{flow:"other.example"},{version:2},{withheld:true}])assert.equal(workflowRunMatches(installed,installed,{...run,...change},false),false);
 assert.equal(workflowRunMatches(installed,installed,run,true),false);assert.equal(workflowRunMatches(installed,undefined,run,false),false);
});
