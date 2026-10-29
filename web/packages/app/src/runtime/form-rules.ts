import type {Api} from "@platform/kernel";
import {evaluateRule,ruleDependencies,type RuleContext,type RuleSchema,type RuleValue} from "./rule-engine";

type LocalResult=RuleValue&{error?:string};
export function formRuleValues(info:Pick<Api.EntityInfo,"fields"|"localRules">,values:Record<string,unknown>,clock:{me:string;tenant?:string;now:string;today:string}):Record<string,LocalResult>{
 const kind=(field:Api.FieldInfo)=>["text","longtext"].includes(field.type)?"text":field.type;
 const schema:RuleSchema={refs:Object.fromEntries(info.fields.map(field=>[field.name,{kind:kind(field),...field.ref?{object:field.ref}:{}}]))};
 schema.refs["id"]={kind:"text"};schema.refs["caller.id"]={kind:"text"};schema.refs["caller.tenant"]={kind:"text"};
 const context:RuleContext={...clock,refs:{}};
 for(const [name,type]of Object.entries(schema.refs))context.refs[name]={kind:type.kind,status:!Object.hasOwn(values,name)||values[name]===undefined?"missing":values[name]===null?"null":"value",...values[name]!==undefined?{value:values[name]}:{}};
 context.refs["caller.id"]={kind:"text",status:"value",value:clock.me};
 context.refs["caller.tenant"]={kind:"text",status:clock.tenant===undefined?"missing":"value",...clock.tenant!==undefined?{value:clock.tenant}:{}};
 const result:Record<string,LocalResult>={},visited=new Set<string>();
 const evaluate=(name:string)=>{
  if(visited.has(name))return;
  visited.add(name);
  const expr=info.localRules![name]!;
  for(const dependency of ruleDependencies(expr))if(info.localRules?.[dependency])evaluate(dependency);
  try{
   const evaluated=evaluateRule(expr,schema,context);
   context.refs[name]=evaluated.value;
   result[name]=evaluated.value;
  }catch(error){
   const issue=(error as {issue?:{code:string}})?.issue;
   result[name]=issue&&["missing","null","withheld","incomplete","pending"].includes(issue.code)?{kind:expr.kind,status:issue.code}:{kind:expr.kind,status:"error",error:error instanceof Error?error.message:String(error)};
   context.refs[name]={kind:expr.kind,status:result[name]!.status==="error"?"withheld":result[name]!.status};
  }
 };
 for(const name of Object.keys(info.localRules??{}))evaluate(name);
 return result;
}
