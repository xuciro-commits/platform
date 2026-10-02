import { Select, Toggles, t, type EntityInfo } from "@platform/ui";
import { widgetContract } from "@platform/app";
import type { Api } from "@platform/kernel";
import { variableAccessible } from "../../page-layout";

export type TableDraft = { collectionVariable?:string; selectionVariable?:string; fields?:string[]; selection?:string; object?:string; filterVariable?:string; query?:string; parentSelection?:string; relation?:string };
export type TableInspectorPorts = { section:TableDraft; document:Api.PageDocument; object:string; info?:EntityInfo; overlay?:string; itemOwner?:string; onChange:(patch:Partial<TableDraft>)=>void };

/** Only table-specific ports; common object/property controls stay shared. */
export function TableInspector({section,document,object,info,overlay,itemOwner,onChange}:TableInspectorPorts) {
  const contract=widgetContract("table")!;
  const collection=contract.inputPorts.find(p=>p.bindingField==="collectionVariable")!;
  const selection=contract.outputPorts.find(p=>p.bindingField==="selectionVariable")!;
  return <>
    {!overlay&&!itemOwner&&<label className="grid gap-1 text-xs">{t("Shared record selection")}<Select value={section.selectionVariable??""} onChange={event=>onChange({selectionVariable:event.target.value||undefined,selection:undefined})}><option value="">{t("Keep selection in this page")}</option>{Object.entries(document.variables??{}).filter(([,v])=>v.type===selection.type&&v.mode==="shared"&&v.writable&&v.source?.object?.name===(section.object||object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
    <label className="grid gap-1 text-xs">{t("Table query window")}<Select value={section.collectionVariable??""} onChange={event=>{
      const variable=document.variables?.[event.target.value],query=variable?.source?.query?document.queries?.[variable.source.query]:undefined,target=query?.object||variable?.source?.object;
      onChange({collectionVariable:event.target.value||undefined,filterVariable:undefined,query:undefined,parentSelection:undefined,relation:undefined,...(target?{object:target.name===object?undefined:target.name}:{})});
    }}><option value="">{t("Use the table's own query")}</option>{Object.entries(document.variables??{}).filter(([,v])=>variableAccessible(v,undefined,overlay)&&v.type===collection.type&&(v.source?.kind==="plan"||v.mode==="shared"&&!!v.source?.object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>
    <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Fields it shows")}</legend><Toggles options={(info?.fields??[]).map(f=>({value:f.name,label:f.title}))} value={section.fields??[]} onChange={fields=>onChange({fields})}/></fieldset>
  </>;
}
