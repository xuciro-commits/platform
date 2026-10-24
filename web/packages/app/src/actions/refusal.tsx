import type {Api,Entry} from "@platform/kernel";
import {Button, Disclosure, Rejection, language, t, useWorkspace} from "@platform/ui";
import {useQuery} from "@tanstack/react-query";
import {useHost, useOpenRecord} from "../index";

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
 const {client,source}=useHost(),{open}=useWorkspace(),openRecord=useOpenRecord(),target=entry?.submission.target,key=entry?.submission.idempotencyKey;
 const waiting=entry&&["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_UNKNOWN","SUBMISSION_STATE_SENDING"].includes(entry.state);
 const evidence=useQuery({queryKey:[client.connection.token,client.connection.tenant,source.scope,"refusal-answer",key],enabled:!!target?.type&&!!target.id&&!!key&&!waiting,queryFn:async()=>{
   const page=await client.get<Api.BusinessRefusalPage>(`/v1/business-refusals?type=${encodeURIComponent(target!.type!)}&id=${encodeURIComponent(target!.id!)}&key=${encodeURIComponent(key!)}&limit=1`,true);
   return page.records.find(item=>item.key===key)??null;
 },retry:false});
 const result=evidence.data,category=result?.category??(entry?.outcome==="ERROR_CODE_POLICY_DENIED"||entry?.outcome==="ERROR_CODE_NOT_AUTHORITY"?"policy":entry?.issues?.some(issue=>!!issue.constraint)?"constraint":"");
 const policy=category==="policy",constraint=category==="constraint",business=category==="business";
 const reason=result?.facts?.[0]?refusalFactMessage(result.facts[0]):message;
 const advice=policy?t("Ask an administrator to review access before trying again."):constraint?t("Correct the marked values before submitting a new intent."):business?t("Review the business reason and correct the inputs or follow the declared correction path."):undefined;
 return <div className="grid min-w-0 gap-2">
  <Rejection title={waiting?t("Awaiting confirmation"):refusalCategoryName(category)} message={reason} advice={advice}
   waiting={!!waiting} tone={business?"info":undefined}
   code={result?.code||entry?.outcome} requestKey={key}
   trail={result?.facts?.length?result.facts.map((fact,index)=><div key={index} className="grid min-w-0 gap-0.5 rounded border border-border p-2">
    <p className="break-all font-mono">{refusalLevelName(fact.level)}{fact.code?` · ${fact.code}`:""}</p>
    <p className="whitespace-pre-wrap break-words">{refusalFactMessage(fact)}</p>
   </div>):undefined}/>
  {waiting&&<div className="flex flex-wrap gap-2">
   <Button size="sm" variant="ghost" onClick={()=>open({view:"requests"})}>{t("View request status")}</Button>
   {target?.type&&target.id&&<Button size="sm" variant="ghost" onClick={()=>openRecord(`${target.type}/${target.id}`)}>{t("Open the original record")}</Button>}
  </div>}
  {original&&<Disclosure summary={t("Original refused inputs")}><pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(original,null,2)}</pre><p className="text-xs text-muted">{t("Retry original inputs repeats the saved request. Corrected inputs or a refreshed definition create a linked correction.")}</p></Disclosure>}
 </div>;
}

export function actionEntryInputs(entry:Entry,fallback:Record<string,unknown>):Record<string,unknown> {
 try { const raw=entry.submission.payload;if(!raw)return fallback;const parsed=JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(raw),c=>c.charCodeAt(0))));return parsed&&typeof parsed==="object"&&!Array.isArray(parsed)?parsed:fallback; } catch { return fallback; }
}
