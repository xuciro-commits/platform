import assert from "node:assert/strict";
import test from "node:test";
import { checkPortValues, navigationValues, readPageEnvelope } from "./page-values.ts";

const ports={record:{variable:"record",type:"record",object:{app:"sample",kind:"object",name:"sample.record"},required:true},flag:{variable:"flag",type:"boolean"}};
test("page interfaces transfer references without fields and reject missing or mismatched input",()=>{
  assert.equal(checkPortValues(ports,{record:{object:"sample.record",id:"A"},flag:false}),undefined);
  for(const input of [{},{record:{object:"sample.other",id:"A"}},{record:{object:"sample.record",id:"A",private:"secret"}},{record:{object:"sample.record",id:"A"},flag:"true"}])assert.ok(checkPortValues(ports,input));
  assert.deepEqual(navigationValues({record:{variable:"item"}},{item:{status:"value",value:{kind:"record",reference:{object:"sample.record",id:"A"}}}}),{record:{object:"sample.record",id:"A"}});
  assert.throws(()=>navigationValues({record:{variable:"item"}},{item:{status:"pending"}}),/unavailable/);
  assert.equal(readPageEnvelope({version:1,values:{},script:"bad"}),undefined);
});
