import {registerHooks} from "node:module";
registerHooks({resolve(specifier,context,next){try{return next(specifier,context)}catch(error){if(specifier.startsWith("./")&&!specifier.endsWith(".ts"))return next(`${specifier}.ts`,context);throw error;}}});
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
const { compileVariables, evaluateVariables }=await import("./variables.ts");
const root = new URL("../../../../../capabilities/server/platform/pageui/", import.meta.url);
const contract = JSON.parse(readFileSync(new URL("widgets.json", root))).runtime;
const vectors = JSON.parse(readFileSync(new URL("variables.vectors.json", root)));
test("declared decimal parsing follows the original text input and retains invalid, pending and refused state",()=>{const variables={text:{scope:"page",type:"string",mode:"state",initial:"0.10"},number:{scope:"page",type:"decimal",mode:"derived",expression:{op:"parse-decimal",args:[{variable:"text"}]}}};assert.equal(compileVariables(variables,contract).issues.length,0);assert.deepEqual(evaluateVariables(variables,{},contract).number,{status:"value",value:{kind:"decimal",value:"0.1"}});assert.equal(evaluateVariables(variables,{text:""},contract).number.code,"Invalid numeric value.");assert.equal(evaluateVariables(variables,{text:"1e3"},contract).number.status,"error");const resource={...variables,text:{scope:"page",type:"string",mode:"input"}};assert.equal(evaluateVariables(resource,{},contract,{text:{status:"pending"}}).number.status,"pending");assert.equal(evaluateVariables(resource,{},contract,{text:{status:"error",code:"Denied"}}).number.status,"error");});
for (const vector of vectors) test(vector.name, () => {
  assert.equal(compileVariables(vector.variables, contract).issues.length === 0, vector.valid);
  if (!vector.valid) return;
  const values = (state) => Object.fromEntries(Object.entries(evaluateVariables(vector.variables, state, contract, vector.resources, vector.owner, vector.overlay,(_reference,field)=>({status:"value",value:vector.properties?.[field]}))).map(([id, result]) => {
    assert.equal(result.status, "value"); return [id, result.value];
  }));
  assert.deepEqual(values({}), vector.values);
  if (vector.state) assert.deepEqual(values(vector.state), vector.updated);
});
test("runtime rejects wrong state types and propagates bounded result errors", () => {
  const variables = vectors[0].variables;
  assert.equal(evaluateVariables(variables, { tab: true }, contract).tab.status, "error");
  const result = evaluateVariables(variables, { tab: "a".repeat(4096) }, contract);
  assert.equal(result.tab.status, "value");
  assert.equal(result.label.status, "error");
  // Evaluation never mutates the document or shares a state snapshot.
  assert.equal(evaluateVariables(variables, {}, contract).tab.value, "first");
});

test("resource presence preserves pending, empty and refusal without coercing record objects", () => {
  const variables = vectors.find((vector) => vector.name === "typed record output").variables;
  assert.deepEqual(evaluateVariables(variables, {}, contract, { selected: { status: "empty" } }).hasRecord, { status: "value", value: false });
  assert.equal(evaluateVariables(variables, {}, contract, { selected: { status: "pending" } }).hasRecord.status, "pending");
  assert.equal(evaluateVariables(variables, {}, contract, { selected: { status: "error", code: "Denied" } }).hasRecord.status, "error");
  assert.equal(evaluateVariables(variables, {}, contract, { selected: { status: "value", value: "untyped record" } }).hasRecord.status, "error");
});

test("an overlay can evaluate only its own locals; page evaluation exposes no hidden local value", () => {
  const variables = vectors.find((vector) => vector.name === "typed overlay local scope").variables;
  assert.equal(evaluateVariables(variables, { local: "private" }, contract).local.status, "empty");
  assert.equal(evaluateVariables(variables, { local: "private" }, contract, {}, undefined, "other").label.status, "empty");
  assert.equal(evaluateVariables(variables, { local: "private" }, contract, {}, undefined, "panel").label.value, "pageprivate");
});

test("unfinished numeric drafts retain text and stop dependent evaluation while valid text normalizes exactly",()=>{
 const vars=vectors.find(v=>v.name==="exact decimal arithmetic graph").variables;
 const invalid=evaluateVariables(vars,{a:{kind:"decimal",value:"-0."}},contract);assert.equal(invalid.a.status,"error");assert.equal(invalid.a.draft,"-0.");assert.equal(invalid.sum.status,"error");
 const valid=evaluateVariables(vars,{a:{kind:"decimal",value:"0.100"}},contract);assert.equal(valid.a.draft,"0.100");assert.deepEqual(valid.sum.value,{kind:"decimal",value:"0.3"});
});

test("property evaluation follows source state and never reads a value from an obsolete reference",()=>{const v=vectors.find(v=>v.name==="typed record property graph");let reads=0;const lookup=()=>{reads++;return {status:"value",value:true}};for(const status of ["empty","pending","error"]){const result=evaluateVariables(v.variables,{},contract,{record:{status,code:"Denied"}},undefined,undefined,lookup);assert.equal(result.active.status,status);}assert.equal(reads,0);});
