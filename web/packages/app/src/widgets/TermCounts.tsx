import {useMemo} from "react";
import {Panel,TermCounts,useChartData,t,type ChartSource,type ChartSpec,type EntityInfo} from "@platform/ui";
import {pageUIManifest} from "@platform/kernel";
import type {QueryWindow} from "./QueryWindowFrame";
import {termCounts} from "./terms";
export function TermCountsRenderer({object,window,field,info,source,label}:{object:string;window?:QueryWindow;field:string;info?:EntityInfo;source?:ChartSource;label:string}){
 const descriptor=info?.fields.find(f=>f.name===field);
 if(!window||window.error)return <Panel role={window?.error?"alert":"status"}>{t(window?.error??"Query window is unavailable.")}</Panel>;
 if(!descriptor||!["text","choice"].includes(descriptor.type)||field==="count"||field.includes(":"))return <Panel role="alert">{t("Term count field is unavailable or incompatible.")}</Panel>;
 const {domain,search,set,archived,traversal}=window.query,spec:ChartSpec={data:{entity:object,domain,search,set,archived,traversal},mark:"bar",encoding:{x:{field,type:"nominal"},y:{aggregate:"count",type:"quantitative"}}};
 return <CompleteTerms spec={spec} source={source} field={field} label={label}/>;
}
function CompleteTerms({spec,source,field,label}:{spec:ChartSpec;source?:ChartSource;field:string;label:string}){
 const reader=useMemo(()=>source&&({...source,aggregate:(object:string,query:Parameters<ChartSource["aggregate"]>[1])=>source.aggregate(object,{...query,maxRows:pageUIManifest.runtime.terms.maxGroups})}),[source]);
 const {data,error}=useChartData(spec,reader);if(error)return <Panel role="alert">{t("Term counts could not be loaded.")}</Panel>;
 if(!data)return <p role="status">{t("Loading term counts…")}</p>;
 const terms=termCounts(data,field);if(!terms)return <Panel role="alert">{t("Term counts are invalid or exceed their budget.")}</Panel>;
 return <div className="grid min-w-0 gap-2"><p className="text-xs text-muted">{t("Counts cover all matching authorized records; ties keep host group order.")}</p><TermCounts terms={terms} label={label}/></div>;
}
