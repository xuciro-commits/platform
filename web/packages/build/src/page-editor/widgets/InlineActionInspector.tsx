import {useHost} from "@platform/app";
import {Select,t} from "@platform/ui";

export function InlineActionInspector({section,object,onChange}:{section:{object?:string;actions?:string[]};object:string;onChange:(patch:{actions:string[]})=>void}) {
 const {catalog}=useHost(),type=section.object||object;
 return <><label className="grid gap-1 text-xs">{t("Inline record action")}<Select aria-label={t("Inline record action")} value={section.actions?.[0]??""} onChange={e=>onChange({actions:e.target.value?[e.target.value]:[]})}><option value="">{t("Choose one record action")}</option>{catalog.filter(a=>a.target===type&&!a.new).map(a=><option key={a.schema} value={a.schema}>{a.title}</option>)}</Select></label><p className="text-xs text-muted">{t("The inline form uses the original action inputs, permissions, approvals and record revision.")}</p></>;
}
