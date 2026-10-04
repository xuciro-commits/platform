import {Input,t} from "@platform/ui";
import type {AuthoringSection} from "../draft";
import type {Api} from "@platform/kernel";
export function ExternalFrameInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}) {
 const c=section.externalFrame;
 const update=(patch:Partial<Api.PageExternalFrame>)=>onChange({externalFrame:{url:"",origin:"",...c,...patch}});
 return <><label className="grid gap-1 text-xs">{t("External document URL")}<Input value={c?.url??""} maxLength={2048} onChange={e=>update({url:e.target.value})}/></label>
 <label className="grid gap-1 text-xs">{t("Reviewed external origin")}<Input value={c?.origin??""} onChange={e=>update({origin:e.target.value})}/></label>
 <label className="grid gap-1 text-xs">{t("External document height")}<Input type="number" min={1} max={4096} value={c?.height??240} onChange={e=>update({height:Number(e.target.value)})}/></label>
 <p className="text-xs text-muted">{t("Isolated external document. Scripts, forms and platform data access are disabled.")}</p></>;
}
