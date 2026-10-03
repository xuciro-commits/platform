import {Input,Select,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {variableAccessible} from "../../page-layout";
import type {AuthoringSection} from "../draft";

export function BooleanInspector({section,document,overlay,itemOwner,onChange}:{
  section:AuthoringSection;document:Api.PageDocument;overlay?:string;itemOwner?:string;onChange:(patch:Partial<AuthoringSection>)=>void;
}) {
  return <>
    <label className="grid gap-1 text-xs">{t("Boolean state variable")}<Select value={section.booleanVariable??""} onChange={e=>onChange({booleanVariable:e.target.value||undefined})}>
      <option value="">{t("Choose a boolean state variable")}</option>
      {Object.entries(document.variables??{}).filter(([,v])=>!itemOwner&&v.type==="boolean"&&v.mode==="state"&&["page","overlay"].includes(v.scope)&&variableAccessible(v,undefined,overlay)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}
    </Select></label>
    <label className="grid gap-1 text-xs">{t("Switch label")}<Input maxLength={1024} value={section.booleanLabel??section.title??""} onChange={e=>onChange({booleanLabel:e.target.value})}/></label>
    <p className="text-xs text-muted">{t("The switch writes the original boolean state. Visibility, enabled conditions and queries consume that same value.")}</p>
  </>;
}
