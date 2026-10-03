import {pageUIManifest} from "@platform/kernel";
import {timestampParts,validCivilDate} from "../components/date";
import {entityFrom,type EntityInfo,type EntityRecord} from "./Records";
import {t} from "../i18n";

export type RecordAvatarStackProps={records:readonly EntityRecord[];info:EntityInfo;labelField:string;detailFields?:readonly string[];total:number;label?:string};
const titleTypes=["text","longtext","choice","reference"],scalarTypes=[...titleTypes,"integer","decimal","money","date","datetime","boolean"];
function scalar(type:string,value:unknown):boolean {
 if(value==null)return true;
 if(titleTypes.includes(type))return typeof value==="string";
 if(type==="integer")return typeof value==="number"&&Number.isSafeInteger(value);
 if(type==="decimal")return typeof value==="number"&&Number.isFinite(value);
 if(type==="boolean")return typeof value==="boolean";
 if(type==="date")return typeof value==="string"&&validCivilDate(value);
 if(type==="datetime")return typeof value==="string"&&!!timestampParts(value);
 if(type==="money"&&typeof value==="object"&&!Array.isArray(value)){const original=value as Record<string,unknown>;return typeof original.amount==="number"&&Number.isSafeInteger(original.amount)&&typeof original.currency==="string"&&/^[A-Z]{3}$/.test(original.currency);}
 return false;
}
function initials(label:string):string {
 const words=label.replace(/[\p{Cc}\u200e\u200f\u202a-\u202e\u2066-\u2069]/gu,"").trim().split(/\s+/u).filter(Boolean),graphemes=(value:string)=>typeof Intl.Segmenter==="function"?Array.from(new Intl.Segmenter(undefined,{granularity:"grapheme"}).segment(value),part=>part.segment):Array.from(value);
 return graphemes(words.slice(0,2).map(word=>graphemes(word)[0]??"").join("").toLocaleUpperCase()).slice(0,2).join("")||"?";
}

/** The caller supplies the original authorized first window and its complete total. */
export function RecordAvatarStack({records,info,labelField,detailFields=[],total,label}:RecordAvatarStackProps) {
 const limits=pageUIManifest.runtime.contextViews,title=info.fields.find(field=>field.name===labelField),details=detailFields.map(name=>name==="id"?{name,title:t("ID"),type:"text" as const}:info.fields.find(field=>field.name===name));
 if(!Array.isArray(records)||records.length>limits.maxAvatarWindow||!Number.isSafeInteger(total)||total<records.length||new Set(records.map(record=>record?.id)).size!==records.length||records.some(record=>!record||typeof record.id!=="string"||!record.id||record.archived===true||!Number.isSafeInteger(record.revision)||record.revision<0)||labelField!=="id"&&(!title||!titleTypes.includes(title.type))||detailFields.length>limits.maxDetailFields||detailFields.includes(labelField)||new Set(detailFields).size!==detailFields.length||details.some(field=>!field||!scalarTypes.includes(field.type)))return <p role="alert">{t("Avatar records, fields or total are unavailable or incompatible.")}</p>;
 const originalRecords:readonly EntityRecord[]=records;
 if(originalRecords.some(record=>labelField!=="id"&&!scalar(title!.type,record[labelField])||details.some(field=>!scalar(field!.type,record[field!.name]))))return <p role="alert">{t("Avatar values are incompatible with their original fields.")}</p>;
 const entity=entityFrom(info);
 return <section aria-label={label??t("Record avatars")} className="flex min-w-0 flex-wrap items-center gap-2">{!records.length?<p className="text-xs text-muted">{t(total===0?"No matching people records.":"No people records in this loaded window.")}</p>:<ul className="flex shrink-0">{originalRecords.map(record=>{
  const originalLabel=(labelField==="id"?record.id:record[labelField]) as string|null|undefined,name=originalLabel??record.id;
  const properties=details.map(field=>({field:field!,value:field!.name==="id"?record.id:entity.fields[field!.name]!.display(record[field!.name],record),text:field!.name==="id"?record.id:entity.fields[field!.name]!.text(record[field!.name])})),tooltip=[name,record.id,...properties.map(({field,text})=>`${field.title}: ${text}`)].join(" · ");
  return <li key={record.id} title={tooltip} className="-ml-1.5 shrink-0 first:ml-0"><span aria-hidden="true" className="inline-flex size-7 items-center justify-center rounded-full border-2 border-surface bg-primary text-[10px] font-semibold text-primary-foreground">{initials(name)}</span><div className="sr-only"><span>{name}</span>{" "}<span>{record.id}</span>{properties.length>0&&<dl>{properties.map(({field,value})=><div key={field.name}><dt>{field.title}</dt><dd>{value}</dd></div>)}</dl>}</div></li>;
 })}</ul>}<p role="status" className="break-words text-xs text-muted">{t("Showing {shown} of {total} records.",{shown:records.length,total})}</p></section>;
}
