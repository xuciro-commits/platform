import {registerHooks} from "node:module";
import {readFileSync} from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";
const manifest=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url)));
registerHooks({resolve(s,c,n){if(s==="@platform/kernel")return {url:"data:text/javascript,"+encodeURIComponent(`export const pageUIManifest=${JSON.stringify(manifest)}`),shortCircuit:true};return n(s,c)}});
const {termCounts}=await import("./terms.ts");
const data=()=>({columns:[{name:"status",kind:"group",type:"nominal"},{name:"count",kind:"measure",type:"quantitative"}],rows:[{status:"B",count:2},{status:"A",count:2},{status:"constructor",count:4},{status:null,count:1},{status:"—",count:1},{status:"",count:1}]});
test("term counts validate original columns, preserve typed identity and keep host tie order",()=>{assert.deepEqual(termCounts(data(),"status").map(t=>t.value),["constructor","B","A",null,"—",""]);for(const change of [d=>d.rows[0].count=NaN,d=>d.rows[0].count=0,d=>d.rows[0].count=Number.MAX_SAFE_INTEGER+1,d=>d.rows[0].status={},d=>d.rows.push(d.rows[0]),d=>d.columns[0].type="quantitative",d=>d.columns[1].money=true,d=>d.rows=Array.from({length:65},(_,i)=>({status:String(i),count:1}))]){const d=data();change(d);assert.equal(termCounts(d,"status"),undefined);}assert.deepEqual(termCounts({...data(),rows:[]},"status"),[]);assert.equal(termCounts(data(),"count"),undefined);});
