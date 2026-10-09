import {Button,InterfaceRecordLookup,Panel,RecordLookup,t,type RecordSource,type EntityRecord,type InterfaceRecordData} from "@platform/ui";
import type {VariableResult} from "../runtime/variables";
import type {Api} from "@platform/kernel";
import type {QueryWindow} from "./QueryWindowFrame";
export function RecordPickerRenderer({source,window,type,fields,title,selected,enabled,value,onSelect,confirmation,interfaceReference,onInterfaceSelect,onOpen}:{source?:RecordSource;confirmation?:"empty"|"pending"|"value"|"error";value?:VariableResult;window?:QueryWindow;type:string;fields?:Api.PageRecordPicker;title:string;selected?:EntityRecord;enabled?:boolean;onSelect:(record?:EntityRecord)=>void;interfaceReference?:{object:string;id:string};onInterfaceSelect?:(row?:InterfaceRecordData)=>void;onOpen?:(type:string,record:EntityRecord)=>void}){
 if(!source||!fields||!window)return <Panel role="status">{t("Record picker window is unavailable.")}</Panel>;
 if(window.error)return <Panel role="alert">{t(window.error)}</Panel>;
 if(window.query.limit!==20||!!window.query.offset||JSON.stringify(window.query.sort)!==JSON.stringify(["id"]))return <Panel role="alert">{t("Record picker window bounds or ordering changed.")}</Panel>;
 if(window.interface){
  const candidate=interfaceReference?{type:interfaceReference.object,id:interfaceReference.id}:undefined;
  const typedWindow={...window,page:window.interfacePage};
  return <div className="grid min-w-0 gap-2"><InterfaceRecordLookup source={source} name={window.interface} window={typedWindow} labelField={fields.labelField} value={candidate} ariaLabel={fields.label?.trim()||title} disabled={enabled===false} onChange={ref=>{
    if(enabled===false)return;
    const row=ref?window.interfacePage?.records.find(r=>r.type===ref.type&&r.id===ref.id):undefined;
    if(!ref||row)onInterfaceSelect?.(row);
  }}/><Button size="sm" disabled={enabled===false||confirmation!=="value"||!selected||!interfaceReference} onClick={()=>{if(selected&&interfaceReference&&confirmation==="value")onOpen?.(interfaceReference.object,selected);}}>{t("Open selected record")}</Button>{confirmation==="pending"&&<p role="status" className="text-xs text-muted">{t("Confirming record access…")}</p>}{confirmation==="error"&&<p role="alert" className="text-xs text-danger">{t("The record could not be confirmed.")}</p>}<p className="text-xs text-muted">{t("Selection retains the original object type and record ID.")}</p></div>;
 }
 const caption=fields.label??"",id=value?.status==="value"&&typeof value.value==="string"?value.value:selected?.id,current=id===selected?.id?selected:undefined;if(value&&value.status!=="value"||value?.status==="value"&&typeof value.value!=="string")return <Panel role="alert">{t("Picker ID state is unavailable or incompatible.")}</Panel>;
 return <div className="grid min-w-0 gap-1">{caption&&<span className="break-words text-xs">{caption}</span>}<RecordLookup source={source} type={type} window={window} labelField={fields.labelField} selectedRecord={current} value={id} ariaLabel={caption.trim()?caption:title} disabled={enabled===false} onChange={id=>{if(enabled===false)return;const record=id?window.page?.records.find(r=>r.id===id):undefined;if(!id||record)onSelect(record);}}/>{value&&id&&!window.page?.records.some(r=>r.id===id)&&<p role="status" className="break-words text-xs text-muted">{t("The original ID is outside the current candidate window: {id}.",{id})}</p>}{confirmation==="pending"&&<p role="status" className="text-xs text-muted">{t("Confirming record access…")}</p>}{confirmation==="error"&&<p role="alert" className="text-xs text-danger">{t("The record could not be confirmed. The original ID is unchanged.")}</p>}<p className="text-xs text-muted">{t("Up to 20 authorized candidates from the original query.")}</p></div>;
}
