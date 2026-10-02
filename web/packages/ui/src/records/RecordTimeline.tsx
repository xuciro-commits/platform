import {Button} from "../primitives/button";
import {t} from "../i18n";
import type {EntityRecord} from "./Records";

export type TimelineFields={start:string;end?:string;label:string;group?:string;kind:"date"|"datetime"};
const day=86_400_000;
/** Civil dates never pass through the browser's local zone. Datetimes must
 * carry their own offset; Date.parse is used only after shape/range checks. */
export function timelineTime(value:unknown,kind:TimelineFields["kind"]):number|undefined {
 if(typeof value!=="string")return;
 const date=(text:string)=>{
  const m=/^(\d{4})-(\d{2})-(\d{2})$/.exec(text);if(!m)return;
  const y=Number(m[1]),month=Number(m[2]),d=Number(m[3]),stamp=new Date(0);stamp.setUTCFullYear(y,month-1,d);stamp.setUTCHours(0,0,0,0);
  if(stamp.getUTCFullYear()!==y||stamp.getUTCMonth()!==month-1||stamp.getUTCDate()!==d)return;
  return stamp.getTime();
 };
 if(kind==="date")return date(value);
 const m=/^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/.exec(value);
 if(!m||date(m[1]!)===undefined||Number(m[2])>23||Number(m[3])>59||Number(m[4])>59)return;
 if(m[5]!=="Z"&&(Number(m[5]!.slice(1,3))>23||Number(m[5]!.slice(4))>59))return;
 const stamp=Date.parse(value);return Number.isFinite(stamp)?stamp:undefined;
}
export function timelineRows(records:EntityRecord[],fields:TimelineFields) {
 const rows=records.flatMap(record=>{
  const start=timelineTime(record[fields.start],fields.kind),end=fields.end?timelineTime(record[fields.end],fields.kind):start;
  if(start===undefined||end===undefined||end<start)return [];
  const label=record[fields.label],group=fields.group?record[fields.group]:undefined;
  return [{record,start,end,label:typeof label==="string"&&label?label:record.id,group:typeof group==="string"&&group?group:""}];
 }).sort((a,b)=>a.group.localeCompare(b.group)||a.start-b.start||a.record.id.localeCompare(b.record.id));
 const min=rows.length?Math.min(...rows.map(r=>r.start)):0,max=rows.length?Math.max(...rows.map(r=>r.end)):0;
 return {rows,invalid:records.length-rows.length,min,max:max>min?max:min+(fields.kind==="date"?day:3_600_000)};
}

/** Authorized bounded records in, original selection out. This presentation
 * owns neither record reads nor scheduling/interval mutation. */
export function RecordTimeline({records,fields,selected,onSelect,label}: {
 records:EntityRecord[];fields:TimelineFields;selected?:string;onSelect:(record?:EntityRecord)=>void;label:string;
}) {
 if(records.length>200)return <p role="alert">{t("Timeline records exceed their display bound.")}</p>;
 const model=timelineRows(records,fields),span=model.max-model.min;
 const text=(stamp:number)=>fields.kind==="date"?new Date(stamp).toISOString().slice(0,10):new Date(stamp).toISOString().replace(".000Z","Z");
 const groups=[...new Set(model.rows.map(r=>r.group))];
 return <div role="region" aria-label={label} className="grid min-w-0 grid-cols-1 gap-3">
 <p className="text-xs text-muted">{t(fields.kind==="date"?"Civil dates · intervals exclude the end date":"UTC times · intervals exclude the end instant")}</p>
 {model.invalid>0&&<p role="status" className="text-xs text-warning">{t("{count} records have missing, invalid or reversed times.",{count:model.invalid})}</p>}
 {!model.rows.length?<p className="text-sm text-muted">{t("No dated records in this window.")}</p>:<>
 <div className="flex min-w-0 flex-wrap justify-between gap-2 border-b border-border pb-2 text-[10px] tabular-nums text-muted"><time>{text(model.min)}</time><time>{text(model.max)}</time></div>
 <div className="max-h-[28rem] overflow-auto">{groups.map(group=><div key={group} className="mb-3 grid gap-2">
 {fields.group&&<h3 className="text-xs font-semibold">{group||t("Unassigned resource")}</h3>}
 {model.rows.filter(r=>r.group===group).map(row=><div key={row.record.id} className="grid min-w-0 grid-cols-1 gap-1 rounded border border-border p-2">
 <div className="flex min-w-0 flex-wrap items-baseline justify-between gap-1"><span className="truncate text-xs font-medium">{row.label}</span><span className="text-[10px] tabular-nums text-muted">{text(row.start)}{row.end!==row.start?` → ${text(row.end)}`:""}</span></div>
 <div className="relative h-7 rounded bg-row-hover"><Button size="sm" variant={selected===row.record.id?"primary":"ghost"} aria-label={row.label} aria-pressed={selected===row.record.id} title={`${row.label} · ${text(row.start)}${row.end!==row.start?` → ${text(row.end)}`:""}`} className="absolute top-0 h-7 min-w-5 max-w-full overflow-hidden border border-primary bg-primary/15 px-1 text-primary" style={{left:`${Math.min(98,100*(row.start-model.min)/span)}%`,width:`${Math.max(2,100*(row.end-row.start)/span)}%`}} onClick={()=>onSelect(selected===row.record.id?undefined:row.record)}><span className="truncate text-[10px]">{row.end===row.start?"●":row.label}</span></Button></div>
 </div>)}
 </div>)}</div>
 </>}
 </div>;
}
