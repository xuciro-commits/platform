import test from "node:test";
import assert from "node:assert/strict";
import {parseDecimal,isDecimal,compareDecimal,decimalArithmetic} from "./decimal.ts";
test("decimal text, arithmetic and comparison remain exact outside binary number precision",()=>{
 const d=text=>parseDecimal(text);
 assert.deepEqual(decimalArithmetic(d("0.1"),d("0.2")),d("0.3"));assert.equal(compareDecimal(d("9007199254740993"),d("9007199254740992")),1);
 assert.deepEqual(decimalArithmetic(d("-0.1"),d("0.2"),true),d("-0.3"));assert.equal(d("1.200").value,"1.2");assert.equal(d("-0").value,"0");
 for(const text of ["","01","+1","1.","1e99999","NaN","9".repeat(129)])assert.equal(d(text),undefined);
 assert.equal(isDecimal({kind:"decimal",value:"1",extra:true}),false);assert.throws(()=>decimalArithmetic(d("9".repeat(128)),d("1")));
});
