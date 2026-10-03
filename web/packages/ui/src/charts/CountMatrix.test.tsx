import {afterEach,expect,test} from "vitest";
import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {CountMatrix,countMatrix} from "./CountMatrix";
import type {AggregateData} from "./spec";
afterEach(cleanup);
const data=(rows:AggregateData["rows"]):AggregateData=>({columns:[{name:"a",title:"Rows",kind:"group",type:"nominal"},{name:"b",title:"Columns",kind:"group",type:"nominal"},{name:"count",title:"Count",kind:"measure",type:"quantitative"}],rows});
test("typed count matrix separates placeholder values and concatenation collisions and keeps exact totals",()=>{
 const d=data([{a:null,b:"Y",count:1},{a:"",b:"Y",count:2},{a:"—",b:"Y",count:3},{a:"A|B",b:"C",count:4},{a:"A",b:"B|C",count:5},{a:1,b:"Y",count:6},{a:"1",b:"Y",count:7}]),m=countMatrix(d,"a","b")!;
 expect(m.rows).toHaveLength(7);expect(m.columns).toHaveLength(3);expect(m.total).toBe(28n);expect(new Set(m.rows.map(a=>a.key)).size).toBe(7);
 const cell=(a:unknown,b:unknown)=>m.cells.get(m.rows.find(row=>row.value===a)!.key)?.get(m.columns.find(col=>col.value===b)!.key)??0n;expect(cell("A|B","C")).toBe(4n);expect(cell("A","B|C")).toBe(5n);expect(cell("A","C")).toBe(0n);
 const huge=countMatrix(data([{a:"A",b:"X",count:Number.MAX_SAFE_INTEGER},{a:"B",b:"X",count:Number.MAX_SAFE_INTEGER}]),"a","b")!;expect(huge.total).toBe(18014398509481982n);expect(countMatrix(d,"a","b",{stringAxes:true})).toBeUndefined();
});
test("count matrix refuses truncated, duplicate, non-count, unsafe and over-budget answers",()=>{
 for(const count of [0,-1,1.5,Infinity,"1",Number.MAX_SAFE_INTEGER+1])expect(countMatrix(data([{a:"A",b:"B",count}]),"a","b")).toBeUndefined();
 expect(countMatrix(data([{a:"A",b:"B",count:1},{a:"A",b:"B",count:2}]),"a","b")).toBeUndefined();expect(countMatrix(data([{a:"A",count:1}]),"a","b")).toBeUndefined();expect(countMatrix(data([{a:{},b:"B",count:1}]),"a","b")).toBeUndefined();expect(countMatrix(data(Array.from({length:65},(_,i)=>({a:String(i),b:"B",count:1}))),"a","b",{maxAxis:64})).toBeUndefined();expect(countMatrix(data(Array.from({length:65},(_,i)=>({a:String(i),b:String(i),count:1}))),"a","b")).toBeUndefined();
 const wrong=data([{a:"A",b:"B",count:1}]);wrong.columns[2]!.kind="group";expect(countMatrix(wrong,"a","b")).toBeUndefined();
});
test("heatmap cells preserve zero intersections and send both raw strings; missing axes and disabled mode never write, empty sets retain Clear",()=>{
 const d=data([{a:"A",b:"X",count:2},{a:"B",b:"Y",count:3},{a:null,b:"Y",count:1},{a:"",b:"Y",count:1}]);let selection:unknown,cleared=0;const select=(a:unknown,b:unknown)=>{selection=[a,b];},clear=()=>cleared++;
 const view=render(<CountMatrix data={d} rows="a" columns="b" heatmap onSelect={select} onClear={clear}/>);fireEvent.click(screen.getByRole("button",{name:"A × Y: 0"}));expect(selection).toEqual(["A","Y"]);expect((screen.getByRole("button",{name:"No value (missing) × Y: 1"}) as HTMLButtonElement).disabled).toBe(true);fireEvent.click(screen.getByRole("button",{name:"Empty text × Y: 1"}));expect(selection).toEqual(["","Y"]);
 view.rerender(<CountMatrix data={d} rows="a" columns="b" heatmap enabled={false} onSelect={select}/>);expect((screen.getByRole("button",{name:"A × X: 2"}) as HTMLButtonElement).disabled).toBe(true);
 view.rerender(<CountMatrix data={data([])} rows="a" columns="b" heatmap onClear={clear}/>);expect(screen.getByRole("status").textContent).toContain("No matching records");fireEvent.click(screen.getByRole("button",{name:"Clear cell filters"}));expect(cleared).toBe(1);
});
