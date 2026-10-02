import {useEffect,useRef,useState} from "react";
import type {ColumnDef} from "@tanstack/react-table";
import {DataTable} from "../components/DataTable";
import {Button} from "../primitives/button";
import type {Entity} from "../fields/entity";
import {t} from "../i18n";
import type {EntityRecord} from "./Records";

/** The caller supplies authorized fields and the original submission route. */
export type RecordEditPort={schema:string;fields:string[];scope:string;maxRows:number;preview?:boolean;submit:(baseline:EntityRecord,patch:Record<string,unknown>)=>Promise<{accepted:boolean;error?:string}>};
const emptyRows=()=>Object.create(null) as Record<string,PendingRow>;
type PendingRow={baseline:EntityRecord;patch:Record<string,unknown>;error?:string};
export function EditableRecordGrid({data,columns,entity,height,onOpen,loading,empty,port}:{data?:EntityRecord[];columns:ColumnDef<EntityRecord,unknown>[];entity:Entity<EntityRecord>;height:number|string;onOpen?:(record:EntityRecord)=>void;loading:boolean;empty:React.ReactNode;port?:RecordEditPort}){
 const [editing,setEditing]=useState(false),[rows,setRows]=useState<Record<string,PendingRow>>(emptyRows),[busy,setBusy]=useState(false),[notice,setNotice]=useState("");
 const alive=useRef(true),lock=useRef(false),currentPort=useRef(port);currentPort.current=port;
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 // Confirmed removal clears no-longer-authorized/viewed record snapshots. A
 // normal pending refresh keeps the opened revision, so it cannot rebase edits.
 useEffect(()=>{if(!data)return;const ids=new Set(data.filter(r=>!r.archived).map(r=>r.id));setRows(old=>{const next=Object.assign(emptyRows(),Object.fromEntries(Object.entries(old).filter(([id])=>ids.has(id))));if(Object.keys(next).length!==Object.keys(old).length)setNotice(t("Edits for records no longer in this view were cleared."));return next;});},[data]);
 const stage=(record:EntityRecord,field:string,value:unknown)=>{
  if(busy||record.archived||!port?.fields.includes(field))return;
  setNotice("");setRows(old=>{if(!old[record.id]&&Object.keys(old).length>=port.maxRows){setNotice(t("The staged row limit has been reached."));return old;}const baseline=old[record.id]?.baseline??structuredClone(record),patch={...old[record.id]?.patch};if(Object.is(value,baseline[field]))delete patch[field];else patch[field]=value;const next=Object.assign(emptyRows(),old);if(Object.keys(patch).length)next[record.id]={baseline,patch};else delete next[record.id];return next;});
 };
 const submit=async()=>{
  if(lock.current||!port||port.preview)return;lock.current=true;setBusy(true);setNotice("");let accepted=0;
  try{for(const [id,row] of Object.entries(rows)){
   if(!alive.current)break;
   const problem=Object.entries(row.patch).find(([name,value])=>{const field=entity.fields[name];return !port.fields.includes(name)||!field||field.required&&(value===""||value===undefined)||!field.schema.safeParse(value).success;});
   let result:{accepted:boolean;error?:string};if(problem)result={accepted:false,error:t("Enter a valid value for {field}.",{field:entity.fields[problem[0]]?.label??problem[0]})};else try{result=await currentPort.current!.submit(row.baseline,row.patch);}catch{result={accepted:false,error:t("The edit could not be submitted. Your staged values are still here.")};}
   if(!alive.current)break;
   if(result.accepted)accepted++;
   setRows(old=>{const next=Object.assign(emptyRows(),old);if(result.accepted)delete next[id];else next[id]={...row,error:result.error??t("The host refused this edit.")};return next;});
  }if(alive.current&&accepted)setNotice(t("Submitted {count} rows. Failed edits remain staged.",{count:accepted}));}
  finally{lock.current=false;if(alive.current)setBusy(false);}
 };
 const shown=(data??[]).map(record=>({...record,...rows[record.id]?.patch})),editableColumns=columns.map(column=>column.meta?.field?{...column,meta:{...column.meta,field:{...column.meta.field,readOnly:column.meta.field.readOnly||!port?.fields.includes(column.id??"")||busy}}}:column);
 return <div className="grid min-w-0 gap-2">
 {port&&<div className="flex flex-wrap items-center gap-2">{editing?<><Button size="sm" disabled={busy} onClick={()=>{setEditing(false);setRows(emptyRows());setNotice("");}}>{t("Cancel cell edits")}</Button><Button size="sm" variant="primary" disabled={busy||port.preview||Object.keys(rows).length===0} onClick={submit}>{busy?t("Submitting…"):t("Submit cell edits")}</Button><span className="text-xs text-muted">{t("{count} rows staged",{count:Object.keys(rows).length})}</span></>:<Button size="sm" onClick={()=>setEditing(true)}>{t("Edit cells")}</Button>}{editing&&<p className="text-xs text-muted">{t("Double-click or press Enter/F2 to edit. Enter or Tab stages the cell; submit uses the original record action.")}</p>}{port.preview&&<p className="text-xs text-muted">{t("Actions do not run while you compose.")}</p>}</div>}
 {notice&&<p role="status" className="text-xs text-muted">{notice}</p>}{Object.entries(rows).filter(([,row])=>row.error).map(([id,row])=><p key={id} role="alert" className="text-xs text-danger">{id}: {row.error}</p>)}
 <DataTable key={JSON.stringify([editing,busy])} data={shown} columns={editableColumns as never} getRowId={r=>r.id} height={height} searchable={false} onRowClick={editing?undefined:onOpen} onCellEdit={editing&&!busy?stage:undefined} loading={loading} empty={empty}/>
 </div>;
}
