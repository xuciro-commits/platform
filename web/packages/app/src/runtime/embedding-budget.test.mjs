import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
registerHooks({resolve(s,c,next){try{return next(s,c)}catch(e){if(s.startsWith("./"))return next(s+".ts",c);throw e;}}});
const {PageEmbeddingBudget,pageEmbeddingCost}=await import("./embedding-budget.ts");
test("repeated mounted children consume shared original read budgets and retirement releases their reservations",()=>{
 const root={instances:1,queries:1,records:12,sections:3},budget=new PageEmbeddingBudget({instances:16,queries:8,records:512,sections:256},root),cost={instances:1,queries:1,records:100,sections:3},owners=Array.from({length:6},()=>({}));for(const owner of owners.slice(0,5))assert.equal(budget.reserve(owner,cost),true);assert.equal(budget.reserve(owners[5],cost),false);assert.equal(budget.reserve(owners[0],cost),true);budget.release(owners[0]);assert.equal(budget.reserve(owners[5],cost),true);
});
test("original loop multipliers count expanded query rows rather than unique query descriptors",()=>{
 const page={sections:[],document:{root:"root",nodes:{root:{kind:"rows",children:["loop"]},loop:{kind:"loop",loop:{limit:4},children:[]}},queries:{plain:{limit:10},children:{itemOwner:"loop",limit:20}}}};assert.equal(pageEmbeddingCost(page).records,90);const budget=new PageEmbeddingBudget({instances:16,queries:8,records:512,sections:256},{instances:1,queries:0,records:0,sections:0});assert.equal(budget.reserve({}, {...pageEmbeddingCost(page),records:Infinity}),false);
});
test("dormant nested loops retain their original ownership and consume expanded budgets",()=>{
 const page={sections:[],document:{root:"root",nodes:{root:{kind:"rows",children:["outer"]},outer:{kind:"loop",loop:{limit:4},children:[]},inner:{kind:"loop",loop:{limit:3},children:[]}},unusedWidgets:[{node:"inner",parent:"outer"}],queries:{hidden:{itemOwner:"inner",limit:20}}}};
 assert.equal(pageEmbeddingCost(page).records,240);
});
test("external browsing contexts consume the same instance budget before a draft can mount them",()=>{
 const cost=pageEmbeddingCost({sections:Array.from({length:16},()=>({externalFrame:{url:"https://docs.example.com/report",origin:"https://docs.example.com"}}))}),budget=new PageEmbeddingBudget({instances:16,queries:8,records:512,sections:256},cost);assert.equal(cost.instances,17);assert.equal(budget.valid,false);assert.equal(budget.reserve({}, {instances:1,queries:0,records:0,sections:0}),false);
});

test("embedding cost follows inventory ownership and restores the original read budget on mounting",()=>{
 const page={sections:[{id:'unused',widget:'table',collectionVariable:'spare'}],document:{uiProfile:'platform.page.v2.97',root:'root',nodes:{root:{kind:'rows',children:[]},unused:{kind:'widget',section:'unused'}},unusedWidgets:[{node:'unused',parent:'root'}],queries:{spare:{limit:100}},variables:{spare:{type:'object-set',mode:'resource',scope:'page',source:{kind:'plan',query:'spare'}}}}};
 assert.equal(pageEmbeddingCost(page).queries,0);assert.equal(pageEmbeddingCost(page).records,0);page.document.nodes.root.children.push('unused');assert.equal(pageEmbeddingCost(page).records,100);
});
