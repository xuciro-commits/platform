import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {MultipleChoiceInput} from "./MultipleChoiceInput";
afterEach(cleanup);
test("multiple choices change one declared value while preserving unmatched original selections and empty sets",()=>{
 const writes:string[][]=[],onChange=(value:string[])=>writes.push(value),props={title:"Choice",label:"Status",options:["A","B"],onChange};const {rerender}=render(<MultipleChoiceInput {...props} value={["retired","A"]}/>);
 expect(screen.getByRole("status").textContent).toContain("retired");fireEvent.click(screen.getByRole("button",{name:"B"}));expect(writes).toEqual([["retired","A","B"]]);fireEvent.click(screen.getByRole("button",{name:"A"}));expect(writes.at(-1)).toEqual(["retired"]);
 rerender(<MultipleChoiceInput {...props} value={[]}/>);expect(screen.getByRole("button",{name:"A"}).getAttribute("aria-pressed")).toBe("false");expect(writes.length).toBe(2);rerender(<MultipleChoiceInput {...props} value={["A"]} disabled/>);fireEvent.click(screen.getByRole("button",{name:"B"}));expect(writes.length).toBe(2);
});
test("a full original set blocks additions but still permits declared removals",()=>{
 const value=Array.from({length:64},(_,i)=>String(i)),writes:string[][]=[];render(<MultipleChoiceInput title="Full selection" value={value} options={["0","new"]} onChange={next=>writes.push(next)}/>);expect(screen.getByRole("button",{name:"new"}).hasAttribute("disabled")).toBe(true);fireEvent.click(screen.getByRole("button",{name:"0"}));expect(writes[0]).toEqual(value.slice(1));
});
