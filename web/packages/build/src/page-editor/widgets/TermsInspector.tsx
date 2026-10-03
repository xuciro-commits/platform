import {Select,t} from "@platform/ui";
import {TableInspector,type TableInspectorPorts} from "./TableInspector";
import type {AuthoringSection} from "../draft";
export function TermsInspector(props:Omit<TableInspectorPorts,"section"|"onChange">&{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const {section,info,onChange}=props;return <><TableInspector {...props} widget="term-counts" showFields={false}/><label className="grid gap-1 text-xs">{t("Term count field")}<Select value={section.group??""} onChange={e=>onChange({group:e.target.value})}><option value="">{t("Choose a field")}</option>{info?.fields.filter(f=>["text","choice"].includes(f.type)&&f.name!=="count"&&!f.name.includes(":")).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label><p className="text-xs text-muted">{t("Read complete authorized group counts. Record window paging does not limit the totals; excess groups are refused.")}</p></>;
}
