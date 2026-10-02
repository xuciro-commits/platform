import {isDecimal,type DecimalValue} from "./decimal";
import type { Api, pageUIManifest } from "@platform/kernel";
import type { EntityInfo, RecordQuery } from "@platform/ui";
import type { ResourceValue, VariableResult } from "./variables";
import type { QueryView } from "./Session";

/** Resolve only the explicitly selected retained query version. */
export function boundQueryDefinition(definition: Api.Definition | undefined, binding: Api.AssetBinding | undefined): Api.Definition | undefined {
 if(!definition||!binding)return undefined;
 if(definition.version===binding.sourceVersion&&definition.query)return definition;
 const query=definition.queryVersions?.[binding.sourceVersion];
 return query?{...definition,version:binding.sourceVersion,query,queryVersions:undefined}:undefined;
}
export function variablePlan(page:Api.Page,variableID:string):string|undefined {
 const variable=page.document?.variables?.[variableID],source=variable?.source;
 if(source?.kind==="plan")return source.query;
 if(source?.kind==="query"){const section=page.sections?.find((s)=>s.id===source.section);const bound=page.document?.variables?.[section?.collectionVariable??""];if(bound?.source?.kind==="plan")return bound.source.query;}
 return undefined;
}
export const planKey = (id: string) => `plan/${id}`;
type Contract = typeof pageUIManifest.runtime.query;
export type QueryPlanResult = { status: "value"; object: string; query: RecordQuery; signature: string } | { status: "empty" | "pending" } | { status: "error"; code: string };
const validID = /^[A-Za-z][A-Za-z0-9._:-]{0,79}$/;
type QueryValueResult = Exclude<VariableResult,{status:"value"}> | {status:"value";value:string|boolean|number|DecimalValue|ResourceValue};
const failed = (code: string): QueryPlanResult => ({ status: "error", code });

export function queryView(plan: Api.PageQuery, base: QueryPlanResult, view: QueryView | undefined, info: EntityInfo | undefined, named: Api.Definition | undefined, contract: Contract): QueryPlanResult {
  named=plan.query?boundQueryDefinition(named,plan.query):named;
  if(base.status!=="value" || !view) return base;
  const query={...base.query};
  if(view.offset!==undefined){if(!Number.isInteger(view.offset)||view.offset<0||view.offset>contract.maxOffset)return failed("Query window offset exceeds its budget.");query.offset=view.offset;}
  if(view.search!==undefined){if(plan.search)return failed("This plan owns its search parameter.");if(new TextEncoder().encode(view.search).length>4096)return failed("Query search requires text.");query.search=view.search;}
  if(view.sort!==undefined){if(named?.query?.sort?.length)return failed("The named query owns its ordering.");if(view.sort.length<1||view.sort.length>contract.maxSort||view.sort.some((key)=>{const field=key.replace(/^-/,"");return !validID.test(field)||!["id","created","changed"].includes(field)&&!info?.fields.some((f)=>f.name===field&&!['references','tags','lines','json'].includes(f.type));}))return failed("Query sort field is unavailable.");query.sort=view.sort;}
  return {...base,query,signature:JSON.stringify([base.object,query])};
}

/** Build the finite read shape from member-visible descriptors and explicit
 * values. It emits the original RecordQuery, never source text or SQL. */
