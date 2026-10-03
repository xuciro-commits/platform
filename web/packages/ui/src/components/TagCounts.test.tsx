import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {pageUIManifest} from "@platform/kernel";
import {TagCounts} from "./TagCounts";
import type {TermCount} from "./TermCounts";
afterEach(cleanup);
const terms:TermCount[]=[{value:null,count:5},{value:"",count:4},{value:"No value (missing)",count:3},{value:"Empty text",count:2},{value:"constructor",count:1}];
test("fixed tag counts preserve null, empty and literal display collisions with independent original filter writes",()=>{
 const writes:(string|undefined)[]=[],view=render(<TagCounts terms={terms} label="Tags" selected={[""]} onSelect={value=>writes.push(value)}/>);
 const list=screen.getByRole("list",{name:"Tags"}),items=within(list).getAllByRole("listitem");expect(items).toHaveLength(5);expect(within(items[0]!).queryByRole("button")).toBeNull();expect(within(items[0]!).getByText("No value (missing)")).toBeTruthy();
 fireEvent.click(within(items[1]!).getByRole("button"));expect(writes).toEqual([""]);expect(within(items[1]!).getByRole("button").getAttribute("aria-pressed")).toBe("true");
 fireEvent.click(within(items[2]!).getByRole("button"));expect(writes.at(-1)).toBe("No value (missing)");fireEvent.click(within(items[3]!).getByRole("button"));expect(writes.at(-1)).toBe("Empty text");
 expect(within(items[2]!).getByRole("button").getAttribute("aria-pressed")).toBe("false");
 view.rerender(<TagCounts terms={terms} label="Tags" selected={["No value (missing)"]} onSelect={value=>writes.push(value)}/>);expect(within(items[2]!).getByRole("button").getAttribute("aria-pressed")).toBe("true");expect(within(items[1]!).getByRole("button").getAttribute("aria-pressed")).toBe("false");
 fireEvent.click(screen.getByRole("button",{name:"Clear group filter"}));expect(writes.at(-1)).toBeUndefined();
});
test("tag values remain literal, counts stay exact, and read-only groups create no writes or controls",()=>{
 const {container}=render(<TagCounts terms={[{value:"<b>literal</b>",count:Number.MAX_SAFE_INTEGER},{value:"—",count:1}]} label="Tags" selected={["—"]}/>);expect(screen.getByText("<b>literal</b>")).toBeTruthy();expect(container.querySelector("b")).toBeNull();expect(screen.getByText(`· ${Number.MAX_SAFE_INTEGER}`)).toBeTruthy();expect(screen.getByText("—")).toBeTruthy();expect(screen.queryByRole("button")).toBeNull();
});
test("disabled tag selection retains current groups but cannot write or clear",()=>{
 const writes:(string|undefined)[]=[],view=render(<TagCounts terms={terms} label="Tags" selected={[""]} enabled={false} onSelect={value=>writes.push(value)}/>);for(const button of screen.getAllByRole("button")){expect((button as HTMLButtonElement).disabled).toBe(true);fireEvent.click(button);}expect(writes).toEqual([]);
 view.rerender(<TagCounts terms={terms} label="Tags" selected={[""]} enabled onSelect={value=>writes.push(value)}/>);fireEvent.click(screen.getByRole("button",{name:"constructor · 1"}));expect(writes).toEqual(["constructor"]);
});
test("invalid, coerced, duplicate or over-budget counts are refused as a whole and empty groups stay explicit",()=>{
 const view=render(<TagCounts terms={[]} label="Tags"/>);expect(screen.getByRole("status").textContent).toBe("No terms in the matching records.");
 const invalid:unknown[]=[[{value:"A",count:1},{value:"A",count:2}],[{value:0,count:1}],[{value:["A"],count:1}],[{value:"A",count:0}],[{value:"A",count:1.5}],[{value:"A",count:Number.MAX_SAFE_INTEGER+1}],Array.from({length:pageUIManifest.runtime.terms.maxGroups+1},(_,index)=>({value:String(index),count:1}))];
 for(const terms of invalid){view.rerender(<TagCounts terms={terms as TermCount[]} label="Tags"/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByRole("list")).toBeNull();}
});
