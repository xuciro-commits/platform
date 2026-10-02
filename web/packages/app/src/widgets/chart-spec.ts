import type { AggregateData, ChartSpec, Encoding, Mark } from "@platform/ui";

export function compileChartSpec({object,title,group,measure="count",mark="bar",chartVariant,kpi=false,domain=[]}:{object:string;title?:string;group?:string;measure?:string;mark?:string;chartVariant?:string;kpi?:boolean;domain?:unknown[]}):ChartSpec {
 const [op,field]=measure.split(":"),[by,unit]= (group??"").split(":");
 const value:Encoding={field,type:"quantitative",aggregate:op as Encoding["aggregate"]},category:Encoding={field:by,type:unit?"temporal":"nominal",timeUnit:unit as Encoding["timeUnit"]};
 return {title:kpi?title:undefined,data:{entity:object,domain},mark:kpi?"kpi":mark==="arc"&&chartVariant?{type:"arc",donut:chartVariant==="donut",showValues:true}:mark as Mark,encoding:kpi?{y:value}:mark==="arc"?{theta:value,color:category}:{x:category,y:value}};
}

/** Shares require finite, nonnegative quantities in a single compatible unit. */
export function checkPieData(spec:ChartSpec,data:AggregateData):boolean {
 const mark=typeof spec.mark==="string"?spec.mark:spec.mark.type;
 if(mark!=="arc")return true;
 const value=spec.encoding.theta??spec.encoding.y,op=value?.aggregate;
 if(op!=="count"&&op!=="sum"||data.columns.some(c=>c.kind==="measure"&&c.money))return false;
 const column=op==="count"?"count":`${op}:${value?.field??""}`;
 return data.rows.every(row=>typeof row[column]==="number"&&Number.isFinite(row[column])&&Number(row[column])>=0);
}