export function compileQueryPlan(plan: Api.PageQuery, variables: Record<string, Api.PageVariable>, values: Record<string, VariableResult>, info: EntityInfo | undefined, named: Api.Definition | undefined, contract: Contract, sections:Api.Section[]=[]): QueryPlanResult {
  if(plan.set)return failed("A set plan requires its source graph.");
  if (!info || info.type !== plan.object.name || plan.object.kind !== "object" || plan.limit < 1 || plan.limit > contract.maxLimit || !Number.isInteger(plan.limit) || !Number.isInteger(plan.offset ?? 0) || (plan.offset ?? 0) < 0 || (plan.offset ?? 0) > contract.maxOffset || (plan.conditions?.length ?? 0) > contract.maxConditions || (plan.sort?.length ?? 0) > contract.maxSort) return failed("Query plan is unavailable or exceeds its budget.");
  const field = (name: string) => ["id", "created", "changed"].includes(name) ? { name, type: name === "id" ? "text" : "datetime", ref: undefined } : info.fields.find((field) => field.name === name);
  const usesPlan = (id: string, seen = new Set<string>()): boolean => { if (seen.has(id)) return false; seen.add(id); const variable = variables[id]; return variable?.mode==="property"&&!!variable.source?.variable&&usesPlan(variable.source.variable,seen)||variable?.source?.kind === "plan" || !!variable?.source?.section && !!sections.find((s)=>s.id===variable.source!.section)?.collectionVariable || !!variable?.expression?.args.some((arg) => arg.variable && usesPlan(arg.variable, seen)); };
  const read = (binding: Api.PageValue): QueryValueResult => {
    if (!!binding.variable === (binding.literal !== undefined)) return { status: "error", code: "Query value needs one variable or literal." };
    if (!binding.variable) {
      const value=binding.literal;
      if (!isDecimal(value)&&!["string","boolean","number"].includes(typeof value) || typeof value === "number" && !Number.isFinite(value) || typeof value === "string" && new TextEncoder().encode(value).length>4096) return {status:"error",code:"Query value needs a bounded scalar literal."};
      return {status:"value",value:value as string|boolean|number|DecimalValue};
    }
    const variable = variables[binding.variable];
    if (!variable || !(variable.scope==="page"||variable.scope==="application"||variable.scope==="overlay"&&variable.owner===plan.owner&&!!plan.owner) || usesPlan(binding.variable)) return { status: "error", code: "Query parameter escapes its input scope." };
    return values[binding.variable] ?? { status: "empty" };
  };
  const scalar = (value: QueryValueResult & { status: "value" }) => typeof value.value === "object" ? value.value.kind === "decimal"?value.value:value.value.kind === "record" ? value.value.reference.id : undefined : value.value;
  const compatible = (name: string, value: QueryValueResult & { status: "value" }) => {
    const f = field(name), raw = value.value;
    if (!f || !validID.test(name)) return false;
    if (isDecimal(raw))return ["integer","decimal"].includes(f.type);
    if (typeof raw === "object") return raw.kind === "record" && f.type === "reference" && raw.reference.object === f.ref;
    return f.type === "boolean" ? typeof raw === "boolean" : ["integer", "decimal"].includes(f.type) ? typeof raw === "number" && Number.isFinite(raw)
      : ["text", "choice", "reference", "date", "datetime"].includes(f.type) && typeof raw === "string" && new TextEncoder().encode(raw).length <= 4096;
  };
  const domain: unknown[] = [];
  let sort = plan.sort ?? ["id"], limit = plan.limit;
  if (plan.query) {
    named=plan.query?boundQueryDefinition(named,plan.query):named;
    if (!named?.query || named.version !== plan.query.sourceVersion || named.ref.app !== plan.query.ref.app || named.ref.name !== plan.query.ref.name || named.ref.kind !== "query" || named.query.object !== plan.object.name) return failed("The named query version is unavailable.");
    const declaration = named.query;
    if (Array.isArray(declaration.domain)) domain.push(...declaration.domain);
    else if (declaration.domain !== undefined && declaration.domain !== null) return failed("The named query domain is unavailable.");
    if (domain.some((term) => Array.isArray(term) && !field(String(term[0])))) return failed("A named query field is unavailable.");
    if (declaration.by) {
      if (!plan.for) return failed("The named query needs its parent input.");
      const value = read(plan.for); if (value.status !== "value") return value;
      if (!compatible(declaration.by, value)) return failed("Query parameter type does not match its field.");
      if (!scalar(value)) return { status: "empty" };
      domain.push([declaration.by, "=", scalar(value)]);
    } else if (plan.for) return failed("The named query has no parent input.");
    if (declaration.sort?.length) sort = declaration.sort;
    limit = Math.min(limit, declaration.limit && declaration.limit > 0 ? declaration.limit : 200);
  } else if (plan.for) return failed("Query parent input requires a named query.");
  for (const condition of plan.conditions ?? []) {
    const value = read(condition.value); if (value.status !== "value") return value;
    const f = field(condition.field);
    if (!compatible(condition.field, value) || !(contract.operators as readonly string[]).includes(condition.op) || condition.op === "like" && !["text", "choice"].includes(f!.type) || ["boolean", "reference"].includes(f!.type) && !["=", "!="].includes(condition.op)) return failed("Query parameter type does not match its field.");
    domain.push([condition.field, condition.op, scalar(value)]);
  }
  if (sort.some((name) => !field(name.replace(/^-/, "")))) return failed("Query sort field is unavailable.");
  let search: string | undefined;
  if (plan.search) { const value = read(plan.search); if (value.status !== "value") return value; if (typeof value.value !== "string" || new TextEncoder().encode(value.value).length > 4096) return failed("Query search requires text."); search = value.value; }
  const query: RecordQuery = { domain, sort, offset: plan.offset ?? 0, limit, ...(search === undefined ? {} : { search }) };
  return { status: "value", object: info.type, query, signature: JSON.stringify([info.type, query]) };
}

