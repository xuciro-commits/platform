import {useEffect,useRef,useState} from 'react';
import type {Api} from '@platform/kernel';
import type {EntityRecord,RecordQuery,RecordSource} from '@platform/ui';
import {useHost} from '../index';
import type {WidgetBindingContext} from '../widgets/bindings';
import type {QueryWindow} from '../widgets/QueryWindowFrame';
import {canonicalModelSection,materializeModelSection,modelEntity,modelFieldKey,modelWindowKey,modelWindowWidget,readModelPath} from './model-bindings';
import {inputSlot} from './PageNavigation';
import {recordResourceSlot,selectionSlot} from './resources';
import type {VariableResult} from './variables';

type State={identity:string;data?:Api.PageModelResult;error?:string};
/** This adapter calls original host reads and materializes widget ports. It
 * never evaluates a business predicate or treats a supplied record as fact. */
export function usePageModel(bound:WidgetBindingContext,source:RecordSource|undefined){
 const {client,definitions}=useHost(),model=bound.section.model,session=bound.session;
 const [view,setView]=useState<{base:string;query:RecordQuery}>({base:'',query:{}}),[round,refresh]=useState(0),[state,setState]=useState<State>();
 const records:Record<string,Api.PageModelRecord>={},values:Record<string,unknown>={};
 const required=new Set([model?.set?.recordVariable,model?.options?.set?.recordVariable,...Object.values(model?.options?.rule?.inputs??{})].filter((v):v is string=>!!v));
 let ready=!!source&&!!session;
 for(const id of required){const variable=bound.page.document?.variables?.[id];if(variable?.type==='record'){const slot=recordResourceSlot(bound.page,id)??inputSlot(id),record=session?.confirmedSelected(slot),reference=session?.snapshot().records[slot];if(!record||reference?.status!=='value')ready=false;else records[id]={object:reference.value.object,id:reference.value.id};}else {const value=bound.modelValues?.[id];if(value?.status!=='value')ready=false;else values[id]=value.value;}}
 if(model?.relation){const parentSlot=selectionSlot(bound.page,bound.section,true),record=session?.confirmedSelected(parentSlot);if(!record)ready=false;else records.$parent={object:model.relation.parentType,id:record.id};}
 if(model&&['detail','record-card'].includes(bound.section.widget)){const record=bound.selected??bound.confirmedRecord;if(!record)ready=false;else records.$record={object:model.type,id:record.id};}
 const canonicalPage={...bound.page,sections:bound.page.sections?.map(canonicalModelSection)},definition=definitions.find(d=>d.page?.name===bound.page.name&&d.page.object.name===bound.page.object.name);
 const active=!!model&&modelWindowWidget(bound.section)&&!!session&&!!source&&!bound.modelInterface&&bound.contextReadCurrent!==false;
	const producer=!['detail','record-card'].includes(bound.section.widget);
	if(bound.section.collectionVariable&&!model?.set&&!model?.options?.set&&(!bound.window||!bound.window.page||bound.window.error))ready=false;
 const base=JSON.stringify([source?.scope,bound.aggregateScope,model,records,values,bound.modelDraft?canonicalPage:definition?.contentVersion,bound.window?.query,bound.narrowed,ready]);
 const query:RecordQuery={...bound.window?.query,...(view.base===base?view.query:{}),offset:bound.section.widget==='record-picker'?0:view.base===base?view.query.offset??0:0,limit:bound.section.widget==='record-picker'?20:bound.window?.query.limit??25,sort:bound.section.widget==='record-picker'?['id']:view.base===base?view.query.sort??['id']:['id']};
 const signature=JSON.stringify([base,query]);
 const identity=JSON.stringify([signature,source?.revision,round]),current=useRef(identity);current.current=identity;
 const key=modelWindowKey(bound.section);
 useEffect(()=>{
  if(!active||!ready||!model){if(active&&producer)session?.adoptModelWindow(key,'');return;}
  let mounted=true;const valid=()=>mounted&&current.current===identity&&source?.scope===session?.snapshotScope();
  if(producer)session?.adoptModelWindow(key,signature);
  const request:Api.PageModelRead={page:definition?.ref??{app:'build',kind:'page',name:bound.page.name},...bound.modelDraft||!definition?{draft:canonicalPage}:{contentVersion:definition.contentVersion},section:bound.section.id??'',records,values,offset:query.offset,limit:query.limit,search:query.search,sort:query.sort,query:bound.window?.query??{domain:Object.entries(bound.narrowed[model.type]??{}).filter(([,v])=>v!==undefined&&v!=='').map(([name,value])=>[name,'=',value])}};
  void client.get<Api.PageModelResult>(`/v1/page-model/read\n${JSON.stringify(request)}`,true).then(async data=>{
   if(!valid())return;
   const paths=(model.fields??[]).filter(f=>(f.path?.length??0)>1&&data.fields.some(visible=>visible.name===modelFieldKey(f)));
   if(paths.length&&!data.hidden){data={...data,records:await Promise.all(data.records.map(async raw=>{const record={...raw} as EntityRecord;for(const field of paths){delete record[modelFieldKey(field)];record[modelFieldKey(field)]=await readModelPath(source!,model.type,record,field.path!,valid);}return record;}))};}
   if(!valid())return;if(producer)session?.adoptModelWindow(key,signature,{object:model.type,query,records:data.records as EntityRecord[],total:data.total,partial:data.partial});setState({identity,data});
  }).catch(()=>{if(valid()){if(producer)session?.adoptModelWindow(key,'',undefined,'Model binding could not be read. Check its declared source and permissions.');setState({identity,error:'Model binding could not be read. Check its declared source and permissions.'});}});
  return()=>{mounted=false;};
 },[active,ready,identity,client,session]);
 useEffect(()=>active&&model?source?.watchList?.(model.type,{},()=>refresh(n=>n+1)):undefined,[active,model?.type,source?.scope]);
 useEffect(()=>()=>{if(producer)session?.adoptModelWindow(key,'')},[session,key,producer]);
 const matching=state?.identity===identity?state:undefined,data=matching?.data;
 const payload=data?{kind:'object-set' as const,window:{object:model!.type,query,records:data.records.map(r=>({object:model!.type,id:String(r.id)})),total:data.total,complete:!data.partial&&data.records.length===data.total}}:undefined;
 const status:VariableResult=!ready?{status:'empty'}:matching?.error?{status:'error',code:matching.error}:!data?{status:'pending'}:data.records.length?{status:'value',value:payload!}:{status:'empty',value:payload};
 const window:QueryWindow={query:{...query,...data?.sort?.length?{sort:data.sort}:{}},sortLocked:data?.sortLocked,page:data?{records:data.records as EntityRecord[],total:data.total,partial:data.partial}:undefined,error:matching?.error,maxOffset:10000,onChange:patch=>setView({base,query:{...query,...patch}})};
 let recordSource=source,section=bound.section;
 if(model&&source){
  section=materializeModelSection(section,source.entity);
  // Interface plans keep their original source and permission scope, even
  // when their window search changes. Only active object-model reads adapt it.
  if(active){
   if(data?.labelField){if(section.widget==='record-picker')section={...section,recordPicker:{...section.recordPicker!,labelField:data.labelField}};if(section.widget==='record-list')section={...section,cardLabel:data.labelField};if(section.widget==='record-card')section={...section,recordCard:{...section.recordCard!,labelField:data.labelField}};}
   const info=modelEntity(source.entity(model.type),model,source.entity),fields=[...info?.fields??[]];for(const field of data?.fields??[]){const i=fields.findIndex(f=>f.name===field.name);if(i<0)fields.push(field);else fields[i]=field;}
   const original=source;
   recordSource={...source,scope:JSON.stringify([source.scope,signature]),entity:type=>type===model.type&&info?{...info,fields}:original.entity(type),get:async(type,id)=>{const value=await original.get(type,id),row=type===model.type?data?.records.find(r=>r.id===id):undefined;return row?{...value,record:{...value.record,...row} as EntityRecord}:value;}};
  }
 }
 return {active,ready,data,error:matching?.error,source:recordSource,bound:{...bound,section,...active?{window,collection:status,modelAccess:data?.access,recordSource,readSource:recordSource}:{}} as WidgetBindingContext,status};
}
