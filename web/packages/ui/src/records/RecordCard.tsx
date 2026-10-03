import {EntityCard} from "../components/EntityCard";
import {entityFrom,type EntityInfo,type EntityRecord} from "./Records";
import {t} from "../i18n";
import {pageUIManifest,type Api} from "@platform/kernel";

/** One already-confirmed original record. Field formatting belongs to the existing entity projection. */
export function RecordCard({record,info,fields,config}:{record:EntityRecord;info:EntityInfo;fields:string[];config:Api.PageRecordCard}) {
 const label=info.fields.find(f=>f.name===config.labelField),limits=pageUIManifest.runtime.recordCard;
 if(typeof record.id!=="string"||!record.id||config.labelField!=="id"&&(!label||!["text","longtext","choice","reference"].includes(label.type))||fields.length>limits.maxFields||new Set(fields).size!==fields.length||!limits.tones.includes(config.tone as typeof limits.tones[number])||fields.some(name=>{const f=info.fields.find(f=>f.name===name);return !f||!["text","longtext","choice","reference","integer","decimal","money","date","datetime","boolean"].includes(f.type);}))return <p role="alert">{t("Record card fields, title or identity are unavailable or incompatible.")}</p>;
 const raw=config.labelField==="id"?record.id:record[config.labelField];if(raw!=null&&typeof raw!=="string")return <p role="alert">{t("Record card title is incompatible with its original field.")}</p>;
 const entity=entityFrom(info),properties:[string,React.ReactNode][]=fields.map(name=>[info.fields.find(f=>f.name===name)!.title,entity.fields[name]!.display(record[name],record)]);
 return <div className="grid min-w-0 gap-1"><span className="text-xs text-muted">{info.title}</span><EntityCard title={raw??record.id} subtitle={record.id} tone={config.tone as "neutral"|"info"|"success"|"warning"|"danger"} properties={properties} propertyKeys={fields}/></div>;
}
