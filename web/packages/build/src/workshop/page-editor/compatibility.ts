import {pageUIProfile,supportsPageUIProfile,widgetContract,pageVariableContract} from "@platform/app";
import {queryInventoryBudget} from "@platform/app/query-inventory";
import type {Api} from "@platform/kernel";
import type {PageDraft} from "./draft";

export type CompatibilityIssue={section:string;widget:string;version?:number;code:"unknown-widget"|"unsupported-config"};
export type ProfileUpgrade={baseline:string;from:string;to:string};

/** Compatibility discovery cannot grant a renderer or migrate an unknown version. */
export function pageCompatibility(draft:PageDraft){
 const issues:CompatibilityIssue[]=[];
 for(const section of draft.sections){
  const contract=widgetContract(section.widget);
  if(!contract)issues.push({section:section.id??"",widget:section.widget,version:section.configVersion,code:"unknown-widget"});
  else if(section.configVersion!==contract.configVersion)issues.push({section:section.id??"",widget:section.widget,version:section.configVersion,code:"unsupported-config"});
 }
 const supported=draft.document.formatVersion===2&&supportsPageUIProfile(draft.document.uiProfile);
 const current=budget(draft);
 const target=budget({...draft,document:{...draft.document,uiProfile:pageUIProfile}});
 const canUpgrade=supported&&issues.length===0&&draft.document.uiProfile!==pageUIProfile&&target.valid;
 return {from:draft.document.uiProfile,to:pageUIProfile,supported,issues,current,target,
  upgrade:canUpgrade?Object.freeze({baseline:JSON.stringify(draft),from:draft.document.uiProfile,to:pageUIProfile}) satisfies ProfileUpgrade:undefined};
}

function budget(draft:PageDraft){return queryInventoryBudget(draft.document,draft.sections as unknown as Api.Section[],pageVariableContract.query);}

/** Apply only the reviewed profile change to an unchanged local draft. The host
 * still validates bindings and permissions when saving and freezing a candidate. */
export function applyProfileUpgrade(draft:PageDraft,review:ProfileUpgrade):PageDraft|undefined{
 if(review.baseline!==JSON.stringify(draft)||review.from!==draft.document.uiProfile||review.to!==pageUIProfile)return;
 if(!pageCompatibility(draft).upgrade)return;
 const next=structuredClone(draft);next.document.uiProfile=review.to;return next;
}
