import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { RecordList, RecordPage, type EntityRecord, type RecordPageData, type RecordSource, type RecordView } from "./Records";

afterEach(cleanup);
Element.prototype.getBoundingClientRect = () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON: () => ({}) });
for (const [key, value] of [["offsetWidth", 800], ["offsetHeight", 280]] as const) Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
const pending = <T,>() => { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; };
const entity = { app: "sample", type: "sample.item", title: "Item", plural: "Items", display: "id", fields: [], standard: [] };
const record = (id: string) => ({ id, revision: 1, created: {}, changed: {} } as EntityRecord);
test("column presentation keeps exact values, original field order and authorized fields while hidden search preserves the caller query",()=>{
 const info={...entity,fields:[{name:"title",title:"Title",type:"text" as const},{name:"amount",title:"Amount",type:"decimal" as const},{name:"status",title:"State",type:"choice" as const,choices:["ready"],choiceTitles:["Ready"]},{name:"at",title:"At",type:"datetime" as const}]},list=vi.fn(),change=vi.fn(),source:RecordSource={entity:()=>info,list,get:vi.fn()};
 const value="12345678901234567890.12345678901234567890";
render(<RecordList source={source} type={info.type} fields={["amount","title","amount","status","at"]} showSearch={false} columnPresentation={[{field:"id",title:"Record ID",width:90},{field:"amount",title:"Exact amount",formatter:"numeric"},{field:"status",formatter:"badge"},{field:"at",formatter:"date"},{field:"hidden",title:"Private title"}]} window={{query:{search:"fixed",limit:1},page:{records:[{...record("A"),title:"Record A",amount:value,status:"ready",at:"2026-10-02T14:30:00Z",hidden:"Private"}],total:2},maxOffset:10,onChange:change}}/>);
 expect(screen.getAllByRole("columnheader").map(c=>c.textContent?.trim())).toEqual(["Record ID","Exact amount","Title","State","At"]);
 expect(screen.getByRole("cell",{name:"12,345,678,901,234,567,890.12345678901234567890"})).toBeTruthy();expect(screen.getByRole("cell",{name:"2026-10-02"})).toBeTruthy();expect(screen.getByRole("cell",{name:"Ready"})).toBeTruthy();expect(screen.queryByText("Private title")).toBeNull();expect(screen.queryByText("Private")).toBeNull();expect(screen.queryByRole("textbox",{name:"Search"})).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"Next page"}));expect(change).toHaveBeenCalledWith({offset:1});expect(list).not.toHaveBeenCalled();
});
test("multi-selection chooses only the caller window and keeps modifier selection separate from the active record",()=>{
 const changed=vi.fn(),opened=vi.fn(),info={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]},source:RecordSource={entity:()=>info,list:vi.fn(),get:vi.fn()},data=[{...record("A"),title:"A title"},{...record("B"),title:"B title"},{...record("C"),title:"C title"}];let ids:string[]=[];
 const props=()=>({source,type:info.type,onOpen:opened,window:{query:{limit:3},page:{records:data,total:100},maxOffset:100,onChange:vi.fn()},selectionSet:{selectedIDs:ids,records:[],status:"empty" as const,maxRecords:3,onChange:changed}});const {rerender}=render(<RecordList {...props()}/>);fireEvent.click(screen.getByRole("checkbox",{name:"Select A"}));expect(changed).toHaveBeenLastCalledWith(["A"]);expect(opened).toHaveBeenLastCalledWith(data[0]);ids=["A"];rerender(<RecordList {...props()}/>);fireEvent.click(screen.getByRole("checkbox",{name:"Select B"}));expect(changed).toHaveBeenLastCalledWith(["A","B"]);ids=["A","B"];rerender(<RecordList {...props()}/>);fireEvent.click(screen.getByRole("checkbox",{name:"Select this window"}));expect(changed).toHaveBeenLastCalledWith(["A","B","C"]);expect(changed.mock.calls.at(-1)![0].length).toBe(3);
});

