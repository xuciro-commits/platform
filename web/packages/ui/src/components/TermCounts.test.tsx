import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {TermCounts} from "./TermCounts";
afterEach(cleanup);
test("terms keep empty, null, literal dash and prototype-like labels distinct and remain read-only",()=>{
 const {container,rerender}=render(<TermCounts label="Terms" terms={[{value:null,count:5},{value:"",count:4},{value:"—",count:3},{value:"constructor",count:2},{value:"<b>literal</b>",count:1}]}/>);expect(screen.getAllByRole("listitem")).toHaveLength(5);expect(screen.getByText("No value")).toBeTruthy();expect(screen.getByText("Empty text")).toBeTruthy();expect(screen.getByText("—")).toBeTruthy();expect(screen.getByText("constructor")).toBeTruthy();expect(screen.getByText("<b>literal</b>")).toBeTruthy();expect(container.querySelector("b")).toBeNull();expect(screen.queryByRole("button")).toBeNull();rerender(<TermCounts label="Terms" terms={[]}/>);expect(screen.getByRole("status").textContent).toContain("No terms");rerender(<TermCounts label="Terms" terms={[{value:"A",count:1},{value:"A",count:2}]}/>);expect(screen.getByRole("alert")).toBeTruthy();
});
