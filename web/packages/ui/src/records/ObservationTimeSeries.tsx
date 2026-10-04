import {useState} from "react";
import {t} from "../i18n";
import type {EntityInfo} from "./Records";
import {observationTimeSeries,observationWindowCurrent,type ObservationSignal,type ObservationWindow} from "./observation-model";

export type ObservationTimeSeriesProps={scope:string;window?:ObservationWindow;info:EntityInfo;timeField:string;signals:ObservationSignal[];label:string;loading?:boolean;error?:string;readCurrent?:boolean;reference?:number};
/** Pure ordered business observations; no sampling, clock, generated samples or reader. */
export function ObservationTimeSeries(props:ObservationTimeSeriesProps){return <TimeSeriesSurface key={props.scope} {...props}/>;}
function TimeSeriesSurface({scope,window,info,timeField,signals,label,loading,error,readCurrent=true,reference}:ObservationTimeSeriesProps){
 const [focus,setFocus]=useState<string>();
 if(error)return <p role="alert">{t(error)}</p>;
 if(!readCurrent||loading||!window||window.scope!==scope)return <p role="status">{t("Loading original observations…")}</p>;
 if(!observationWindowCurrent(window,scope))return <p role="alert">{t("The original observation window is unavailable or incompatible.")}</p>;
 const records=window.records;
 const model=observationTimeSeries(records,info,timeField,signals,reference);
 if(!model)return <p role="alert">{t("Observation fields, timestamps, identities or numeric values are unavailable or incompatible.")}</p>;
 if(!records.length)return <p role="status">{t("No original observations in this window.")}</p>;
 const caption=(s:typeof model.series[number],p:typeof s.points[number])=>`${p.record.id} · ${p.time} · ${s.title}: ${p.value===null?t("No value (missing)"):`${p.value}${s.unit?" "+s.unit:""}`}`;
 const active=model.series.flatMap(s=>s.points.map(p=>({key:JSON.stringify([s.field,p.record.id]),text:caption(s,p)}))).find(p=>p.key===focus);
 return <div role="group" aria-label={label} className="grid min-w-0 gap-3"><p role="status" className="text-xs text-muted">{t("{records} original observations · {missing} without event time",{records:model.records,missing:model.missingTime})}</p>{model.series.map((s,index)=><div key={s.field} className="grid min-w-0 gap-1"><p className="break-words text-xs font-medium">{s.title}{s.unit?` (${s.unit})`:""} · {s.min===undefined?t("No value"):`${s.min}–${s.max}`} · {t("{missing} missing values",{missing:s.missing})}</p>{s.points.some(p=>p.value!==null)?<><svg role="group" aria-label={t("{signal} by original event time",{signal:s.title})} viewBox="0 0 460 160" preserveAspectRatio="none" className="h-32 w-full" style={{color:["var(--tone-info)","var(--tone-warning)","var(--tone-success)"][index]}}><line x1="35" x2="425" y1="130" y2="130" stroke="var(--border)"/>{s.referenceY!==undefined&&<line role="img" aria-label={t("SLO {value}",{value:reference!})} x1="35" x2="425" y1={s.referenceY} y2={s.referenceY} stroke="var(--tone-danger)" strokeDasharray="4 3"/>}{s.segments.map((segment,n)=><polyline key={n} fill="none" stroke="currentColor" strokeWidth="1.5" points={segment.map(p=>`${p.x},${p.y}`).join(" ")}/>)}{s.points.filter(p=>p.value!==null).map(p=>{const key=JSON.stringify([s.field,p.record.id]);return <circle key={p.record.id} role="img" tabIndex={0} aria-label={caption(s,p)} cx={p.x} cy={p.y} r="2.5" fill="currentColor" className="focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring" onMouseEnter={()=>setFocus(key)} onMouseLeave={()=>setFocus(undefined)} onFocus={()=>setFocus(key)} onBlur={()=>setFocus(undefined)}><title>{caption(s,p)}</title></circle>;})}</svg><div className="flex min-w-0 flex-wrap justify-between gap-1 text-[10px] text-muted"><span className="break-all">{model.from}</span><span className="break-all">{model.to}</span></div></>:<p role="status">{t("No numeric values in this record window.")}</p>}</div>)}<p role="status" className="min-h-4 break-all text-xs text-muted">{active?.text??t("Focus a point to inspect its original record, timestamp and value.")}</p><p className="text-xs text-muted">{t("Each signal keeps its own unit and scale. Missing values break the line; timestamps retain their original UTC offsets.")}</p></div>;
}

export type ObservationAvailabilityProps=Omit<ObservationTimeSeriesProps,"signals"|"reference">&{signal:ObservationSignal;slo:number;summary?:{scope:string;field:string;average?:number;count:string}};
export function ObservationAvailability({signal,slo,summary,...props}:ObservationAvailabilityProps){
 if(props.error)return <p role="alert">{t(props.error)}</p>;
 if(props.readCurrent===false||props.loading||!props.window||props.window.scope!==props.scope||!summary||summary.scope!==props.scope||summary.field!==signal.field)return <p role="status">{t("Loading original observations…")}</p>;
 const {average,count}=summary;
 if(!/^(0|[1-9]\d*)$/.test(count)||count.length>32||!Number.isFinite(slo)||average!==undefined&&!Number.isFinite(average)||count==="0"&&average!==undefined||!observationWindowCurrent(props.window,props.scope)||BigInt(count)!==BigInt(props.window.total)||!observationTimeSeries(props.window.records,props.info,props.timeField,[signal],slo))return <p role="alert">{t("The original availability summary is unavailable or incompatible.")}</p>;
 return <div className="grid min-w-0 gap-2"><p role="status" className="break-words text-xs">{t("Complete authorized average")} · {average===undefined?t("No value"):`${average.toLocaleString(undefined,{maximumFractionDigits:1})}${signal.unit?" "+signal.unit:""}`} · {t("{count} records",{count})} · {t("SLO {value}",{value:slo})}</p><ObservationTimeSeries {...props} signals={[signal]} reference={slo}/></div>;
}
