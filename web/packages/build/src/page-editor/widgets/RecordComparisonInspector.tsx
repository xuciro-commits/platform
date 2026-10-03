import {Select,Toggles,t} from "@platform/ui";
import type {AuthoringSection} from "../draft";
import type {TableInspectorPorts} from "./TableInspector";

export function RecordComparisonInspector({section,info,onChange}:Omit<TableInspectorPorts,"section"|"onChange">&{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}) {
 const c=section.recordComparison??{labelField:"id"};
 return <><label className="grid gap-1 text-xs">{t("Comparison title field")}<Select value={c.labelField} onChange={e=>onChange({recordComparison:{labelField:e.target.value}})}><option value="id">{t("Record ID")}</option>{info?.fields.filter(f=>["text","longtext","choice","reference"].includes(f.type)).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label><fieldset className="grid gap-1 text-xs"><legend>{t("Comparison fields")}</legend><Toggles options={(info?.fields??[]).filter(f=>["text","longtext","choice","reference","integer","decimal","money","date","datetime","boolean"].includes(f.type)).map(f=>({value:f.name,label:f.title}))} value={section.fields??[]} onChange={fields=>onChange({fields})}/></fieldset><p className="text-xs text-muted">{t("Compare 2–4 original confirmed records. Select up to 64 scalar fields; differences use field types and values, and formatting stays with the original field owner.")}</p></>;
}
