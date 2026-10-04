import assert from "node:assert/strict";
import test from "node:test";
import {registerHooks} from "node:module";
registerHooks({resolve(s,c,n){try{return n(s,c)}catch(e){if(s.startsWith("./")||s.startsWith("../"))return n(s+".ts",c);throw e;}}});
const {createAIReader}=await import("../ai/service.ts");
const attempt={id:"CALL",app:"build",name:"chat",version:1,source:"A",question:"Original question",history:["PRIOR"]};
test("an unanswered original AI request resends its own key and cannot create a second model call",async()=>{
 const entries=[{submission:{tenantId:"tenant",principalId:"member",target:{type:"build.function-call",id:"CALL"},idempotencyKey:"original",schema:{name:"build.function-call.start"},payload:btoa(JSON.stringify({app:"build",name:"chat",version:1,source:"A",question:"Original question",history:["PRIOR"]}))},state:"SUBMISSION_STATE_UNKNOWN"}];let sends=0;
 const host={client:{connection:{tenant:"tenant",principal:"member"},authorities:{outbox:entries}},can:()=>true,resend:async()=>{sends++;entries[0].state="SUBMISSION_STATE_CONFIRMED";},decide:async()=>{throw Error("duplicate call")},source:{},me:{principalId:"member"}},reader=createAIReader(host,()=>true);
 assert.equal(await reader.request(attempt),true);assert.equal(sends,1);assert.equal(entries[0].submission.idempotencyKey,"original");
});
test("results retain current caller, source and fixed function identity and reject stale or withheld answers",async()=>{
 let active=true;const record={id:"CALL",app:"build",function:"chat",version:1,source:"build.note/A",member:"member",state:"ready",contract:{name:"chat",object:"build.note"},output:"{}"},host={client:{},can:()=>true,me:{principalId:"member"},source:{get:async()=>({record})}},reader=createAIReader(host,()=>active);
 assert.equal((await reader.read(attempt)).id,"CALL");
 for(const patch of [{member:"other"},{source:"build.note/B"},{version:2},{withheld:true}]){Object.assign(record,patch);await assert.rejects(reader.read(attempt));Object.assign(record,{member:"member",source:"build.note/A",version:1,withheld:false});}
 host.source.get=async()=>{active=false;return {record}};await assert.rejects(reader.read(attempt),/scope has ended/);
});

test("a changed unanswered AI payload is never resent as the current attempt",async()=>{let sends=0;const host={can:()=>true,client:{connection:{tenant:"tenant",principal:"member"},authorities:{outbox:[{state:"SUBMISSION_STATE_UNKNOWN",submission:{tenantId:"tenant",principalId:"member",target:{type:"build.function-call",id:"CALL"},schema:{name:"build.function-call.start"},payload:btoa("{}")}}]}},resend:async()=>{sends++;}},reader=createAIReader(host,()=>true);await assert.rejects(reader.request(attempt),/not confirmed/);assert.equal(sends,0);});
