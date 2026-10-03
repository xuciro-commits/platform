import type {Api} from "@platform/kernel";
import {Tag,type Tone} from "../components/StatusTag";
import {t} from "../i18n";
import {timelineTime} from "./RecordTimeline";
import type {EntityInfo,EntityRecord} from "./Records";

const supportedTones=["neutral","info","success","warning","danger"];
export function ganttRange(start:string,end:string) {
 const a=timelineTime(start,"date"),b=timelineTime(end,"date");
 return a!==undefined&&b!==undefined&&!start.startsWith("0000")&&!end.startsWith("0000")&&a<b?{start:a,end:b}:undefined;
}
export function recordGanttRows(records:EntityRecord[],info:EntityInfo,fields:Api.PageRecordGantt,maxRows=20) {
 const start=info.fields.find(f=>f.name===fields.startField),end=info.fields.find(f=>f.name===fields.endField),title=info.fields.find(f=>f.name===fields.titleField),status=info.fields.find(f=>f.name===fields.statusField),range=ganttRange(fields.rangeStart,fields.rangeEnd);
 if(!range||!start||!end||!["date","datetime"].includes(start.type)||!["date","datetime"].includes(end.type)||fields.titleField!=="id"&&(!title||!["text","longtext","choice","reference"].includes(title.type))||status?.type!=="choice"||!Number.isInteger(maxRows)||maxRows<1||maxRows>20||fields.tones.length>32||new Set(fields.tones.map(t=>t.value)).size!==fields.tones.length||fields.tones.some(t=>!status.choices?.includes(t.value)||!supportedTones.includes(t.tone)))return;
 const rows=records.slice(0,maxRows).map(record=>{
  const a=timelineTime(record[fields.startField],start.type as "date"|"datetime"),b=timelineTime(record[fields.endField],end.type as "date"|"datetime"),invalid=a===undefined||b===undefined||b<a,value=String(record[fields.statusField]??""),tone=(fields.tones.find(t=>t.value===value)?.tone??"neutral") as Tone;
  const outside=!invalid&&((a!)>=range.end||(b!)<range.start||(b!)===range.start&&a!==b),left=invalid||outside?undefined:100*(Math.max(a!,range.start)-range.start)/(range.end-range.start),width=invalid||outside?undefined:100*(Math.min(b!,range.end)-Math.max(a!,range.start))/(range.end-range.start);
  const time=(stamp:number,kind:string)=>kind==="date"?new Date(stamp).toISOString().slice(0,10):new Date(stamp).toISOString().replace(".000Z","Z");
  return {record,title:fields.titleField==="id"?record.id:String(record[fields.titleField]??record.id),start:a===undefined?undefined:time(a,start.type),end:b===undefined?undefined:time(b,end.type),status:status.choiceTitles?.[status.choices?.indexOf(value)??-1]??value,tone,invalid,outside,left,width};
 });
 if(rows.some(r=>!r.record.id)||new Set(rows.map(r=>r.record.id)).size!==rows.length)return;
 return {rows,range};
}

/** Original ordered window in; a fixed viewport out. No scheduling writes. */
export function RecordGantt({records,info,fields,maxRows=20}:{records:EntityRecord[];info:EntityInfo;fields:Api.PageRecordGantt;maxRows?:number}) {
 const model=recordGanttRows(records,info,fields,maxRows);
 if(!model)return <p role="alert">{t("Gantt fields, range or tone mappings are unavailable or incompatible.")}</p>;
 const color:Record<Tone,string>={neutral:"bg-muted",info:"bg-[var(--tone-info)]",success:"bg-[var(--tone-success)]",warning:"bg-[var(--tone-warning)]",danger:"bg-[var(--tone-danger)]"};
 return <div className="grid min-w-0 grid-cols-1 gap-2"><p role="status" className="text-xs text-muted">{t("Showing {rows} of {records} tasks in this window.",{rows:model.rows.length,records:records.length})}</p><p className="text-xs text-muted">{t("Civil dates retain their day; datetimes use UTC. Display intervals exclude the end. The fixed range does not filter this window.")}</p>{!model.rows.length?<p className="text-sm text-muted">{t("No tasks in this window.")}</p>:<div className="overflow-auto"><table className="w-full min-w-[500px] text-xs"><thead><tr className="border-b border-border text-left"><th className="w-48 p-2">{t("Task")}</th><th className="p-2"><div className="flex justify-between gap-2"><time>{fields.rangeStart}</time><time>{fields.rangeEnd}</time></div></th></tr></thead><tbody>{model.rows.map(row=><tr key={row.record.id} className="border-b border-border"><th scope="row" className="p-2 text-left font-normal"><div className="break-words">{row.record.id} · {row.title}</div><Tag label={row.status||t("Status unavailable")} tone={row.tone}/></th><td className="p-2">{row.start&&row.end&&<p className="mb-1 text-[10px] tabular-nums text-muted">{row.start} → {row.end}</p>}{row.invalid?<span role="status" className="text-warning">{t("Task times are missing, invalid or reversed.")}</span>:row.outside?<span className="text-muted">{t("Task is outside the display range.")}</span>:<div className="relative h-4 overflow-hidden rounded bg-row-hover"><span role="img" aria-label={`${row.title} · ${row.start} → ${row.end} · ${row.status}`} className={`absolute top-0 h-full rounded ${color[row.tone]}`} style={{left:`${row.left}%`,width:row.width===0?"2px":`${row.width}%`}}/></div>}</td></tr>)}</tbody></table></div>}</div>;
}
