import {Panel,RecordCalendar,t,type EntityInfo,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
import {pageVariableContract} from "../runtime/PageRuntime";
export function RecordCalendarRenderer({window,info,fields,selected,onSelect}:{window?:QueryWindow;info?:EntityInfo;fields?:Api.PageRecordCalendar;selected?:EntityRecord;onSelect:(record?:EntityRecord)=>void}) {
 const date=info?.fields.find(f=>f.name===fields?.dateField),label=info?.fields.find(f=>f.name===fields?.labelField);
 if(!fields||!date||!["date","datetime"].includes(date.type)||fields.labelField!=="id"&&(!label||!["text","longtext","choice","reference"].includes(label.type)))return <Panel role="alert">{t("Calendar fields are unavailable or incompatible.")}</Panel>;
 return <QueryWindowFrame window={window} description={t("Calendar counts and records cover the current query window.")}>{page=><RecordCalendar records={page.records} fields={{...fields,kind:date.type as "date"|"datetime"}} selected={selected?.id} onSelect={onSelect} maxRecords={pageVariableContract.recordCalendar.maxRecords}/>}</QueryWindowFrame>;
}
