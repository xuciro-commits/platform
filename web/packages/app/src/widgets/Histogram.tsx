import {pageUIManifest} from "../pageui/manifest";
import type * as PageUI from "../pageui/types";
import {useMemo} from "react";
import {ContextHelp,Histogram,Panel,useChartData,t,validHistogram,type ChartSource,type ChartSpec,type EntityInfo} from "@platform/ui";
import type {QueryWindow} from "./QueryWindowFrame";
export function HistogramRenderer({object,window,fields,info,source,label}:{object:string;window?:QueryWindow;fields?:PageUI.PageHistogram;info?:EntityInfo;source?:ChartSource;label:string}){
 if(!window||window.error)return <Panel role={window?.error?"alert":"status"}>{t(window?.error??"Query window is unavailable.")}</Panel>;
 const field=info?.fields.find(f=>f.name===fields?.field);if(!fields||!field||!["integer","decimal"].includes(field.type)||!Number.isInteger(fields.bins)||fields.bins<1||fields.bins>pageUIManifest.runtime.histogram.maxBins)return <Panel role="alert">{t("Histogram field or bins are unavailable or incompatible.")}</Panel>;
 const {domain,search,set,archived,traversal}=window.query,spec:ChartSpec={data:{entity:object,domain,search,set,archived,traversal},mark:"bar",encoding:{x:{field:fields.field,type:"quantitative"},y:{aggregate:"count",type:"quantitative"}}};return <CompleteHistogram spec={spec} source={source} fields={fields} label={label}/>;
}
function CompleteHistogram({spec,source,fields,label}:{spec:ChartSpec;source?:ChartSource;fields:PageUI.PageHistogram;label:string}){
 const reader=useMemo(()=>source&&({...source,scope:JSON.stringify([source.scope,fields]),watchAggregate:source.watchAggregate?((object:string,query:Parameters<ChartSource["aggregate"]>[1],changed:()=>void)=>{const {domain,search,set,archived,traversal}=query;return source.watchAggregate!(object,{domain,search,set,archived,traversal,histogram:fields,maxRows:pageUIManifest.runtime.histogram.maxBins},changed);}):undefined,aggregate:(object:string,query:Parameters<ChartSource["aggregate"]>[1])=>{const {domain,search,set,archived,traversal}=query;return source.aggregate(object,{domain,search,set,archived,traversal,histogram:fields,maxRows:pageUIManifest.runtime.histogram.maxBins});}}),[source,fields.field,fields.bins]);const {data,error}=useChartData(spec,reader);if(error)return <Panel role="alert">{t("Histogram could not be loaded.")}</Panel>;if(!data)return <p role="status">{t("Loading histogram…")}</p>;if(data.columns.length||data.rows.length||!validHistogram(data.histogram,fields.field,fields.bins))return <Panel role="alert">{t("Histogram result is invalid or incomplete.")}</Panel>;
 return <div className="grid min-w-0 gap-2"><ContextHelp title={label||t("Histogram")}>{t("Distribution covers all matching authorized records.")}</ContextHelp><Histogram value={data.histogram} label={label}/></div>;
}
