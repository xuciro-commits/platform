import {isNumber,isStringSet,isDecimal,parseDecimal,decimalDraft,compareDecimal,decimalArithmetic,type ScalarValue} from "./decimal";
import type { Api, pageUIManifest } from "@platform/kernel";
import type { QueryWindow, RecordReference } from "./Session";

type Contract = typeof pageUIManifest.runtime;
type Variables = Record<string, Api.PageVariable>;
type Scalar = ScalarValue;
export type VariableIssue = { variable: string; code: string };
export type PropertyReader=(reference:RecordReference,field:string,type:string)=>VariableResult;
export type ResourceValue = import("./collection-input").CollectionInput | import("@platform/ui").StatisticsValue | {kind:"record-set";object:string;records:RecordReference[]} | { kind: "record"; reference: RecordReference } | { kind: "filter"; object: string; fields: Record<string, unknown> } | { kind: "object-set"; window: QueryWindow };
export type VariableResult = { status: "value"; value: Scalar | ResourceValue; draft?:string } | { status: "empty"; value?: ResourceValue } | { status: "pending" } | { status: "error"; code: string; draft?:string };
const validID = /^[A-Za-z][A-Za-z0-9._:-]{0,79}$/;
const bytes = (value: string) => new TextEncoder().encode(value).length;
const valueType = (value: unknown, contract: Contract) => isNumber(value)?"number":isStringSet(value)?"string-set":isDecimal(value,contract.decimal.maxBytes)?"decimal":typeof value === "boolean" ? "boolean"
  : typeof value === "string" && bytes(value) <= contract.maxStringBytes ? "string" : "";

/** Finite presentation graph. The supplied contract is the generated Go
 * manifest; explicit dependencies are the only edges. No expression text.
 */
