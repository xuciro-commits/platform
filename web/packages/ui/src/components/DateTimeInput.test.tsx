import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {DateTimeInput} from "./DateTimeInput";
import {timestampParts,validTimestamp,withTimestampOffset} from "./date";
afterEach(cleanup);
test("offset timestamp parsing preserves nanosecond text and requires an explicit known offset",()=>{
 expect(timestampParts("2028-02-29T08:30:45.123456789+08:00")).toEqual({local:"2028-02-29T08:30:45",fraction:"123456789",offset:"+08:00"});
 for(const value of ["0001-01-01T00:00:00Z","9999-12-31T23:59:59.123456789+23:59"])expect(validTimestamp(value)).toBe(true);
 for(const value of ["2026-02-29T08:00:00Z","2028-02-29T24:00:00Z","2028-02-29T08:00:60Z","2028-02-29T08:00Z","2028-02-29T08:00:00","2028-02-29T08:00:00-00:00","2028-02-29T08:00:00+24:00","2028-02-29T08:00:00.1234567890Z","0000-01-01T00:00:00Z"])expect(validTimestamp(value)).toBe(false);
 expect(withTimestampOffset("2028-02-29T08:30","+08:00")).toBe("2028-02-29T08:30:00+08:00");expect(withTimestampOffset("2028-02-29T08:30:45.123456789Z","+08:00")).toBe("2028-02-29T08:30:45.123456789Z");expect(withTimestampOffset("","Z")).toBe("");expect(withTimestampOffset("2028-02-29","Z")).toBeUndefined();
});
test("native edits preserve fractional precision and offset, and invalid drafts remain repairable",()=>{
 const writes:string[]=[],props={title:"Business time",label:"Time",offset:"Z",onChange:(value:string)=>writes.push(value)}, {rerender}=render(<DateTimeInput {...props} value="2028-02-29T08:30:45.123456789+08:00"/>);
 fireEvent.change(screen.getByLabelText("Time"),{target:{value:"2028-03-01T09:30:46"}});expect(writes.at(-1)).toBe("2028-03-01T09:30:46.123456789+08:00");
 fireEvent.change(screen.getByLabelText("Time UTC offset"),{target:{value:"Z"}});expect(writes.at(-1)).toBe("2028-02-29T08:30:45.123456789Z");
 rerender(<DateTimeInput {...props} value="2026-02-30T08:30:45Z"/>);expect(screen.getByLabelText("Time datetime draft")).toHaveProperty("value","2026-02-30T08:30:45Z");expect(screen.getByRole("alert")).toBeTruthy();fireEvent.change(screen.getByLabelText("Time datetime draft"),{target:{value:"2028-02-29T08:30:45Z"}});expect(writes.at(-1)).toBe("2028-02-29T08:30:45Z");
 rerender(<DateTimeInput {...props} value=""/>);const count=writes.length;expect(screen.getByLabelText("Time")).toHaveProperty("value","");expect(writes.length).toBe(count);fireEvent.change(screen.getByLabelText("Time"),{target:{value:"2028-03-01T09:30"}});expect(writes.at(-1)).toBe("2028-03-01T09:30:00Z");
 rerender(<DateTimeInput {...props} disabled value="2028-02-29T08:30:45Z"/>);fireEvent.change(screen.getByLabelText("Time"),{target:{value:"2028-03-01T09:30:46"}});expect(writes.length).toBe(count+1);
});
