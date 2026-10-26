import {Toggles,t} from "@platform/ui";
import {pageVariableContract} from "@platform/app";
import type {AuthoringSection} from "../draft";

export function RecordViewInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const titles:Record<string,string>={overview:t("Overview"),properties:t("Properties"),links:t("Links"),history:t("History")};
 return <fieldset className="grid gap-1 text-xs"><legend>{t("Record tabs")}</legend><Toggles options={pageVariableContract.recordView.tabs.map(value=>({value,label:titles[value]!}))} value={section.recordView?.tabs??[...pageVariableContract.recordView.tabs]} onChange={tabs=>onChange({recordView:{tabs}})}/></fieldset>;
}
