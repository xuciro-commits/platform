import {afterEach,expect,test} from "vitest";
import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {RecordSparkline,recordSparklinePoints} from "./RecordSparkline";
import type {EntityInfo,EntityRecord} from "./Records";
afterEach(cleanup);
const info={type:"sample.note",title:"Note",fields:[{name:"value",title:"Availability",type:"decimal"}]} as EntityInfo;
const row=(id:string,value:unknown):EntityRecord=>({id,revision:1,created:{by:"test",at:"2026-10-03T00:00:00Z"},changed:{by:"test",at:"2026-10-03T00:00:00Z"},value});
test("sparkline preserves ordered identities, missing gaps and real zero instead of coercing records or implying time",()=>{
 const m=recordSparklinePoints([row("B",2),row("A",null),row("C",0),row("D",-1)],info,"value")!;expect(m.points.map(p=>[p.id,p.index,p.value])).toEqual([["B",0,2],["A",1,null],["C",2,0],["D",3,-1]]);expect(m.segments.map(s=>s.map(p=>p.id))).toEqual([["B"],["C","D"]]);expect(m.missing).toBe(1);expect([m.min,m.max]).toEqual([-1,2]);expect(recordSparklinePoints([],info,"value")?.segments).toEqual([]);expect(recordSparklinePoints([row("A",undefined)],info,"value")?.missing).toBe(1);
});
test("sparkline refuses hidden fields, unknown or nonfinite values, unsafe integers, duplicate IDs and oversized windows",()=>{
 for(const value of ["2",false,{},NaN,Infinity])expect(recordSparklinePoints([row("A",value)],info,"value")).toBeUndefined();expect(recordSparklinePoints([row("A",Number.MAX_SAFE_INTEGER+1)],{...info,fields:[{name:"value",title:"Integer",type:"integer"}]},"value")).toBeUndefined();expect(recordSparklinePoints([row("A",1)],{...info,fields:[]},"value")).toBeUndefined();expect(recordSparklinePoints([row("A",1),row("A",2)],info,"value")).toBeUndefined();expect(recordSparklinePoints(Array.from({length:31},(_,i)=>row(String(i),i)),info,"value")).toBeUndefined();expect(recordSparklinePoints([row("A",-Number.MAX_VALUE),row("B",Number.MAX_VALUE)],info,"value")).toBeUndefined();
});
test("the composite keeps exact scalar text, accessible point identities and missing-value feedback without writes",()=>{
 const view=render(<RecordSparkline valueText="9007199254740993" label="All records" suffix=" assets" records={[row("A",1),row("B",null),row("C",0)]} info={info} field="value"/>);expect(screen.getByText("9007199254740993 assets")).toBeTruthy();expect(screen.getByText(/3 ordered records · 1 missing/)).toBeTruthy();const plot=screen.getByRole("group",{name:"Ordered record sparkline"});expect(within(plot).getAllByRole("img")).toHaveLength(2);fireEvent.focus(within(plot).getByRole("img",{name:"C · Availability: 0"}));expect(screen.getAllByRole("status").some(node=>node.textContent==="C · Availability: 0")).toBe(true);expect(screen.queryByRole("button")).toBeNull();expect(view.container.querySelectorAll("polyline")).toHaveLength(2);
 view.rerender(<RecordSparkline label="All records" records={[row("A",null)]} info={info} field="value"/>);expect(screen.getByText("No scalar value.")).toBeTruthy();expect(screen.getByText("No numeric values in this record window.")).toBeTruthy();expect(screen.queryByRole("group",{name:"Ordered record sparkline"})).toBeNull();
});
