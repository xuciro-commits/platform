import assert from "node:assert/strict";
import test from "node:test";
import {createRecordExploration,explorationRelations,originalDirectoryItems} from "./reader.ts";
const object=name=>({app:"business",kind:"object",name:`data.${name}`}),asset=object("asset"),sensor=object("sensor"),alert=object("alert");
const binding=name=>({ref:{app:"business",kind:"link-type",name},sourceVersion:"original-1"}),links=[{ref:binding("sensors").ref,version:"later-2",linkVersions:{"original-1":{parent:asset,child:sensor,via:"asset",forward:"Sensors",reverse:"Asset"}}},{ref:binding("alerts").ref,version:"original-1",linkType:{parent:asset,child:alert,via:"asset",forward:"Alerts",reverse:"Asset"}}];
const infos=[{app:"business",type:asset.name,fields:[{name:"title",type:"text"}]},{app:"business",type:sensor.name,fields:[{name:"name",type:"text"},{name:"asset",type:"reference",ref:asset.name}]},{app:"business",type:alert.name,fields:[{name:"name",type:"text"},{name:"asset",type:"reference",ref:asset.name}]}];
const config={objects:[{object:asset,labelField:"title"},{object:sensor,labelField:"name"},{object:alert,labelField:"name"}],relations:[{id:"sensors",binding:binding("sensors")},{id:"alerts",binding:binding("alerts")}],outputs:[]},current={object:asset,id:"original-root"};
const row=(id,name)=>({id,revision:1,name}),defer=()=>{let resolve;const promise=new Promise(r=>resolve=r);return{promise,resolve}};
test("search around reuses retained authorized traversal windows and totals, with independent per-type identities",async()=>{
 const calls=[],source={scope:"member",revision:1,entity:type=>infos.find(i=>i.type===type),get:async(type,id)=>({record:row(id,"Confirmed")}),list:async(type,query)=>{calls.push({type,query});return {records:[row("same-id",type)],total:27};}},reader=createRecordExploration({source,definitions:links,active:()=>true});
 const frame=await reader.around(config,current);assert.equal(frame.length,2);assert.equal(frame[0].page.total,27);assert.equal(frame[1].page.total,27);assert.notEqual(frame[0].object.name,frame[1].object.name);assert.equal(calls[0].query.limit,20);assert.equal(calls[0].query.offset,0);assert.deepEqual(calls[0].query.sort,["id"]);assert.deepEqual(calls[0].query.traversal,{binding:binding("sensors"),direction:"forward",id:current.id});await reader.around(config,current);assert.equal(calls.length,2);
 const reverse=explorationRelations(source,links,config,{object:sensor,id:"same-id"});assert.equal(reverse[0].direction,"reverse");assert.deepEqual(reverse[0].object,asset);assert.equal((await reader.confirm({object:sensor,id:"same-id"})).name,"Confirmed");
});
test("private relationship fields, wrong owners and missing retained versions refuse rather than fabricating empty neighborhoods",async()=>{
 const source={scope:"member",entity:type=>infos.find(i=>i.type===type),list:async()=>({records:[],total:0})};
 for(const broken of [{...source,entity:type=>{const info=source.entity(type);return type===sensor.name?{...info,fields:info.fields.filter(f=>f.name!=="asset")}:info;}},{...source,entity:type=>{const info=source.entity(type);return type===sensor.name?{...info,app:"other"}:info;}}])assert.throws(()=>explorationRelations(broken,links,config,current));
 assert.throws(()=>explorationRelations(source,[{...links[0],linkVersions:{}}],config,current));
 const failed=createRecordExploration({source:{...source,list:async()=>{throw Error("Permission refused");}},definitions:links,active:()=>true});const frame=await failed.around(config,current);assert.ok(frame.every(r=>r.error==="Permission refused"&&!r.page));
});
test("retired scopes and wrong record identity never publish late or substituted records",async()=>{
 const pending=defer(),source={scope:"member-A",entity:type=>infos.find(i=>i.type===type),list:()=>pending.promise,get:async()=>({record:row("wrong","Unconfirmed")})},reader=createRecordExploration({source,definitions:links,active:()=>true});await assert.rejects(reader.confirm({object:sensor,id:"same-id"}),/unavailable or incompatible/);const answer=reader.around(config,current);source.scope="member-B";pending.resolve({records:[row("old","Private")],total:1});await assert.rejects(answer,/scope has ended/);
});
test("Vertex retains two original 4 and 3 windows with actual totals and Compass resolves only declared original assets",async()=>{
 const calls=[],source={scope:"member",entity:type=>infos.find(i=>i.type===type),list:async(type,query)=>{calls.push(query);return {records:Array.from({length:query.limit},(_,n)=>row(`ID${n}`,type)),total:10};}},reader=createRecordExploration({source,definitions:links,active:()=>true}),groups=[{id:"sensors",binding:binding("sensors"),direction:"forward",limit:4,badge:"S",tone:"success"},{id:"alerts",binding:binding("alerts"),direction:"forward",limit:3,badge:"A",tone:"danger"}];
 const frame=await reader.neighborhood({groups},current);assert.deepEqual(calls.map(c=>c.limit),[4,3]);assert.deepEqual(frame.map(c=>c.page.total),[10,10]);assert.deepEqual(frame.map(c=>c.object),[sensor,alert]);
 assert.equal(originalDirectoryItems({items:[{id:"first",label:"Original placeholder",asset:binding("sensors")}]},links)[0].asset.sourceVersion,"original-1");assert.deepEqual(originalDirectoryItems({items:[{id:"first",label:"Original placeholder",asset:{...binding("sensors"),sourceVersion:"missing"}}]},links),[]);
});
test("an old revision cannot return old records or erase the replacement window cache",async()=>{
 const old=defer(),fresh=defer();let reads=0;
 const source={scope:"member",revision:1,entity:type=>infos.find(i=>i.type===type),list:()=>++reads===1?old.promise:fresh.promise},one={...config,relations:[config.relations[0]]},reader=createRecordExploration({source,definitions:links,active:()=>true});
 const before=reader.around(one,current);source.revision=2;const after=reader.around(one,current);
 old.resolve({records:[row("old","Old private frame")],total:1});const retired=await before;assert.equal(retired[0].page,undefined);assert.match(retired[0].error,/changed/);
 fresh.resolve({records:[row("fresh","New original frame")],total:1});assert.equal((await after)[0].page.records[0].id,"fresh");assert.equal((await reader.around(one,current))[0].page.records[0].id,"fresh");assert.equal(reads,2);
});
