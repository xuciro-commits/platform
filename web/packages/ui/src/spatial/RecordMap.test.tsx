import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {landRingPath} from "./world-land";
import {mapPoints,RecordMap} from "./RecordMap";
import type {EntityInfo,EntityRecord} from "../records/Records";
afterEach(cleanup);
const info={type:"sample.asset",fields:[{name:"latitude",type:"decimal",title:"Latitude"},{name:"longitude",type:"decimal",title:"Longitude"},{name:"name",type:"text",title:"Name"}]} as EntityInfo,fields={latitudeField:"latitude",longitudeField:"longitude",labelField:"name"},records=[{id:"A",name:"Same",latitude:0,longitude:0},{id:"B",name:"Same",latitude:0,longitude:0},{id:"Missing",latitude:null,longitude:10},{id:"Outside",latitude:91,longitude:0}].map(row=>({...row,revision:1,created:{by:"test",at:"2026-10-04T00:00:00Z"},changed:{by:"test",at:"2026-10-04T00:00:00Z"}})) as EntityRecord[];
test("geographic bounds preserve missing values and stable IDs even at overlapping coordinates",()=>{const model=mapPoints(records,info,fields)!;expect(model.points.map(p=>p.record.id)).toEqual(["A","B"]);expect(model.missing).toBe(1);expect(model.invalid).toBe(1);expect(model.points[0]).toMatchObject({latitude:0,longitude:0,x:500,y:250});expect(mapPoints([{id:"Wrong",latitude:"0",longitude:0}] as unknown as EntityRecord[],info,fields)).toBeUndefined();});
test("accessible map records keep independent IDs and respect the caller's disabled selection",()=>{const selected:string[]=[];const view=render(<RecordMap records={records} info={info} fields={fields} clusterEnabled={false} onSelect={r=>selected.push(r.id)}/>);fireEvent.click(screen.getAllByRole("button",{name:"Same · B"}).at(-1)!);expect(selected).toEqual(["B"]);view.rerender(<RecordMap records={records} info={info} fields={fields} enabled={false} onSelect={r=>selected.push(r.id)}/>);fireEvent.click(screen.getAllByRole("button",{name:"Same · A"}).at(-1)!);expect(selected).toEqual(["B"]);});

test("a coast crossing the antimeridian is split into both map edges",()=>{const path=landRingPath([[179,10],[-179,10],[-179,12],[179,12],[179,10]]);expect(path.match(/M/g)).toHaveLength(2);expect(path).not.toContain("NaN");});
