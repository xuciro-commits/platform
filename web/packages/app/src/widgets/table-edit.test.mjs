import {registerHooks} from "node:module";
import {readFileSync} from "node:fs";
const manifest=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
registerHooks({resolve(specifier,context,next){if(specifier==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)}`),shortCircuit:true};try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
import assert from "node:assert/strict";
import test from "node:test";
const {tableEditableFields,sectionWriteActions,defaultTableEdit}=await import("./table-edit.ts");

const info={type:"sample.note",fields:[
 {name:"title",type:"text",label:"Title"},
 {name:"bucket",type:"choice",choices:["A","B"]},
 {name:"body",type:"longtext"},
 {name:"frozen",type:"text",readOnly:true},
 {name:"lines",type:"lines"},
],standard:["sample.note.edit"],lifecycle:{field:"state",states:[]}};
const edit={name:"sample.note.edit",schema:"sample.note.edit",target:"sample.note",payload:[{name:"title"},{name:"bucket"},{name:"body"},{name:"frozen"}]};

test("page and section action sets intersect and explicit empties forbid section writes",()=>{
 const page={actions:[{app:"sample",kind:"action",name:"sample.note.edit"},{app:"sample",kind:"action",name:"sample.note.send"}]};
 assert.deepEqual(sectionWriteActions(page,{}),["sample.note.edit","sample.note.send"]);
 assert.deepEqual(sectionWriteActions(page,{actions:[]}),[]);
 assert.deepEqual(sectionWriteActions(page,{actions:[{app:"sample",kind:"action",name:"sample.note.send"}]}),["sample.note.send"]);
 assert.deepEqual(sectionWriteActions(page,{actions:[{app:"sample",kind:"action",name:"sample.note.close"}]}),[]);
 assert.deepEqual(sectionWriteActions({...page,actions:[]},{}),[]);
});

test("a defaulted table edit derives from the member catalog and the displayed fields under the original gates",()=>{
 const catalog=[edit,{name:"sample.note.close",schema:"sample.note.close",target:"sample.note",payload:[]}];
 const derived=defaultTableEdit(info,catalog,["sample.note.edit"]);
 assert.equal(derived?.schema,"sample.note.edit");
 assert.deepEqual(tableEditableFields(info,derived,["title","bucket","body","frozen","lines"]),["title","bucket","body"]);
 assert.equal(defaultTableEdit(info,catalog,[]),undefined);
 assert.equal(defaultTableEdit(info,catalog,["sample.note.close"]),undefined);
 assert.equal(defaultTableEdit(info,[{...edit,needsApproval:true}],["sample.note.edit"]),undefined);
 assert.equal(defaultTableEdit(info,[{...edit,payload:[{name:"title",required:true}]}],["sample.note.edit"]),undefined);
 assert.equal(defaultTableEdit(info,[{...edit,name:"sample.other.edit",schema:"sample.other.edit"}],["sample.other.edit"]),undefined);
 assert.deepEqual(tableEditableFields(info,defaultTableEdit(info,catalog,["sample.note.edit"],["bucket"]),["bucket"]),["bucket"]);
 assert.equal(defaultTableEdit(info,catalog,["sample.note.edit"],["frozen"]),undefined);
 assert.equal(defaultTableEdit(undefined,catalog,["sample.note.edit"]),undefined);
 assert.equal(defaultTableEdit(info,[{...edit,target:"sample.other"}],["sample.note.edit"]),undefined);
});
