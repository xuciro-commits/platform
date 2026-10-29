import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if((specifier.startsWith("./")||specifier.startsWith("../"))&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
const {formRuleValues}=await import("./form-rules.ts");
const clock={me:"member",now:"2026-10-28T04:00:00Z",today:"2026-10-27"};
const fields=[{name:"price",type:"decimal"},{name:"qty",type:"integer"},{name:"amount",type:"decimal",readOnly:true},{name:"total",type:"decimal",readOnly:true}];
const mul={op:"*",kind:"decimal",args:[{kind:"decimal",ref:"price"},{kind:"integer",ref:"qty"}]};
const info={fields,localRules:{total:{op:"+",kind:"decimal",args:[{kind:"decimal",ref:"amount"},{kind:"decimal",lit:{value:"0.1"}}]},amount:mul}};
test("form preview evaluates the original Expr with exact inputs without changing the form",()=>{
 const input={price:"0.1",qty:2},before=structuredClone(input),result=formRuleValues(info,input,clock);
 assert.equal(result.amount.value,"0.2");assert.equal(result.total.value,"0.3");assert.deepEqual(input,before);
});
test("missing, null and incomplete sources never turn into zero or an implicit host read",()=>{
 const result=formRuleValues(info,{price:null},clock);
 assert.notEqual(result.amount.status,"value");assert.notEqual(result.total.status,"value");
 const incomplete=formRuleValues({fields:[{name:"total",type:"integer"}],localRules:{total:{op:"count",kind:"integer",agg:{source:"unloaded"}}}},{},clock);
 assert.equal(incomplete.total.status,"error");assert.equal(incomplete.total.value,undefined);
});
test("form preview uses the explicitly captured business day",()=>{
 const result=formRuleValues({fields:[{name:"day",type:"date"}],localRules:{day:{op:"$today",kind:"date"}}},{},clock);
 assert.equal(result.day.value,"2026-10-27");
});
