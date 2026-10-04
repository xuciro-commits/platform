import {timestampParts} from "../components/date";
import type {EntityInfo,EntityRecord} from "./Records";

export type ObservationSignal={field:string;unit:string;group?:string};
export type ObservationMetadata={time:string;plant?:string;device?:string;asset?:string;status?:string};
export type ObservationWindow={scope:string;records:readonly EntityRecord[];total:number};
export function observationWindowCurrent(window:ObservationWindow,scope:string){return window.scope===scope&&Number.isSafeInteger(window.total)&&window.total>=window.records.length;}
export function observationFields(info:EntityInfo,signals:readonly ObservationSignal[],metadata:ObservationMetadata) {
 const fields=Object.values(metadata).filter((v):v is string=>!!v);
 if(!metadata.time||info.fields.find(f=>f.name===metadata.time)?.type!=="datetime"||signals.length<1||signals.length>100||new Set(signals.map(s=>s.field)).size!==signals.length||new Set(fields).size!==fields.length||signals.some(s=>fields.includes(s.field)||s.unit.length>64||(s.group?.length??0)>128||!info.fields.some(f=>f.name===s.field&&["integer","decimal"].includes(f.type)))||fields.some(name=>name!==metadata.time&&!info.fields.some(f=>f.name===name&&["text","longtext","choice","reference"].includes(f.type))))return;
 return {fields,signals};
}
export function observationIdentities(records:readonly EntityRecord[],maxRecords:number) {
 return records.length<=maxRecords&&records.every(r=>typeof r.id==="string"&&r.id.length>0&&r.id.length<=1024&&Number.isSafeInteger(r.revision)&&r.revision>0)&&new Set(records.map(r=>r.id)).size===records.length;
}
export function observationTableRecords(records:readonly EntityRecord[],info:EntityInfo,signals:readonly ObservationSignal[],metadata:ObservationMetadata){
 if(!observationFields(info,signals,metadata)||!observationIdentities(records,100000))return false;
 const integers=new Set(info.fields.filter(f=>f.type==="integer").map(f=>f.name));
 return records.every(r=>Object.values(metadata).every(field=>r[field]==null||field===metadata.time? r[field]==null||observationInstant(r[field])!==undefined:typeof r[field]==="string")&&signals.every(s=>r[s.field]==null||typeof r[s.field]==="number"&&Number.isFinite(r[s.field])&&(!integers.has(s.field)||Number.isSafeInteger(r[s.field]))));
}
/** UTC geometry retains nanosecond order, original offsets and literal timestamps. */
export function observationInstant(value:unknown):bigint|undefined {
 if(typeof value!=="string")return;
 const parts=timestampParts(value),ms=Date.parse(value);
 if(!parts||!Number.isSafeInteger(ms))return;
 return BigInt(ms)*1_000_000n+BigInt(parts.fraction.padEnd(9,"0").slice(3)||"0");
}
export function observationTimeSeries(records:readonly EntityRecord[],info:EntityInfo,time:string,signals:readonly ObservationSignal[],reference?:number) {
 if(!observationFields(info,signals,{time})||signals.length>3||!observationIdentities(records,500)||reference!==undefined&&!Number.isFinite(reference))return;
 const points: {record:EntityRecord;instant:bigint;time:string;values:(number|null)[]}[]=[];let missingTime=0;
 for(const record of records){
  if(record[time]==null){missingTime++;continue;}
  const instant=observationInstant(record[time]);if(instant===undefined)return;
  const values=signals.map(s=>record[s.field]==null?null:record[s.field]);
  if(values.some((v,i)=>v!==null&&(typeof v!=="number"||!Number.isFinite(v)||info.fields.find(f=>f.name===signals[i]!.field)?.type==="integer"&&!Number.isSafeInteger(v))))return;
  points.push({record,instant,time:record[time] as string,values:values as(number|null)[]});
 }
 points.sort((a,b)=>a.instant<b.instant?-1:a.instant>b.instant?1:a.record.id<b.record.id?-1:a.record.id>b.record.id?1:0);
 const from=points[0]?.instant,to=points.at(-1)?.instant,span=from!==undefined&&to!==undefined?to-from:0n;
 const series=signals.map((signal,index)=>{
  const numbers=points.flatMap(p=>p.values[index]===null?[]:[p.values[index]!]),extent=[...numbers,...(reference!==undefined&&index===0?[reference]:[])],min=extent.length?Math.min(...extent):undefined,max=extent.length?Math.max(...extent):undefined;
  if(min!==undefined&&max!==undefined&&!Number.isFinite(max-min))return;
  const y=(v:number)=>min===max?70:130-(v-min!)/(max!-min!)*112;
  const coordinates=points.map(p=>({...p,value:p.values[index]!,x:span===0n?230:35+Number(p.instant-from!)/Number(span)*390,y:p.values[index]===null?undefined:y(p.values[index]!)})),segments:typeof coordinates[]=[];let part:typeof coordinates=[];
  for(const p of coordinates){if(p.value===null){if(part.length)segments.push(part);part=[];}else part.push(p);}if(part.length)segments.push(part);
  return {...signal,min,max,points:coordinates,segments,missing:points.length-numbers.length,referenceY:reference===undefined?undefined:y(reference),title:info.fields.find(f=>f.name===signal.field)!.title};
 });
 if(series.some(s=>!s))return;
 return {series:series as NonNullable<typeof series[number]>[],missingTime,from:points[0]?.time,to:points.at(-1)?.time,records:records.length};
}

export type ObservationStatisticsValue={scope:string;signal:string;threshold:number;windowRows:number;count:number;valid:number;above:number;mean?:number;min?:number;max?:number};
export function observationThresholdDraft(text:string):number|undefined {
 if(!/^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/.test(text.trim()))return;
 const n=Number(text);return Number.isFinite(n)?n===0?0:n:undefined;
}
export function observationStatisticsCurrent(value:ObservationStatisticsValue,scope:string,signal:string,threshold:number,windowRows:number) {
 if(value.scope!==scope||value.signal!==signal||value.threshold!==threshold||value.windowRows!==windowRows)return "stale";
 if(!Number.isSafeInteger(windowRows)||windowRows<1||windowRows>100000||!Number.isFinite(threshold)||![value.count,value.valid,value.above].every(v=>Number.isSafeInteger(v)&&v>=0)||value.count>windowRows||value.valid>value.count||value.above>value.valid)return "invalid";
 const numbers=[value.min,value.mean,value.max];
 if(value.valid===0)return value.above===0&&numbers.every(v=>v===undefined)?"current":"invalid";
 if(numbers.some(v=>typeof v!=="number"||!Number.isFinite(v))||value.min!>value.mean!||value.mean!>value.max!)return "invalid";
 return "current";
}
