import {afterEach,expect,test} from "vitest";
import {cleanup,render,screen} from "@testing-library/react";
import {RecordCard} from "./RecordCard";
import type {EntityInfo,EntityRecord} from "./Records";
afterEach(cleanup);
const info={type:"sample.note",title:"Assets",fields:[{name:"name",title:"Title",type:"text"},{name:"first",title:"Value",type:"integer"},{name:"second",title:"Value",type:"boolean"},{name:"day",title:"Date",type:"date"}]} as EntityInfo;
const row=(id:string):EntityRecord=>({id,revision:1,created:{by:"test",at:"2026-10-03T00:00:00Z"},changed:{by:"test",at:"2026-10-03T00:00:00Z"},name:"<b>Same title</b>",first:0,second:false,day:"2026-10-03"});
test("record cards preserve stable identity, literal duplicate titles and independently keyed formatted scalar properties",()=>{
 const {container,rerender}=render(<RecordCard record={row("A")} info={info} fields={["first","second"]} config={{labelField:"name",tone:"warning"}}/>);expect(screen.getByText("<b>Same title</b>")).toBeTruthy();expect(screen.getByText("A")).toBeTruthy();expect(screen.getAllByText("Value")).toHaveLength(2);expect(screen.getByText("0")).toBeTruthy();expect(container.querySelector("b")).toBeNull();expect(screen.queryByRole("button")).toBeNull();rerender(<RecordCard record={row("B")} info={info} fields={["second"]} config={{labelField:"name",tone:"info"}}/>);expect(screen.getByText("B")).toBeTruthy();expect(screen.queryByText("A")).toBeNull();expect(screen.getAllByText("Value")).toHaveLength(1);expect(screen.queryByText("0")).toBeNull();
});
test("record cards reject hidden titles, unknown or duplicate fields, incompatible title values and unbounded presentation",()=>{
 const props={record:row("A"),info,fields:["first"],config:{labelField:"name",tone:"info"}},v=render(<RecordCard {...props} info={{...info,fields:info.fields.filter(f=>f.name!=="name")}}/>);expect(screen.getByRole("alert")).toBeTruthy();v.rerender(<RecordCard {...props} fields={["first","first"]}/>);expect(screen.getByRole("alert")).toBeTruthy();v.rerender(<RecordCard {...props} record={{...row("A"),name:1}}/>);expect(screen.getByRole("alert")).toBeTruthy();v.rerender(<RecordCard {...props} config={{...props.config,tone:"red"}}/>);expect(screen.getByRole("alert")).toBeTruthy();v.rerender(<RecordCard {...props} record={{...row("A"),name:null}}/>);expect(screen.getAllByText("A")).toHaveLength(2);
});
