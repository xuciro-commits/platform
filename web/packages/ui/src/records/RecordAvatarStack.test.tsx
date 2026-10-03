import {cleanup,render,screen,within} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {pageUIManifest} from "@platform/kernel";
import {RecordAvatarStack} from "./RecordAvatarStack";
import type {EntityInfo,EntityRecord} from "./Records";
afterEach(cleanup);
const info={type:"sample.person",title:"People",fields:[{name:"name",title:"Name",type:"text"},{name:"shift",title:"Shift",type:"choice"},{name:"active",title:"Active",type:"boolean"},{name:"hours",title:"Hours",type:"integer"}]} as EntityInfo;
const person=(id:string):EntityRecord=>({id,revision:1,created:{by:"owner",at:"2026-10-03T00:00:00Z"},changed:{by:"owner",at:"2026-10-03T00:00:00Z"},name:"<b>Same person</b>",shift:"night",active:false,hours:0,private:"Hidden shift"});
test("avatars preserve duplicate names as separate original records, original window order and full authorized count",()=>{
 const view=render(<RecordAvatarStack records={[person("B"),person("A")]} info={info} labelField="name" detailFields={["shift"]} total={25}/>);expect(screen.getByRole("status").textContent).toBe("Showing 2 of 25 records.");const rows=screen.getAllByRole("listitem");expect(rows).toHaveLength(2);expect(within(rows[0]!).getByText("B")).toBeTruthy();expect(within(rows[1]!).getByText("A")).toBeTruthy();expect(screen.getAllByText("<b>Same person</b>")).toHaveLength(2);expect(screen.getAllByText("night")).toHaveLength(2);expect(rows[0]!.title).toContain("Shift: night");expect(view.container.querySelector("b")).toBeNull();expect(screen.queryByText("Hidden shift")).toBeNull();
 view.rerender(<RecordAvatarStack records={[person("A")]} info={info} labelField="name" detailFields={["hours","active"]} total={1}/>);expect(screen.queryByText("B")).toBeNull();expect(screen.getByText("0")).toBeTruthy();expect(screen.getByText("No")).toBeTruthy();
});
test("safe initials remain presentation only while original graphemes, blank labels and stable IDs remain literal",()=>{
 const view=render(<RecordAvatarStack records={[{...person("E"),name:"👨‍👩‍👧‍👦 王"},{...person("EMPTY"),name:""},{...person("NULL"),name:null}]} info={info} labelField="name" total={3}/>);expect(screen.getByText("👨‍👩‍👧‍👦 王")).toBeTruthy();expect(view.container.querySelector('span[aria-hidden="true"]')!.textContent).toBe("👨‍👩‍👧‍👦王");expect(screen.getByText("?")).toBeTruthy();expect(screen.getAllByText("NULL")).toHaveLength(2);expect(screen.queryByRole("button")).toBeNull();
 view.rerender(<RecordAvatarStack records={[{...person("UPPER"),name:"ßam Zoe"},{...person("BIDI"),name:"\u202eTaylor Chen"}]} info={info} labelField="name" total={2}/>);const originalInitials=view.container.querySelectorAll('span[aria-hidden="true"]');expect(originalInitials[0]!.textContent).toBe("SS");expect(originalInitials[1]!.textContent).toBe("TC");expect(screen.getByText("\u202eTaylor Chen")).toBeTruthy();
});
test("empty windows and complete totals remain distinct, and malformed identities/values/private fields refuse",()=>{
 const props={records:[person("A")],info,labelField:"name",total:1},view=render(<RecordAvatarStack {...props} records={[]} total={0}/>);expect(screen.getByText("No matching people records.")).toBeTruthy();view.rerender(<RecordAvatarStack {...props} records={[]} total={20}/>);expect(screen.getByText("No people records in this loaded window.")).toBeTruthy();expect(screen.getByRole("status").textContent).toBe("Showing 0 of 20 records.");
 for(const patch of [{total:0},{records:[person("A"),person("A")],total:2},{records:[{...person("A"),archived:true}]},{records:[{...person("A"),name:false}]},{labelField:"private"},{detailFields:["private"]},{detailFields:["shift","shift"]},{detailFields:["name"]},{detailFields:["shift","active","hours"]},{records:Array.from({length:pageUIManifest.runtime.contextViews.maxAvatarWindow+1},(_,index)=>person(String(index))),total:10}]){view.rerender(<RecordAvatarStack {...props} {...patch}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByRole("list")).toBeNull();}
 view.rerender(<RecordAvatarStack {...props} detailFields={["id"]}/>);expect(screen.getByText("ID")).toBeTruthy();expect(screen.getAllByText("A")).toHaveLength(2);
});