test("cell staging submits changed fields per row and preserves failed baselines through ordinary refresh",async()=>{
 const info={...entity,fields:[{name:"title",title:"Title",type:"text" as const},{name:"locked",title:"Locked",type:"text" as const,readOnly:true}]},source:RecordSource={scope:"actor",entity:()=>info,list:vi.fn(),get:vi.fn()};
 const submit=vi.fn(async(r:EntityRecord,_patch:Record<string,unknown>)=>({accepted:r.id==="A",error:r.id==="B"?"CONFLICT":undefined})),port={schema:"sample.item.edit",scope:"binding",fields:["title"],maxRows:64,submit};
 let data=[{...record("A"),title:"A old",locked:"No edit"},{...record("B"),title:"B old",locked:"No edit"}];const props=()=>({source,type:info.type,fields:["title","locked"],inlineEdit:port,window:{query:{limit:100},page:{records:data,total:2},maxOffset:100,onChange:vi.fn()}});
 const {rerender}=render(<RecordList {...props()}/>);fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));
 const stage=(old:string,value:string)=>{const cell=screen.getByRole("cell",{name:old});fireEvent.doubleClick(cell);const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value}});fireEvent.keyDown(input,{key:"Enter"});};
 stage("A old","A new");stage("B old","B draft");fireEvent.doubleClick(screen.getAllByRole("cell",{name:"No edit"})[0]!);expect(screen.getAllByRole("textbox").length).toBe(1);fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));
 await waitFor(()=>expect(submit).toHaveBeenCalledTimes(2));expect(submit.mock.calls[0]![0].revision).toBe(1);expect(submit.mock.calls[0]![1]).toEqual({title:"A new"});expect(screen.getByRole("cell",{name:"A old"})).toBeTruthy();expect(screen.getByRole("alert").textContent).toContain("CONFLICT");expect(screen.getByRole("cell",{name:"B draft"})).toBeTruthy();
 data=[{...data[0]!,revision:2,title:"A new"},{...data[1]!,revision:2,title:"B remote"}];rerender(<RecordList {...props()}/>);fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));await waitFor(()=>expect(submit).toHaveBeenCalledTimes(3));expect(submit.mock.calls[2]![0].revision).toBe(1);
 fireEvent.click(screen.getByRole("button",{name:"Cancel cell edits"}));fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));stage("B remote","B retry");fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));await waitFor(()=>expect(submit).toHaveBeenCalledTimes(4));expect(submit.mock.calls[3]![0].revision).toBe(2);
 rerender(<RecordList {...props()} window={{...props().window,query:{search:"different",limit:100}}}/>);expect(screen.queryByRole("button",{name:"Cancel cell edits"})).toBeNull();expect(screen.queryByRole("cell",{name:"B retry"})).toBeNull();
});
test("retiring the edit scope ignores a pending result and does not submit remaining rows",async()=>{
 const response=pending<{accepted:boolean}>(),submit=vi.fn(()=>response.promise),info={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]},source:RecordSource={scope:"actor",entity:()=>info,list:vi.fn(),get:vi.fn()},data=[{...record("A"),title:"A old"},{...record("B"),title:"B old"}],port={schema:"sample.item.edit",scope:"binding",fields:["title"],maxRows:64,submit};
 const props={source,type:info.type,inlineEdit:port,window:{query:{limit:100},page:{records:data,total:2},maxOffset:100,onChange:vi.fn()}};const {rerender}=render(<RecordList {...props}/>);fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));for(const label of ["A old","B old"]){const cell=screen.getByRole("cell",{name:label});fireEvent.doubleClick(cell);const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value:label+" draft"}});fireEvent.keyDown(input,{key:"Enter"});}
 fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));await waitFor(()=>expect(submit).toHaveBeenCalledTimes(1));rerender(<RecordList {...props} source={{...source,scope:"other actor"}} inlineEdit={{...port,scope:"other binding"}}/>);await act(async()=>response.resolve({accepted:true}));expect(submit).toHaveBeenCalledTimes(1);expect(screen.queryByText(/Submitted 1 rows/)).toBeNull();expect(screen.queryByRole("cell",{name:"B old draft"})).toBeNull();
});
test("preview cell edits can be staged but never call the submission port",()=>{
 const submit=vi.fn(),info={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]},source:RecordSource={entity:()=>info,list:vi.fn(),get:vi.fn()};render(<RecordList source={source} type={info.type} inlineEdit={{schema:"sample.item.edit",fields:["title"],scope:"preview",maxRows:64,preview:true,submit}} window={{query:{limit:100},page:{records:[{...record("A"),title:"Original"}],total:1},maxOffset:100,onChange:vi.fn()}}/>);fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));const cell=screen.getByRole("cell",{name:"Original"});fireEvent.doubleClick(cell);const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value:"Preview draft"}});fireEvent.keyDown(input,{key:"Enter"});const button=screen.getByRole("button",{name:"Submit cell edits"});expect((button as HTMLButtonElement).disabled).toBe(true);fireEvent.click(button);expect(submit).not.toHaveBeenCalled();
});
test("opaque business record IDs cannot change the staging dictionary prototype",async()=>{
 const submit=vi.fn(async()=>({accepted:true})),info={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]},source:RecordSource={entity:()=>info,list:vi.fn(),get:vi.fn()};render(<RecordList source={source} type={info.type} inlineEdit={{schema:"sample.item.edit",fields:["title"],scope:"ids",maxRows:64,submit}} window={{query:{limit:100},page:{records:[{...record("__proto__"),title:"Old"}],total:1},maxOffset:100,onChange:vi.fn()}}/>);fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));const cell=screen.getByRole("cell",{name:"Old"});fireEvent.doubleClick(cell);const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value:"New"}});fireEvent.keyDown(input,{key:"Enter"});fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));await waitFor(()=>expect(submit).toHaveBeenCalledTimes(1));
});

