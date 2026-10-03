import {useState} from "react";
import {pageUIManifest} from "@platform/kernel";
import {t} from "../i18n";
import type {EntityInfo,EntityRecord} from "./Records";

/** Original window order and identity determine X. Missing values break the line; they never become zero. */
export function recordSparklinePoints(records:EntityRecord[],info:EntityInfo,field:string,maxPoints:number=pageUIManifest.runtime.sparkline.maxPoints) {
 const descriptor=info.fields.find(f=>f.name===field);
 if(!descriptor||!["integer","decimal"].includes(descriptor.type)||!Number.isInteger(maxPoints)||maxPoints<1||maxPoints>pageUIManifest.runtime.sparkline.maxPoints||records.length>maxPoints||records.some(r=>typeof r.id!=="string"||!r.id||r.id.length>1024)||new Set(records.map(r=>r.id)).size!==records.length)return;
 const points=records.map((record,index)=>({id:record.id,index,value:record[field]==null?null:record[field]}));
 if(points.some(p=>p.value!==null&&(typeof p.value!=="number"||!Number.isFinite(p.value)||descriptor.type==="integer"&&!Number.isSafeInteger(p.value))))return;
 const values=points.flatMap(p=>typeof p.value==="number"?[p.value]:[]),min=Math.min(0,...values),max=Math.max(1,...values);if(!Number.isFinite(max-min))return;
 const coordinates=points.map(p=>({...p,value:p.value as number|null,x:5+(points.length<2?0:p.index/(points.length-1))*150,y:p.value===null?undefined:45-((p.value as number)-min)/(max-min)*40})),segments:typeof coordinates[]=[];let current:typeof coordinates=[];
 for(const point of coordinates){if(point.value===null){if(current.length)segments.push(current);current=[];}else current.push(point);}if(current.length)segments.push(current);
 return {points:coordinates,segments,missing:points.length-values.length,min,max,title:descriptor.title};
}

/** A caller-owned scalar with an independently authorized ordered record mini-line. */
export function RecordSparkline({valueText,label,suffix="",records,info,field}:{valueText?:string;label:string;suffix?:string;records?:EntityRecord[];info?:EntityInfo;field?:string}) {
 const [focus,setFocus]=useState<string>(),model=records&&info&&field?recordSparklinePoints(records,info,field):undefined;
 if(records&&(!model||!info||!field))return <p role="alert">{t("Sparkline record fields, identities or values are unavailable or incompatible.")}</p>;
 const active=model?.points.find(p=>p.id===focus),caption=(p:NonNullable<typeof model>["points"][number])=>`${p.id} · ${model?.title}: ${p.value===null?t("No value (missing)"):p.value}`;
 return <div className="grid min-w-0 gap-2">{label&&<span className="break-all text-xs font-medium">{label}</span>}<div className="flex min-w-0 flex-wrap items-center justify-between gap-3"><span className="break-all text-2xl font-semibold tabular-nums">{valueText===undefined?"—":`${valueText}${suffix}`}</span>{model&&model.points.some(p=>p.value!==null)&&<svg role="group" aria-label={t("Ordered record sparkline")} viewBox="0 0 160 50" className="h-12 w-40 shrink-0 text-primary">{model.segments.map((segment,index)=><polyline key={index} fill="none" stroke="currentColor" strokeWidth="1.5" points={segment.map(p=>`${p.x},${p.y}`).join(" ")}/>)}{model.points.filter(p=>p.value!==null).map(p=><circle key={p.id} tabIndex={0} role="img" aria-label={caption(p)} cx={p.x} cy={p.y} r="2" fill="currentColor" className="focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring" onMouseEnter={()=>setFocus(p.id)} onMouseLeave={()=>setFocus(undefined)} onFocus={()=>setFocus(p.id)} onBlur={()=>setFocus(undefined)}><title>{caption(p)}</title></circle>)}</svg>}</div>{valueText===undefined&&<p role="status" className="text-xs text-muted">{t("No scalar value.")}</p>}{model&&<><p role="status" className="text-xs text-muted">{t("{shown} ordered records · {missing} missing values · record order is not a time trend",{shown:model.points.length,missing:model.missing})}</p>{!model.points.some(p=>p.value!==null)&&<p role="status" className="text-xs text-muted">{t("No numeric values in this record window.")}</p>}<p role="status" className="min-h-4 break-all text-xs text-muted">{active?caption(active):t("Focus a point or inspect the individual record values.")}</p><details><summary className="cursor-pointer text-xs">{t("Individual sparkline records")}</summary><ul className="mt-1 grid max-h-40 gap-1 overflow-auto text-xs">{model.points.map(p=><li key={p.id} className="break-all">{caption(p)}</li>)}</ul></details></>}</div>;
}
