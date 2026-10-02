import {expect,test} from "vitest";
import {recordChartSpec} from "./RecordChart";
import {aggregateQuery,aggregateValues} from "../charts/spec";
import {toOption} from "../charts/echarts";
import type {EntityInfo,EntityRecord} from "./Records";

const info={type:"sample.item",title:"Item",plural:"Items",fields:[{name:"name",title:"Name",type:"text"},{name:"qty",title:"Quantity",type:"decimal"}]} as EntityInfo;
const row=(id:string,qty:unknown):EntityRecord=>({id,revision:1,created:{by:"test",at:"2026-10-02T00:00:00Z"},changed:{by:"test",at:"2026-10-02T00:00:00Z"},name:"Same label",qty});
const fields={mark:"line" as const,xField:"name",yField:"qty"};
test("record charts keep duplicate labels, original order and stable identities instead of aggregating or dropping points",()=>{
 const records=[row("B",2),row("A",1)],spec=recordChartSpec(records,info,fields,40)!;expect(aggregateQuery(spec)).toBeUndefined();const values="values" in spec.data?spec.data.values:[];expect(aggregateValues(spec,values)).toEqual([{id:"B",x:"Same label",y:2},{id:"A",x:"Same label",y:1}]);
 const option=toOption(spec,values,[],{palette:["blue"],foreground:"black",muted:"gray",border:"gray"}) as {xAxis:{data:string[]};series:{type:string;data:unknown[]}[]};expect(option.xAxis.data).toEqual(["Same label","Same label"]);expect(option.series[0]!.type).toBe("line");expect(option.series[0]!.data).toEqual([{id:"B",name:"Same label",value:2},{id:"A",name:"Same label",value:1}]);
 const changed=recordChartSpec(records,info,{...fields,mark:"bar",xField:"id"},40)!;expect("values" in changed.data&&changed.data.values.map(v=>v.x)).toEqual(["B","A"]);expect(changed.mark).toBe("bar");
});
test("the explicit point budget clips only the given window, and hidden or invalid fields and values reject the chart",()=>{
 const records=Array.from({length:42},(_,i)=>row(String(i),i)),spec=recordChartSpec(records,info,fields,40)!;expect("values" in spec.data&&spec.data.values.length).toBe(40);expect(recordChartSpec(records,info,fields,41)).toBeUndefined();expect(recordChartSpec(records,{...info,fields:info.fields.filter(f=>f.name!=="qty")},fields,40)).toBeUndefined();expect(recordChartSpec(records,info,{...fields,xField:"private"},40)).toBeUndefined();expect(recordChartSpec([row("A",Infinity)],info,fields,40)).toBeUndefined();expect(recordChartSpec([row("A",1),row("A",2)],info,fields,40)).toBeUndefined();
});
