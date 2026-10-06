import {Checkbox,Select,t} from "@platform/ui";
import {pageVariableContract} from "@platform/app";
import type {AuthoringSection} from "../draft";

/** Presentation settings share the original detail record and field ports. */
export function DetailInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const value=section.detailPresentation??{columns:1,hideNull:false};
 return <><Checkbox checked={!!value.hideNull} onChange={hideNull=>onChange({detailPresentation:{...value,hideNull}})}>{t("Hide empty properties")}</Checkbox><label className="grid gap-1 text-xs">{t("Property columns")}<Select value={String(value.columns)} onChange={e=>onChange({detailPresentation:{...value,columns:Number(e.target.value)}})}>{Array.from({length:pageVariableContract.detailPresentation.maxColumns},(_,i)=><option key={i} value={i+1}>{i+1}</option>)}</Select></label><p className="text-xs text-muted">{t("Empty means missing, null or empty text. Zero and false stay visible.")}</p></>;
}
