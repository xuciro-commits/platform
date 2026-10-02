import {useEffect} from "react";
import {pageUIManifest,type Api} from "@platform/kernel";
import {useHost,findDefinition} from "../index";
import type {ApplicationSession} from "./ApplicationSessions";
import {compileQueryPlans,queryView,boundQueryDefinition,planKey} from "./query-plans";
import type {VariableResult} from "./variables";
import type {QueryView} from "./Session";

/** Application and page owners use the same compiler and read cache. The
 * application session determines sharing and lifetime, never another reader. */
export function useApplicationQueries(session:ApplicationSession|undefined, values:Record<string,VariableResult>) {
 const {source,definitions}=useHost(),plans=session?.queries??{},variables=session?.variables??{},reads=session?.reads,snapshot=reads?.snapshot();
 const named=(plan:Api.PageQuery)=>plan.query?findDefinition(definitions,plan.query.ref):undefined;
 const base=compileQueryPlans(plans,variables,()=>values,type=>source.entity(type),named,pageUIManifest.runtime.query,[],()=>true,(id,result)=>snapshot?.views[planKey(id)]?.base===result.signature?snapshot.views[planKey(id)]:undefined);
 const compiled=base.map(([id,result])=>[id,queryView(plans[id]!,result,result.status==="value"&&snapshot?.views[planKey(id)]?.base===result.signature?snapshot.views[planKey(id)]:undefined,source.entity(plans[id]!.object.name),named(plans[id]!),pageUIManifest.runtime.query)] as const);
 const key=JSON.stringify(compiled);
 useEffect(()=>{
  if(!reads||session?.retired)return;
  reads.updateSource(source);
  for(const [id,result] of base)reads.reconcileQueryBase(planKey(id),result.status==="value"?result.signature:undefined);
  for(const [id,plan] of compiled){if(plan.status==="value")void reads.querySource(planKey(id)).list(plan.object,plan.query).catch(()=>{});else reads.clearQuery(planKey(id));}
 },[reads,session,key,snapshot?.views,source,source.scope,source.revision]);
 const resources:Record<string,VariableResult>={},windows:Record<string,NonNullable<Parameters<typeof import("@platform/ui").RecordList>[0]["window"]>>={},signatures:Record<string,string>={};
 for(const [id,v] of Object.entries(variables)) {
  if(v.mode!=="resource"||v.source?.kind!=="plan")continue;
  const queryID=v.source.query??"",result=compiled.find(([key])=>key===queryID)?.[1],plan=plans[queryID];
  if(!result||!plan){resources[id]={status:"error",code:"Query plan is unavailable or exceeds its budget."};continue;}
  if(result.status!=="value"){resources[id]=result;continue;}
  const state=snapshot?.queries[planKey(queryID)],window=state&&"value" in state?state.value:undefined, matches=window&&JSON.stringify([window.object,window.query])===result.signature;
  signatures[id]=result.signature;
  const failed=state?.status==="error"&&reads?.querySignature(planKey(queryID))===result.signature;
  resources[id]=failed?{status:"error",code:"Resource read failed"}:state?.status==="pending"||!matches?{status:"pending"}:window!.records.length?{status:"value",value:{kind:"object-set",window:window!}}:{status:"empty",value:{kind:"object-set",window:window!}};
  windows[id]={query:result.query,page:reads?.queryPage(planKey(queryID),result.signature),error:failed?"Resource read failed":undefined,searchLocked:!!plan.search,sortLocked:!!boundQueryDefinition(named(plan),plan.query)?.query?.sort?.length,maxOffset:pageUIManifest.runtime.query.maxOffset,
   onChange:(change:QueryView)=>{const initial=base.find(([key])=>key===queryID)?.[1];if(!reads||initial?.status!=="value")return;const next=queryView(plan,initial,{...(snapshot?.views[planKey(queryID)]?.base===initial.signature?snapshot.views[planKey(queryID)]:{}),...change},source.entity(plan.object.name),named(plan),pageUIManifest.runtime.query);if(next.status==="value")reads.setQueryView(planKey(queryID),initial.signature,change);},
  };
 }
 return {resources,windows,signatures,retry:(id:string)=>{const queryID=variables[id]?.source?.query;const result=base.find(([key])=>key===queryID)?.[1];if(queryID&&result?.status==="value")reads?.setQueryView(planKey(queryID),result.signature,{});}};
}
