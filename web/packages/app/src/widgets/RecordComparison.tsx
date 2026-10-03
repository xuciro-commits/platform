import {Panel,RecordComparison,t,type EntityInfo,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";

/** The session supplies complete, authorized selected records; pending sets never render cached rows. */
export function RecordComparisonRenderer({records,status,info,fields,config}:{records:EntityRecord[];status?:"empty"|"pending"|"value"|"error";info?:EntityInfo;fields:string[];config?:Api.PageRecordComparison}) {
 if(status==="error")return <Panel role="alert">{t("The selected records could not be loaded.")}</Panel>;
 if(status==="pending")return <p role="status" className="text-xs text-muted">{t("Confirming selected record access…")}</p>;
 if(status!=="value"||records.length<2)return <p role="status" className="text-xs text-muted">{t("Select two to four records to compare.")}</p>;
 if(!info||!config)return <Panel role="alert">{t("Record comparison configuration is unavailable.")}</Panel>;
 return <RecordComparison records={records} info={info} fields={fields} labelField={config.labelField}/>;
}
