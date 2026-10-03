import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {ChoiceInput} from "./ChoiceInput";
import {validChoiceInput} from "./choice";
afterEach(cleanup);
test("all choice presentations retain empty or unmatched caller state until an explicit permitted choice",()=>{
 const writes:string[]=[],onChange=(value:string)=>writes.push(value),props={title:"Choice",label:"Status",options:["Open","Closed"],onChange};
 const {rerender}=render(<ChoiceInput {...props} variant="select" value="retired"/>);expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("retired");expect(screen.getByRole("status").textContent).toContain("retired");expect(writes).toEqual([]);fireEvent.change(screen.getByRole("combobox"),{target:{value:""}});expect(writes).toEqual([""]);
 rerender(<ChoiceInput {...props} variant="radio" value=""/>);expect(screen.getAllByRole("radio").every(input=>!(input as HTMLInputElement).checked)).toBe(true);fireEvent.click(screen.getByText("Closed"));expect(writes.at(-1)).toBe("Closed");expect((screen.getByRole("radio",{name:"Closed"}) as HTMLInputElement).checked).toBe(false);
 rerender(<ChoiceInput {...props} variant="segments" value="retired"/>);expect(screen.getByRole("button",{name:"Open"}).getAttribute("aria-pressed")).toBe("false");fireEvent.click(screen.getByRole("button",{name:"Open"}));expect(writes.at(-1)).toBe("Open");
 rerender(<ChoiceInput {...props} variant="segments" value="Open" disabled/>);fireEvent.click(screen.getByRole("button",{name:"Closed"}));expect(writes.at(-1)).toBe("Open");
});
test("radio instances use separate native group names and empty captions retain accessible identity",()=>{
 render(<><ChoiceInput title="First" variant="radio" options={["A","B"]} value="A" onChange={()=>{}}/><ChoiceInput title="Second" label="" variant="radio" options={["A","B"]} value="B" onChange={()=>{}}/></>);const inputs=screen.getAllByRole("radio") as HTMLInputElement[];expect(inputs[0]!.name).not.toBe(inputs[2]!.name);expect(inputs[0]!.checked).toBe(true);expect(inputs[3]!.checked).toBe(true);expect(screen.getByRole("radiogroup",{name:"Second"})).toBeTruthy();
});
test("choice budgets refuse duplicate, empty and unsupported options while an empty list remains explicit",()=>{
 expect(validChoiceInput({variant:"select",options:[]})).toBe(true);for(const config of [{variant:"unknown",options:[]},{variant:"select",options:["A","A"]},{variant:"select",options:[""]},{variant:"select",options:["x".repeat(257)]},{variant:"select",options:Array.from({length:65},(_,i)=>String(i))}])expect(validChoiceInput(config)).toBe(false);
 render(<ChoiceInput title="Empty choices" value="" options={[]} variant="select" onChange={()=>{}}/>);expect(screen.getByRole("status").textContent).toBe("No choices available.");
});
