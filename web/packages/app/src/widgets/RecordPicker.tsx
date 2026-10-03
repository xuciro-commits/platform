import {Panel,RecordLookup,t,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {useHost} from "../index";
import type {QueryWindow} from "./QueryWindowFrame";
export function RecordPickerRenderer({window,type,fields,title,selected,enabled,onSelect}:{window?:QueryWindow;type:string;fields?:Api.PageRecordPicker;title:string;selected?:EntityRecord;enabled?:boolean;onSelect:(record?:EntityRecord)=>void}){
 const {source}=useHost();if(!fields||!window)return <Panel role="status">{t("Record picker window is unavailable.")}</Panel>;
 if(window.error)return <Panel role="alert">{t(window.error)}</Panel>;
 if(window.query.limit!==20||!!window.query.offset||JSON.stringify(window.query.sort)!==JSON.stringify(["id"]))return <Panel role="alert">{t("Record picker window bounds or ordering changed.")}</Panel>;
 const caption=fields.label??"";
 return <div className="grid min-w-0 gap-1">{caption&&<span className="break-words text-xs">{caption}</span>}<RecordLookup source={source} type={type} window={window} labelField={fields.labelField} selectedRecord={selected} value={selected?.id} ariaLabel={caption.trim()?caption:title} disabled={enabled===false} onChange={id=>{if(enabled===false)return;const record=id?window.page?.records.find(r=>r.id===id):undefined;if(!id||record)onSelect(record);}}/><p className="text-xs text-muted">{t("Up to 20 authorized candidates from the original query.")}</p></div>;
}
