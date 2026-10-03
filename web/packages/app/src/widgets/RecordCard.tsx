import {Panel,RecordCard,t,type EntityInfo,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
export function RecordCardRenderer({record,status,info,fields,config}:{record?:EntityRecord;status?:"empty"|"pending"|"value"|"error";info?:EntityInfo;fields:string[];config?:Api.PageRecordCard}) {
 if(status==="error")return <Panel role="alert">{t("The record card could not be loaded.")}</Panel>;
 if(status==="pending")return <p role="status" className="text-xs text-muted">{t("Confirming record access…")}</p>;
 if(!record||status!=="value")return <p role="status" className="text-xs text-muted">{t("No confirmed record for this card.")}</p>;
 if(!info||!config)return <Panel role="alert">{t("Record card configuration is unavailable.")}</Panel>;
 return <RecordCard record={record} info={info} fields={fields} config={config}/>;
}
