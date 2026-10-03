import type {Api} from "@platform/kernel";
import {t} from "../i18n";
import {Tag,type Tone} from "../components/StatusTag";
import type {EntityInfo,EntityRecord} from "./Records";
import {timelineTime} from "./RecordTimeline";

const tones=["neutral","info","success","warning","danger"];
export function recordEventRows(records:EntityRecord[],info:EntityInfo,fields:Api.PageRecordEvents,maxEvents=30) {
 const time=info.fields.find(f=>f.name===fields.timeField),title=info.fields.find(f=>f.name===fields.titleField),severity=info.fields.find(f=>f.name===fields.severityField);
 if(!time||!["date","datetime"].includes(time.type)||fields.titleField!=="id"&&(!title||!["text","longtext","choice","reference"].includes(title.type))||severity?.type!=="choice"||!Number.isInteger(maxEvents)||maxEvents<1||maxEvents>30||fields.tones.length>32||new Set(fields.tones.map(t=>t.value)).size!==fields.tones.length||fields.tones.some(t=>!severity.choices?.includes(t.value)||!tones.includes(t.tone)))return;
 const rows=records.slice(0,maxEvents).map(record=>{const stamp=timelineTime(record[fields.timeField],time.type as "date"|"datetime"),value=String(record[fields.severityField]??""),tone=(fields.tones.find(t=>t.value===value)?.tone??"neutral") as Tone;return {id:record.id,title:fields.titleField==="id"?record.id:String(record[fields.titleField]??record.id),time:stamp===undefined?undefined:time.type==="date"?new Date(stamp).toISOString().slice(0,10):new Date(stamp).toISOString().replace(".000Z","Z"),severity:severity.choiceTitles?.[severity.choices?.indexOf(value)??-1]??value,tone};});
 if(rows.some(r=>!r.id)||new Set(rows.map(r=>r.id)).size!==rows.length)return;
 return rows;
}

/** The caller owns the authorized ordered window and business-time mapping. */
export function RecordEvents({records,info,fields,maxEvents=30}:{records:EntityRecord[];info:EntityInfo;fields:Api.PageRecordEvents;maxEvents?:number}) {
 const rows=recordEventRows(records,info,fields,maxEvents);
 if(!rows)return <p role="alert" className="text-sm text-danger">{t("Event fields or tone mappings are unavailable or incompatible.")}</p>;
 const color:Record<Tone,string>={neutral:"bg-muted",info:"bg-[var(--tone-info)]",success:"bg-[var(--tone-success)]",warning:"bg-[var(--tone-warning)]",danger:"bg-[var(--tone-danger)]"};
 return <div className="grid min-w-0 grid-cols-1 gap-2"><p role="status" className="text-xs text-muted">{t("Showing {events} of {records} events in this window.",{events:rows.length,records:records.length})}</p>{!rows.length?<p className="text-sm text-muted">{t("No events in this window.")}</p>:<ol className="ml-2 grid min-w-0 grid-cols-1 gap-3 border-l border-border pl-4">{rows.map(row=><li key={row.id} className="relative grid min-w-0 grid-cols-1 gap-1"><span aria-hidden className={`absolute -left-[21px] top-1.5 h-2 w-2 rounded-full ${color[row.tone]}`}/><div className="flex min-w-0 flex-wrap items-start justify-between gap-2"><span className="break-words text-sm font-medium">{row.title}</span><Tag label={row.severity||t("Severity unavailable")} tone={row.tone}/></div>{row.time?<time dateTime={row.time} className="text-xs tabular-nums text-muted">{row.time}</time>:<span role="status" className="text-xs text-warning">{t("Event time unavailable.")}</span>}</li>)}</ol>}</div>;
}
