import {useEffect,useRef,useState} from "react";
import type {ColumnDef} from "@tanstack/react-table";
import {DataTable} from "../components/DataTable";
import {Dialog} from "../primitives/dialog";
import {Checkbox} from "../primitives/controls";
import {Button} from "../primitives/button";
import type {Entity} from "../fields/entity";
import {t} from "../i18n";
import type {EntityRecord} from "./Records";

/** The caller supplies authorized fields and the original submission route. */
export type RecordSelectionPort={selectedIDs:string[];records:EntityRecord[];status:"empty"|"pending"|"value"|"error";maxRecords:number;onChange:(ids:string[])=>void};
export type RecordEditPort={schema:string;fields:string[];scope:string;maxRows:number;preview?:boolean;alwaysEditing?:boolean;confirmation?:{title:string;description:string};enabled?:boolean;submit:(baseline:EntityRecord,patch:Record<string,unknown>)=>Promise<{accepted:boolean;error?:string}>};
const emptyRows=()=>Object.create(null) as Record<string,PendingRow>;
type PendingRow={baseline:EntityRecord;patch:Record<string,unknown>;error?:string};
export function EditableRecordGrid({data,columns,entity,height,onOpen,loading,empty,port,selectionSet}:{selectionSet?:RecordSelectionPort;data?:EntityRecord[];columns:ColumnDef<EntityRecord,unknown>[];entity:Entity<EntityRecord>;height:number|string;onOpen?:(record:EntityRecord)=>void;loading:boolean;empty:React.ReactNode;port?:RecordEditPort}){
 const [editing,setEditing]=useState(!!port?.alwaysEditing),[rows,setRows]=useState<Record<string,PendingRow>>(emptyRows),[busy,setBusy]=useState(false),[notice,setNotice]=useState("");
 const [review,setReview]=useState(false),reviewedRows=useRef<Record<string,PendingRow>|undefined>(undefined);
 const alive=useRef(true),lock=useRef(false),currentPort=useRef(port);currentPort.current=port;
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 // Confirmed removal clears no-longer-authorized/viewed record snapshots. A
 // normal pending refresh keeps the opened revision, so it cannot rebase edits.
 useEffect(()=>{if(!data)return;const ids=new Set(data.filter(r=>!r.archived).map(r=>r.id));setRows(old=>{const next=Object.assign(emptyRows(),Object.fromEntries(Object.entries(old).filter(([id])=>ids.has(id))));if(Object.keys(next).length!==Object.keys(old).length)setNotice(t("Edits for records no longer in this view were cleared."));return next;});},[data]);
 const stage=(record:EntityRecord,field:string,value:unknown)=>{
  if(busy||review||port?.enabled===false||record.archived||!port?.fields.includes(field))return;
  setNotice("");setRows(old=>{if(!old[record.id]&&Object.keys(old).length>=port.maxRows){setNotice(t("The staged row limit has been reached."));return old;}const baseline=old[record.id]?.baseline??structuredClone(record),patch={...old[record.id]?.patch};if(Object.is(value,baseline[field]))delete patch[field];else patch[field]=value;const next=Object.assign(emptyRows(),old);if(Object.keys(patch).length)next[record.id]={baseline,patch};else delete next[record.id];return next;});
 };
 const submit=async()=>{
  if(lock.current||!port||port.preview||port.enabled===false)return;lock.current=true;setBusy(true);setNotice("");let accepted=0;
  try{for(const [id,row] of Object.entries(reviewedRows.current??rows)){
   if(!alive.current||currentPort.current?.enabled===false||currentPort.current?.scope!==port.scope)break;
   const values=port.alwaysEditing?Object.fromEntries(port.fields.map(name=>[name,Object.hasOwn(row.patch,name)?row.patch[name]:row.baseline[name]])):row.patch;
   const problem=Object.entries(values).find(([name,value])=>{const field=entity.fields[name];return !port.fields.includes(name)||!field||field.required&&(value===""||value===undefined||value===null)||!field.schema.safeParse(value).success;});
   let result:{accepted:boolean;error?:string};if(problem)result={accepted:false,error:t("Enter a valid value for {field}.",{field:entity.fields[problem[0]]?.label??problem[0]})};else try{result=await currentPort.current!.submit(row.baseline,row.patch);}catch{result={accepted:false,error:t("The edit could not be submitted. Your staged values are still here.")};}
   if(!alive.current||currentPort.current?.scope!==port.scope)break;
   if(result.accepted)accepted++;
   setRows(old=>{const next=Object.assign(emptyRows(),old);if(result.accepted)delete next[id];else next[id]={...row,error:result.error??t("The host refused this edit.")};return next;});
  }if(alive.current&&accepted)setNotice(t("Submitted {count} rows. Failed edits remain staged.",{count:accepted}));}
  finally{reviewedRows.current=undefined;lock.current=false;if(alive.current)setBusy(false);}
 };
 const anchor=useRef<string|undefined>(undefined);
 const choose=(record:EntityRecord,modifiers?:{shift:boolean;toggle:boolean})=>{
  if(selectionSet){let ids:string[];const current=selectionSet.selectedIDs;
   if(modifiers?.shift&&anchor.current&&data?.some(r=>r.id===anchor.current)){const a=data.findIndex(r=>r.id===anchor.current),b=data.findIndex(r=>r.id===record.id);ids=data.slice(Math.min(a,b),Math.max(a,b)+1).filter(r=>!r.archived).map(r=>r.id);}
   else if(modifiers?.toggle)ids=current.includes(record.id)?current.filter(id=>id!==record.id):[...current,record.id];else ids=[record.id];
   if(ids.length>selectionSet.maxRecords){setNotice(t("The selection limit has been reached."));return;}anchor.current=record.id;selectionSet.onChange(ids);
  }onOpen?.(record);
 };
 const shown=(data??[]).map(record=>({...record,...rows[record.id]?.patch})),editableColumns=columns.map(column=>column.meta?.field?{...column,meta:{...column.meta,field:{...column.meta.field,readOnly:column.meta.field.readOnly||!port?.fields.includes(column.id??"")||busy||review||port?.enabled===false}}}:column);
 const selectionLabel=entity.primary!=="id"?entity.primary:columns.find(c=>c.id&&entity.fields[c.id])?.id??"id";
 const visibleIDs=(data??[]).filter(r=>!r.archived).map(r=>r.id),all=visibleIDs.length>0&&visibleIDs.every(id=>selectionSet?.selectedIDs.includes(id));
 const tableColumns=selectionSet?[{id:"selection",header:()=> <Checkbox checked={all} disabled={!all&&visibleIDs.length>selectionSet.maxRecords||editing} onChange={checked=>selectionSet.onChange(checked?visibleIDs:[])}>{t("Select this window")}</Checkbox>,cell:({row}:{row:{original:EntityRecord}})=><span onClick={e=>e.stopPropagation()}><Checkbox checked={selectionSet.selectedIDs.includes(row.original.id)} disabled={row.original.archived||editing||!selectionSet.selectedIDs.includes(row.original.id)&&selectionSet.selectedIDs.length>=selectionSet.maxRecords} onChange={()=>choose(row.original,{shift:false,toggle:true})}><span className="sr-only">{t("Select {record}",{record:row.original.id})}</span></Checkbox></span>,meta:{width:160}},...editableColumns]:editableColumns;
 return <div className="grid min-w-0 gap-2">
 {port&&<div className="flex flex-wrap items-center gap-2">{editing?<><Button size="sm" disabled={busy} onClick={()=>{if(!port.alwaysEditing)setEditing(false);setRows(emptyRows());setNotice("");}}>{t("Cancel cell edits")}</Button><Button size="sm" variant="primary" disabled={busy||port.preview||port.enabled===false||Object.keys(rows).length===0} onClick={()=>{if(port.confirmation){reviewedRows.current=structuredClone(rows);setReview(true);}else void submit();}}>{busy?t("Submitting…"):t("Submit cell edits")}</Button><span className="text-xs text-muted">{t("{count} rows staged",{count:Object.keys(rows).length})}</span></>:<Button size="sm" onClick={()=>setEditing(true)}>{t("Edit cells")}</Button>}{editing&&<p className="text-xs text-muted">{t("Double-click or press Enter/F2 to edit. Enter or Tab stages the cell; submit uses the original record action.")}</p>}{port.preview&&<p className="text-xs text-muted">{t("Actions do not run while you compose.")}</p>}</div>}
 {notice&&<p role="status" className="text-xs text-muted">{notice}</p>}{Object.entries(rows).filter(([,row])=>row.error).map(([id,row])=><p key={id} role="alert" className="text-xs text-danger">{id}: {row.error}</p>)}
 {port?.confirmation&&<Dialog open={review} onOpenChange={open=>{setReview(open);if(!open)reviewedRows.current=undefined;}} title={port.confirmation.title}><div className="grid gap-3"><p className="text-sm">{port.confirmation.description}</p><ul className="grid gap-1 text-xs">{Object.entries(reviewedRows.current??{}).map(([id,row])=><li key={id} className="break-all">{id} · {t("Revision {revision}",{revision:row.baseline.revision})} · {Object.keys(row.patch).map(name=>entity.fields[name]?.label??name).join(", ")}</li>)}</ul><p className="text-xs text-muted">{t("Each row uses its original action and opened revision. Accepted requests may await approval; failed rows remain staged.")}</p><div className="flex justify-end gap-2"><Button onClick={()=>{reviewedRows.current=undefined;setReview(false);}}>{t("Cancel")}</Button><Button variant="primary" disabled={port.preview||port.enabled===false} onClick={()=>{setReview(false);void submit();}}>{t("Confirm row actions")}</Button></div></div></Dialog>}
 <DataTable key={JSON.stringify([editing,busy])} data={shown} columns={tableColumns as never} selectedIds={selectionSet?.selectedIDs} getRowId={r=>r.id} height={height} searchable={false} onRowClick={editing?undefined:selectionSet||onOpen?choose:undefined} onCellEdit={editing&&!busy&&!review&&port?.enabled!==false?stage:undefined} loading={loading} empty={empty}/>
 {selectionSet&&<div className="grid gap-1 text-xs"><p role="status">{t("{count} records selected in this window",{count:selectionSet.selectedIDs.length})}</p>{selectionSet.status==="pending"&&<p role="status">{t("Checking selected records…")}</p>}{selectionSet.status==="error"&&<p role="alert">{t("Selection read failed")}</p>}{selectionSet.status==="value"&&<ul aria-label={t("Selected records")} className="flex flex-wrap gap-2">{selectionSet.records.map(record=><li key={record.id}><Button size="sm" variant="ghost" onClick={()=>onOpen?.(record)}>{selectionLabel==="id"?record.id:entity.fields[selectionLabel]?.text(record[selectionLabel])??record.id} · {record.id}</Button></li>)}</ul>}</div>}
 </div>;
}