test("a controlled RecordList renders its caller window without a second read and emits bounded view changes",()=>{
  const list=vi.fn(),change=vi.fn();
  const source:RecordSource={entity:()=>entity,list,get:async()=>{throw new Error("unused")}};
  const {rerender}=render(<RecordList source={source} type={entity.type} window={{query:{sort:["id"],offset:0,limit:1},page:{records:[record("WINDOW")],total:2},maxOffset:10,onChange:change}}/>);
  expect(screen.getByRole("cell",{name:"WINDOW"})).toBeTruthy();expect(list).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"Next page"}));expect(change).toHaveBeenLastCalledWith({offset:1});
  fireEvent.change(screen.getByRole("textbox",{name:"Search"}),{target:{value:"term"}});expect(change).toHaveBeenLastCalledWith({search:"term",offset:0});
  rerender(<RecordList source={source} type={entity.type} window={{query:{search:"fixed",sort:["id"],limit:1},page:{records:[],total:0},searchLocked:true,sortLocked:true,maxOffset:10,onChange:change}}/>);
  expect((screen.getByRole("textbox",{name:"Search"}) as HTMLInputElement).disabled).toBe(true);
  expect((screen.getByRole("combobox",{name:"Sort"}) as HTMLSelectElement).disabled).toBe(true);
  expect(screen.queryByRole("cell",{name:"WINDOW"})).toBeNull();expect(list).not.toHaveBeenCalled();
});

test("RecordList cannot replace a newer query result with an earlier response", async () => {
  const requests = new Map<string, ReturnType<typeof pending<RecordPageData>>>();
  const source: RecordSource = { entity: () => entity, get: async () => { throw new Error("unused"); }, list: (_, query) => {
    const result = pending<RecordPageData>(); requests.set(query.search ?? "", result); return result.promise;
  } };
  render(<RecordList source={source} type={entity.type} height={280} />);
  await waitFor(() => expect(requests.has("")).toBe(true));
  fireEvent.change(screen.getByRole("textbox", { name: "Search" }), { target: { value: "new" } });
  await waitFor(() => expect(requests.has("new")).toBe(true));
  await act(async () => { requests.get("new")!.resolve({ records: [record("NEW")], total: 1 }); });
  expect(screen.getByRole("cell", { name: "NEW" })).toBeTruthy();
  await act(async () => { requests.get("")!.resolve({ records: [record("OLD")], total: 1 }); });
  expect(screen.queryByRole("cell", { name: "OLD" })).toBeNull();
  expect(screen.getByRole("cell", { name: "NEW" })).toBeTruthy();
});

test("RecordPage ignores a completed read for the previous record", async () => {
  const requests = new Map<string, ReturnType<typeof pending<RecordView>>>();
  const source: RecordSource = { entity: () => entity, list: async () => ({ records: [], total: 0 }), get: (_, id) => {
    const result = pending<RecordView>(); requests.set(id, result); return result.promise;
  } };
  const view = render(<RecordPage source={source} type={entity.type} id="OLD" detailOnly />);
  view.rerender(<RecordPage source={source} type={entity.type} id="NEW" detailOnly />);
  await act(async () => { requests.get("NEW")!.resolve({ record: record("NEW") } as RecordView); });
  expect(screen.getByRole("heading", { name: "NEW" })).toBeTruthy();
  await act(async () => { requests.get("OLD")!.resolve({ record: record("OLD") } as RecordView); });
  expect(screen.queryByRole("heading", { name: "OLD" })).toBeNull();
  expect(screen.getByRole("heading", { name: "NEW" })).toBeTruthy();
});
