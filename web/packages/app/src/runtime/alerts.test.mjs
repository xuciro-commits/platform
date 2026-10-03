import {registerHooks} from "node:module";
registerHooks({resolve(s,c,next){try{return next(s,c)}catch(e){if(s.startsWith("./"))return next(`${s}.ts`,c);throw e;}}});
import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
const {evaluateAlert}=await import("./alerts.ts");
const limits=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/platform/pageui/widgets.json",import.meta.url))).runtime.alertBanner;
const config={threshold:"0",tone:"danger",message:"{value} affected; literal {value} <img src=x>"},value=raw=>({status:"value",value:{kind:"decimal",value:raw}});
test("alerts compare exact scalars and only interpolate the original first token as plain text",()=>{
 assert.deepEqual(evaluateAlert(value("9007199254740993"),{...config,threshold:"9007199254740992"},limits),{status:"message",text:"9007199254740993 affected; literal {value} <img src=x>"});
 assert.equal(evaluateAlert(value("0"),config,limits).status,"clear");assert.equal(evaluateAlert(value("-1"),config,limits).status,"clear");
 assert.equal(evaluateAlert(value("0.00000000000000000001"),config,limits).status,"message");assert.equal(evaluateAlert(value("0.1"),{...config,threshold:"0.1"},limits).status,"clear");
});
test("missing, pending, refused and incompatible values replace prior alert messages without becoming zero",()=>{
 assert.equal(evaluateAlert(undefined,config,limits).status,"empty");assert.equal(evaluateAlert({status:"pending"},config,limits).status,"pending");assert.deepEqual(evaluateAlert({status:"error",code:"Denied"},config,limits),{status:"error",code:"Denied"});
 for(const raw of ["bad","0.0","1e3","-0"]){assert.equal(evaluateAlert(value(raw),config,limits).status,"error")}
 for(const raw of [0,"1",{kind:"number",value:1},null])assert.equal(evaluateAlert({status:"value",value:raw},config,limits).status,"error");
 for(const invalid of [undefined,{...config,threshold:"0.0"},{...config,tone:"custom"},{...config,message:""},{...config,message:"中".repeat(1366)}])assert.equal(evaluateAlert(value("1"),invalid,limits).status,"error");
});
