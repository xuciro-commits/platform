import {useEffect} from "react";
import {pageUIManifest,type Api} from "@platform/kernel";
import type {PageSessionStore} from "./Session";
import type {QueryPlanResult} from "./query-plans";
import {planKey} from "./query-plans";
import {countQuery,countBudget} from "./counts";
import type {VariableResult} from "./variables";

/** Same read owner and lifecycle as windows, with complete-set scalar output. */
export function usePageCounts(variables:Record<string,Api.PageVariable>,compiled:ReadonlyArray<readonly [string,QueryPlanResult]>,session?:PageSessionStore,document?:Pick<Api.PageDocument,"nodes"|"queries">) {
 const counts=Object.entries(variables).filter(([,v])=>v.mode==="aggregate"&&v.source?.kind==="count"&&compiled.some(([id])=>id===v.source?.query));
 const requests=counts.map(([id,v])=>[id,v.source!.query!,compiled.find(([id])=>id===v.source!.query!)![1]] as const),reader=session?.readSource();
 const allowed=countBudget(variables,document?.queries??{},document?.nodes??{},pageUIManifest.runtime.aggregate);
 const key=JSON.stringify(requests.map(([,id,result])=>[id,result.status==="value"?{...result,query:countQuery(result.query)}:result]));
 useEffect(()=>{
  if(!session)return;
  for(const [,id,result] of requests){if(allowed&&result.status==="value")void session.count(planKey(id),result.object,countQuery(result.query));else session.clearCount(planKey(id));}
 },[session,key,allowed,reader?.scope,reader?.revision,session?.snapshot().counts]);
 const resources:Record<string,VariableResult>={};
 for(const [id,query,result] of requests)resources[id]=!allowed?{status:"error",code:"Count read budget exceeded."}:result.status==="value"?(session?.countResource(planKey(query),result.object,countQuery(result.query))??{status:"empty"}):result;
 return resources;
}
