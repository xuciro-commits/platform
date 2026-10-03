import {Select,t} from "@platform/ui";
import type {AuthoringSection} from "../draft";
import {TableInspector,type TableInspectorPorts} from "./TableInspector";

export function TagCountsInspector(props:Omit<TableInspectorPorts,"section"|"onChange">&{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}) {
 const {section,document,info,overlay,onChange}=props;
 return <><TableInspector {...props} widget="tag-counts" showFields={false}/><label className="grid gap-1 text-xs">{t("Tag grouping field")}<Select value={section.group??""} onChange={e=>onChange({group:e.target.value})}><option value="">{t("Choose a field")}</option>{info?.fields.filter(f=>["text","choice"].includes(f.type)&&f.name!=="count"&&!f.name.includes(":")).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label><label className="grid gap-1 text-xs">{t("Tag group filter output")}<Select value={section.groupValueVariable??""} onChange={e=>onChange({groupValueVariable:e.target.value||undefined})}><option value="">{t("No filter output")}</option>{Object.entries(document.variables??{}).filter(([,v])=>v.mode==="state"&&v.type==="string"&&v.scope===(overlay?"overlay":"page")&&v.owner===overlay).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label><p className="text-xs text-muted">{t("Tags show complete authorized scalar group counts. Optional selection writes the original text state; missing groups cannot be selected.")}</p></>;
}
