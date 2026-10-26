import {Button} from "../primitives/button";
import {t} from "../i18n";
import type {Api} from "@platform/kernel";
import type {EntityRecord,EntityInfo} from "./Records";
export function leaderboardRows(records:EntityRecord[],info:EntityInfo,fields:Api.PageLeaderboard) {
 const numeric=info.fields.find(f=>f.name===fields.valueField),label=info.fields.find(f=>f.name===fields.labelField);
 if(!numeric||!["integer","decimal"].includes(numeric.type)||fields.labelField!=="id"&&(!label||!["text","longtext","choice","reference"].includes(label.type))||!Number.isInteger(fields.limit)||fields.limit<1||fields.limit>32||records.length>fields.limit)return;
 const rows=records.map((record,index)=>{const raw=record[fields.valueField],value=typeof raw==="number"?raw:typeof raw==="string"&&/^-?(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(raw)?Number(raw):NaN;return {record,rank:index+1,value,label:fields.labelField==="id"?record.id:String(record[fields.labelField]??record.id)};});
 if(rows.some(row=>!row.record.id||!Number.isFinite(row.value))||new Set(rows.map(r=>r.record.id)).size!==rows.length)return;
 return {rows,fieldTitle:numeric.title,max:Math.max(1,...rows.map(r=>Math.abs(r.value)))};
}
/** The original host orders the bounded Top-N; this component never ranks a partial unsorted window. */
export function RecordLeaderboard({records,info,fields,total,selected,onSelect}:{records:EntityRecord[];info:EntityInfo;fields:Api.PageLeaderboard;total:number;selected?:string;onSelect:(record:EntityRecord)=>void}) {
 const model=leaderboardRows(records,info,fields);
 if(!model)return <p role="alert">{t("Leaderboard fields, limit or values are unavailable or incompatible.")}</p>;
 return <div className="grid min-w-0 gap-2"><p role="status" className="text-xs text-muted">{t("Top {shown} of {total} matching records · {field}",{shown:records.length,total,field:model.fieldTitle})}</p>{!records.length?<p className="text-sm text-muted">{t("No ranked records.")}</p>:<ol className="grid min-w-0 gap-1">{model.rows.map(row=><li key={row.record.id}><Button variant={selected===row.record.id?"primary":"ghost"} aria-pressed={selected===row.record.id} aria-label={row.label} className="grid h-auto w-full min-w-0 grid-cols-[1.5rem_minmax(0,1fr)_auto] gap-2 p-2 text-left" onClick={()=>onSelect(row.record)}><span className="text-xs tabular-nums">{row.rank}</span><span className="min-w-0"><span className="block truncate text-xs">{row.label}</span><span className="mt-1 block h-1.5 overflow-hidden rounded bg-row-hover"><span className="block h-full bg-primary" style={{width:`${Math.abs(row.value)/model.max*100}%`}}/></span></span><span className="break-all text-xs tabular-nums">{row.value.toLocaleString(undefined,{maximumFractionDigits:1})}</span></Button></li>)}</ol>}</div>;
}
