import {Input,Select,t} from "@platform/ui";
import {pageVariableContract} from "@platform/app";
import type {AuthoringSection} from "../draft";

export function MetricInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const value=section.metricPresentation??{formatter:"number",variant:"card",tone:"neutral"},limits=pageVariableContract.metricPresentation;
 return <fieldset className="grid gap-2"><legend>{t("Metric presentation")}</legend><Input aria-label={t("Metric prefix")} value={value.prefix??""} maxLength={limits.maxUnitBytes} onChange={e=>onChange({metricPresentation:{...value,prefix:e.target.value||undefined}})}/><Input aria-label={t("Metric suffix")} value={value.suffix??""} maxLength={limits.maxUnitBytes} onChange={e=>onChange({metricPresentation:{...value,suffix:e.target.value||undefined}})}/>{([["formatter",t("Metric format"),limits.formatters],["variant",t("Metric style"),limits.variants],["tone",t("Metric tone"),limits.tones]] as const).map(([key,label,options])=><label key={key} className="grid gap-1 text-xs">{label}<Select value={value[key]} onChange={e=>onChange({metricPresentation:{...value,[key]:e.target.value}})}>{options.map(option=><option key={option} value={option}>{t(option)}</option>)}</Select></label>)}<p className="text-xs text-muted">{t("The original aggregate owns the value. Units and display styles do not compute a trend or change its query.")}</p></fieldset>;
}
