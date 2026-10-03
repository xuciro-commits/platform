import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Separator} from "./separator";
afterEach(cleanup);
test("horizontal separators retain optional literal labels and independent accessible identity",()=>{
 const {rerender}=render(<Separator name="Group boundary" label="<img src=x> {value}"/>);const line=screen.getByRole("separator",{name:"<img src=x> {value}"});expect(line.getAttribute("aria-orientation")).toBe("horizontal");expect(line.textContent).toContain("<img src=x> {value}");expect(line.hasAttribute("tabindex")).toBe(false);expect(document.querySelector("img")).toBeNull();
 for(const label of [undefined,""]){rerender(<Separator name="Group boundary" label={label}/>);expect(screen.getByRole("separator",{name:"Group boundary"}).textContent).toBe("");}
});
