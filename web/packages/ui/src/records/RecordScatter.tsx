import {useState} from "react";
import {Button} from "../primitives/button";
import {t} from "../i18n";
import type {Api} from "@platform/kernel";
import type {EntityInfo,EntityRecord} from "./Records";

/** Only the caller's authorized window is examined. Missing coordinates never become zero. */
export function scatterPoints(records:EntityRecord[],info:EntityInfo,fields:Api.PageRecordScatter,maxPoints=100) {
 const x=info.fields.find(f=>f.name===fields.xField),y=info.fields.find(f=>f.name===fields.yField),color=info.fields.find(f=>f.name===fields.colorField),label=info.fields.find(f=>f.name===fields.labelField);
 if(!x||!y||![x,y].every(f=>["integer","decimal"].includes(f.type))||!color||!["text","choice"].includes(color.type)||fields.labelField!=="id"&&(!label||!["text","longtext","choice","reference"].includes(label.type))||!Number.isInteger(maxPoints)||maxPoints<1||maxPoints>100||records.length>maxPoints)return;
 if(records.some(r=>typeof r.id!=="string"||!r.id||r.id.length>1024)||new Set(records.map(r=>r.id)).size!==records.length)return;
 const points:{record:EntityRecord;x:number;y:number;label:string;category?:string}[]=[],categories:(string|undefined)[]=[];let missing=0;
 for(const record of records){
  const rawX=record[x.name],rawY=record[y.name],rawColor=record[color.name],rawLabel=fields.labelField==="id"?record.id:record[fields.labelField];
  const valid=(raw:unknown,type:string)=>raw==null||typeof raw==="number"&&Number.isFinite(raw)&&(type!=="integer"||Number.isSafeInteger(raw));
  if(!valid(rawX,x.type)||!valid(rawY,y.type)||rawColor!=null&&typeof rawColor!=="string"||rawLabel!=null&&typeof rawLabel!=="string")return;
  if(rawX==null||rawY==null){missing++;continue;}
  const category=rawColor==null?undefined:rawColor as string;
  if(!categories.includes(category))categories.push(category);
  points.push({record,x:rawX as number,y:rawY as number,label:rawLabel==null?record.id:rawLabel as string,category});
 }
 const minX=Math.min(0,...points.map(p=>p.x)),maxX=Math.max(1,...points.map(p=>p.x)),minY=Math.min(0,...points.map(p=>p.y)),maxY=Math.max(1,...points.map(p=>p.y));
 if(!Number.isFinite(maxX-minX)||!Number.isFinite(maxY-minY))return;
 return {points,categories,missing,minX,maxX,minY,maxY,xTitle:x.title,yTitle:y.title};
}

/** Stable record IDs, rather than coordinates or display names, own selection. */
export function RecordScatter({records,info,fields,selected,enabled=true,onSelect}:{records:EntityRecord[];info:EntityInfo;fields:Api.PageRecordScatter;selected?:string;enabled?:boolean;onSelect:(record:EntityRecord)=>void}) {
 const [hover,setHover]=useState<string>(),model=scatterPoints(records,info,fields);
 if(!model)return <p role="alert">{t("Scatter fields, record identities or numeric values are unavailable or incompatible.")}</p>;
 const W=480,H=240,P=38,color=(category?:string)=>`var(${["--tone-info","--tone-success","--tone-warning","--tone-danger","--chart-5","--chart-6"][model.categories.indexOf(category)%6]})`,categoryLabel=(category?:string)=>category===undefined?t("No value"):category===""?t("Empty text"):category;
 const title=(point:typeof model.points[number])=>`${point.label} · ${point.record.id} · ${model.xTitle}: ${point.x} · ${model.yTitle}: ${point.y} · ${categoryLabel(point.category)}`,active=model.points.find(p=>p.record.id===hover)||model.points.find(p=>p.record.id===selected);
 return <div className="grid min-w-0 gap-2"><p role="status" className="text-xs text-muted">{t("{points} plotted records · {missing} records with missing coordinates",{points:model.points.length,missing:model.missing})}</p>{!model.points.length?<p className="text-sm text-muted">{t("No records with both coordinates in this window.")}</p>:<>
 <svg role="group" aria-label={t("Record scatter plot")} viewBox={`0 0 ${W} ${H}`} className="w-full text-xs">
 <path d={`M${P} ${P}V${H-P}H${W-P}`} fill="none" stroke="var(--border)"/>
 <text x={P} y={H-P+15} fill="var(--muted)">{model.minX}</text><text x={W-P} y={H-P+15} textAnchor="end" fill="var(--muted)">{model.maxX}</text><text x={P-5} y={P} textAnchor="end" fill="var(--muted)">{model.maxY}</text><text x={P-5} y={H-P} textAnchor="end" fill="var(--muted)">{model.minY}</text>
 <text x={W/2} y={H-3} textAnchor="middle" fill="var(--foreground)">{model.xTitle}</text><text x={P} y={P-12} fill="var(--foreground)">{model.yTitle}</text>
 {model.points.map(point=><circle key={point.record.id} role="button" tabIndex={enabled?0:-1} aria-disabled={!enabled} aria-pressed={selected===point.record.id} aria-label={title(point)} cx={P+(point.x-model.minX)/(model.maxX-model.minX)*(W-2*P)} cy={H-P-(point.y-model.minY)/(model.maxY-model.minY)*(H-2*P)} r={selected===point.record.id?6:4} fill={color(point.category)} stroke={selected===point.record.id?"var(--foreground)":"var(--surface)"} strokeWidth={selected===point.record.id?2:1} className="cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary" onMouseEnter={()=>setHover(point.record.id)} onMouseLeave={()=>setHover(undefined)} onFocus={()=>setHover(point.record.id)} onBlur={()=>setHover(undefined)} onClick={()=>{if(enabled)onSelect(point.record);}} onKeyDown={event=>{if(enabled&&(event.key==="Enter"||event.key===" ")){event.preventDefault();onSelect(point.record);}}}><title>{title(point)}</title></circle>)}
 </svg><ul aria-label={t("Scatter categories")} className="flex flex-wrap gap-x-3 gap-y-1 text-xs">{model.categories.map((category,index)=><li key={index} className="flex min-w-0 items-center gap-1"><span aria-hidden="true" className="h-2 w-2 shrink-0 rounded-full" style={{background:color(category)}}/><span className="break-all">{categoryLabel(category)}</span></li>)}</ul><p role="status" className="min-h-4 break-all text-xs text-muted">{active?title(active):t("Hover or focus a point; select a record to inspect it.")}</p>
 <details><summary className="cursor-pointer text-xs">{t("Individual records, including overlapping points")}</summary><ul className="mt-2 grid max-h-60 gap-1 overflow-auto">{model.points.map(point=><li key={point.record.id}><Button disabled={!enabled} variant={selected===point.record.id?"primary":"ghost"} aria-pressed={selected===point.record.id} className="h-auto w-full justify-start whitespace-normal break-all text-left text-xs" onClick={()=>onSelect(point.record)}>{title(point)}</Button></li>)}</ul></details></>}</div>;
}
