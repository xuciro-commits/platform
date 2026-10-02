import {Select,groupable,measurable,t} from "@platform/ui";
import {widgetContract} from "@platform/app";
import type {TableDraft,TableInspectorPorts} from "./TableInspector";
import {AggregateSource} from "./AggregateSource";

type ChartDraft=TableDraft&{mark?:string;group?:string;measure?:string};
const titles:Record<string,string>={bar:"Bar chart",line:"Line chart",area:"Area chart",arc:"Pie chart"};
export function ChartInspector(props:Omit<TableInspectorPorts,"section"|"onChange">&{section:ChartDraft;onChange:(patch:Partial<ChartDraft>)=>void}) {
 const {section,info,onChange}=props,mark=section.mark??"bar",trend=mark==="line"||mark==="area";
 const groups=(info?groupable(info):[]).filter(g=>!trend||g.value.includes(":")&&(["created","changed"].includes(g.value.split(":")[0]!)||info?.fields.some(f=>f.name===g.value.split(":")[0]&&["date","datetime"].includes(f.type))));
 const measures=(info?measurable(info):[]).filter(m=>mark!=="arc"||m.value==="count"||m.value.startsWith("sum:")&&info?.fields.find(f=>f.name===m.value.slice(4))?.type!=="money");
 const contract=widgetContract("chart")!;
 const marks=contract.componentID==="chart"?contract.propsSchema.properties.mark.enum:[];
 return <><AggregateSource {...props} widget="chart"/>
  <label className="grid gap-1 text-xs">{t("Chart type")}<Select value={mark} onChange={e=>onChange({mark:e.target.value})}>{marks.map(m=><option key={m} value={m}>{t(titles[m]!)}</option>)}</Select></label>
  <label className="grid gap-1 text-xs">{t("Grouped by")}<Select value={section.group??""} onChange={e=>onChange({group:e.target.value})}><option value="">{t("Choose a field")}</option>{section.group&&!groups.some(g=>g.value===section.group)&&<option value={section.group}>{t("Unavailable field")}</option>}{groups.map(g=><option key={g.value} value={g.value}>{g.label}</option>)}</Select></label>
  <label className="grid gap-1 text-xs">{t("Measure")}<Select value={section.measure??"count"} onChange={e=>onChange({measure:e.target.value})}>{section.measure&&!measures.some(m=>m.value===section.measure)&&<option value={section.measure}>{t("Unavailable measure")}</option>}{measures.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}</Select></label>
 </>;
}
