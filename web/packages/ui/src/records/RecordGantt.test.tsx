import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {RecordGantt,recordGanttRows,ganttRange} from "./RecordGantt";
import type {EntityInfo,EntityRecord} from "./Records";
afterEach(cleanup);
const info={type:"test.task",app:"test",title:"Task",plural:"Tasks",fields:[{name:"begin",title:"Start",type:"datetime"},{name:"due",title:"End",type:"date"},{name:"title",title:"Task",type:"text"},{name:"state",title:"Status",type:"choice",choices:["open","done"]}]} as EntityInfo;
const fields={startField:"begin",endField:"due",titleField:"title",statusField:"state",rangeStart:"2026-09-01",rangeEnd:"2026-11-01",tones:[{value:"done",tone:"success"}]};
const row=(id:string,begin:string,due:string,state="open"):EntityRecord=>({id,revision:1,created:{by:"test",at:"2000-01-01T00:00:00Z"},changed:{by:"test",at:"2000-01-01T00:00:00Z"},title:id,begin,due,state});
test("Gantt retains original window order and mixed civil/UTC intervals with fixed range clipping",()=>{
 const records=[row("Z","2026-10-01T23:30:00-02:00","2026-10-04","done"),row("A","2026-08-01T00:00:00Z","2026-12-01"),row("point","2026-09-01T00:00:00Z","2026-09-01"),row("end","2026-11-01T00:00:00Z","2026-11-01"),row("before","2026-08-01T00:00:00Z","2026-09-01"),row("bad","2026-10-03T00:00:00Z","2026-10-02"),row("missing","invalid","2026-10-02")];
 const model=recordGanttRows(records,info,fields)!;expect(model.rows.map(r=>r.record)).toEqual(records);expect(model.rows[0]!.start).toBe("2026-10-02T01:30:00Z");expect(model.rows[0]!.tone).toBe("success");expect(model.rows[1]!.left).toBe(0);expect(model.rows[1]!.width).toBe(100);expect(model.rows[2]!.width).toBe(0);expect(model.rows[2]!.outside).toBe(false);expect(model.rows.slice(3,5).every(r=>r.outside)).toBe(true);expect(model.rows.slice(5).every(r=>r.invalid)).toBe(true);
 expect(ganttRange("2026-02-30","2026-11-01")).toBeUndefined();expect(ganttRange("0000-01-01","2026-11-01")).toBeUndefined();expect(ganttRange("0001-01-01","9999-12-31")).toBeTruthy();expect(ganttRange("2026-11-01","2026-09-01")).toBeUndefined();
});
test("Gantt reports its bounded original window, invalid/outside intervals and unavailable field mappings",()=>{
 const records=Array.from({length:21},(_,i)=>row(`R${i}`,i===1?"bad":"2026-09-01T00:00:00Z",i===2?"2026-08-01":"2026-09-03"));records[2]=row("R2","2026-08-01T00:00:00Z","2026-08-02");const {rerender}=render(<RecordGantt records={records} info={info} fields={fields}/>);expect(screen.getByText("Showing 20 of 21 tasks in this window.")).toBeTruthy();expect(screen.queryByText("R20 · R20")).toBeNull();expect(screen.getByText("Task times are missing, invalid or reversed.")).toBeTruthy();expect(screen.getByText("Task is outside the display range.")).toBeTruthy();
 rerender(<RecordGantt records={records} info={{...info,fields:info.fields.filter(f=>f.name!=="begin")}} fields={fields}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(recordGanttRows(records,info,{...fields,tones:[{value:"hidden",tone:"success"}]})).toBeUndefined();expect(recordGanttRows(records,info,{...fields,tones:[{value:"done",tone:"red"}]})).toBeUndefined();
});
