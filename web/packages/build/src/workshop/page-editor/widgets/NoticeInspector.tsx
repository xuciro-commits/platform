import {Checkbox,Input,Select,Textarea,t} from "@platform/ui";
import {pageVariableContract} from "@platform/app";
import type {Api} from "@platform/kernel";
import type {AuthoringSection} from "../draft";
export function NoticeInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const c=section.notice??{tone:"info",message:""},limits=pageVariableContract.notice,patch=(value:Partial<Api.PageNotice>)=>onChange({notice:{...c,...value}});
 return <><Checkbox checked={c.title!==undefined} onChange={checked=>patch({title:checked?"":undefined})}>{t("Show notice title")}</Checkbox>{c.title!==undefined&&<label className="grid gap-1 text-xs">{t("Notice title")}<Input maxLength={limits.maxTitleBytes} value={c.title} onChange={e=>patch({title:e.target.value})}/></label>}<label className="grid gap-1 text-xs">{t("Notice text")}<Textarea maxLength={limits.maxMessageBytes} value={c.message} onChange={e=>patch({message:e.target.value})}/></label><label className="grid gap-1 text-xs">{t("Notice tone")}<Select value={c.tone} onChange={e=>patch({tone:e.target.value})}>{limits.tones.map(tone=><option key={tone} value={tone}>{t(tone)}</option>)}</Select></label><p className="text-xs text-muted">{t("Use plain text for operator instructions. HTML and placeholders remain literal; a static note performs no reads or actions.")}</p></>;
}
