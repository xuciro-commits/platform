import { Select, groupable, measurable, t } from "@platform/ui";
import type { TableDraft, TableInspectorPorts } from "./TableInspector";
import { AggregateSource } from "./AggregateSource";

type PivotDraft=TableDraft&{group?:string;columnGroup?:string;measure?:string};
export function PivotInspector({section,document,object,info,overlay,onChange}:Omit<TableInspectorPorts,"section"|"onChange">&{section:PivotDraft;onChange:(patch:Partial<PivotDraft>)=>void}) {
  const groups=info?groupable(info):[],measures=info?measurable(info):[];
  return <>
    <AggregateSource section={section} document={document} object={object} info={info} overlay={overlay} onChange={onChange} widget="pivot"/>
    <label className="grid gap-1 text-xs">{t("Rows grouped by")}<Select value={section.group??""} onChange={event=>onChange({group:event.target.value,columnGroup:section.columnGroup===event.target.value?undefined:section.columnGroup})}><option value="">{t("Choose a field")}</option>{groups.map(g=><option key={g.value} value={g.value}>{g.label}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Columns grouped by")}<Select value={section.columnGroup??""} onChange={event=>onChange({columnGroup:event.target.value||undefined})}><option value="">{t("No column grouping")}</option>{groups.filter(g=>g.value!==section.group).map(g=><option key={g.value} value={g.value}>{g.label}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Measure")}<Select value={section.measure??"count"} onChange={event=>onChange({measure:event.target.value})}>{measures.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}</Select></label>
  </>;
}
