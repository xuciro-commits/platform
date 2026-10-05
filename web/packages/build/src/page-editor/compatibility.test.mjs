import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
import {registerHooks} from "node:module";
const manifest=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
registerHooks({resolve(specifier,context,next){
 if(specifier==="@platform/app")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIProfile=${JSON.stringify(manifest.uiProfile)},pageVariableContract=${JSON.stringify(manifest.runtime)};const manifest=${JSON.stringify(manifest)};export const supportsPageUIProfile=p=>manifest.supportedProfiles.includes(p);export const widgetContract=id=>manifest.widgets.find(w=>w.componentID===id);`),shortCircuit:true};
 if(specifier==="@platform/app/query-inventory")return {url:new URL("../../../app/src/runtime/query-inventory.ts",import.meta.url).href,shortCircuit:true};
 return next(specifier,context);
}});
const {pageCompatibility,applyProfileUpgrade}=await import("./compatibility.ts");
const fixture=()=>({title:"Original",description:"",selections:[],sections:[{id:"table",widget:"table",configVersion:1,fields:["name"],collectionVariable:"assetWindow"}],document:{formatVersion:2,uiProfile:"platform.page.v2.96",root:"root",nodes:{root:{kind:"rows",children:["table"]},table:{kind:"widget",section:"table"}},variables:{assetWindow:{scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query:"original"}}},queries:{original:{object:{app:"original",kind:"object",name:"original.asset"},limit:20,query:{ref:{app:"original",kind:"query",name:"assets"},sourceVersion:"1.query-7"}}}}});

test("a reviewed profile upgrade preserves configuration, resource identity and the original draft bytes",()=>{
 const original=fixture(),bytes=JSON.stringify(original),review=pageCompatibility(original);
 assert.ok(review.upgrade);assert.equal(review.current.active.size,1);assert.equal(review.target.active.size,1);
 const next=applyProfileUpgrade(original,review.upgrade);
 assert.equal(next.document.uiProfile,manifest.uiProfile);
 assert.equal(JSON.stringify(original),bytes);
 assert.deepEqual({...next,document:{...next.document,uiProfile:original.document.uiProfile}},original);
 next.sections[0].fields.push("changed");assert.deepEqual(original.sections[0].fields,["name"]);
 assert.equal(pageCompatibility(next).upgrade,undefined,"current profiles need no ceremonial upgrade");
});
test("unknown formats, profiles, widget identities and configuration versions cannot acquire a migration",()=>{
 for(const mutate of [d=>d.document.formatVersion=3,d=>d.document.uiProfile="platform.page.v2.999",d=>d.sections[0].widget="foreign",d=>d.sections[0].configVersion=2,d=>delete d.sections[0].configVersion]){
  const draft=fixture();mutate(draft);const bytes=JSON.stringify(draft);assert.equal(pageCompatibility(draft).upgrade,undefined);assert.equal(JSON.stringify(draft),bytes);
 }
});
test("stale reviews cannot overwrite later edits or select an unreviewed destination",()=>{
 const original=fixture(),review=pageCompatibility(original).upgrade;
 for(const mutate of [d=>d.title="New draft",d=>d.sections[0].fields.push("pressure"),d=>d.document.queries.original.limit=10]){
  const next=structuredClone(original);mutate(next);assert.equal(applyProfileUpgrade(next,review),undefined);
 }
 assert.equal(applyProfileUpgrade(original,{...review,to:"platform.page.v2.999"}),undefined);
});
test("the review exposes actual inventory activation differences and refuses target budget overflow",()=>{
 const draft=fixture();draft.document.nodes.root.children=[];draft.document.unusedWidgets=[{node:"table",parent:"root"}];
 const report=pageCompatibility(draft);assert.equal(report.current.active.size,1);assert.equal(report.target.active.size,0);assert.equal(report.current.total,20);assert.equal(report.target.total,0);assert.ok(report.upgrade);
 draft.document.queries.original.limit=manifest.runtime.query.maxDeclaredTotalLimit+1;
 assert.equal(pageCompatibility(draft).target.valid,false);assert.equal(pageCompatibility(draft).upgrade,undefined);
});