export function compileVariables(variables: Variables, contract: Contract) {
  const issues: VariableIssue[] = [], order: string[] = [], visiting = new Set<string>(), visited = new Set<string>();
  const fail = (variable: string, code: string) => { issues.push({ variable, code }); return ""; };
  if (Object.keys(variables).length > contract.maxVariables) return { order, issues: [{ variable: "", code: "Variable limit exceeded" }] };
  const visit = (id: string): string => {
    const variable = variables[id];
    if (!variable) return fail(id, "Missing variable");
    if (visiting.has(id)) return fail(id, "Cyclic variable dependency");
    if (visited.has(id)) return variable.type;
    visited.add(id); visiting.add(id);
    if (!validID.test(id) || (variable.scope !== contract.scope && variable.scope !== contract.loop.scope && variable.scope !== contract.overlay.scope && variable.scope !== contract.application.scope) || ([contract.scope,contract.application.scope].includes(variable.scope as "page" | "application") ? !!variable.owner : !validID.test(variable.owner ?? "")) || !(contract.valueTypes as readonly string[]).includes(variable.type) || bytes(variable.title ?? "") > 1024) fail(id, "Unsupported variable type or scope");
    if (variable.scope === contract.overlay.scope && (!(contract.overlay.valueTypes as readonly string[]).includes(variable.type) || !(contract.overlay.modes as readonly string[]).includes(variable.mode))) fail(id, "Overlay variable needs a supported local value or resource");
    if (variable.scope === contract.application.scope && !(contract.application.valueTypes as readonly string[]).includes(variable.type)) fail(id,"Application variable needs a supported scalar or resource");
    if (variable.writable && variable.mode !== "shared") fail(id,"Only shared bindings declare writable");
    if(variable.type==="record-set"&&(!["page","overlay"].includes(variable.scope)||variable.mode!=="resource"||variable.source?.kind!=="records"))fail(id,"Resource source type mismatch");
    if(variable.type==="string-set"&&(!["page","overlay"].includes(variable.scope)||!["state","constant"].includes(variable.mode)))fail(id,"Unsupported variable type or scope");
    if(variable.type==="statistics"&&(variable.mode!=="aggregate"||!["page","overlay"].includes(variable.scope)))fail(id,"Statistics need a read-only scoped declaration");
    if(variable.type==="number"&&(!["aggregate","constant","derived"].includes(variable.mode)||variable.scope==="application"))fail(id,"Number needs a read-only scoped declaration");
    if(variable.source?.measure&&(variable.mode!=="aggregate"||variable.source.kind!=="aggregate"&&variable.source.kind!=="statistics"))fail(id,"Measure needs an aggregate scalar");
    if (variable.mode !== "resource" && variable.mode !== "property" && variable.mode !== "aggregate" && variable.mode !== "shared" && variable.source) fail(id, "Only resource or shared variables may declare a source");
    if(variable.source?.object&&!((variable.mode==="shared"&&["object-set","record","filter"].includes(variable.type))||(variable.mode==="resource"&&variable.scope==="application"&&["record","filter"].includes(variable.type))||variable.mode==="property"))fail(id,"Only shared windows declare an object requirement");
    if(variable.source?.fields?.length&&!(variable.scope==="application"&&variable.mode==="resource"&&variable.type==="filter"))fail(id,"Only an application filter declares fields");
    if(variable.source?.field&&variable.mode!=="property")fail(id,"Only a property source declares a field");
    if(variable.mode==="aggregate") {
      const source=variable.source;if(!source||!((variable.type==="decimal"&&source.kind===contract.aggregate.source&&!source.measure)||(variable.type==="number"&&source.kind==="aggregate"&&/^(sum|avg|min|max):[A-Za-z][A-Za-z0-9._:-]{0,79}$/.test(source.measure??""))||(variable.type==="statistics"&&source.kind==="statistics"&&validID.test(source.measure??"")))||!validID.test(source.query??"")||source.section||source.node||source.variable||variable.initial!==undefined||variable.expression)fail(id,"Aggregate needs only a count query source");
    } else if(variable.mode==="property") {
      const source=variable.source,parent=variables[source?.variable??""];if(!source||source.kind!=="property"||!validID.test(source.variable??"")||!validID.test(source.field??"")||!source.object||source.object.kind!=="object"||!source.object.app||!source.object.name||source.section||source.node||source.query||source.fields?.length||variable.expression||variable.initial!==undefined||!["string","boolean","decimal"].includes(variable.type))fail(id,"Property needs a typed record and field source");
      if(parent?.scope==="loop-item"&&(variable.scope!=="loop-item"||parent.owner!==variable.owner)||parent?.scope==="overlay"&&(variable.scope!=="overlay"||parent.owner!==variable.owner))fail(id,"Property source escapes its scope");if(visit(source?.variable??"")!=="record")fail(id,"Property source must be a record");
    } else if (variable.mode === "shared") {
      if(["record","filter"].includes(variable.type)&&(!variable.source?.object||variable.source.object.kind!=="object"||!variable.source.object.app||!variable.source.object.name))fail(id,"Shared record needs an object requirement");
      if(variable.type==="object-set"&&(variable.writable||!variable.source?.object||variable.source.object.kind!=="object"||!variable.source.object.app||!variable.source.object.name))fail(id,"Shared window needs a read-only object requirement");
      if (variable.scope !== "application" || !variable.source || variable.source.kind !== "application" || !validID.test(variable.source.variable ?? "") || variable.source.section || variable.source.node || variable.source.query || variable.initial !== undefined || variable.expression) fail(id,"Shared binding needs only an application variable source");
    } else if (variable.mode === "resource" && variable.source?.kind === "plan") {
      if (!["page","overlay","application","loop-item"].includes(variable.scope) || variable.type !== "object-set" || !validID.test(variable.source.query ?? "") || variable.source.section || variable.source.node || variable.source.variable || variable.initial !== undefined || variable.expression) fail(id,"Plan source needs a scoped query window");
    } else if(variable.mode === "resource"&&variable.scope==="application"&&["record","filter"].includes(variable.source?.kind??"")) {
      if(variable.type!==variable.source?.kind||!variable.source?.object||variable.source.object.kind!=="object"||!variable.source.object.app||!variable.source.object.name||variable.source.section||variable.source.node||variable.source.query||variable.source.variable||variable.initial!==undefined||variable.expression)fail(id,"Application record needs only an object source");
      if(variable.type==="filter"&&(!variable.source?.fields?.length||variable.source.fields.length>contract.application.maxFilterFields||new Set(variable.source.fields).size!==variable.source.fields.length||variable.source.fields.some((field)=>!validID.test(field))))fail(id,"Application filter needs bounded fields");
    } else if (variable.mode === "resource") {
      if (!variable.source || variable.source.variable || variable.source.query || variable.scope === "application" || (variable.scope === contract.loop.scope ? variable.type !== "record" || variable.source.kind !== contract.loop.source || variable.source.node !== variable.owner || !!variable.source.section : !validID.test(variable.source.section ?? "") || !!variable.source.node) || variable.expression || variable.initial !== undefined || (variable.scope !== contract.loop.scope && !contract.resources.some((resource) => resource.kind === variable.source!.kind && resource.type === variable.type))) fail(id, "Resource source type mismatch");
    } else if (variable.mode === "input") {
      if (variable.scope !== "page" || variable.source || variable.expression || !(contract.interface.valueTypes as readonly string[]).includes(variable.type) || variable.initial !== undefined && valueType(variable.initial, contract) !== variable.type) fail(id, "Invalid page input variable");
    } else if (variable.mode === "state" || variable.mode === "constant") {
      if (variable.expression || valueType(variable.initial, contract) !== variable.type) fail(id, "Initial value type mismatch");
    } else if (variable.mode === "derived") {
      const expr = variable.expression, op = contract.operators.find((op) => op.id === expr?.op);
      if (variable.initial !== undefined || !expr || !op || !Array.isArray(expr.args)) fail(id, "Unsupported variable expression");
      else {
        if (expr.args.length < op.minArgs || expr.args.length > op.maxArgs || variable.type !== op.output) fail(id, "Operator arity or output type mismatch");
        const types = expr.args.map((arg) => {
          if (!arg || typeof arg !== "object") return fail(id, "Argument needs a variable or literal");
          if (!!arg.variable === (arg.literal !== undefined)) return fail(id, "Argument needs a variable or literal");
          if (arg.variable && variables[arg.variable]?.scope === contract.loop.scope && (variable.scope !== contract.loop.scope || variable.owner !== variables[arg.variable]?.owner)) return fail(id, "Item dependency escapes its loop scope");
          if (arg.variable && variables[arg.variable]?.scope === contract.overlay.scope && (variable.scope !== contract.overlay.scope || variable.owner !== variables[arg.variable]?.owner)) return fail(id, "Overlay dependency escapes its owner scope");
          return arg.variable ? visit(arg.variable) : valueType(arg.literal, contract);
        });
        if (types.some((type) => !type || (op.input === "resource" ? !contract.resources.some((resource) => resource.type === type) : type !== (op.input === "same" ? types[0] : op.input) || op.input === "same" && !["string", "boolean","decimal","number"].includes(type)))) fail(id, "Argument type mismatch");
      }
    } else fail(id, "Unsupported variable mode");
    visiting.delete(id); order.push(id);
    return variable.type;
  };
  Object.keys(variables).sort().forEach(visit);
  return { order, issues };
}

