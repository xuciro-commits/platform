import {Checkbox,Select,t} from "@platform/ui";
import {pageVariableContract,useHost} from "@platform/app";
import type {AuthoringSection} from "../draft";

/** Presentation settings share the original detail record and field ports. */
export function DetailInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const value=section.detailPresentation??{columns:1,hideNull:false};
 const codes=(useHost().entities.find(e=>e.type===section.object)?.fields??[]).filter(f=>f.type==="text").map(f=>f.name);
 return <><Checkbox checked={!!value.hideNull} onChange={hideNull=>onChange({detailPresentation:{...value,hideNull}})}>{t("Hide empty properties")}</Checkbox><label className="grid gap-1 text-xs">{t("Property columns")}<Select value={String(value.columns)} onChange={e=>onChange({detailPresentation:{...value,columns:Number(e.target.value)}})}>{Array.from({length:pageVariableContract.detailPresentation.maxColumns},(_,i)=><option key={i} value={i+1}>{i+1}</option>)}</Select></label><p className="text-xs text-muted">{t("Empty means missing, null or empty text. Zero and false stay visible.")}</p>
 <label className="grid gap-1 text-xs">{t("Barcode")}<Select value={value.barcode??""} onChange={e=>onChange({detailPresentation:{...value,barcode:e.target.value||undefined}})}><option value="">{t("None")}</option><option value="id">{t("Record ID")}</option>{codes.map(name=><option key={name} value={name}>{name}</option>)}</Select></label>
 <Checkbox checked={!!value.print} onChange={print=>onChange({detailPresentation:{...value,print:print||undefined}})}>{t("Offer printing as a label")}</Checkbox>
 <p className="text-xs text-muted">{t("The barcode is Code 128 over a text field; printing prints only this detail.")}</p></>;
}
