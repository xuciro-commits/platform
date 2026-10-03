import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Spacer} from "./LayoutRegion";
afterEach(cleanup);
test("blank space creates no accessible content, focus target or artificial record identity",()=>{
 const {container,rerender}=render(<Spacer size={16}/>);
 const blank=container.firstElementChild!;expect(blank.getAttribute("aria-hidden")).toBe("true");expect(blank.textContent).toBe("");expect(blank.hasAttribute("role")).toBe(false);expect(blank.hasAttribute("tabindex")).toBe(false);expect(blank.querySelector("button,input,a")).toBeNull();
 rerender(<Spacer size={0}/>);expect(screen.queryByRole("region")).toBeNull();expect(screen.queryByRole("separator")).toBeNull();expect(screen.queryByRole("note")).toBeNull();
});
