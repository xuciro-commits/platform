import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
import assert from "node:assert/strict";
import test from "node:test";
const {authzExplainRequest}=await import("./explain-request.ts");

test("the explain asks about the refused action's schema, not the authority that carries it",()=>{
 const submission={schema:{name:"sample.note.publish"},authority:"build",principalId:"member-2",target:{type:"sample.note",id:"N-1"}};
 assert.deepEqual(authzExplainRequest(submission,"me-1","tenant-a"),{
  principal:"member-2",tenant:"tenant-a",kind:"write",type:"sample.note",record:"N-1",action:"sample.note.publish",
 });
 // A creation refusal has no record; the caller's own principal fills in.
 const created={schema:{name:"sample.note.create"},authority:"build",principalId:"",target:{type:"sample.note"}};
 assert.deepEqual(authzExplainRequest(created,"me-1","tenant-a"),{
  principal:"me-1",tenant:"tenant-a",kind:"create",type:"sample.note",action:"sample.note.create",
 });
});
