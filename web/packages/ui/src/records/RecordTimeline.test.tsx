import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {RecordTimeline,timelineRows,timelineTime} from "./RecordTimeline";
import type {EntityRecord} from "./Records";
afterEach(cleanup);
const row=(id:string,start:string,end?:string):EntityRecord=>({id,revision:1,created:{at:"2026-01-01",by:"test"},changed:{at:"2026-01-01",by:"test"},label:id,start,end,team:"Team"});
test("civil days and explicit RFC3339 offsets have stable UTC meaning",()=>{
 expect(timelineTime("2024-02-29","date")).toBe(Date.UTC(2024,1,29));
 for(const value of ["2026-02-29","2026-13-01","2026-01-32","2026-10-01T00:00:00Z"])expect(timelineTime(value,"date")).toBeUndefined();
 expect(timelineTime("2026-10-01T12:30:00+02:30","datetime")).toBe(timelineTime("2026-10-01T10:00:00Z","datetime"));
 for(const value of ["2026-10-01T10:00:00","2026-02-30T10:00:00Z","2026-10-01T24:00:00Z"])expect(timelineTime(value,"datetime")).toBeUndefined();
});
test("invalid and reversed intervals are counted instead of inventing time",()=>{
 const result=timelineRows([row("good","2026-10-01","2026-10-03"),row("backwards","2026-10-04","2026-10-01"),row("missing","",""),row("point","2026-10-02","2026-10-02")],{start:"start",end:"end",label:"label",group:"team",kind:"date"});
 expect(result.invalid).toBe(2);expect(result.rows.map(r=>r.record.id)).toEqual(["good","point"]);expect(result.rows[1]!.start).toBe(result.rows[1]!.end);
});
test("selection sends the original record and stale rows vanish on window replacement",()=>{
 const record=row("FIRST","2026-10-01"),select=vi.fn(),fields={start:"start",label:"label",kind:"date" as const};
 const view=render(<RecordTimeline records={[record]} fields={fields} label="Schedule" onSelect={select}/>);
 fireEvent.click(screen.getByRole("button",{name:"FIRST"}));expect(select).toHaveBeenLastCalledWith(record);
 view.rerender(<RecordTimeline records={[record]} fields={fields} selected="FIRST" label="Schedule" onSelect={select}/>);fireEvent.click(screen.getByRole("button",{name:"FIRST"}));expect(select).toHaveBeenLastCalledWith(undefined);
 view.rerender(<RecordTimeline records={[row("SECOND","2026-10-02")]} fields={fields} label="Schedule" onSelect={select}/>);expect(screen.queryByRole("button",{name:"FIRST"})).toBeNull();expect(screen.getByRole("button",{name:"SECOND"})).toBeTruthy();
});
