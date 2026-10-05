import {createRecordActionSubmitter} from "../record-actions";
import {useEffect,useMemo,useRef} from "react";
import {Panel,RecordActionGrid,RecordCards,Textarea,t,type RecordSource} from "@platform/ui";
import {useHost} from "../index";
import type {Api} from "@platform/kernel";
import type {QueryWindow} from "./QueryWindowFrame";
import {QueryWindowFrame} from "./QueryWindowFrame";
import type {VariableResult} from "../runtime/variables";
import {actionTableParameters} from "./action-table";

export function NotepadRenderer({value,onChange,enabled,label,readCurrent=true}:{value?:VariableResult;onChange?:(value:string)=>void;enabled?:boolean;label:string;readCurrent?:boolean}) {
 if(!readCurrent||value?.status==="pending")return <p role="status">{t("Loading…")}</p>;
 if(value?.status!=="value"||typeof value.value!=="string")return <Panel role="alert">{t("The original session note is unavailable.")}</Panel>;
 return <div className="grid min-w-0 gap-2"><Textarea aria-label={label} value={value.value} disabled={enabled===false||!onChange} className="h-36 font-mono text-xs" onChange={event=>{if(enabled!==false)onChange?.(event.target.value);}}/><p className="text-xs text-muted">{t("This note belongs to this page session. Reloading or closing its panel restores the published initial text.")}</p></div>;
}
export function RecordTilesRenderer({window,object,labelField,selected,onSelect,label,readCurrent=true}:{window?:QueryWindow;object:string;labelField:string;selected?:string;onSelect?:(record:import("@platform/ui").EntityRecord)=>void;label:string;readCurrent?:boolean}) {
 const {source}=useHost(),info=source.entity(object);if(!readCurrent)return <p role="status">{t("Loading…")}</p>;if(!info)return <Panel role="alert">{t("The original tile object is unavailable.")}</Panel>;
 return <QueryWindowFrame paging={false} window={window} description={t("Eight original record tiles in ID order; this is a selection view, not a geographic map.")}>{page=><RecordCards label={label} records={page.records} info={info} fields={[]} labelField={labelField} layout="tiles" selected={selected} onSelect={onSelect}/>}</QueryWindowFrame>;
}
export function ActionTableRenderer({config,actionSchema,object,window,readSource,scope,live,enabled=true,readCurrent=true}:{config?:Api.PageActionTable;actionSchema?:string;object:string;window?:QueryWindow;readSource?:RecordSource;scope:string;live:boolean;enabled?:boolean;readCurrent?:boolean}) {
 const host=useHost(),info=host.source.entity(object),action=host.catalog.find(a=>a.schema===actionSchema),parameters=actionTableParameters(info,action,config?.parameters??[]),identity=JSON.stringify([scope,window?.query]),current=useRef({scope:identity,enabled,readCurrent,active:true});current.current={scope:identity,enabled,readCurrent,active:current.current.active};useEffect(()=>{current.current.active=true;return()=>{current.current.active=false;};},[]);const last=useRef<{identity:string;page:import("@platform/ui").RecordPageData}|undefined>(undefined);if(window?.page)last.current={identity,page:window.page};const page=window?.page??(last.current?.identity===identity&&!window?.error?last.current.page:undefined);
 const submitOriginal=useMemo(()=>createRecordActionSubmitter(host,()=>current.current.active&&current.current.scope===identity&&current.current.enabled&&current.current.readCurrent),[host.client,identity]);
 const port=useMemo(()=>action&&parameters&&({schema:action.schema,fields:parameters.map(p=>p.parameter),scope:identity,preview:!live,alwaysEditing:true,enabled:enabled&&readCurrent,maxRows:50,confirmation:{title:t("Review row actions"),description:t("Run {action} for the staged original records?",{action:action.title})},submit:async(record:import("@platform/ui").EntityRecord,patch:Record<string,unknown>)=>{if(!current.current.active||current.current.scope!==identity||!current.current.enabled||!current.current.readCurrent)return {accepted:false,error:t("The original action window has ended.")};const payload=Object.assign(Object.create(null),Object.fromEntries(parameters.map(p=>[p.parameter,record[p.parameter]])),patch);return submitOriginal(action.schema,{type:object,id:record.id},record.revision,payload);}}),[action,parameters,identity,enabled,readCurrent,live,submitOriginal]);
 if(!readCurrent)return <p role="status">{t("Loading…")}</p>;if(!info||!action||!parameters||!port)return <Panel role="alert">{t("The original row action or parameter bindings are unavailable.")}</Panel>;
 if(window?.error)return <Panel role="alert">{t(window.error)}</Panel>;if(!page)return <p role="status">{t("Loading…")}</p>;return <div className="grid min-w-0 gap-2"><p role="status" className="text-xs text-muted">{t("Showing {shown} of {total} matching records",{shown:page.records.length,total:page.total})} · {t("Fifty original records in ID order; each staged row is an independent revision-bound action.")}</p><RecordActionGrid key={JSON.stringify([identity,action,config])} records={page.records} info={info} parameters={parameters} payload={action.payload} source={readSource??host.source} port={port}/></div>;
}
