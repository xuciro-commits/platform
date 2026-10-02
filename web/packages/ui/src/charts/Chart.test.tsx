import {act,cleanup,render,screen,waitFor} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Chart} from "./Chart";
import type {AggregateData,ChartSpec} from "./spec";
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
