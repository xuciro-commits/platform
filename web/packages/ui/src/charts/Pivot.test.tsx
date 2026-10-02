import {act,cleanup,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Pivot} from "./Pivot";
import type {AggregateData} from "./spec";
afterEach(cleanup);
const data=(value:number):AggregateData=>({columns:[{name:"row",title:"Row",kind:"group",type:"nominal"},{name:"column",title:"Column",kind:"group",type:"nominal"},{name:"count",title:"Count",kind:"measure",type:"quantitative"}],rows:[{row:"R",column:"C",count:value}]});
const deferred=()=>{let resolve!:(d:AggregateData)=>void,reject!:(e:Error)=>void;const promise=new Promise<AggregateData>((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject};};
test("sparse complete counts show zero intersections and correct row, column and grand totals without making other measures zero",async()=>{
 const sparse=data(1);sparse.rows=[{row:"A",column:"X",count:2},{row:"B",column:"Y",count:3}];const props={type:"sample.note",query:{},rows:"row",columns:"column",measure:"count"},source={aggregate:async()=>sparse};const {rerender}=render(<Pivot {...props} source={source}/>);const a=(await screen.findByRole("rowheader",{name:"A"})).parentElement!;expect(within(a).getAllByRole("cell").map(c=>c.textContent)).toEqual(["2","0","2"]);const total=screen.getByRole("rowheader",{name:"Total"}).parentElement!;expect(within(total).getAllByRole("cell").map(c=>c.textContent)).toEqual(["2","3","5"]);
 const sums={...sparse,rows:[{row:"A",column:"X","sum:qty":2},{row:"B",column:"Y","sum:qty":3}]};rerender(<Pivot {...props} measure="sum:qty" source={{aggregate:async()=>sums}}/>);await waitFor(()=>expect(screen.getByRole("rowheader",{name:"A"})).toBeTruthy());expect(within(screen.getByRole("rowheader",{name:"A"}).parentElement!).getAllByRole("cell").map(c=>c.textContent)).toEqual(["2","","2"]);
});
test("pivot sends complete predicates and immediately hides obsolete or refused values when scope changes",async()=>{
 const pending:ReturnType<typeof deferred>[]=[];const aggregate=(_:string,q:unknown)=>{expect(q).toEqual({set:{op:"union",inputs:[{},{}]},maxRows:4096,groups:["row","column"],measures:["count"]});const p=deferred();pending.push(p);return p.promise;};
 const props={type:"sample.note",query:{set:{op:"union",inputs:[{},{}]}},rows:"row",columns:"column",measure:"count"};
 const {rerender}=render(<Pivot {...props} source={{aggregate,scope:"one"}}/>);await waitFor(()=>expect(pending.length).toBe(1));await act(async()=>pending[0]!.resolve(data(620)));expect(screen.getAllByText("620")).toHaveLength(4);
 rerender(<Pivot {...props} source={{aggregate,scope:"two"}}/>);expect(screen.queryByText("620")).toBeNull();await waitFor(()=>expect(pending.length).toBe(2));
 rerender(<Pivot {...props} source={{aggregate,scope:"three"}}/>);await waitFor(()=>expect(pending.length).toBe(3));await act(async()=>pending[1]!.resolve(data(999)));expect(screen.queryByText("999")).toBeNull();await act(async()=>pending[2]!.reject(new Error("Denied")));expect(screen.getByRole("alert").textContent).toContain("Denied");
});
test("pivot rejects a sparse matrix whose rendered cells exceed the bound instead of truncating",async()=>{
 const sparse=data(1);sparse.rows=Array.from({length:65},(_,i)=>({row:`R${i}`,column:`C${i}`,count:1}));render(<Pivot source={{aggregate:async()=>sparse}} type="sample.note" query={{}} rows="row" columns="column" measure="count"/>);await waitFor(()=>expect(screen.getByRole("alert").textContent).toContain("cell limit"));expect(screen.queryByRole("table")).toBeNull();
});
