import {Select,Toggles,t,type EntityInfo} from "@platform/ui";
import type {AuthoringSection} from "../draft";

export function StatusTrackerInspector({section,info,onChange}:{section:AuthoringSection;info?:EntityInfo;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const l=info?.lifecycle,field=info?.fields.find(f=>f.name===l?.field);
 if(!l||!field)return <p role="alert" className="text-xs text-danger">{t("Choose an object with an original lifecycle.")}</p>;
 return <><label className="grid gap-1 text-xs">{t("Lifecycle field")}<Select value={section.statusTracker?.field??""} onChange={e=>onChange({statusTracker:e.target.value?{field:l.field,stages:l.states.map(s=>s.name)}:undefined})}><option value="">{t("Choose the original lifecycle field")}</option><option value={l.field}>{field.title}</option></Select></label><fieldset className="grid gap-1 text-xs"><legend>{t("Displayed stages")}</legend><Toggles options={l.states.map(s=>({value:s.name,label:s.title}))} value={section.statusTracker?.stages??[]} onChange={stages=>onChange({statusTracker:{field:l.field,stages}})}/></fieldset><p className="text-xs text-muted">{t("The current state comes from the original record. Stage order does not prove past completion or run a transition.")}</p></>;
}
