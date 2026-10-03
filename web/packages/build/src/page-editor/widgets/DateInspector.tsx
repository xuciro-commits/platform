import {Input,Select,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {variableAccessible} from "../../page-layout";
import type {AuthoringSection} from "../draft";
export function DateInspector({section,document,overlay,itemOwner,onChange}:{section:AuthoringSection;document:Api.PageDocument;overlay?:string;itemOwner?:string;onChange:(patch:Partial<AuthoringSection>)=>void}){
 return <><label className="grid gap-1 text-xs">{t("Date state variable")}<Select value={section.dateVariable??""} onChange={e=>onChange({dateVariable:e.target.value||undefined})}><option value="">{t("Choose a text state variable")}</option>{Object.entries(document.variables??{}).filter(([,v])=>!itemOwner&&v.type==="string"&&v.mode==="state"&&["page","overlay"].includes(v.scope)&&variableAccessible(v,undefined,overlay)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label><label className="grid gap-1 text-xs">{t("Date label")}<Input maxLength={1024} value={section.dateLabel??""} onChange={e=>onChange({dateLabel:e.target.value})}/></label><p className="text-xs text-muted">{t("Bind the original civil date text. Empty and invalid drafts remain explicit; queries validate dates before reading.")}</p></>;
}
