import {RecordScatter,t,type EntityInfo,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
export function RecordScatterRenderer({window,info,fields,selected,enabled,onSelect,confirmation}:{confirmation?:"empty"|"pending"|"value"|"error";window?:QueryWindow;info?:EntityInfo;fields?:Api.PageRecordScatter;selected?:EntityRecord;enabled?:boolean;onSelect:(record?:EntityRecord)=>void}) {
 if(!fields||!info)return <p role="alert">{t("Scatter fields, record identities or numeric values are unavailable or incompatible.")}</p>;
 return <div className="grid min-w-0 gap-1">{confirmation==="pending"&&<p role="status" className="text-xs text-muted">{t("Confirming record access…")}</p>}{confirmation==="error"&&<p role="alert" className="text-xs text-danger">{t("The selected record could not be confirmed.")}</p>}<QueryWindowFrame window={window} description={t("This scatter plot shows individual records in the current authorized query window.")}>{page=><RecordScatter records={page.records} info={info} fields={fields} selected={selected?.id} enabled={enabled} onSelect={onSelect}/>}</QueryWindowFrame></div>;
}
