import {expect,test} from "vitest";
import {fireEvent,render,screen,cleanup,within} from "@testing-library/react";
import {RecordScatter,scatterPoints} from "./RecordScatter";
import type {EntityInfo,EntityRecord} from "./Records";
const info={type:"sample.note",title:"Note",fields:[{name:"x",title:"Pressure",type:"integer"},{name:"y",title:"Exposure",type:"decimal"},{name:"state",title:"State",type:"choice"},{name:"name",title:"Name",type:"text"}]} as EntityInfo;
const fields={xField:"x",yField:"y",colorField:"state",labelField:"name"};
const row=(id:string,x:unknown=2,y:unknown=3,state:unknown="open"):EntityRecord=>({id,revision:1,created:{by:"test",at:"2026-10-03T00:00:00Z"},changed:{by:"test",at:"2026-10-03T00:00:00Z"},name:"Same name",x,y,state});
test("scatter keeps original order, coincident identities, exact numeric values and distinct empty color categories",()=>{
 const records=[row("B"),row("A"),row("empty",-2,0,""),row("none",0,-5,null),row("missing",null,8)],m=scatterPoints(records,info,fields)!;
 expect(m.points.map(p=>[p.record.id,p.x,p.y])).toEqual([["B",2,3],["A",2,3],["empty",-2,0],["none",0,-5]]);expect(m.points[0]?.record).toBe(records[0]);expect(m.missing).toBe(1);expect(m.categories).toEqual(["open","",undefined]);expect([m.minX,m.maxX,m.minY,m.maxY]).toEqual([-2,2,-5,3]);expect(scatterPoints([],info,fields)?.points).toEqual([]);
});
test("scatter rejects coercion, unsafe integers, duplicate identities, hidden fields and oversized windows",()=>{
 for(const bad of ["2",false,NaN,Infinity,Number.MAX_SAFE_INTEGER+1,2.5])expect(scatterPoints([row("A",bad)],info,fields)).toBeUndefined();
 expect(scatterPoints([row("A",1,-Infinity)],info,fields)).toBeUndefined();expect(scatterPoints([row("A"),row("A")],info,fields)).toBeUndefined();expect(scatterPoints([row("")],info,fields)).toBeUndefined();expect(scatterPoints([row("A",1,2,{})],info,fields)).toBeUndefined();
 expect(scatterPoints([row("A")],{...info,fields:info.fields.filter(f=>f.name!=="x")},fields)).toBeUndefined();expect(scatterPoints(Array.from({length:101},(_,i)=>row(String(i))),info,fields)).toBeUndefined();
 expect(scatterPoints([row("A",-Number.MAX_VALUE),row("B",Number.MAX_VALUE)],{...info,fields:info.fields.map(f=>({...f,type:f.name==="x"?"decimal":f.type}))},fields)).toBeUndefined();
});
test("each coincident SVG point and accessible record entry can select its own original identity; disabled mode refuses",()=>{
 let selected="";const records=[row("A"),row("B")],onSelect=(r:EntityRecord)=>{selected=r.id;};const view=render(<RecordScatter records={records} info={info} fields={fields} onSelect={onSelect}/>),plot=screen.getByRole("group",{name:"Record scatter plot"}),points=within(plot).getAllByRole("button");fireEvent.keyDown(points[0]!,{key:"Enter"});expect(selected).toBe("A");fireEvent.keyDown(points[1]!,{key:" "});expect(selected).toBe("B");fireEvent.click(screen.getByText("Individual records, including overlapping points"));fireEvent.click(screen.getAllByRole("button",{name:/Same name · A/}).find(element=>element.tagName==="BUTTON")!);expect(selected).toBe("A");
 view.rerender(<RecordScatter records={records} info={info} fields={fields} enabled={false} onSelect={onSelect}/>);fireEvent.click(within(plot).getAllByRole("button")[1]!);expect(selected).toBe("A");cleanup();
});
