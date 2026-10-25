import {apiErrorMessage,type Api,type Entry} from "@platform/kernel";
import {Button, Disclosure, Rejection, language, t, useWorkspace} from "@platform/ui";
import {useQuery} from "@tanstack/react-query";
import {useState} from "react";
import {useHost, useOpenRecord} from "../index";
import {authzExplainRequest} from "./explain-request";

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
  <Rejection title={waiting?t("Waiting for the server"):refusalCategoryName(category)} message={reason} advice={advice}
   waiting={!!waiting} tone={business?"info":undefined}
   code={result?.code||entry?.outcome} requestKey={key}
   trail={result?.facts?.length?result.facts.map((fact,index)=><div key={index} className="grid min-w-0 gap-0.5 rounded border border-border p-2">
    <p className="break-all font-mono">{refusalLevelName(fact.level)}{fact.code?` · ${fact.code}`:""}</p>
    <p className="whitespace-pre-wrap break-words">{refusalFactMessage(fact)}</p>
   </div>):undefined}/>
  {waiting&&<div className="flex flex-wrap gap-2">
   <Button size="sm" variant="ghost" onClick={()=>open({view:"outbox"})}>{t("View outbox")}</Button>
   {target?.type&&target.id&&<Button size="sm" variant="ghost" onClick={()=>openRecord(`${target.type}/${target.id}`)}>{t("Open the original record")}</Button>}
  </div>}
  {policy&&!waiting&&<ExplainAccess entry={entry}/>}
  {original&&<Disclosure summary={t("Original refused inputs")}><pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(original,null,2)}</pre><p className="text-xs text-muted">{t("Retry original inputs repeats the saved request. Corrected inputs or a refreshed definition create a linked correction.")}</p></Disclosure>}
 </div>;
}

/** The ordered authorization verdict behind an access refusal, for the
 * administrators and auditors allowed to ask the engine why. The request is
 * the generated AuthzRequest — the refused submission's own action schema, its
 * principal, and the record it targeted — under this member's read scope. */
function ExplainAccess({ entry }: { entry?: Entry }) {
 const {client,source,me}=useHost();
 const [asking,setAsking]=useState(false);
 const target=entry?.submission.target,submission=entry?.submission;
 const action=submission?.schema?.name||""; // the refused action itself, not the authority carrying it
 const explain=useQuery({queryKey:[client.connection.token,client.connection.tenant,source.scope,"authz-explain",submission?.principalId,me.tenantId,action,target?.type,target?.id],enabled:asking&&!!target?.type&&!!action,retry:false,queryFn:async()=>{
   const request=authzExplainRequest(submission as Entry["submission"],me.principalId,me.tenantId);
   const asked=await client.call<Api.Verdict & {error?:{detail?:string}}>("POST","/v1/authz/explain",request);
   if(!asked.ok)throw Object.assign(new Error(apiErrorMessage(asked.body)??t("The authorization engine could not be asked.")),{refused:asked.status===401||asked.status===403});
   return asked.body;
 }});
 const refused=(explain.error as {refused?:boolean}|null)?.refused===true;
 return <div className="grid gap-1">
  <Button size="sm" variant="ghost" onClick={()=>setAsking(v=>!v)}>{asking?t("Hide the authorization explain"):t("Explain the authorization")}</Button>
  {asking&&(explain.isPending?<p className="text-xs text-muted">{t("Asking the authorization engine…")}</p>
   :explain.isError?(refused
     ?<p className="text-xs text-muted">{t("Only administrators and auditors may read the authorization explain.")}</p>
     :<div className="grid justify-items-start gap-1 text-xs"><p className="text-xs text-danger">{explain.error instanceof Error?explain.error.message:t("The authorization engine could not be asked.")}</p>
       <Button size="sm" variant="ghost" onClick={()=>void explain.refetch()}>{t("Retry")}</Button></div>)
   :explain.data&&<div className="grid min-w-0 gap-1 rounded border border-border p-2 text-xs">
    <p>{explain.data.allow?t("The engine would allow this now."):explain.data.reason||t("The engine refuses.")} <span className="font-mono text-muted">{explain.data.rule}</span></p>
    {explain.data.layers.map((layer,index)=><p key={index} className="break-words"><span className={layer.allowed?"text-success":"text-danger"}>{layer.allowed?"✓":"✗"}</span> <span className="font-medium">{layer.name}</span>{layer.role?` · ${layer.role}`:""}{layer.policy?` · ${layer.policy}`:""} — {layer.reason}</p>)}
   </div>)}
 </div>;
}

export function actionEntryInputs(entry:Entry,fallback:Record<string,unknown>):Record<string,unknown> {
 try { const raw=entry.submission.payload;if(!raw)return fallback;const parsed=JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(raw),c=>c.charCodeAt(0))));return parsed&&typeof parsed==="object"&&!Array.isArray(parsed)?parsed:fallback; } catch { return fallback; }
}
