import {useEffect} from "react";
import {pageUIManifest,type Api} from "@platform/kernel";
import type {PageSessionStore} from "./Session";
import type {QueryPlanResult} from "./query-plans";
import {planKey} from "./query-plans";
import {aggregateQuery,aggregateBudget} from "./aggregates";
import type {VariableResult} from "./variables";

/** Same read owner and lifecycle as windows, with complete-set scalar output. */
export function usePageAggregates(variables:Record<string,Api.PageVariable>,compiled:ReadonlyArray<readonly [string,QueryPlanResult]>,session?:PageSessionStore,document?:Pick<Api.PageDocument,"nodes"|"queries">) {
 const aggregates=Object.entries(variables).filter(([,v])=>v.mode==="aggregate"&&["count","aggregate","statistics"].includes(v.source?.kind??"")&&compiled.some(([id])=>id===v.source?.query));
 const requests=aggregates.map(([id,v])=>[id,v.source!.query!,compiled.find(([id])=>id===v.source!.query!)![1],v.source!.kind==="statistics"?["count",...(["min","avg","max","sum"] as const).map(op=>`${op}:${v.source!.measure}`)]:v.source!.kind==="aggregate"?v.source!.measure!:"count"] as const),reader=session?.readSource();
 const allowed=aggregateBudget(variables,document?.queries??{},document?.nodes??{},pageUIManifest.runtime.aggregate);
 const key=JSON.stringify(requests.map(([,id,result,measure])=>[id,result.status==="value"?{...result,query:aggregateQuery(result.query,measure)}:result]));
 useEffect(()=>{
  if(!session)return;
  for(const [,id,result,measure] of requests){if(allowed&&result.status==="value")void session.aggregateScalar(planKey(id),result.object,aggregateQuery(result.query,measure));else session.clearAggregate(planKey(id));}
 },[session,key,allowed,reader?.scope,reader?.revision,session?.snapshot().aggregates]);
 const resources:Record<string,VariableResult>={};
 for(const [id,query,result,measure] of requests)resources[id]=!allowed?{status:"error",code:"Count read budget exceeded."}:result.status==="value"?(session?.aggregateResource(planKey(query),result.object,aggregateQuery(result.query,measure))??{status:"empty"}):result;
 return resources;
}
