import type {Api,Entry} from "@platform/kernel";
import {Disclosure, Notice, language, t} from "@platform/ui";
import {useQuery} from "@tanstack/react-query";
import {useHost} from "../index";

export const refusalCategoryName=(category:string)=>t(({business:"Business refused",constraint:"Constraint refused",policy:"Access refused",technical:"Technical failure",input:"Input refused"} as Record<string,string>)[category]??"Request refused");
export const refusalLevelName=(level:string)=>t(({block:"Transaction blocked",flag:"Accepted with a flag",route:"Routed for correction"} as Record<string,string>)[level]??level);
export const refusalFactMessage=(fact:Api.BusinessRefusalFact)=>fact.translations?.[language()]||fact.message;

/** Compare the actual input values, independent of object property order. */
export function actionInputSignature(value: unknown): string {
 if(Array.isArray(value))return `[${value.map(actionInputSignature).join(",")}]`;
 if(value&&typeof value==="object")return `{${Object.entries(value).filter(([,item])=>item!==undefined).sort(([a],[b])=>a.localeCompare(b)).map(([key,item])=>`${JSON.stringify(key)}:${actionInputSignature(item)}`).join(",")}}`;
 return JSON.stringify(value)??"undefined";
}

/** Present the authority's result without turning a business refusal into a permission rule. */
export function ActionRefusal({entry,message,original}:{entry?:Entry;message:string;original?:Record<string,unknown>}) {
 const {client,source}=useHost(),target=entry?.submission.target,key=entry?.submission.idempotencyKey;
 const waiting=entry&&["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_UNKNOWN","SUBMISSION_STATE_SENDING"].includes(entry.state);
 const evidence=useQuery({queryKey:[client.connection.token,client.connection.tenant,source.scope,"refusal-answer",key],enabled:!!target?.type&&!!target.id&&!!key&&!waiting,queryFn:async()=>{
   const page=await client.get<Api.BusinessRefusalPage>(`/v1/business-refusals?type=${encodeURIComponent(target!.type!)}&id=${encodeURIComponent(target!.id!)}&key=${encodeURIComponent(key!)}&limit=1`,true);
   return page.records.find(item=>item.key===key)??null;
 },retry:false});
 const result=evidence.data,category=result?.category??(entry?.outcome==="ERROR_CODE_POLICY_DENIED"||entry?.outcome==="ERROR_CODE_NOT_AUTHORITY"?"policy":entry?.issues?.some(issue=>!!issue.constraint)?"constraint":"");
 const policy=category==="policy",constraint=category==="constraint",business=category==="business";
 const title=waiting?t("Awaiting confirmation"):refusalCategoryName(category),reason=result?.facts?.[0]?refusalFactMessage(result.facts[0]):message;
 return <div className="grid min-w-0 gap-2">
  <Notice title={title} message={reason} tone={waiting?"warning":business?"info":"danger"}/>
  {(result?.code||entry?.outcome)&&<p className="break-all font-mono text-xs text-muted">{result?.code||entry?.outcome}</p>}
  {policy&&<p className="text-xs text-muted">{t("Ask an administrator to review access before trying again.")}</p>}
  {constraint&&<p className="text-xs text-muted">{t("Correct the marked values before submitting a new intent.")}</p>}
  {business&&<p className="text-xs text-muted">{t("Review the business reason and correct the inputs or follow the declared correction path.")}</p>}
  {original&&<Disclosure summary={t("Original refused inputs")}><pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(original,null,2)}</pre><p className="text-xs text-muted">{t("Unchanged inputs repeat the original request. Changed inputs create a linked correction.")}</p></Disclosure>}
 </div>;
}

export function actionEntryInputs(entry:Entry,fallback:Record<string,unknown>):Record<string,unknown> {
 try { const raw=entry.submission.payload;if(!raw)return fallback;const parsed=JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(raw),c=>c.charCodeAt(0))));return parsed&&typeof parsed==="object"&&!Array.isArray(parsed)?parsed:fallback; } catch { return fallback; }
}
