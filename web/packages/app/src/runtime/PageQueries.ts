import {activeQueryPlans} from "./query-inventory";
import {collectionInput, type CollectionInput} from "./collection-input";
import {usePageAggregates} from "./PageAggregates";
import { useEffect, useRef, useState } from "react";
import { pageUIManifest, type Api } from "@platform/kernel";
import { findDefinition, useHost } from "../index";
import type { PageSessionStore, PageSessionSnapshot, QueryView } from "./Session";
import type { VariableResult } from "./variables";
import { boundQueryDefinition, compileQueryPlans, queryView, variablePlan, planKey } from "./query-plans";

export function usePageQueries(page: Api.Page, values: Record<string, VariableResult>, session: PageSessionStore, snapshot: PageSessionSnapshot, overlays:Record<string,Record<string,VariableResult>>={}, editingOverlay?:string,itemOwner?:string,keepOnUnmount=false) {
  const reader=session.readSource();
  const { source, definitions } = useHost(), activePlans=page.document?activeQueryPlans(page.document,page.sections??[],pageUIManifest.runtime.query.inventoryUIProfile):new Set<string>(), plans = Object.fromEntries(Object.entries(page.document?.queries??{}).filter(([id,plan])=>activePlans.has(id)&&(plan.itemOwner??undefined)===itemOwner));
  const pickerTitle=(id:string)=>(page.sections??[]).find(s=>s.widget==="record-picker"&&page.document?.variables?.[s.collectionVariable??""]?.source?.query===id)?.recordPicker?.labelField;
  const picker=(id:string)=>(page.sections??[]).some(s=>s.widget==="record-picker"&&page.document?.variables?.[s.collectionVariable??""]?.source?.query===id);
  const avatars=(id:string)=>(page.sections??[]).some(s=>s.widget==="avatar-stack"&&[s.collectionVariable,s.avatar?.contextCollectionVariable].some(v=>v&&page.document?.variables?.[v]?.source?.query===id));
  const recordWork=(id:string)=>(page.sections??[]).some(s=>(s.widget==="action-table"||s.recordList?.layout==="tiles")&&page.document?.variables?.[s.collectionVariable??""]?.source?.query===id);
  const analysisAxes=(id:string)=>(page.sections??[]).some(s=>s.analysis?.kind==="record-axes"&&page.document?.variables?.[s.collectionVariable??""]?.source?.query===id);
  const resourceList=(id:string)=>(page.sections??[]).some(s=>s.widget==="resource-list"&&page.document?.variables?.[s.collectionVariable??""]?.source?.query===id);
  const observation=(id:string)=>(page.sections??[]).some(s=>s.widget==="observation"&&[s.observation?.kind==="availability"?undefined:s.collectionVariable,s.observationHistoryVariable,s.observationContextVariable].some(v=>v&&page.document?.variables?.[v]?.source?.query===id));
  const fixedObservation=(id:string)=>(page.sections??[]).some(s=>s.widget==="observation"&&[s.observation?.kind==="series"||s.observation?.kind==="statistics"&&!s.recordVariable?s.collectionVariable:undefined,s.observationHistoryVariable,s.observationContextVariable].some(v=>v&&page.document?.variables?.[v]?.source?.query===id));
  const view=(id:string,result:{signature:string})=>{const v=snapshot.views[planKey(id)];return !ranked(id)&&!avatars(id)&&!resourceList(id)&&!analysisAxes(id)&&!recordWork(id)&&!fixedObservation(id)&&v?.base===result.signature?picker(id)?{search:v.search}:observation(id)?{search:v.search,offset:v.offset}:v:undefined;};
  const ranked=(id:string)=>(page.sections??[]).some(s=>s.widget==="record-leaderboard"&&page.document?.variables?.[s.collectionVariable??""]?.source?.query===id);
  const active=(owner?:string)=>!owner||owner===editingOverlay||values[page.document?.overlays?.[owner]?.openVariable??""]?.status==="value"&&(values[page.document!.overlays![owner]!.openVariable] as {value:unknown}).value===true;
  const [builderState,setBuilderState]=useState<Record<string,{key:string;conditions:Api.PageQueryCondition[]}>>({});
  const builderIdentity=JSON.stringify([source.scope,reader.scope,page.document,page.sections]),builderEpoch=useRef({identity:builderIdentity,generation:0});
  if(builderEpoch.current.identity!==builderIdentity)builderEpoch.current={identity:builderIdentity,generation:builderEpoch.current.generation+1};
  const builderBases:Record<string,CollectionInput>={};
  const builderKey=(s:Api.Section,input:CollectionInput)=>JSON.stringify([builderEpoch.current.generation,source.scope,reader.scope,page.document,page.sections,s.id,s.collectionBuilder,s.collectionVariable,s.collectionOutputVariable,snapshot.scalars[page.document?.overlays?.[page.document?.variables?.[s.collectionOutputVariable??""]?.owner??""]?.openVariable??""]===true?session.overlayEpoch(page.document!.variables![s.collectionOutputVariable!]!.owner??""):0,input]);
  const virtual=Object.fromEntries((page.sections??[]).filter(s=>s.widget==="collection-builder").flatMap(s=>{const v=page.document?.variables?.[s.collectionVariable??""],q=v?.source?.kind==="plan"?plans[v.source.query??""]:undefined;return q?[[`builder/${s.id}`,{owner:q.owner,object:q.object,input:s.collectionOutputVariable,limit:1} as Api.PageQuery]]:[];}));
  const allPlans={...plans,...virtual};
  const builderSections=(page.sections??[]).filter(s=>s.widget!=="collection-builder"||!page.document?.unusedWidgets?.some(entry=>page.document?.nodes[entry.node]?.section===s.id));
  const allBase = compileQueryPlans(allPlans,page.document?.variables??{},owner=>itemOwner?values:owner?overlays[owner]??{}:values,type=>source.entity(type),plan=>plan.query?findDefinition(definitions,plan.query.ref):undefined,pageUIManifest.runtime.query,builderSections,active,(id,result)=>view(id,result),(s,input)=>{builderBases[s.id!]=input;const state=builderState[s.id!];return state?.key===builderKey(s,input)?state.conditions:[];});
  const base=allBase.filter(([id])=>!!plans[id]);
  const builders=Object.fromEntries((page.sections??[]).filter(s=>s.widget==="collection-builder").map(s=>{const result=allBase.find(([id])=>id===`builder/${s.id}`)?.[1],input=builderBases[s.id!];return [s.id!,{key:input?builderKey(s,input):"",result:result?.status==="value"&&result.collection?{status:"value" as const,value:result.collection}:result??{status:"error" as const,code:"Collection builder source is unavailable."},apply:(conditions:Api.PageQueryCondition[])=>{if(!input||!active(page.document?.variables?.[s.collectionOutputVariable??""]?.owner))return;setBuilderState(old=>({...old,[s.id!]:{key:builderKey(s,input),conditions}}));}}];}));
  const compiled = base.map(([id,result]) => [id,queryView(plans[id]!,result,result.status==="value"?view(id,result):undefined,source.entity(plans[id]!.object.name),plans[id]?.query?findDefinition(definitions,plans[id]!.query!.ref):undefined,pageUIManifest.runtime.query,pickerTitle(id))] as const);
  const aggregates=usePageAggregates(page.document?.variables??{},compiled,session,page.document);
  const [round, rerun] = useState(0);
  const requestKey = JSON.stringify(compiled);
  useEffect(() => {
    for(const [id,result] of base)session.reconcileQueryBase(planKey(id),result.status==="value"?result.signature:undefined);
    for (const [id, plan] of compiled) {
      if (plan.status === "value") void session.querySource(planKey(id)).list(plan.object, plan.query).catch(() => {});
      else session.clearQuery(planKey(id));
    }
  }, [session, requestKey, source.scope, source.revision, reader.scope,reader.revision, round]);
  const ids = JSON.stringify(Object.keys(plans));
  useEffect(() => () => {if(!keepOnUnmount)session.resetQueries(JSON.parse(ids).map(planKey))}, [session, ids,keepOnUnmount]);
  const resources: Record<string, VariableResult> = Object.fromEntries((page.sections??[]).filter(s=>s.widget==="collection-builder"&&s.collectionOutputVariable).map(s=>[s.collectionOutputVariable!,builders[s.id!]?.result as VariableResult])), signatures: Record<string, string> = {};
  for (const id of Object.keys(page.document?.variables ?? {})) {
    const queryID=variablePlan(page,id);if(queryID===undefined||!plans[queryID])continue;
    const plan = compiled.find(([id]) => id === queryID)?.[1];
    if (!plan) { resources[id] = { status: "error", code: "Query plan is unavailable or exceeds its budget." }; continue; }
    if (plan.status !== "value") { resources[id] = plan; continue; }
    signatures[queryID] = plan.signature;
    const state = snapshot.queries[planKey(queryID)];
    const window = state && "value" in state ? state.value : undefined;
    const matches = window && JSON.stringify([window.object, window.query]) === plan.signature;
    resources[id] = state?.status === "error" && session.querySignature(planKey(queryID)) === plan.signature ? { status: "error", code: "Resource read failed" } : state?.status === "pending" || !matches ? { status: "pending" }
      : window!.records.length ? { status: "value", value: { kind: "object-set", window: window! } } : { status: "empty", value: { kind: "object-set", window: window! } };
  }
  const collectionInputs:Record<string,VariableResult>={};
  const bindings=(id:string,seen=new Set<string>()):Api.AssetBinding[]=>{if(seen.has(id))return [];seen.add(id);const q=plans[id];if(!q)return [];const inherited=q.input?values[q.input]:undefined;return [...q.query?[q.query]:[],...q.set?.inputs.flatMap(source=>bindings(source,seen))??[],...inherited?.status==="value"&&inherited.value&&typeof inherited.value==="object"&&inherited.value.kind==="object-set-input"?inherited.value.bindings??[]:[]];};
  for(const variable of Object.keys(page.document?.variables??{})){
   const id=variablePlan(page,variable),plan=id?plans[id]:undefined,result=compiled.find(([key])=>key===id)?.[1];if(!plan||!result)continue;
   if(result.status!=="value"){collectionInputs[variable]=result;continue;}
   if(snapshot.queries[planKey(id!)]?.status==="error"&&session.querySignature(planKey(id!))===result.signature){collectionInputs[variable]={status:"error",code:"Resource read failed"};continue;}
   const original=bindings(id!),distinct=Array.from(new Map(original.map(b=>[JSON.stringify(b),b])).values()),sortLocked=!!result.sortLocked||!!(plan.query&&boundQueryDefinition(findDefinition(definitions,plan.query.ref),plan.query)?.query?.sort?.length);
   collectionInputs[variable]={status:"value",value:collectionInput(plan.object,result.query,distinct,sortLocked)};
  }
  const windows = Object.fromEntries(compiled.map(([id,result]) => [id,result.status==="value" ? {
    query:result.query,page:session.queryPage(planKey(id),result.signature),error:snapshot.queries[planKey(id)]?.status==="error"&&session.querySignature(planKey(id))===result.signature?"Resource read failed":undefined,
    inputSearch:picker(id)?snapshot.views[planKey(id)]?.search??"":undefined,searchLocked:ranked(id)||!picker(id)&&!!plans[id]?.search,sortLocked:!!result.sortLocked||observation(id)||ranked(id)||avatars(id)||resourceList(id)||analysisAxes(id)||recordWork(id)||picker(id)||!!(plans[id]?.query&&boundQueryDefinition(findDefinition(definitions,plans[id]!.query!.ref),plans[id]!.query)?.query?.sort?.length),maxOffset:fixedObservation(id)||ranked(id)||avatars(id)||resourceList(id)||analysisAxes(id)||recordWork(id)||picker(id)?0:pageUIManifest.runtime.query.maxOffset,
    onChange:(change:QueryView)=>{if(fixedObservation(id)||observation(id)&&change.sort!==undefined||ranked(id)||avatars(id)||resourceList(id)||analysisAxes(id)||recordWork(id)||picker(id)&&(change.sort!==undefined||change.offset!==undefined&&change.offset!==0))return;const original=base.find(([key])=>key===id)?.[1];if(original?.status!=="value")return;const next=queryView(plans[id]!,original,{...(snapshot.views[planKey(id)]?.base===original.signature?snapshot.views[planKey(id)]:{}),...change},source.entity(plans[id]!.object.name),plans[id]?.query?findDefinition(definitions,plans[id]!.query!.ref):undefined,pageUIManifest.runtime.query,pickerTitle(id));if(next.status==="value")session.setQueryView(planKey(id),original.signature,change);}
  }:undefined]));
  return { builders, collectionInputs:{...Object.fromEntries((page.sections??[]).filter(s=>s.widget==="collection-builder"&&s.collectionOutputVariable).map(s=>[s.collectionOutputVariable!,builders[s.id!]?.result as VariableResult])),...collectionInputs}, resources:{...resources,...aggregates}, signatures, windows, retry: (id: string) => { session.resetQueries([planKey(id)]); rerun((round) => round + 1); } };
}
