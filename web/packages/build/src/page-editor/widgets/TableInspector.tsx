import { Checkbox, Select, Toggles, t, type EntityInfo } from "@platform/ui";
import { tableEditableFields,useHost,widgetContract } from "@platform/app";
import type { Api } from "@platform/kernel";
import { variableAccessible } from "../../page-layout";

export type TableDraft = { inlineEdit?:{action:string;fields:string[]};collectionVariable?:string; selectionVariable?:string; fields?:string[]; selection?:string; object?:string; filterVariable?:string; query?:string; parentSelection?:string; relation?:string };
export type TableInspectorPorts = { section:TableDraft; document:Api.PageDocument; object:string; info?:EntityInfo; overlay?:string; itemOwner?:string; onChange:(patch:Partial<TableDraft>)=>void };

/** Only table-specific ports; common object/property controls stay shared. */
export function TableInspector({section,document,object,info,overlay,itemOwner,onChange,widget="table",showFields=true}:TableInspectorPorts&{widget?:"table"|"record-timeline"|"kanban";showFields?:boolean}) {
  const {catalog}=useHost(),edit=catalog.find(a=>a.schema===(section.object||object)+".edit"),editable=tableEditableFields(info,edit,section.fields??[]);
  const contract=widgetContract(widget)!;
  const collection=contract.inputPorts.find(p=>p.bindingField==="collectionVariable")!;
  const selection=contract.outputPorts.find(p=>p.bindingField==="selectionVariable")!;
  return <>
    {!overlay&&!itemOwner&&<label className="grid gap-1 text-xs">{t("Shared record selection")}<Select value={section.selectionVariable??""} onChange={event=>onChange({selectionVariable:event.target.value||undefined,selection:undefined})}><option value="">{t("Keep selection in this page")}</option>{Object.entries(document.variables??{}).filter(([,v])=>v.type===selection.type&&v.mode==="shared"&&v.writable&&v.source?.object?.name===(section.object||object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
    <label className="grid gap-1 text-xs">{t(widget==="table"?"Table query window":widget==="kanban"?"Kanban query window":"Timeline query window")}<Select value={section.collectionVariable??""} onChange={event=>{
      const variable=document.variables?.[event.target.value],query=variable?.source?.query?document.queries?.[variable.source.query]:undefined,target=query?.object||variable?.source?.object;
      onChange({collectionVariable:event.target.value||undefined,filterVariable:undefined,query:undefined,parentSelection:undefined,relation:undefined,...(target?{object:target.name===object?undefined:target.name}:{})});
    }}><option value="">{t(widget==="table"?"Use the table's own query":"Choose a query window")}</option>{Object.entries(document.variables??{}).filter(([,v])=>variableAccessible(v,undefined,overlay)&&v.type===collection.type&&(v.source?.kind==="plan"||v.mode==="shared"&&!!v.source?.object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>
    {showFields&&<fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Fields it shows")}</legend><Toggles options={(info?.fields??[]).map(f=>({value:f.name,label:f.title}))} value={section.fields??[]} onChange={fields=>onChange({fields})}/></fieldset>}
    {widget==="table"&&<><Checkbox checked={!!section.inlineEdit} onChange={checked=>onChange({inlineEdit:checked?{action:"",fields:[]}:undefined})}>{t("Enable cell editing")}</Checkbox>{section.inlineEdit&&<><label className="grid gap-1 text-xs">{t("Original table edit action")}<Select value={section.inlineEdit.action} onChange={e=>onChange({inlineEdit:{action:e.target.value,fields:[]}})}><option value="">{t("Choose the original edit action")}</option>{edit&&editable.length>0&&<option value={edit.schema}>{edit.title}</option>}</Select></label><fieldset className="grid gap-1 text-xs"><legend>{t("Editable table fields")}</legend><Toggles options={editable.map(name=>({value:name,label:info?.fields.find(f=>f.name===name)?.title??name}))} value={section.inlineEdit.fields} onChange={fields=>onChange({inlineEdit:{...section.inlineEdit!,fields}})}/></fieldset><p className="text-xs text-muted">{t("Cell edits use the original field patch action and opened record revision. Failed rows remain staged.")}</p></>}</>}
  </>;
}
