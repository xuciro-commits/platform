import {Chart,type ChartSpec,type AggregateQuery,t} from "@platform/ui";
import {useMemo} from "react";
import {checkPieData} from "./chart-spec";

export function ChartRenderer({spec,source,height=240}:{spec:ChartSpec;source?:Parameters<typeof Chart>[0]["source"];height?:number}) {
 const reader=useMemo(()=>source&&({...source,aggregate:async(object:string,query:AggregateQuery)=>{
  const data=await source.aggregate(object,{...query,maxRows:4096});
  if(!checkPieData(spec,data))throw new Error(t("Pie values must be finite nonnegative quantities without mixed currencies."));
  return data;
 }}),[source,spec]);
 return <Chart spec={spec} source={reader} height={height} frame={false}/>;
}
