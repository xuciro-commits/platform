import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
const vectors=JSON.parse(readFileSync(new URL("../../../../../capabilities/server/internal/rules/conformance.json",import.meta.url)));
for(const vector of vectors)test(`Go/Wasm conformance: ${vector.name}`,()=>{
 const result=JSON.parse(globalThis.platformRuleEval(JSON.stringify(vector.request)));
 if(vector.error){assert.equal(result.error?.code,vector.error);return;}
 assert.equal(result.error,undefined);assert.deepEqual(result.value.value,vector.value);
 if(vector.reason)assert.equal(result.reasons[0].code,vector.reason);
});
