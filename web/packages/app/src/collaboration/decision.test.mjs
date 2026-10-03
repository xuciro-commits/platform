import assert from "node:assert/strict";
import test from "node:test";
const {confirmedDecision}=await import("./decision.ts");
const entry=(key,state,tenant="T")=>({submission:{idempotencyKey:key,tenantId:tenant},state});
test("confirmation belongs to the same tenant and exact invocation even when unrelated answers follow",()=>{
 const confirmed=entry("mine","SUBMISSION_STATE_CONFIRMED"),denied=entry("other","SUBMISSION_STATE_REFUSED");assert.equal(confirmedDecision([confirmed,denied],"T","mine"),true);assert.equal(confirmedDecision([entry("mine","SUBMISSION_STATE_REFUSED"),entry("other","SUBMISSION_STATE_CONFIRMED")],"T","mine"),false);assert.equal(confirmedDecision([entry("mine","SUBMISSION_STATE_CONFIRMED","Other")],"T","mine"),false);for(const state of ["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_UNKNOWN"])assert.equal(confirmedDecision([entry("mine",state),entry("other","SUBMISSION_STATE_CONFIRMED")],"T","mine"),false);assert.equal(confirmedDecision([],"T","mine"),false);
});
