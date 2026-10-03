import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Checkbox} from "./controls";
afterEach(cleanup);
test("checkbox keeps caller state, native label activation and disabled change semantics",()=>{
 const writes:boolean[]=[],onChange=(value:boolean)=>writes.push(value),{rerender}=render(<Checkbox checked={false} onChange={onChange}>Active assets</Checkbox>),input=screen.getByRole("checkbox",{name:"Active assets"}) as HTMLInputElement;
 fireEvent.click(screen.getByText("Active assets"));expect(writes).toEqual([true]);expect(input.checked).toBe(false);
 rerender(<Checkbox checked={true} onChange={onChange}>Active assets</Checkbox>);fireEvent.click(input);expect(writes).toEqual([true,false]);expect(input.checked).toBe(true);
 rerender(<Checkbox checked={true} disabled onChange={onChange}>Active assets</Checkbox>);fireEvent.click(screen.getByText("Active assets"));expect(writes).toEqual([true,false]);
});
test("empty visible checkbox label uses the supplied accessible identity",()=>{
 render(<Checkbox checked={false} ariaLabel="Quiet checkbox" onChange={()=>{}}><span/></Checkbox>);expect(screen.getByRole("checkbox",{name:"Quiet checkbox"})).toBeTruthy();
});
