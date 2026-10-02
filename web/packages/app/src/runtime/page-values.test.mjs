import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
import assert from "node:assert/strict";
import test from "node:test";
const { checkPortValues, navigationValues, readPageEnvelope,portValues }=await import("./page-values.ts");

const ports={record:{variable:"record",type:"record",object:{app:"sample",kind:"object",name:"sample.record"},required:true},flag:{variable:"flag",type:"boolean"}};
test("page interfaces transfer references without fields and reject missing or mismatched input",()=>{
  assert.equal(checkPortValues(ports,{record:{object:"sample.record",id:"A"},flag:false}),undefined);
  for(const input of [{},{record:{object:"sample.other",id:"A"}},{record:{object:"sample.record",id:"A",private:"secret"}},{record:{object:"sample.record",id:"A"},flag:"true"}])assert.ok(checkPortValues(ports,input));
  assert.deepEqual(navigationValues({record:{variable:"item"}},{item:{status:"value",value:{kind:"record",reference:{object:"sample.record",id:"A"}}}}),{record:{object:"sample.record",id:"A"}});
  assert.throws(()=>navigationValues({record:{variable:"item"}},{item:{status:"pending"}}),/unavailable/);
  assert.equal(readPageEnvelope({version:1,values:{},script:"bad"}),undefined);
});

test("page interface transfers exact decimals and rejects invalid or untyped numeric values",()=>{
 const ports={amount:{variable:"amount",type:"decimal",required:true}},value={kind:"decimal",value:"9007199254740993"};
 assert.equal(checkPortValues(ports,{amount:value}),undefined);assert.deepEqual(portValues(ports,{amount:{status:"value",value}}),{amount:value});
 assert.equal(typeof checkPortValues(ports,{amount:9007199254740993}),"string");assert.equal(typeof checkPortValues(ports,{amount:{kind:"decimal",value:"1."}}),"string");
});
