import {renderingLimits} from "../gen/presentation-limits";
import type {RecordCardOptions} from "../presentation";
import {EntityCard} from "../components/EntityCard";
import {entityFrom,type EntityInfo,type EntityRecord,type RecordSource} from "./Records";
import {t} from "../i18n";

export function RecordCard({record,info,fields,config,source,onOpenReference}:{record:EntityRecord;info:EntityInfo;fields:string[];config:RecordCardOptions;source?:RecordSource;onOpenReference?:(type:string,id:string)=>void}) {
 const label=info.fields.find(f=>f.name===config.labelField),limits=renderingLimits.runtime.recordCard;
 if(typeof record.id!=="string"||!record.id||config.labelField!=="id"&&(!label||!["text","longtext","choice","reference"].includes(label.type))||fields.length>limits.maxFields||new Set(fields).size!==fields.length||!limits.tones.includes(config.tone as typeof limits.tones[number])||fields.some(name=>{const f=info.fields.find(f=>f.name===name);return !f||!["text","longtext","choice","reference","integer","decimal","money","date","datetime","boolean"].includes(f.type);}))return <p role="alert">{t("Record card fields, title or identity are unavailable or incompatible.")}</p>;
 const raw=config.labelField==="id"?record.id:record[config.labelField];if(raw!=null&&typeof raw!=="string")return <p role="alert">{t("Record card title is incompatible with its original field.")}</p>;
 const entity=entityFrom(info,{},source,onOpenReference),properties:[string,React.ReactNode][]=fields.map(name=>[info.fields.find(f=>f.name===name)!.title,entity.fields[name]!.display(record[name],record)]);
 return <div className="grid min-w-0 gap-1"><span className="text-xs text-muted">{info.title}</span><EntityCard title={raw??record.id} subtitle={record.id} tone={config.tone as "neutral"|"info"|"success"|"warning"|"danger"} properties={properties} propertyKeys={fields}/></div>;
}
