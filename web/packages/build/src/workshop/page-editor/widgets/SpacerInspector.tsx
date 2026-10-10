import {Input,t} from "@platform/ui";
import {pageUIManifest} from "@platform/kernel";
import type {AuthoringSection} from "../draft";
export function SpacerInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const value=section.spacer?.size;
 return <><label className="grid gap-1 text-xs">{t("Blank region size (px)")}<Input draftKey="blank-region-size-px" optional={false} type="number" min={0} max={pageUIManifest.layout.maxSize} step="any" value={typeof value==="number"&&Number.isFinite(value)?value:""} onChange={e=>onChange({spacer:{size:e.target.valueAsNumber}})}/></label><p className="text-xs text-muted">{t("The size controls this blank region only. Zero and fractions are preserved; parent container gaps are configured separately. Blank space has no accessible content or focus target.")}</p></>;
}
