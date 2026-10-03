import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Notice} from "./Notice";
afterEach(cleanup);
test("semantic notices announce caller text and never evaluate HTML",()=>{
 const {rerender}=render(<Notice title="Affected records" message="12 affected <img src=x>" tone="danger"/>);
 expect(screen.getByRole("alert",{name:"Affected records"}).textContent).toContain("12 affected <img src=x>");expect(document.querySelector("img")).toBeNull();
 rerender(<Notice title="Information" message="Ready" tone="info"/>);expect(screen.getByRole("status",{name:"Information"})).toBeTruthy();expect(screen.queryByRole("alert")).toBeNull();
});
