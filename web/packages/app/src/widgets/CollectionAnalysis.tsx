import {DerivedMean,Panel,RecordScatter,Select,t,type EntityInfo,type ChartSource} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {TermCountsRenderer} from "./TermCounts";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
import type {VariableResult} from "../runtime/variables";

type Props={config?:Api.PageCollectionAnalysis;object:string;info?:EntityInfo;window?:QueryWindow;source?:ChartSource;label:string;values:Record<string,VariableResult>;xVariable?:string;yVariable?:string;countVariable?:string;meanVariable?:string;onAxis?:(id:string,value:string)=>void;enabled?:boolean};
const scalar=(v?:VariableResult)=>v?.status==="value"&&typeof v.value==="string"?v.value:undefined;
/** Original complete statistics and authorized windows share the canonical owners. */
export function CollectionAnalysisRenderer({config,object,info,window,source,label,values,xVariable,yVariable,countVariable,meanVariable,onAxis,enabled=true}:Props) {
 if(!config||!info)return <Panel role="alert">{t("Collection analysis fields or configuration are unavailable.")}</Panel>;
 if(config.kind==="status-bars"||config.kind==="signed-counts")return <TermCountsRenderer object={object} window={window} field={config.groupField??""} info={info} source={source} label={label} bars={config.kind==="status-bars"} steps={config.kind==="signed-counts"?config.steps:undefined}/>;
 if(config.kind==="derived-mean"){
  const count=values[countVariable??""],mean=values[meanVariable??""];
  if(count?.status==="error"||mean?.status==="error")return <Panel role="alert">{t(count?.status==="error"?count.code:mean?.status==="error"?mean.code:"Original analysis values could not be read.")}</Panel>;
  if(!count||!mean||count.status==="pending"||mean.status==="pending")return <p role="status">{t("Loading original collection statistics…")}</p>;
  if(count.status!=="value"||typeof count.value!=="object"||count.value.kind!=="decimal"||mean.status!=="empty"&&(mean.status!=="value"||typeof mean.value!=="object"||mean.value.kind!=="number"))return <Panel role="alert">{t("Original derived mean values are unavailable or incompatible.")}</Panel>;
  return <DerivedMean count={count.value.value} mean={mean.status==="value"&&typeof mean.value==="object"&&mean.value.kind==="number"?mean.value.value:undefined} field={config.field??""} unit={config.unit??""}/>;
 }
 const fields=config.fields??[],x=scalar(values[xVariable??""]),y=scalar(values[yVariable??""]);
 if(config.kind!=="record-axes"||fields.length!==4||!x||!y||!fields.includes(x)||!fields.includes(y)||fields.some(name=>!info.fields.some(f=>f.name===name&&["integer","decimal"].includes(f.type))))return <Panel role="alert">{t("Original analysis axes are unavailable or incompatible.")}</Panel>;
 const axes=[{id:xVariable!,value:x,label:"Analysis X axis"},{id:yVariable!,value:y,label:"Analysis Y axis"}];
 return <div className="grid min-w-0 gap-2"><div className="grid min-w-0 grid-cols-2 gap-2">{axes.map(axis=><label key={axis.id} className="grid min-w-0 gap-1 text-xs">{t(axis.label)}<Select value={axis.value} disabled={!enabled||!onAxis} onChange={event=>{if(enabled&&fields.includes(event.target.value))onAxis?.(axis.id,event.target.value);}}>{fields.map(field=><option key={field} value={field}>{info.fields.find(f=>f.name===field)?.title??field}</option>)}</Select></label>)}</div><QueryWindowFrame paging={false} window={window} description={t("This analysis shows the first 80 authorized records in original ID order; changing axes only changes presentation.")}>{page=><RecordScatter records={page.records} info={info} fields={{xField:x,yField:y,labelField:"id"}} maxPoints={80}/>}</QueryWindowFrame></div>;
}
