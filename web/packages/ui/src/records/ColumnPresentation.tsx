import type {ColumnDef} from "@tanstack/react-table";
import {pageUIManifest,type Api} from "@platform/kernel";
import type {Entity} from "../fields/entity";
import {Tag} from "../components/StatusTag";
import type {EntityInfo,EntityRecord} from "./Records";

export type RecordColumnPresentation=Api.PageTableColumn;

/** Decimal strings keep all digits; formatting never changes the editor value. */
const numericText=(value:unknown)=>{
 if(typeof value==="number")return Number.isFinite(value)?value.toLocaleString(undefined,{maximumFractionDigits:20}):"—";
 if(typeof value!=="string"||!/^[-]?\d+(?:\.\d+)?$/.test(value))return "—";
 const [whole,fraction]=value.split("."),separator=new Intl.NumberFormat().formatToParts(1000).find(p=>p.type==="group")?.value??",";
 return whole!.replace(/\B(?=(\d{3})+(?!\d))/g,separator)+(fraction===undefined?"":(new Intl.NumberFormat().formatToParts(1.1).find(p=>p.type==="decimal")?.value??".")+fraction);
};

/** Override visible column presentation while retaining the original field metadata. */
export function presentRecordColumns(columns:ColumnDef<EntityRecord,any>[],entity:Entity<EntityRecord>,info:EntityInfo,presentation:RecordColumnPresentation[]=[]){
 const limits=pageUIManifest.runtime.tablePresentation;
 return columns.map(column=>{
  const p=presentation.find(p=>p.field===column.id);if(!p)return column;
  const field=entity.fields[p.field],type=info.fields.find(f=>f.name===p.field)?.type;
  const formatter=limits.formatters.find(f=>f.id===p.formatter);
  const valid=!!field&&!!type&&(formatter?.fieldTypes as readonly string[]|undefined)?.includes(type);
  const cell:ColumnDef<EntityRecord,any>["cell"]=valid?c=>{
   const value=c.getValue();if(value===undefined||value===null||value==="")return <span className="text-muted">—</span>;
   if(p.formatter==="numeric")return <span className="tabular-nums">{numericText(value)}</span>;
   if(p.formatter==="date")return <span className="tabular-nums">{String(value).slice(0,10)}</span>;
   if(p.formatter==="badge")return field.type==="singleSelect"?field.display(value,c.row.original):<Tag label={field.text(value)}/>;
   return <span>{field.text(value)}</span>;
  }:column.cell;
  return {...column,header:p.title&&new TextEncoder().encode(p.title).length<=limits.maxTitleBytes?p.title:column.header,cell,meta:{...column.meta,width:p.width&&p.width>=limits.minWidth&&p.width<=limits.maxWidth?p.width:column.meta?.width,...(valid&&p.formatter==="numeric"?{align:"right" as const}:{})}};
 });
}
