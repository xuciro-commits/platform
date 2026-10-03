import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {Notice} from "./Notice";
afterEach(cleanup);
test("semantic notices announce caller text and never evaluate HTML",()=>{
 const {rerender}=render(<Notice title="Affected records" message="12 affected <img src=x>" tone="danger"/>);
 expect(screen.getByRole("alert",{name:"Affected records"}).textContent).toContain("12 affected <img src=x>");expect(document.querySelector("img")).toBeNull();
 rerender(<Notice title="Information" message="Ready" tone="info"/>);expect(screen.getByRole("status",{name:"Information"})).toBeTruthy();expect(screen.queryByRole("alert")).toBeNull();
});
test("static notes use their independent label and preserve empty or missing display titles",()=>{
 const {rerender}=render(<Notice title="" label="Operator instructions" message="<img src=x> {value} **literal**" tone="danger" role="note"/>);
 const note=screen.getByRole("note",{name:"Operator instructions"});expect(note.textContent).toContain("<img src=x> {value} **literal**");expect(note.querySelector("strong")).toBeNull();expect(screen.queryByRole("alert")).toBeNull();expect(document.querySelector("img")).toBeNull();
 rerender(<Notice label="Empty note" message="" tone="success" role="note"/>);expect(screen.getByRole("note",{name:"Empty note"})).toBeTruthy();expect(screen.queryByRole("status")).toBeNull();
 rerender(<Notice title="Visible title" label="Widget name" message="Text" tone="info" role="note"/>);expect(screen.getByRole("note",{name:"Widget name"}).querySelector("strong")?.textContent).toBe("Visible title");
});
