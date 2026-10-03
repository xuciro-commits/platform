import {Checkbox,Input,t} from "@platform/ui";
import {pageVariableContract} from "@platform/app";
import type {AuthoringSection} from "../draft";
export function SeparatorInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const c=section.separator??{};
 return <><Checkbox checked={c.label!==undefined} onChange={checked=>onChange({separator:{label:checked?"":undefined}})}>{t("Show separator label")}</Checkbox>{c.label!==undefined&&<label className="grid gap-1 text-xs">{t("Separator label")}<Input maxLength={pageVariableContract.separator.maxLabelBytes} value={c.label} onChange={e=>onChange({separator:{label:e.target.value}})}/></label>}<p className="text-xs text-muted">{t("Use a horizontal semantic separator for operator groups. Empty or absent labels stay visually empty; the widget name preserves accessible identity.")}</p></>;
}
