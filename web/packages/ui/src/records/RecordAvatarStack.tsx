import {renderingLimits} from "../gen/presentation-limits";

import {timestampParts,validCivilDate} from "../components/date";
import {entityFrom,type EntityInfo,type EntityRecord} from "./Records";
import {Avatar} from "@astryxdesign/core/Avatar";
import {AvatarGroup} from "@astryxdesign/core/AvatarGroup";
import {Text} from "@astryxdesign/core/Text";
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

export function RecordAvatarStack({records,info,labelField,detailFields=[],total,label}:RecordAvatarStackProps) {
 const limits=renderingLimits.runtime.contextViews;
 if(!info||!Array.isArray(records)||records.length>limits.maxAvatarWindow||typeof total!=="number"||!Number.isSafeInteger(total)||total<records.length||new Set(records.map(record=>record?.id)).size!==records.length)return <Text as="p" role="alert">{t("The record avatars are unavailable or incompatible.")}</Text>;
 const labelFieldInfo=info.fields.find(field=>field.name===labelField),details=detailFields.map(name=>info.fields.find(field=>field.name===name));
 if(labelField!=="id"&&(!labelFieldInfo||!titleTypes.includes(labelFieldInfo.type))||detailFields.length>limits.maxDetailFields||new Set(detailFields).size!==detailFields.length||details.some(field=>!field||!scalarTypes.includes(field.type)))return <Text as="p" role="alert">{t("Avatar labels or details are incompatible with their original fields.")}</Text>;
 const originalRecords:readonly EntityRecord[]=records;
 if(originalRecords.some(record=>labelField!=="id"&&!scalar(labelFieldInfo!.type,record[labelField])||details.some(field=>!scalar(field!.type,record[field!.name]))))return <Text as="p" role="alert">{t("Avatar values are incompatible with their original fields.")}</Text>;
 const entity=entityFrom(info);
 return <section aria-label={label??t("Record avatars")} className="flex min-w-0 flex-wrap items-center gap-2">{!originalRecords.length?<Text type="supporting">{t(total===0?"No matching people records.":"No people records in this loaded window.")}</Text>:<AvatarGroup size="sm">{originalRecords.map(record=>{const originalLabel=(labelField==="id"?record.id:record[labelField]) as string|null|undefined,name=originalLabel??record.id;const properties=details.map(field=>({field:field!,text:field!.name==="id"?record.id:entity.fields[field!.name]!.text(record[field!.name])})),tooltip=[name,record.id,...properties.map(({field,text})=>`${field.title}: ${text}`)].join(" · ");return <Avatar key={record.id} name={initials(name)} tooltip={tooltip} />;})}</AvatarGroup>}<Text type="supporting" role="status">{t("Showing {shown} of {total} records.",{shown:originalRecords.length,total})}</Text></section>;
}
