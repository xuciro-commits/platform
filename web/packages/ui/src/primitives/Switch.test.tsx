import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Switch} from "./controls";
afterEach(cleanup);
test("switch remains controlled, emits only booleans, and respects disabled state",()=>{
 const writes:boolean[]=[],onChange=(value:boolean)=>writes.push(value),{rerender}=render(<Switch label="Active assets" checked={false} onChange={onChange}/>),button=screen.getByRole("switch",{name:"Active assets"});
 expect(button.getAttribute("aria-checked")).toBe("false");fireEvent.click(button);expect(writes).toEqual([true]);expect(button.getAttribute("aria-checked")).toBe("false");
 rerender(<Switch label="Active assets" checked={true} onChange={onChange}/>);fireEvent.click(button);expect(writes).toEqual([true,false]);expect(button.getAttribute("aria-checked")).toBe("true");
 rerender(<Switch label="Active assets" checked={true} disabled onChange={onChange}/>);fireEvent.click(button);expect(writes).toEqual([true,false]);
});
test("an intentionally empty visible label retains a caller-supplied accessible identity",()=>{
 render(<Switch label="" ariaLabel="Quiet switch" checked={false} onChange={()=>{}}/>);expect(screen.getByRole("switch",{name:"Quiet switch"}).textContent).toBe("");
});
