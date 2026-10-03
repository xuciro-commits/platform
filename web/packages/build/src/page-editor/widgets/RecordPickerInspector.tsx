import {Input,Select,t} from "@platform/ui";
import {TableInspector,type TableInspectorPorts} from "./TableInspector";
import type {AuthoringSection} from "../draft";
export function RecordPickerInspector(props:Omit<TableInspectorPorts,"section"|"onChange">&{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const {section,info,onChange}=props,fields=section.recordPicker??{labelField:"id"},update=(patch:Partial<typeof fields>)=>onChange({recordPicker:{...fields,...patch}});
 return <><TableInspector {...props} widget="record-picker" showFields={false}/><label className="grid gap-1 text-xs">{t("Picker title field")}<Select value={fields.labelField} onChange={e=>update({labelField:e.target.value})}><option value="id">{t("Record ID")}</option>{info?.fields.filter(f=>["text","longtext","choice","reference"].includes(f.type)).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label><label className="grid gap-1 text-xs">{t("Picker label")}<Input maxLength={1024} value={fields.label??""} onChange={e=>update({label:e.target.value})}/></label><p className="text-xs text-muted">{t("Bind an original 20-record ID-sorted query. Search keeps its conditions; selection uses original record authorization.")}</p></>;
}