/** Compile source predicates, never their downloaded windows. Each root owns
 * one immutable request signature and the original read/session lifecycle. */
export function compileQueryPlans(plans:Record<string,Api.PageQuery>,variables:Record<string,Api.PageVariable>,values:(owner?:string)=>Record<string,VariableResult>,entity:(type:string)=>EntityInfo|undefined,named:(plan:Api.PageQuery)=>Api.Definition|undefined,contract:Contract,sections:Api.Section[]=[],active:(owner?:string)=>boolean=()=>true,view?:(id:string,base:Extract<QueryPlanResult,{status:"value"}>)=>QueryView|undefined):ReadonlyArray<readonly [string,QueryPlanResult]> {
 const compile=(root:string):QueryPlanResult=>{
  let nodes=0;
  const target=plans[root];
  const visit=(id:string,depth:number,path:Set<string>):QueryPlanResult=>{
   const plan=plans[id];nodes++;
   if(!plan||path.has(id)||depth>contract.set.maxDepth||nodes>contract.set.maxNodes||plan.object.name!==target?.object.name||plan.object.app!==target?.object.app||plan.owner!==target?.owner)return failed("Set query sources are missing, cyclic or incompatible.");
   if(!active(plan.owner))return {status:"empty"};
   const own=compileQueryPlan({...plan,set:undefined},variables,values(plan.owner),entity(plan.object.name),named(plan),contract,sections);
   const sourceView=(result:QueryPlanResult)=>depth>0&&result.status==="value"?queryView(plan,result,view?.(id,result),entity(plan.object.name),named(plan),contract):result;
   if(!plan.set||own.status!=="value")return sourceView(own);
   if(!(contract.set.operations as readonly string[]).includes(plan.set.op)||plan.set.inputs.length!==2)return failed("Set query sources are missing, cyclic or incompatible.");
   path.add(id);const inputs=plan.set.inputs.map(input=>visit(input,depth+1,path));path.delete(id);
   const error=inputs.find(input=>input.status==="error"),pending=inputs.find(input=>input.status==="pending"),empty=inputs.find(input=>input.status==="empty");
   if(error||pending||empty)return (error??pending??empty)!;
   const predicates=inputs.map(input=>{const q=(input as Extract<QueryPlanResult,{status:"value"}>).query;return {domain:q.domain,search:q.search,set:q.set};});
   const query={...own.query,set:{op:plan.set.op,inputs:predicates}};
   return sourceView({...own,query,signature:JSON.stringify([own.object,query])});
  };
  const result=visit(root,0,new Set());
  if(result.status==="value"&&result.query.set){
   let size=0;const check=(p:Api.RecordSetPredicate):boolean=>{
    size++;if(size>contract.set.maxNodes||Array.isArray(p.domain)&&p.domain.length>contract.set.maxConditions||new TextEncoder().encode(p.search??"").length>contract.set.maxSearchBytes)return false;
    return !p.set||p.set.inputs.every(check);
   };
   if(!check({domain:result.query.domain,search:result.query.search,set:result.query.set})||new TextEncoder().encode(JSON.stringify(result.query)).length>contract.set.maxBytes)return failed("Set query exceeds its predicate budget.");
  }
  return result;
 };
 return Object.keys(plans).map(id=>[id,compile(id)] as const);
}