const operators: Record<Contract["operators"][number]["id"], (values: Scalar[]) => Scalar> = {
 "parse-decimal":([text])=>{const value=parseDecimal(text as string);if(!value)throw new Error("Invalid numeric value.");return value;},
  "parse-number":([text])=>{const parsed=parseDecimal(text as string),value=parsed?Number(parsed.value):NaN;if(!Number.isFinite(value))throw new Error("Invalid numeric value.");return {kind:"number",value};},
  equal: ([a, b]) => isNumber(a)&&isNumber(b)?a.value===b.value:isDecimal(a)&&isDecimal(b)?compareDecimal(a,b)===0:a === b, not: ([a]) => !a,
  and: (values) => values.every((value) => value === true), or: (values) => values.some((value) => value === true),
  concat: (values) => values.join(""),
  present: () => true,
  "decimal-add":([a,b])=>decimalArithmetic(a as import("./decimal").DecimalValue,b as import("./decimal").DecimalValue),
  "decimal-subtract":([a,b])=>decimalArithmetic(a as import("./decimal").DecimalValue,b as import("./decimal").DecimalValue,true),
  "decimal-less":([a,b])=>compareDecimal(a as import("./decimal").DecimalValue,b as import("./decimal").DecimalValue)<0,
};

export function evaluateVariables(variables: Variables, state: Record<string, unknown>, contract: Contract, resources: Record<string, VariableResult> = {}, owner?: string, overlay?: string,property?:PropertyReader,inherited?:Record<string,VariableResult>): Record<string, VariableResult> {
  const compiled = compileVariables(variables, contract), result: Record<string, VariableResult> = {};
  if (compiled.issues.length) return Object.fromEntries(Object.keys(variables).map((id) => [id, { status: "error", code: "Invalid variable graph" }]));
  for (const id of compiled.order) {
    const variable = variables[id]!;
    if (variable.scope === contract.loop.scope && variable.owner !== owner || variable.scope === contract.overlay.scope && variable.owner !== overlay) { result[id] = { status: "empty" }; continue; }
    if(variable.scope!==contract.loop.scope&&inherited?.[id]) { result[id]=inherited[id]!;continue; }
    if(variable.mode==="property") {const parent=result[variable.source!.variable!];if(!parent||parent.status!=="value"){result[id]=parent?.status==="error"?{status:"error",code:"Property source read failed"}:parent??{status:"empty"};continue;}const value=parent.value;if(typeof value!=="object"||value.kind!=="record"||value.reference.object!==variable.source?.object?.name){result[id]={status:"error",code:"Property record object is unavailable"};continue;}const read=property?.(value.reference,variable.source!.field!,variable.type)??{status:"error" as const,code:"Property value is unavailable."};result[id]=read.status==="value"&&valueType(read.value,contract)!==variable.type?{status:"error",code:"Property value type mismatch"}:read;continue;}
    if (variable.mode === "input" || variable.mode === "shared" || variable.mode === "aggregate") { result[id] = resources[id] ?? (variable.initial !== undefined ? { status: "value", value: variable.initial as Scalar } : { status: "empty" }); continue; }
    if (variable.mode === "resource") {
      const value = resources[id] ?? { status: "empty" };
      result[id] = (value.status === "value" || value.status === "empty") && value.value !== undefined && (value.value === null || typeof value.value !== "object" || value.value.kind !== variable.type)
        ? { status: "error", code: "Resource source type mismatch" } : value;
      continue;
    }
    if (variable.mode !== "derived") {
      const value = variable.mode === "state" && Object.hasOwn(state, id) ? state[id] : variable.initial;
      if(variable.type==="decimal"&&decimalDraft(value,contract.decimal.maxBytes)){const parsed=parseDecimal(value.value,contract.decimal.maxBytes);result[id]=parsed?{status:"value",value:parsed,draft:value.value}:{status:"error",code:"Invalid numeric value.",draft:value.value};continue;}
      result[id] = valueType(value, contract) === variable.type ? { status: "value", value: value as Scalar } : variable.type==="decimal"&&decimalDraft(value,contract.decimal.maxBytes)?{status:"error",code:"Invalid numeric value.",draft:value.value}:{ status: "error", code: "State value type mismatch" };
      continue;
    }
    const expression = variable.expression!;
    const inputs = expression.args.map((arg): VariableResult => arg.variable ? result[arg.variable]! : { status: "value", value: arg.literal as Scalar });
    if (inputs.some((input) => input.status === "error")) { result[id] = { status: "error", code: "Variable dependency failed" }; continue; }
    if (inputs.some((input) => input.status === "pending")) { result[id] = { status: "pending" }; continue; }
    if (expression.op === "present" && inputs[0]?.status === "empty") { result[id] = { status: "value", value: false }; continue; }
    if (inputs.some((input) => input.status === "empty")) { result[id] = { status: "empty" }; continue; }
    let value:Scalar;try {value = operators[expression.op as keyof typeof operators](inputs.map((input) => (input as { value: Scalar }).value));}catch {result[id]={status:"error",code:["parse-decimal","parse-number"].includes(expression.op)?"Invalid numeric value.":"Numeric result exceeds its budget."};continue;}
    result[id] = valueType(value, contract) === variable.type ? { status: "value", value } : { status: "error", code: "Variable result limit exceeded" };
  }
  return result;
}
