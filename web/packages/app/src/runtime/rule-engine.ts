import type {Api} from "@platform/kernel";

export type RuleSchema={refs:Record<string,Api.Type>;sets?:Record<string,{element:Api.Type;policy:string}>};
export type RuleValue={kind:string;status:string;value?:unknown};
export type RuleContext={refs:Record<string,RuleValue>;sets?:Record<string,{items:{id:string;fields:Record<string,RuleValue>}[];complete:boolean;status?:string;withheld?:boolean}>;me?:string;now?:string;today?:string};
export type RuleIssue={code:string;at:string;message:string};
type RuleGlobal=typeof globalThis&{Go?:new()=>{importObject:WebAssembly.Imports;run:(instance:WebAssembly.Instance)=>Promise<void>};platformRuleEval?:(raw:string)=>string;platformRuleCompile?:(raw:string)=>string;platformRulePage?:(raw:string)=>string;platformRuleVersion?:string};
const runtime=globalThis as RuleGlobal;
let loading:Promise<void>|undefined;
export function initializeRules():Promise<void>{
 if(runtime.platformRuleVersion==="platform.expr.v1")return Promise.resolve();
 return loading??= (async()=>{
  const runtimeURL="/rules/wasm_exec.js";await import(/* @vite-ignore */ runtimeURL);
  if(!runtime.Go)throw new Error("Rule runtime is unavailable.");
  const go=new runtime.Go(),response=await fetch("/rules/engine.wasm");
  if(!response.ok)throw new Error("Rule module is unavailable.");
  const {instance}=await WebAssembly.instantiate(await response.arrayBuffer(),go.importObject);
  void go.run(instance);
  if(runtime.platformRuleVersion!=="platform.expr.v1")throw new Error("Rule module version differs.");
 })();
}
function call<T>(name:"platformRuleEval"|"platformRuleCompile"|"platformRulePage",data:unknown):T{
 const run=runtime[name];if(!run)throw new Error("Rule runtime is unavailable.");
 const result=JSON.parse(run(JSON.stringify(data)));
 if(result.error)throw Object.assign(new Error(`${result.error.at}: ${result.error.message}`),{issue:result.error});return result as T;
}
export function checkRule(expr:Api.Expr,schema:RuleSchema):void{call("platformRuleCompile",{expr,schema});}
export function evaluateRule(expr:Api.Expr,schema:RuleSchema,context:RuleContext):{value:RuleValue;reasons?:RuleIssue[]}{return call("platformRuleEval",{expr,schema,context});}
export function lowerPageRule(expression:Api.PageExpression,kind:string,schema:RuleSchema):Api.Expr{return call("platformRulePage",{expression,kind,schema});}
export const pageRuleKind=(kind:string)=>kind==="string"?"text":["record","object-set","filter","record-set"].includes(kind)?"reference":kind==="string-set"?"set":kind;
export function pageRuleSchema(variables:Record<string,Api.PageVariable>):RuleSchema{
 const refs:RuleSchema["refs"]={},sets:NonNullable<RuleSchema["sets"]>={};
 for(const [id,v] of Object.entries(variables)){refs[id]={kind:v.rule?.kind??(v.mode==="property"&&v.ruleType?v.ruleType.kind:pageRuleKind(v.type)),...(["record","filter","object-set","record-set"].includes(v.type)?{resource:v.type}:{}),...(v.type==="string-set"?{element:{kind:"text"}}:{}),...(v.ruleType?.fields?{fields:v.ruleType.fields}:{})};if(v.type==="object-set"&&v.ruleType)sets[id]={element:v.ruleType,policy:"visible"};}
 return {refs,sets};
}
export function ruleDependencies(expr:Api.Expr):string[]{return [...new Set([...(expr.ref&&!expr.ref.startsWith("item.")?[expr.ref]:[]),...(expr.agg?[expr.agg.source]:[]),...(expr.args??[]).flatMap(ruleDependencies),...(expr.in?ruleDependencies(expr.in):[])])];}

export function pageRuleFits(carrier:string,kind:string):boolean{return pageRuleKind(carrier)===kind||carrier==="decimal"&&kind==="integer"||carrier==="string"&&["date","datetime","choice","reference"].includes(kind);}
