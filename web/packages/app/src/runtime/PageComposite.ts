import {pageUIManifest} from '@platform/kernel/host-contract';
import {useEffect,useRef,useState} from 'react';
import type {Api} from '@platform/kernel';
import type {RecordSource} from '@platform/ui';
import {useHost} from '../index';
import type {WidgetBindingContext} from '../widgets/bindings';
import {canonicalModelSection,sectionModels,modelWindowKey,primaryModel} from './model-bindings';
import {inputSlot} from './PageNavigation';
import {recordResourceSlot} from './resources';

/** Composite sources use the original page model route and page-session
 * context. Retired scope answers never reappear after a new selection. */
export function usePageComposite(bound:WidgetBindingContext,source:RecordSource|undefined){
 const {client,definitions}=useHost(),session=bound.session,config=bound.section.composite;
 const [round,refresh]=useState(0),[state,setState]=useState<{identity:string;data?:Api.PageModelResult;error?:string}>();
 const records:Record<string,Api.PageModelRecord>={};
 let ready=!!source&&!!session;
 for(const model of sectionModels(bound.section)){
  const id=model.set?.recordVariable;if(!id)continue;
  const slot=recordResourceSlot(bound.page,id)??inputSlot(id),record=session?.confirmedSelected(slot),reference=session?.snapshot().records[slot];
  if(!record||reference?.status!=='value')ready=false;else records[id]={object:reference.value.object,id:reference.value.id};
 }
 const page={...bound.page,sections:bound.page.sections?.map(canonicalModelSection)},definition=definitions.find(d=>d.page?.name===page.name&&d.page.object.name===page.object.name);
 const key=modelWindowKey(bound.section);
 const active=!!config&&!!source&&!!session&&bound.contextReadCurrent!==false;
 const signature=JSON.stringify([source?.scope,bound.aggregateScope,config,records,bound.modelDraft?page:definition?.contentVersion,ready]);
 const identity=JSON.stringify([signature,source?.revision,round]),current=useRef(identity);current.current=identity;
 useEffect(()=>{
  if(!active||!ready){if(config)session?.adoptModelWindow(key,'');return;}
  let mounted=true;const valid=()=>mounted&&current.current===identity&&source?.scope===session?.snapshotScope();
  session?.adoptModelWindow(key,signature);
  const request:Api.PageModelRead={page:definition?.ref??{app:'build',kind:'page',name:page.name},...bound.modelDraft||!definition?{draft:page}:{contentVersion:definition.contentVersion},section:bound.section.id??'',records,offset:0,limit:pageUIManifest.runtime.composite.maxRecords};
  void client.get<Api.PageModelResult>(`/v1/page-model/read\n${JSON.stringify(request)}`,true).then(data=>{if(!valid())return;const primary=data.composite?.sources[bound.section.widget==='model-matrix'?'rows':'nodes'];session?.adoptModelWindow(key,signature,{object:primary?.object??primaryModel(bound.section)!.type,query:{},records:(primary?.records??[]) as import('@platform/ui').EntityRecord[],total:primary?.records.length??0,partial:data.partial});setState({identity,data});},()=>{if(valid()){const error='Composite model bindings could not be read. Check their collections and permissions.';session?.adoptModelWindow(key,'',undefined,error);setState({identity,error});}});
  return()=>{mounted=false;};
 },[active,ready,identity,client,session]);
 useEffect(()=>()=>{if(config)session?.adoptModelWindow(key,'')},[session,key,!!config]);
 const types=sectionModels(bound.section).map(m=>m.type).sort().join('|');
 useEffect(()=>{if(!active)return;const stops=types.split('|').map(type=>source?.watchList?.(type,{},()=>refresh(n=>n+1)));return()=>stops.forEach(stop=>stop?.());},[active,types,source?.scope]);
 const matching=state?.identity===identity?state:undefined;
 return {active,ready,data:matching?.data,error:matching?.error};
}
