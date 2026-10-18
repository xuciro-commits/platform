import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if((specifier.startsWith("./")||specifier.startsWith("../"))&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
const {recordKanbanMoves}=await import("./record-actions.ts");
const lifecycle={field:"state",initial:"open",states:[{name:"open",title:"Open"},{name:"done",title:"Done"},{name:"cancelled",title:"Cancelled"}],transitions:[
 {schema:"sample.item.finish",title:"Finish",from:["open"],to:["done"]},
 {schema:"sample.item.resolve",title:"Resolve",from:["open"],to:["done","cancelled"],toInput:"result"},
]};
const info={type:"sample.item",fields:[{name:"state",type:"choice",choices:["open","done","cancelled"]}],lifecycle};
const finish={schema:"sample.item.finish",target:"sample.item",payload:[]};
const resolve={schema:"sample.item.resolve",target:"sample.item",payload:[{name:"result",type:"string",choices:["done","cancelled"]}]};
test("record board commands follow only the current member catalog and declared lifecycle",()=>{
 assert.deepEqual(recordKanbanMoves(info,[]),[]);
 assert.deepEqual(recordKanbanMoves(info,[finish]),[{schema:finish.schema,title:"Finish",from:["open"],to:"done",input:undefined}]);
 assert.deepEqual(recordKanbanMoves({...info,lifecycle:undefined},[finish,resolve]),[]);
 assert.deepEqual(recordKanbanMoves({...info,fields:[]},[finish,resolve]),[]);
 assert.deepEqual(recordKanbanMoves(info,[{...finish,target:"another.item"}]),[]);
 assert.deepEqual(recordKanbanMoves(info,[{...finish,new:true}]),[]);
});
test("a multi-target transition requires its original declared choice parameter",()=>{
 assert.deepEqual(recordKanbanMoves(info,[resolve]).map(move=>[move.to,move.input]),[["done","result"],["cancelled","result"]]);
 for(const payload of [[],[{name:"other",type:"string",choices:["done","cancelled"]}],[{name:"result",type:"string",choices:["done"]}]])assert.deepEqual(recordKanbanMoves(info,[{...resolve,payload}]),[]);
 assert.deepEqual(recordKanbanMoves({...info,lifecycle:{...lifecycle,transitions:[{...lifecycle.transitions[1],toInput:undefined}]}},[resolve]),[]);
});
