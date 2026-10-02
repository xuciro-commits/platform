import {act,cleanup,render,screen,waitFor} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {Chart} from "./Chart";
import type {AggregateData,AggregateQuery,ChartSpec} from "./spec";
afterEach(cleanup);
const deferred=()=>{let resolve!:(data:AggregateData)=>void,reject!:(error:Error)=>void;const promise=new Promise<AggregateData>((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject};};
const spec=(search:string):ChartSpec=>({data:{entity:"sample.note",search,set:{op:"union",inputs:[{},{}]}},mark:"kpi",encoding:{y:{aggregate:"count",type:"quantitative"}}});
const data=(count:number):AggregateData=>({columns:[{name:"count",title:"Count",kind:"measure",type:"quantitative"}],rows:[{count}]});
test("chart scope and parameter changes immediately hide previous data and drop delayed or refused aggregates",async()=>{
 const requests:ReturnType<typeof deferred>[]=[];const aggregate=()=>{const pending=deferred();requests.push(pending);return pending.promise};
 const {rerender}=render(<Chart spec={spec("A")} source={{aggregate,scope:"member:one"}}/>);
 await waitFor(()=>expect(requests).toHaveLength(1));await act(async()=>requests[0]!.resolve(data(620)));expect(screen.getByText("620")).toBeTruthy();
 rerender(<Chart spec={spec("B")} source={{aggregate,scope:"member:one"}}/>);expect(screen.queryByText("620")).toBeNull();await waitFor(()=>expect(requests).toHaveLength(2));
 rerender(<Chart spec={spec("B")} source={{aggregate,scope:"member:two"}}/>);await waitFor(()=>expect(requests).toHaveLength(3));await act(async()=>requests[1]!.resolve(data(999)));expect(screen.queryByText("999")).toBeNull();await act(async()=>requests[2]!.reject(new Error("Denied")));expect(screen.getByRole("alert").textContent).toContain("Denied");expect(screen.queryByText("620")).toBeNull();
});

test("metric display preserves units and abbreviated signed values without changing the original aggregate request",async()=>{
 const aggregate=vi.fn(async(_object:string,_query:AggregateQuery)=>data(-1200)),base={...spec(""),metric:{prefix:"$",suffix:" / day",formatter:"short",variant:"tag",tone:"warning"}} as ChartSpec;const source={aggregate,scope:"member"};const {rerender}=render(<Chart spec={base} source={source}/>);await waitFor(()=>expect(screen.getByText("$-1.2K / day")).toBeTruthy());expect(aggregate.mock.calls[0]?.[1]).toMatchObject({measures:["count"]});
 rerender(<Chart spec={{...base,metric:{...base.metric!,formatter:"number",prefix:"",suffix:"%"}}} source={source}/>);expect(screen.getByText("-1,200%")).toBeTruthy();expect(aggregate).toHaveBeenCalledTimes(1);
});

test("empty metric means stay absent while empty count stays zero",async()=>{const metric={suffix:"%",formatter:"number",variant:"card",tone:"neutral"},source={aggregate:async()=>({columns:[{name:"avg:qty",title:"Mean",kind:"measure" as const,type:"quantitative" as const}],rows:[]}),scope:"member"};const average={data:{entity:"sample.item"},mark:"kpi",encoding:{y:{field:"qty",aggregate:"avg",type:"quantitative"}},metric} as ChartSpec;const {rerender}=render(<Chart spec={average} source={source}/>);await waitFor(()=>expect(screen.getByText("\u2014%")).toBeTruthy());rerender(<Chart spec={{...spec(""),metric}} source={{aggregate:async()=>data(0),scope:"count"}}/>);await waitFor(()=>expect(screen.getByText("0%")).toBeTruthy());});
