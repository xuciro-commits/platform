import { Select, groupable, measurable, t } from "@platform/ui";
import type { TableDraft, TableInspectorPorts } from "./TableInspector";
import type { Api } from "@platform/kernel";
import { variableAccessible } from "../../page-layout";

type PivotDraft=TableDraft&{group?:string;columnGroup?:string;measure?:string};
export function PivotInspector({section,document,object,info,overlay,onChange}:Omit<TableInspectorPorts,"section"|"onChange">&{section:PivotDraft;onChange:(patch:Partial<PivotDraft>)=>void}) {
  const groups=info?groupable(info):[],measures=info?measurable(info):[];
  return <>
    <label className="grid gap-1 text-xs">{t("Aggregate query set")}<Select value={section.collectionVariable??""} onChange={event=>{
      const v=document.variables?.[event.target.value],target=v?.source?.query?document.queries?.[v.source.query]?.object:v?.source?.object;
      onChange({collectionVariable:event.target.value||undefined,filterVariable:undefined,query:undefined,parentSelection:undefined,relation:undefined,...(target?{object:target.name===object?undefined:target.name}:{} )});
    }}><option value="">{t("Use the widget's own aggregate")}</option>{Object.entries(document.variables??{}).filter(([,v]:[string,Api.PageVariable])=>variableAccessible(v,undefined,overlay)&&v.type==="object-set"&&(v.source?.kind==="plan"||v.mode==="shared"&&!!v.source?.object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Rows grouped by")}<Select value={section.group??""} onChange={event=>onChange({group:event.target.value,columnGroup:section.columnGroup===event.target.value?undefined:section.columnGroup})}><option value="">{t("Choose a field")}</option>{groups.map(g=><option key={g.value} value={g.value}>{g.label}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Columns grouped by")}<Select value={section.columnGroup??""} onChange={event=>onChange({columnGroup:event.target.value||undefined})}><option value="">{t("No column grouping")}</option>{groups.filter(g=>g.value!==section.group).map(g=><option key={g.value} value={g.value}>{g.label}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Measure")}<Select value={section.measure??"count"} onChange={event=>onChange({measure:event.target.value})}>{measures.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}</Select></label>
  </>;
}
