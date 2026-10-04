import {useEffect,useMemo,useRef,useState} from "react";
import {ObservationTable,ObservationStatistics,ObservationTimeSeries,ObservationAvailability,Input,Panel,t,type EntityRecord,type ObservationStatisticsValue} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {useHost} from "../index";
import {createObservationStatisticsReader} from "../exploration/observation-reader";
import type {VariableResult} from "../runtime/variables";
import type {PageSessionStore} from "../runtime/Session";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";

type Props={page:Api.Page;section:Api.Section;window?:QueryWindow;history?:QueryWindow;context?:QueryWindow;asset?:EntityRecord;selectedRow?:string;onRow?:(record:EntityRecord)=>Promise<void>;values:Record<string,VariableResult>;onState?:(id:string,value:string)=>void;session?:PageSessionStore;scope:string;enabled?:boolean;readCurrent?:boolean;live:boolean};
const string=(v?:VariableResult)=>v?.status==="value"&&typeof v.value==="string"?v.value:undefined;
export function ObservationRenderer(props:Props){return <ObservationSurface key={props.scope} {...props}/>;}
function ObservationSurface({page,section,window,history,context,asset,selectedRow,onRow,values,onState,session,scope,enabled=true,readCurrent=true}:Props){
 const host=useHost(),c=section.observation,source=useMemo(()=>session?.readSource()??host.source,[session,host.source]),primary=section.object?.name||page.object.name,planID=page.document?.variables?.[c?.kind==="availability"?section.observationHistoryVariable??"":section.collectionVariable??""]?.source?.query,object=page.document?.queries?.[planID??""]?.object.name??primary,info=host.source.entity(object);
 const current=useRef({scope,active:true,readCurrent});current.current={scope,active:current.current.active,readCurrent};useEffect(()=>{current.current.active=true;return()=>{current.current.active=false;};},[]);
 const reader=useMemo(()=>createObservationStatisticsReader(source,()=>current.current.active&&current.current.scope===scope&&current.current.readCurrent),[source,scope]);
 const signal=string(values[section.observationSignalVariable??""]),threshold=string(values[section.observationThresholdVariable??""]),rows=string(values[section.observationRowsVariable??""]),validThreshold=typeof threshold==="string"&&/^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/.test(threshold)&&Number.isFinite(Number(threshold)),request=c?.kind==="statistics"&&window?.query&&signal&&validThreshold&&["1000","10000","100000"].includes(rows??"")?{object,query:window.query,window:{timeField:c.timeField,field:signal,rows:Number(rows),threshold:threshold!}}:undefined;
 let readScope:string|undefined;try{if(request)readScope=reader.scope(request);}catch{/* the original binding refusal is rendered below */}
 const [answer,setAnswer]=useState<{scope:string;statistics?:ObservationStatisticsValue;error?:string}>();
 useEffect(()=>{if(!request||!readScope||!readCurrent)return;let active=true;void reader.read(request).then(value=>{if(active&&value)setAnswer({scope:readScope,statistics:value.statistics});},()=>{if(active)setAnswer({scope:readScope,error:t("Original observation statistics could not be read.")});});return()=>{active=false;};},[reader,readScope,readCurrent]);
 if(!readCurrent)return <p role="status">{t("Loading original observations…")}</p>;
 if(!c||!info)return <Panel role="alert">{t("Original observation bindings are unavailable.")}</Panel>;
 const signals=c.signals.map(s=>({field:s.field,unit:s.unit,group:s.group})),name=section.title||t("Business observations"),readWindow=(w?:QueryWindow,key=scope)=>w?.page?{scope:key,records:w.page.records,total:w.page.total}:undefined;
 if(c.kind==="table")return <QueryWindowFrame window={window} description={t("Original business-time observations; this page is a window of the complete authorized set.")}>{data=><ObservationTable scope={scope} window={{scope,records:data.records,total:data.total}} info={info} signals={signals} metadata={{time:c.timeField,...c.metadata}} rowHeight={c.rowHeight} selectedId={selectedRow} enabled={enabled} onSelect={enabled&&onRow?record=>{void onRow(record);}:undefined}/>}</QueryWindowFrame>;
 if(c.kind==="series")return <ObservationTimeSeries scope={scope} window={readWindow(window)} info={info} timeField={c.timeField} signals={signals} label={name} error={window?.error}/>;
 if(c.kind==="availability"){
  const count=values[section.observationCountVariable??""],mean=values[section.observationMeanVariable??""],rootInfo=host.source.entity(primary);
  if(count?.status==="error"||mean?.status==="error"||history?.error)return <Panel role="alert">{t("Original availability or history could not be read.")}</Panel>;
  if(count?.status!=="value"||typeof count.value!=="object"||count.value.kind!=="decimal"||!mean||mean.status!=="empty"&&(mean.status!=="value"||typeof mean.value!=="object"||mean.value.kind!=="number")||!rootInfo)return <p role="status">{t("Loading original observations…")}</p>;
  return <ObservationAvailability scope={scope} window={readWindow(history)} info={info} timeField={c.timeField} signal={signals[0]!} slo={99} averageInfo={rootInfo} averageField={c.averageField} summary={{scope,field:c.averageField??"",count:count.value.value,average:mean.status==="value"&&typeof mean.value==="object"&&mean.value.kind==="number"?mean.value.value:undefined}} label={name}/>;
 }
 if(!signal||!rows||!validThreshold||!readScope)return <div className="grid gap-2"><Panel role="alert">{t("Original observation statistics bindings are unavailable or incompatible.")}</Panel><label className="grid gap-1 text-xs">{t("Alarm above")}<Input value={threshold??""} disabled={!enabled||!onState} onChange={event=>{if(enabled)onState?.(section.observationThresholdVariable??"",event.target.value);}}/></label></div>;
 const latest=c.assetObject?asset?context?.page?.records[0]:undefined:window?.page?.records[0],status=answer?.scope===readScope?answer:undefined;
 return <ObservationStatistics scope={readScope} info={info} timeField={c.timeField} signals={signals} signal={signal} threshold={Number(threshold)} windowRows={Number(rows)} windowOptions={[1000,10000,100000]} value={status?.statistics} error={window?.error??context?.error??status?.error} latest={latest?{scope:readScope,signal,record:latest}:undefined} history={c.assetObject?asset?readWindow(context,readScope):undefined:readWindow(window,readScope)} enabled={enabled} onChange={onState?value=>{if(!enabled)return;if(value.signal!==signal)onState(section.observationSignalVariable??"",value.signal);if(value.threshold!==Number(threshold))onState(section.observationThresholdVariable??"",String(value.threshold));if(String(value.windowRows)!==rows)onState(section.observationRowsVariable??"",String(value.windowRows));}:undefined}/>;
}
