import {renderingLimits} from "../gen/presentation-limits";
import {timestampNanoseconds,validCivilDate} from "../components/date";

import {entityFrom,type EntityInfo,type EntityRecord,type RecordSource} from "./Records";
import {t} from "../i18n";

const titleTypes=["text","longtext","choice","reference"];
const fieldTypes=[...titleTypes,"integer","decimal","money","date","datetime","boolean"];

/** Missing values share one identity; incompatible original values have none. */
function originalValue(type:string,value:unknown):string|number|boolean|bigint|null|undefined {
 if(value===null||value===undefined)return null;
 if(titleTypes.includes(type))return typeof value==="string"?value:undefined;
 if(type==="integer")return typeof value==="number"&&Number.isSafeInteger(value)?value:undefined;
 if(type==="decimal")return typeof value==="number"&&Number.isFinite(value)?value:undefined;
 if(type==="boolean")return typeof value==="boolean"?value:undefined;
 if(type==="date")return typeof value==="string"&&validCivilDate(value)?value:undefined;
 if(type==="datetime")return typeof value==="string"?timestampNanoseconds(value):undefined;
 if(type==="money"&&typeof value==="object"&&!Array.isArray(value)){
  const amount=(value as Record<string,unknown>).amount,currency=(value as Record<string,unknown>).currency;
  return typeof amount==="number"&&Number.isSafeInteger(amount)&&typeof currency==="string"&&/^[A-Z]{3}$/.test(currency)?JSON.stringify([amount,currency]):undefined;
 }
 return undefined;
}

/** Compare complete, caller-confirmed original records using permission-filtered field metadata. */
export function RecordComparison({records,info,fields,labelField,source,onOpenReference}:{records:EntityRecord[];info:EntityInfo;fields:string[];labelField:string;source?:RecordSource;onOpenReference?:(type:string,id:string)=>void}) {
 const label=info.fields.find(f=>f.name===labelField),definitions=fields.map(name=>info.fields.find(f=>f.name===name)),limits=renderingLimits.runtime.recordComparison;
 if(labelField!=="id"&&(!label||!titleTypes.includes(label.type))||fields.length<limits.minFields||fields.length>limits.maxFields||new Set(fields).size!==fields.length||definitions.some(f=>!f||!fieldTypes.includes(f.type)))return <p role="alert">{t("Record comparison fields or title are unavailable or incompatible.")}</p>;
 if(records.length>limits.maxRecords)return <p role="alert">{t("Select no more than four records to compare. All selected records are retained.")}</p>;
 if(records.some(record=>!record||typeof record.id!=="string"||!record.id.trim()||record.archived===true)||new Set(records.map(record=>record.id)).size!==records.length)return <p role="alert">{t("Record comparison identities are unavailable or incompatible.")}</p>;
 if(records.length<limits.minRecords)return <p role="status">{t("Select two to four records to compare.")}</p>;
 const titles=records.map(record=>labelField==="id"?record.id:record[labelField]);
 const rows=definitions.map(field=>({field:field!,values:records.map(record=>originalValue(field!.type,record[field!.name]))}));
 if(titles.some(value=>value!=null&&typeof value!=="string")||rows.some(row=>row.values.some(value=>value===undefined)))return <p role="alert">{t("Record comparison values are incompatible with their original fields.")}</p>;
 const entity=entityFrom(info,{},source,onOpenReference);
 return <div role="region" aria-label={t("Scrollable record comparison")} tabIndex={0} className="min-w-0 overflow-x-auto rounded-md border border-border outline-none focus-visible:ring-2 focus-visible:ring-ring">
  <table aria-label={t("Record comparison")} className="w-full border-collapse text-sm">
   <caption className="sr-only">{t("{count} confirmed records",{count:records.length})}</caption>
   <thead><tr className="bg-row-hover"><th scope="col" className="min-w-36 px-3 py-2 text-left">{t("Property")}</th>{records.map((record,index)=><th key={record.id} scope="col" className="min-w-44 max-w-72 px-3 py-2 text-left"><span className="block break-words">{(titles[index] as string|null|undefined)??record.id}</span>{" "}<span className="block break-all text-xs font-normal text-muted">{record.id}</span></th>)}</tr></thead>
   <tbody>{rows.map(({field,values})=>{const differs=values.some(value=>value!==values[0]);return <tr key={field.name} className="border-t border-border" style={differs?{background:"color-mix(in oklch, var(--tone-warning) 10%, transparent)"}:undefined}><th scope="row" className="max-w-72 px-3 py-2 text-left font-medium"><span className="block break-words">{field.title}</span>{" "}<span className={"block text-xs font-normal "+(differs?"":"text-muted")} style={differs?{color:"var(--tone-warning)"}:undefined}>{t(differs?"Different":"Same")}</span></th>{records.map(record=><td key={record.id} className={"max-w-72 break-words px-3 py-2 "+(entity.fields[field.name]!.align==="right"?"text-right":"")}>{entity.fields[field.name]!.display(record[field.name],record)}</td>)}</tr>;})}</tbody>
  </table>
 </div>;
}
