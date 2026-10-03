import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {DateInput} from "./DateInput";
import {validCivilDate} from "./date";
afterEach(cleanup);
test("civil date validation retains Gregorian and year boundaries without timezone normalization",()=>{
 for(const date of ["0001-01-01","9999-12-31","2000-02-29","2028-02-29"])expect(validCivilDate(date)).toBe(true);
 for(const date of ["0000-01-01","10000-01-01","1900-02-29","2026-02-29","2028-04-31","2028-2-01","2028-02-29T00:00:00Z",""])expect(validCivilDate(date)).toBe(false);
});
test("invalid date drafts remain visible and repairable while empty values do not default",()=>{
 const writes:string[]=[],props={title:"Business date",label:"Date",onChange:(value:string)=>writes.push(value)}, {rerender}=render(<DateInput {...props} value="2026-02-30"/>);
 expect(screen.getByRole("textbox",{name:"Date date draft"})).toHaveProperty("value","2026-02-30");expect(screen.getByRole("alert")).toBeTruthy();fireEvent.change(screen.getByRole("textbox",{name:"Date date draft"}),{target:{value:"2028-02-29"}});expect(writes).toEqual(["2028-02-29"]);
 rerender(<DateInput {...props} value=""/>);expect(writes.length).toBe(1);expect(screen.queryByRole("alert")).toBeNull();expect(screen.getByRole("button",{name:"Clear date"}).hasAttribute("disabled")).toBe(true);
 rerender(<DateInput {...props} value="2028-02-29"/>);fireEvent.click(screen.getByRole("button",{name:"Clear date"}));expect(writes.at(-1)).toBe("");
});
