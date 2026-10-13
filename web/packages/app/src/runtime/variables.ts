import {checkRule,evaluateRule,lowerPageRule,pageRuleKind,pageRuleFits,pageRuleSchema,ruleDependencies,type RuleContext,type RuleValue} from "./rule-engine";
import {isNumber,isStringSet,isDecimal,parseDecimal,decimalDraft,type ScalarValue} from "./decimal";
import type { Api, pageUIManifest } from "@platform/kernel";
import type { QueryWindow, RecordReference } from "./Session";

type Contract = typeof pageUIManifest.runtime;
type Variables = Record<string, Api.PageVariable>;
type Scalar = ScalarValue;
export type VariableIssue = { variable: string; code: string };
export type PropertyReader=((reference:RecordReference,field:string,type:string)=>VariableResult)&{clock?:{me?:string;now:string;today:string}};
export type ResourceValue = import("./collection-input").CollectionInput | import("@platform/ui").StatisticsValue | {kind:"record-set";object:string;records:RecordReference[]} | { kind: "record"; reference: RecordReference } | { kind: "filter"; object: string; fields: Record<string, unknown> } | { kind: "object-set"; window: QueryWindow };
export type VariableResult = { status: "value"; value: Scalar | ResourceValue; draft?:string;reasons?:import("./rule-engine").RuleIssue[] } | { status: "empty"; value?: ResourceValue } | { status: "pending" } | { status: "error"; code: string; draft?:string };
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
    if(variable.type==="record-set"&&!(["page","overlay"].includes(variable.scope)&&variable.mode==="resource"&&variable.source?.kind==="records"||variable.scope==="application"&&(variable.mode==="shared"&&variable.source?.kind==="application"||variable.mode==="resource"&&variable.source?.kind==="record-set")))fail(id,"Resource source type mismatch");
    if(variable.type==="string-set"&&(!["page","overlay"].includes(variable.scope)||!["state","constant"].includes(variable.mode)))fail(id,"Unsupported variable type or scope");
    if(variable.type==="statistics"&&(variable.mode!=="aggregate"||!["page","overlay"].includes(variable.scope)))fail(id,"Statistics need a read-only scoped declaration");
    if(variable.type==="number"&&(!(["aggregate","constant","derived"].includes(variable.mode)||variable.mode==="resource"&&variable.source?.kind==="compute")||variable.scope==="application"))fail(id,"Number needs a read-only scoped declaration");
    if(variable.source?.measure&&(variable.mode!=="aggregate"||variable.source.kind!=="aggregate"&&variable.source.kind!=="statistics"))fail(id,"Measure needs an aggregate scalar");
    if (variable.mode !== "resource" && variable.mode !== "property" && variable.mode !== "aggregate" && variable.mode !== "shared" && variable.source) fail(id, "Only resource or shared variables may declare a source");
    if(variable.source?.object&&!((variable.mode==="shared"&&["object-set","record","filter","record-set"].includes(variable.type))||(variable.mode==="resource"&&variable.scope==="application"&&["record","filter","record-set"].includes(variable.type))||variable.mode==="property"))fail(id,"Only shared windows declare an object requirement");
    if(variable.source?.fields?.length&&!(variable.scope==="application"&&variable.mode==="resource"&&variable.type==="filter"))fail(id,"Only an application filter declares fields");
    if(variable.source?.field&&variable.mode!=="property")fail(id,"Only a property source declares a field");
    if(variable.mode==="aggregate") {
      const source=variable.source;if(!source||!((variable.type==="decimal"&&source.kind===contract.aggregate.source&&!source.measure)||(variable.type==="number"&&source.kind==="aggregate"&&/^(sum|avg|min|max):[A-Za-z][A-Za-z0-9._:-]{0,79}$/.test(source.measure??""))||(variable.type==="statistics"&&source.kind==="statistics"&&validID.test(source.measure??"")))||!validID.test(source.query??"")||source.section||source.node||source.variable||variable.initial!==undefined||variable.expression)fail(id,"Aggregate needs only a count query source");
    } else if(variable.mode==="property") {
      const source=variable.source,parent=variables[source?.variable??""];if(!source||source.kind!=="property"||!validID.test(source.variable??"")||!validID.test(source.field??"")||!source.object||source.object.kind!=="object"||!source.object.app||!source.object.name||source.section||source.node||source.query||source.fields?.length||variable.expression||variable.initial!==undefined||!["string","boolean","decimal"].includes(variable.type))fail(id,"Property needs a typed record and field source");
      if(parent?.scope==="loop-item"&&(variable.scope!=="loop-item"||parent.owner!==variable.owner)||parent?.scope==="overlay"&&(variable.scope!=="overlay"||parent.owner!==variable.owner))fail(id,"Property source escapes its scope");if(visit(source?.variable??"")!=="record")fail(id,"Property source must be a record");
    } else if (variable.mode === "shared") {
      if(["record","filter","record-set"].includes(variable.type)&&(!variable.source?.object||variable.source.object.kind!=="object"||!variable.source.object.app||!variable.source.object.name))fail(id,"Shared record needs an object requirement");
      if(variable.type==="object-set"&&(variable.writable||!variable.source?.object||variable.source.object.kind!=="object"||!variable.source.object.app||!variable.source.object.name))fail(id,"Shared window needs a read-only object requirement");
      if (variable.scope !== "application" || !variable.source || variable.source.kind !== "application" || !validID.test(variable.source.variable ?? "") || variable.source.section || variable.source.node || variable.source.query || variable.initial !== undefined || variable.expression) fail(id,"Shared binding needs only an application variable source");
     } else if(variable.mode==="resource"&&variable.source?.kind==="compute") {
      const source=variable.source,c=source.compute,parent=variables[c?.recordVariable??""];
      if(!["number","decimal"].includes(variable.type)||variable.writable||!["page","overlay"].includes(variable.scope)||!c||!validID.test(c.recordVariable)||c.operation.ref.kind!=="compute"||!c.operation.ref.app||!c.operation.ref.name||!c.operation.sourceVersion||!Object.keys(c.inputs).length||Object.keys(c.inputs).length>64||c.outputField&&!validID.test(c.outputField)||source.section||source.node||source.query||source.variable||source.object||source.field||source.fields?.length||source.measure||source.port||variable.expression||variable.initial!==undefined)fail(id,"Compute needs its original typed resource");
      if(parent?.type!=="record"||!(parent.mode==="shared"&&parent.scope==="application"&&parent.source?.kind==="application"&&parent.source.object||parent.mode==="resource"&&parent.source?.kind==="record"&&parent.scope===variable.scope&&parent.owner===variable.owner))fail(id,"Compute input record escapes its owner");
      if(c){if(!Object.values(c.inputs).some(b=>b.source==="subject")||Object.values(c.inputs).some(b=>b.source!=="literal"&&!(b.source==="subject"&&b.path?.length===1&&validID.test(b.path[0]!))))fail(id,"Compute needs a protected scalar input");visit(c.recordVariable);}
    } else if (variable.mode === "resource" && variable.source?.kind === "plan") {
      if (!["page","overlay","application","loop-item"].includes(variable.scope) || variable.type !== "object-set" || !validID.test(variable.source.query ?? "") || variable.source.section || variable.source.node || variable.source.variable || variable.initial !== undefined || variable.expression) fail(id,"Plan source needs a scoped query window");
    } else if(variable.mode === "resource"&&variable.scope==="application"&&["record","filter","record-set"].includes(variable.source?.kind??"")) {
      if(variable.type!==variable.source?.kind||!variable.source?.object||variable.source.object.kind!=="object"||!variable.source.object.app||!variable.source.object.name||variable.source.section||variable.source.node||variable.source.query||variable.source.variable||variable.initial!==undefined||variable.expression)fail(id,"Application record needs only an object source");
      if(variable.type==="filter"&&(!variable.source?.fields?.length||variable.source.fields.length>contract.application.maxFilterFields||new Set(variable.source.fields).size!==variable.source.fields.length||variable.source.fields.some((field)=>!validID.test(field))))fail(id,"Application filter needs bounded fields");
    } else if (variable.mode === "resource") {
      if (!variable.source || variable.source.variable || variable.source.query || variable.scope === "application" || (variable.scope === contract.loop.scope ? variable.type !== "record" || variable.source.kind !== contract.loop.source || variable.source.node !== variable.owner || !!variable.source.section : !validID.test(variable.source.section ?? "") || !!variable.source.node) || variable.expression || variable.initial !== undefined || (variable.scope !== contract.loop.scope && !contract.resources.some((resource) => resource.kind === variable.source!.kind && resource.type === variable.type))) fail(id, "Resource source type mismatch");
    } else if (variable.mode === "input") {
      if (variable.scope !== "page" || variable.source || variable.expression || !(contract.interface.valueTypes as readonly string[]).includes(variable.type) || variable.initial !== undefined && valueType(variable.initial, contract) !== variable.type) fail(id, "Invalid page input variable");
    } else if (variable.mode === "state" || variable.mode === "constant") {
      if (variable.expression || valueType(variable.initial, contract) !== variable.type) fail(id, "Initial value type mismatch");
    } else if (variable.mode === "derived") {
      try{
        if(variable.initial!==undefined||!!variable.rule===!!variable.expression)throw new Error("One rule expression required");
        const schema=pageRuleSchema(variables),expr=variable.rule??lowerPageRule(variable.expression!,pageRuleKind(variable.type),schema);
        for(const ref of ruleDependencies(expr)){
          const dependency=variables[ref];
          if(dependency?.scope===contract.loop.scope&&(variable.scope!==contract.loop.scope||variable.owner!==dependency.owner)||dependency?.scope===contract.overlay.scope&&(variable.scope!==contract.overlay.scope||variable.owner!==dependency.owner))throw new Error("Rule dependency escapes its scope");
          visit(ref);
        }
        if(!pageRuleFits(variable.type,expr.kind))throw new Error("Rule output type mismatch");
        checkRule(expr,schema);
      }catch(error){fail(id,error instanceof Error?error.message:"Invalid rule");}
    } else fail(id, "Unsupported variable mode");
    visiting.delete(id); order.push(id);
    return variable.type;
  };
  Object.keys(variables).sort().forEach(visit);
  return { order, issues };
}

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
      result[id] = (value.status === "value" || value.status === "empty") && value.value !== undefined && (value.value === null || typeof value.value !== "object" || value.value.kind !== variable.type && !(variable.type==="object-set" && variable.source?.kind==="query" && value.value.kind==="object-set-input"))
        ? { status: "error", code: "Resource source type mismatch" } : value;
      continue;
    }
    if (variable.mode !== "derived") {
      const value = variable.mode === "state" && Object.hasOwn(state, id) ? state[id] : variable.initial;
      if(variable.type==="decimal"&&decimalDraft(value,contract.decimal.maxBytes)){const parsed=parseDecimal(value.value,contract.decimal.maxBytes);result[id]=parsed?{status:"value",value:parsed,draft:value.value}:{status:"error",code:"Invalid numeric value.",draft:value.value};continue;}
      result[id] = valueType(value, contract) === variable.type ? { status: "value", value: value as Scalar } : variable.type==="decimal"&&decimalDraft(value,contract.decimal.maxBytes)?{status:"error",code:"Invalid numeric value.",draft:value.value}:{ status: "error", code: "State value type mismatch" };
      continue;
    }
    try {
      const schema=pageRuleSchema(variables),expression=variable.rule??lowerPageRule(variable.expression!,pageRuleKind(variable.type),schema);
      const context:RuleContext={refs:{},...property?.clock};
      for(const ref of ruleDependencies(expression)){
        const dependency=result[ref],kind=schema.refs[ref]!.kind;
        let value:RuleValue={kind,status:"missing"};
        if(dependency?.status==="pending")value.status="pending";
        else if(dependency?.status==="error")value.status="withheld";
        else if(dependency?.status==="empty")value={kind,status:"null"};
        else if(dependency?.status==="value"){
          const raw=dependency.value;
          value={kind,status:"value",value:isStringSet(raw)?raw.values:isDecimal(raw)||isNumber(raw)?raw.value:typeof raw==="object"&&raw!==null?raw.kind==="record"?raw.reference.id:"present":raw};
        }
        context.refs[ref]=value;
      }
      const readPath=(ref:RecordReference,path:string[],kind:string):RuleValue=>{
        const value=property?.(ref,path.join("."),kind)??{status:"error" as const,code:"Rule property is unavailable."};
        if(value.status!=="value")return {kind,status:value.status==="error"?"withheld":value.status==="empty"?"missing":value.status};
        const raw=value.value;return {kind,status:"value",value:isDecimal(raw)||isNumber(raw)?raw.value:isStringSet(raw)?raw.values:raw};
      };
      const visit=(e:Api.Expr,item?:{fields:Record<string,RuleValue>;reference:RecordReference})=>{
        if(e.agg){const resource=result[e.agg.source];if(resource?.status!=="value"){context.sets??={};context.sets[e.agg.source]={items:[],complete:false,status:resource?.status==="error"?"withheld":resource?.status==="pending"?"pending":"missing"};}if(resource?.status==="value"&&typeof resource.value==="object"&&resource.value.kind==="object-set"){
          const window=resource.value.window,fields=schema.sets?.[e.agg.source]?.element.fields??{};
          const needed=new Set<string>();const use=(x:Api.Expr)=>{if(x.ref?.startsWith("item."))needed.add(x.ref.slice(5));for(const a of x.args??[])use(a);if(x.in)use(x.in)};if(e.in)use(e.in);if(e.agg.field?.length)needed.add(e.agg.field[0]!);
          const items=window.records.map(reference=>{const row={id:reference.id,fields:Object.fromEntries([...needed].filter(key=>fields[key]).map(key=>[key,readPath(reference,[key],fields[key]!.kind)]))};const holder={fields:row.fields,reference};if(e.in)visit(e.in,holder);if(e.agg!.field?.length){const path=e.agg!.field;row.fields[path.join(".")]=readPath(reference,path,e.kind)};return row});
          context.sets??={};context.sets[e.agg.source]={items,complete:window.complete};
        }}
        if(e.op==="path"&&e.args?.[0]?.ref){const ref=e.args[0].ref;if(ref.startsWith("item.")&&item){const path=[ref.slice(5),...(e.path??[])];item.fields[path.join(".")]=readPath(item.reference,path,e.kind)}else{const value=result[ref];if(value?.status==="value"&&typeof value.value==="object"&&value.value.kind==="record")context.refs[`${ref}.${e.path?.join(".")}`]=readPath(value.value.reference,e.path??[],e.kind)}}
        for(const a of e.args??[])visit(a,item);if(e.in&&!e.agg)visit(e.in,item);
      };
      visit(expression);
      const evaluated=evaluateRule(expression,schema,context),answer=evaluated.value;
      const value=(variable.type==="decimal"?{kind:"decimal",value:String(answer.value)}:variable.type==="number"?{kind:"number",value:Number(answer.value)}:variable.type==="string-set"?{kind:"string-set",values:answer.value}:answer.value) as Scalar;
      result[id]=valueType(value,contract)===variable.type?{status:"value",value,...(evaluated.reasons?.length?{reasons:evaluated.reasons}:{})}:{status:"error",code:"Variable result limit exceeded"};
    }catch(error){const issue=(error as {issue?:{code:string}}).issue;result[id]=issue?.code==="pending"?{status:"pending"}:issue?.code==="missing"?{status:"empty"}:{status:"error",code:["parse-decimal","parse-number"].includes(variable.rule?.op??variable.expression?.op??"")?"Invalid numeric value.":error instanceof Error?error.message:"Variable evaluation failed"};}

  }
  return result;
}
