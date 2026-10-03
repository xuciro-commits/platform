import {cleanup,render,screen,within} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {RecordHistory,type EntityInfo,type RecordChange} from "./Records";
afterEach(cleanup);
const info={type:"sample.asset",title:"Asset",app:"sample",plural:"Assets",display:"name",standard:[],fields:[{name:"name",title:"Name",type:"text"},{name:"count",title:"Count",type:"integer"}]} as EntityInfo;
const change=(id:string,after:unknown):RecordChange=>({change:id,schema:"sample.asset.edit",by:"original-actor",at:"2026-10-03T12:00:00.123456789Z",fields:[{field:"count",before:0,after}]});
test("original history order/IDs/schema/actor/time and overall revision remain exact with an honest display window",()=>{
 const history=[change("NEW",2),change("OLD",1)],view=render(<RecordHistory info={info} history={history} limit={1} total={2} recordID="ASSET-1" recordRevision={9}/>);expect(screen.getByRole("status").textContent).toBe("Showing 1 of 2 changes.");expect(screen.getByText("NEW")).toBeTruthy();expect(screen.queryByText("OLD")).toBeNull();expect(screen.getByText("sample.asset.edit")).toBeTruthy();expect(screen.getByText("ASSET-1 · Revision 9")).toBeTruthy();expect(view.container.querySelector("time")?.getAttribute("datetime")).toBe(history[0]!.at);
 view.rerender(<RecordHistory info={info} history={history} heading={false}/>);expect(screen.queryByRole("heading")).toBeNull();expect([...view.container.querySelector("ol")!.children].map(row=>row.querySelector(".font-mono")?.textContent)).toEqual(["NEW","OLD"]);expect(screen.queryByRole("button")).toBeNull();
});
test("multiple original puts in one accepted decision retain repeated change ID and ordered distinct field steps",()=>{
 const history=[change("SAME-CHANGE",2),change("SAME-CHANGE",1)],view=render(<RecordHistory info={info} history={history}/>);expect(screen.getAllByText("SAME-CHANGE")).toHaveLength(2);const rows=[...view.container.querySelector("ol")!.children];expect(within(rows[0] as HTMLElement).getByText(/→ 2/)).toBeTruthy();expect(within(rows[1] as HTMLElement).getByText(/→ 1/)).toBeTruthy();expect(screen.queryByRole("alert")).toBeNull();
});
test("history metadata cannot restore hidden business values while original archived state remains visible",()=>{
 const history=[{...change("C",0),fields:[{field:"secret",before:"Private before",after:"Private after"},{field:"archived",before:false,after:true},{field:"name",after:"<b>Literal name</b>"}]}],view=render(<RecordHistory info={info} history={history}/>);expect(screen.queryByText("secret")).toBeNull();expect(screen.queryByText("Private before")).toBeNull();expect(screen.queryByText("Private after")).toBeNull();expect(screen.getByText("Archived")).toBeTruthy();expect(screen.getByText("Archived").closest("li")!.textContent).toContain("true");expect(view.container.textContent).toContain("<b>Literal name</b>");expect(view.container.querySelector("b")).toBeNull();
});
test("optional history limits validate the native budget but legacy omission preserves every supplied change",()=>{
 const history=Array.from({length:120},(_,index)=>change(`C-${index}`,index)),view=render(<RecordHistory info={info} history={history}/>);expect(view.container.querySelector("ol")!.children).toHaveLength(120);view.rerender(<RecordHistory info={info} history={history} limit={2}/>);expect(view.container.querySelector("ol")!.children).toHaveLength(2);expect(screen.getByRole("status").textContent).toBe("Showing 2 of 120 loaded changes; the complete total is unavailable.");for(const limit of [0,101,1.5]){view.rerender(<RecordHistory info={info} history={history} limit={limit}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(view.container.querySelector("ol")).toBeNull();}
});
