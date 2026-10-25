import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { entityFrom, RecordList, RecordPage, RecordLinks, RecordStatus, RecordDeliveries, useRecordView, type EntityRecord, type RecordPageData, type RecordSource, type RecordView } from "./Records";

import { EnterpriseElementLookup,EnterpriseReference,RecordLookup } from "./RecordLookup";
import { ViewVisibilityContext } from "../shell/ViewVisibility";
import { RecordWorkspace } from "./RecordWorkspace";
afterEach(cleanup);
Element.prototype.getBoundingClientRect = () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON: () => ({}) });
for (const [key, value] of [["offsetWidth", 800], ["offsetHeight", 280]] as const) Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
const pending = <T,>() => { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; };
const entity = { app: "sample", type: "sample.item", title: "Item", plural: "Items", display: "id", fields: [], standard: [] };
const record = (id: string) => ({ id, revision: 1, created: {}, changed: {} } as EntityRecord);

test("an unprepared reference search reports its connection requirement without clearing the selected input",async()=>{
 const message="This task was not prepared for offline use. Connect to continue.",change=vi.fn();
 const source:RecordSource={entity:()=>entity,list:async()=>{throw Error(message);},get:vi.fn()};
 render(<RecordLookup source={source} type={entity.type} value="ORIGINAL" onChange={change} ariaLabel="Related record"/>);
 fireEvent.focus(screen.getByRole("combobox",{name:"Related record"}));
 await waitFor(()=>expect(screen.getByRole("alert").textContent).toBe(message));
 expect(screen.queryByText("No matching records.")).toBeNull();expect(change).not.toHaveBeenCalled();
 fireEvent.keyDown(screen.getByRole("combobox",{name:"Related record"}),{key:"Escape"});
 expect((screen.getByRole("combobox",{name:"Related record"}) as HTMLInputElement).value).toBe("ORIGINAL");
});

test("public deliveries distinguish financial acceptance and expose only an authorized receipt link",()=>{
 const open=vi.fn();
 render(<RecordDeliveries deliveries={[{id:"waiting",event:"build/journal",state:"pending",at:"2026-10-22T10:00:00Z"},{id:"posted",event:"build/journal",state:"delivered",at:"2026-10-22T10:01:00Z",receipt:"core.journal/original"},{id:"refused",event:"build/journal",state:"rejected",at:"2026-10-22T10:02:00Z",message:"The posting period is closed. The business document remains accepted."}]} onOpen={open}/>);
 expect(screen.getByText("Business acceptance and delivery are separate results.")).toBeTruthy();
 expect(screen.getAllByRole("button",{name:"Open receipt"})).toHaveLength(1);
 fireEvent.click(screen.getByRole("button",{name:"Open receipt"}));
 expect(open).toHaveBeenCalledWith("core.journal",{id:"original"});
 expect(screen.getByText("The posting period is closed. The business document remains accepted.")).toBeTruthy();
});

test("an unconstrained model timestamp retains its offset and precision when its civil time changes", () => {
  const input = entityFrom({...entity,fields:[{name:"at",title:"At",type:"datetime"}]}).fields.at!.editor!;
  const change = vi.fn();
  render(input({id:"at",value:"2028-02-29T08:30:45.123456789+08:00",onChange:change}));
  fireEvent.change(screen.getByLabelText("At",{exact:true}),{target:{value:"2028-02-29T09:30:45"}});
  expect(change).toHaveBeenCalledWith("2028-02-29T09:30:45.123456789+08:00");
});

test("record workspace marks the row whose details are open and clears it on return", async () => {
  const data = [record("A"), record("B")];
  const source: RecordSource = { entity: () => entity, list: async () => ({ records: data, total: 2 }), get: vi.fn() };
  const props = { title: "Items", source, type: entity.type, onSelect: vi.fn(), detail: (id: string) => <p>Details for {id}</p> };
  const view = render(<RecordWorkspace {...props} selected="A" />);
  await waitFor(() => expect(screen.getByRole("row", { name: "A", selected: true })).toBeTruthy());
  const search = screen.getByRole("textbox", { name: "Search" });
  fireEvent.change(search, { target: { value: "original filter" } });
  fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(props.onSelect).toHaveBeenCalledWith(undefined);
  view.rerender(<RecordWorkspace {...props} selected="B" />);
  await waitFor(() => expect(screen.getByRole("row", { name: "B", selected: true })).toBeTruthy());
  expect(screen.getByRole("row", { name: "A" }).getAttribute("aria-selected")).toBe("false");
  view.rerender(<RecordWorkspace {...props} />);
  expect(screen.queryByRole("row", { selected: true })).toBeNull();
  expect(screen.queryByRole("region", { name: "Selected record" })).toBeNull();
  expect(screen.getByRole("textbox", { name: "Search" })).toBe(search);
  expect((search as HTMLInputElement).value).toBe("original filter");
});

test("record view refresh retains its current value while revisions and reloads ignore older replies", async () => {
  const requests: ReturnType<typeof pending<RecordView>>[] = [];
  const get = vi.fn(() => { const response = pending<RecordView>(); requests.push(response); return response.promise; });
  const release = vi.fn(), watchRecord = vi.fn(() => release);
  const source: RecordSource = { scope: "member", revision: 0, entity: () => entity, list: vi.fn(), get, watchRecord };
  const { result, rerender, unmount } = renderHook(({ source, reload }) => useRecordView(source, entity.type, "A", reload), { initialProps: { source, reload: 0 } });
  expect(result.current.loading).toBe(true);
  const original = { record: record("A") } as RecordView;
  await act(async () => requests[0]!.resolve(original));
  expect(result.current.view).toBe(original);
  expect(result.current.loading).toBe(false);
  rerender({ source: { ...source }, reload: 0 });
  expect(get).toHaveBeenCalledTimes(1);
  expect(watchRecord).toHaveBeenCalledTimes(1);

  const revised = { ...source, revision: 1 };
  rerender({ source: revised, reload: 0 });
  expect(requests).toHaveLength(2);
  expect(result.current.view).toBe(original);
  expect(result.current.loading).toBe(true);
  rerender({ source: revised, reload: 1 });
  expect(requests).toHaveLength(3);
  const latest = { record: { ...record("A"), revision: 3 } } as RecordView;
  await act(async () => requests[2]!.resolve(latest));
  await act(async () => requests[1]!.resolve({ record: { ...record("A"), revision: 2 } } as RecordView));
  expect(result.current.view).toBe(latest);
  expect(result.current.loading).toBe(false);
  expect(watchRecord).toHaveBeenCalledTimes(3);
  expect(release).toHaveBeenCalledTimes(2);
  unmount();
  expect(release).toHaveBeenCalledTimes(3);
});

test("record views clear on record, scope or type changes and reject a mismatched response identity", async () => {
  const requests: ReturnType<typeof pending<RecordView>>[] = [];
  const get = vi.fn(() => { const response = pending<RecordView>(); requests.push(response); return response.promise; });
  const source: RecordSource = { scope: "member-one", entity: () => entity, list: vi.fn(), get };
  const { result, rerender } = renderHook(({ source, type, id }) => useRecordView(source, type, id), { initialProps: { source, type: entity.type, id: "A" } });
  await act(async () => requests[0]!.resolve({ record: record("A") } as RecordView));
  expect(result.current.view?.record.id).toBe("A");
  rerender({ source, type: entity.type, id: "B" });
  expect(result.current.view).toBeUndefined();
  expect(result.current.loading).toBe(true);
  const otherScope = { ...source, scope: "member-two" };
  rerender({ source: otherScope, type: entity.type, id: "B" });
  await act(async () => requests[1]!.resolve({ record: record("B") } as RecordView));
  expect(result.current.view).toBeUndefined();
  await act(async () => requests[2]!.resolve({ record: { ...record("B"), revision: 2 } } as RecordView));
  expect(result.current.view?.record.revision).toBe(2);
  rerender({ source: otherScope, type: "sample.other", id: "B" });
  expect(result.current.view).toBeUndefined();
  await act(async () => requests[3]!.resolve({ record: record("WRONG") } as RecordView));
  expect(result.current.view).toBeUndefined();
  expect(result.current.error).toBeTruthy();
  expect(result.current.loading).toBe(false);
});

test("record view retries failed reads and releases hidden subscriptions before accepting a fresh visible reply", async () => {
  const requests: ReturnType<typeof pending<RecordView>>[] = [];
  const get = vi.fn<RecordSource["get"]>().mockRejectedValueOnce(new Error("Read refused")).mockImplementation(() => {
    const response = pending<RecordView>(); requests.push(response); return response.promise;
  });
  const release = vi.fn(), changed: (() => void)[] = [];
  const watchRecord = vi.fn((_type: string, _id: string, refresh: () => void) => { changed.push(refresh); return release; });
  const source: RecordSource = { scope: "member", entity: () => entity, list: vi.fn(), get, watchRecord };
  let visible = true;
  const { result, rerender, unmount } = renderHook(() => useRecordView(source, entity.type, "A"), {
    wrapper: ({ children }) => <ViewVisibilityContext.Provider value={visible}>{children}</ViewVisibilityContext.Provider>,
  });
  await waitFor(() => expect(result.current.error).toBe("Read refused"));
  expect(result.current.view).toBeUndefined();
  act(() => result.current.refresh());
  expect(result.current.error).toBeUndefined();
  expect(result.current.loading).toBe(true);
  const original = { record: record("A") } as RecordView;
  await act(async () => requests[0]!.resolve(original));
  act(() => changed[0]!());
  expect(result.current.view).toBe(original);
  expect(result.current.loading).toBe(true);
  visible = false; rerender();
  expect(release).toHaveBeenCalledTimes(1);
  await act(async () => requests[1]!.resolve({ record: { ...record("A"), revision: 2 } } as RecordView));
  expect(result.current.view).toBe(original);
  expect(get).toHaveBeenCalledTimes(3);
  visible = true; rerender();
  expect(watchRecord).toHaveBeenCalledTimes(2);
  await act(async () => requests[2]!.resolve({ record: { ...record("A"), revision: 3 } } as RecordView));
  expect(result.current.view?.record.revision).toBe(3);
  expect(result.current.loading).toBe(false);
  unmount();
  expect(release).toHaveBeenCalledTimes(2);
});

test("JSON fields render structured flow outputs and nested line values without treating objects as React children",async()=>{
 const info={...entity,fields:[{name:"outputs",title:"Outputs",type:"json" as const,readOnly:true},{name:"trace",title:"Trace",type:"lines" as const,fields:[{name:"result",title:"Result",type:"json" as const}]}]};
 const outputs={service:{name:"pms",values:[null,false,0]}},row={...record("FLOW"),outputs,trace:[{result:outputs}]},source:RecordSource={entity:()=>info,list:vi.fn(),get:async()=>({record:row,history:[],following:false,related:[],linked:[],activity:[],tasks:[],approvals:[],processes:[],files:[],comments:[]})};
 const list=render(<RecordList source={source} type={info.type} window={{query:{limit:10},page:{records:[row],total:1},maxOffset:10,onChange:vi.fn()}}/>);
 expect(screen.getByRole("cell",{name:JSON.stringify(outputs)})).toBeTruthy();
 list.unmount();
 render(<RecordPage source={source} type={info.type} id="FLOW" detailOnly/>);
 await waitFor(()=>expect(screen.getAllByText(JSON.stringify(outputs))).toHaveLength(2));
});

test("record status highlights only the real current state, never emits transitions and removes hidden lifecycle metadata",async()=>{
 const info={...entity,fields:[{name:"state",title:"State",type:"choice" as const}],lifecycle:{field:"state",initial:"open",states:[{name:"open",title:"Open"},{name:"done",title:"Done"}],transitions:[{name:"close",schema:"sample.item.close",title:"Finish",from:["open"],to:["done"]}]}},get=vi.fn(async()=>({record:{...record("A"),state:"done"}} as unknown as RecordView)),source:RecordSource={scope:"actor",entity:()=>info,list:vi.fn(),get},config={field:"state",stages:["open","done"]};
 const {rerender}=render(<RecordStatus source={source} type={info.type} id="A" config={config}/>);await waitFor(()=>expect(screen.getByText("Done").closest("li")?.getAttribute("aria-current")).toBe("step"));expect(screen.getByText("Open").closest("li")?.getAttribute("aria-current")).toBeNull();expect(screen.queryByRole("button")).toBeNull();expect(get).toHaveBeenCalledTimes(1);
 rerender(<RecordStatus source={source} type={info.type} id="A" config={{field:"state",stages:["open"]}}/>);expect(screen.getByText("Current state is outside the displayed stages.")).toBeTruthy();expect(screen.queryByText("Done")).toBeNull();expect(get).toHaveBeenCalledTimes(1);
 rerender(<RecordStatus source={{...source,scope:"hidden",entity:()=>({...info,fields:[]})}} type={info.type} id="A" config={config}/>);await waitFor(()=>expect(screen.getByRole("alert").textContent).toContain("Lifecycle status is unavailable."));expect(screen.queryByText("Open")).toBeNull();expect(screen.queryByText("Done")).toBeNull();
});
test("standalone links retain declared order and titles, hide unreadable reference groups and open original records",async()=>{
 const child={...entity,type:"sample.child",plural:"Children",fields:[{name:"name",title:"Name",type:"text" as const},{name:"parent",title:"Parent",type:"reference" as const,ref:entity.type,inverse:"children"}]},get=vi.fn(async()=>({record:record("A"),history:[],related:[{type:child.type,field:"parent",records:[{...record("child"),name:"Linked child"}],total:42},{type:child.type,field:"hidden",records:[{...record("secret"),name:"Private child"}],total:1}],linked:[]} as unknown as RecordView)),source:RecordSource={scope:"member1",entity:type=>type===child.type?child:entity,list:vi.fn(),get},open=vi.fn(),groups=[{object:{app:"sample",kind:"object" as const,name:child.type},field:"parent",title:"Published links"},{object:{app:"sample",kind:"object" as const,name:child.type},field:"hidden",title:"Hidden heading"}];
 const {rerender}=render(<RecordLinks source={source} type={entity.type} id="A" groups={groups} onOpen={open}/>);await waitFor(()=>expect(screen.getByRole("cell",{name:"Linked child"})).toBeTruthy());expect(get).toHaveBeenCalledTimes(1);expect(source.list).not.toHaveBeenCalled();expect(screen.getByRole("heading",{name:/Published links/}).textContent).toContain("42");expect(screen.queryByText("Private child")).toBeNull();expect(screen.queryByText("Hidden heading")).toBeNull();expect(screen.queryByRole("tab")).toBeNull();
 fireEvent.click(screen.getByRole("cell",{name:"Linked child"}));expect(open).toHaveBeenCalledWith(child.type,expect.objectContaining({id:"child"}));
 rerender(<RecordLinks source={{...source,scope:"member2",entity:type=>type===child.type?{...child,fields:child.fields.filter(f=>f.name!=="parent")}:entity}} type={entity.type} id="A" groups={groups} onOpen={open}/>);expect(screen.queryByRole("cell",{name:"Linked child"})).toBeNull();await waitFor(()=>expect(screen.getByText("No related records.")).toBeTruthy());expect(get).toHaveBeenCalledTimes(2);
});

test("record tabs share one authorized view and reset after record or member scope changes",async()=>{
 const info={...entity,fields:[{name:"name",title:"Name",type:"text" as const},{name:"qty",title:"Quantity",type:"integer" as const}]},child={...entity,type:"sample.child",plural:"Children",fields:[{name:"name",title:"Name",type:"text" as const}]},get=vi.fn(async(_type:string,id:string)=>({record:{...record(id),name:id,qty:7,hidden:"Private"},tasks:[],approvals:[],processes:[],files:[],comments:[],related:[{type:child.type,field:"parent",records:[{...record("child"),name:"Related child"}],total:3}],history:[{change:"original-change",schema:"sample.item.edit",by:"owner",at:"2026-10-02T12:00:00Z",fields:[{field:"name",before:"Before",after:id}]}]} as unknown as RecordView)),source:RecordSource={scope:"member:1",entity:type=>type===child.type?child:info,list:vi.fn(),get};
 const props={source,type:info.type,id:"A",fields:["name","qty"],recordTabs:["overview","properties","links","history"]};const {rerender}=render(<RecordPage {...props}/>);await waitFor(()=>expect(screen.getByRole("tab",{name:"Overview"})).toBeTruthy());expect(get).toHaveBeenCalledTimes(1);
 fireEvent.click(screen.getByRole("tab",{name:"Properties"}));expect(screen.getByText("Name")).toBeTruthy();expect(screen.queryByText("Private")).toBeNull();fireEvent.click(screen.getByRole("tab",{name:"Links"}));expect(screen.getByRole("cell",{name:"Related child"})).toBeTruthy();fireEvent.click(screen.getByRole("tab",{name:"History"}));expect(screen.getByText("sample.item.edit")).toBeTruthy();expect(get).toHaveBeenCalledTimes(1);
 rerender(<RecordPage {...props} id="B"/>);await waitFor(()=>expect(screen.getByRole("heading",{name:"B"})).toBeTruthy());expect(screen.getByRole("tab",{name:"Overview"}).getAttribute("aria-selected")).toBe("true");fireEvent.click(screen.getByRole("tab",{name:"History"}));rerender(<RecordPage {...props} id="B" source={{...source,scope:"member:2"}}/>);await waitFor(()=>expect(get).toHaveBeenCalledTimes(3));expect(screen.getByRole("tab",{name:"Overview"}).getAttribute("aria-selected")).toBe("true");
});
test("detail empty-value hiding preserves zero, false and empty collections and respects authorized field order",async()=>{
 const info={...entity,fields:[{name:"zero",title:"Zero",type:"integer" as const},{name:"flag",title:"Flag",type:"boolean" as const},{name:"empty",title:"Empty text",type:"text" as const},{name:"missing",title:"Missing",type:"text" as const},{name:"null",title:"Null",type:"text" as const},{name:"tags",title:"Tags",type:"tags" as const}]},view={record:{...record("A"),zero:0,flag:false,empty:"",null:null,tags:[],hidden:"Private"},tasks:[],approvals:[],processes:[],files:[],comments:[],related:[]} as unknown as RecordView,source:RecordSource={scope:"member",entity:()=>info,list:vi.fn(),get:vi.fn(async()=>view)};
 const props={source,type:info.type,id:"A",detailOnly:true,fields:["flag","zero","empty","missing","null","tags","hidden"]};const {rerender}=render(<RecordPage {...props} detailPresentation={{columns:2,hideNull:true}}/>);
 await waitFor(()=>expect(screen.getByText("Zero")).toBeTruthy());expect(screen.getByText("0")).toBeTruthy();expect(screen.getByText("Flag")).toBeTruthy();expect(screen.getByText("Tags")).toBeTruthy();for(const label of ["Empty text","Missing","Null","Private"])expect(screen.queryByText(label)).toBeNull();expect([...document.querySelectorAll("dt")].map(d=>d.textContent)).toEqual(["Flag","Zero","Tags"]);
 rerender(<RecordPage {...props} detailPresentation={{columns:1,hideNull:false}}/>);expect(screen.getByText("Empty text")).toBeTruthy();expect(screen.getByText("Missing")).toBeTruthy();expect(screen.getByText("Null")).toBeTruthy();expect(screen.queryByText("Private")).toBeNull();
});
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
 fireEvent.click(screen.getByRole("button",{name:"Finish editing"}));fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));stage("B remote","B retry");fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));await waitFor(()=>expect(submit).toHaveBeenCalledTimes(4));expect(submit.mock.calls[3]![0].revision).toBe(2);
 // A search change is a read-generation concern, not a teardown: the staged
 // retry and its controls survive it, and Finish editing still protects them.
 rerender(<RecordList {...props()} window={{...props().window,query:{search:"different",limit:100}}}/>);
 expect(screen.getByRole("button",{name:"Finish editing"})).toBeTruthy();expect(screen.getByRole("cell",{name:"B retry"})).toBeTruthy();
 fireEvent.click(screen.getByRole("button",{name:"Finish editing"}));expect(screen.queryByRole("button",{name:"Finish editing"})).toBeNull();
});
test.each(["scope","preview","preview then live"])("retiring the edit %s does not submit remaining rows",async(change)=>{
 const response=pending<{accepted:boolean}>(),submit=vi.fn(()=>response.promise),info={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]},source:RecordSource={scope:"actor",ruleIdentity:{me:"actor",timezone:"UTC"},entity:()=>info,list:vi.fn(),get:vi.fn()},data=[{...record("A"),title:"A old"},{...record("B"),title:"B old"}],port={schema:"sample.item.edit",scope:"binding",fields:["title"],maxRows:64,submit};
 const props={source,type:info.type,inlineEdit:port,window:{query:{limit:100},page:{records:data,total:2},maxOffset:100,onChange:vi.fn()}};const {rerender}=render(<RecordList {...props}/>);fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));for(const label of ["A old","B old"]){const cell=screen.getByRole("cell",{name:label});fireEvent.doubleClick(cell);const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value:label+" draft"}});fireEvent.keyDown(input,{key:"Enter"});}
 fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));await waitFor(()=>expect(submit).toHaveBeenCalledTimes(1));
 if(change==="scope")rerender(<RecordList {...props} source={{...source,scope:"other actor",ruleIdentity:{me:"other actor",timezone:"UTC"}}} inlineEdit={{...port,scope:"other binding"}}/>);
 else {rerender(<RecordList {...props} inlineEdit={{...port,preview:true}}/>);if(change==="preview then live")rerender(<RecordList {...props}/>);}
 await act(async()=>response.resolve({accepted:true}));expect(submit).toHaveBeenCalledTimes(1);
 expect(screen.queryByText(/Submitted 1 rows/)).toBeNull();expect(screen.queryByRole("cell",{name:"B old draft"})).toBeNull();
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

test("a caller window owns enum filters: selections change the owner query and counts answer the effective window",async()=>{
 const choiceInfo={...entity,fields:[{name:"state",title:"State",type:"choice" as const,choices:["open","held","done"]},{name:"owner",title:"Owner",type:"reference" as const,ref:"sample.member"}]};
 const aggregate=vi.fn(async()=>({rows:[{state:"open",count:2},{state:"held",count:1}],groups:["state"],measures:["count"]} as never));
 const source:RecordSource={scope:"member",entity:()=>choiceInfo,list:vi.fn(),get:vi.fn(),aggregate};
 const change=vi.fn();
 const props=()=>({source,type:choiceInfo.type,window:{query:{domain:[["owner","=","MEMBER-1"],["state","in",["open","held"]],["state","in",["open"]]],search:"term",sort:["id"],limit:1},facetQuery:()=>({domain:[["owner","=","MEMBER-1"],["state","in",["open","held"]]],search:"term",archived:true,set:{op:"union",inputs:[{domain:[["id","=","A"]]}]}}),page:{records:[record("WINDOW")],total:1},maxOffset:10,filters:{state:["open"]},onChange:change}});
 const {rerender}=render(<RecordList {...props()}/>);
 // A window that cannot carry predicates does not offer controls it cannot apply.
 rerender(<RecordList {...props()} window={{...props().window,filtersLocked:true}}/>);
 expect(screen.queryByRole("button",{name:/Filters/})).toBeNull();
 rerender(<RecordList {...props()}/>);
 fireEvent.click(screen.getByRole("button",{name:/Filters/}));
 await waitFor(()=>expect(aggregate).toHaveBeenCalled());
 // Counts answer the effective window: fixed conditions and search, without this field's own selection.
 expect((aggregate.mock.calls[0] as unknown[])[1]).toMatchObject({domain:[["owner","=","MEMBER-1"],["state","in",["open","held"]]],search:"term",archived:true,set:{op:"union",inputs:[{domain:[["id","=","A"]]}]}});
 fireEvent.click(screen.getByRole("checkbox",{name:"held"}));
 expect(change).toHaveBeenLastCalledWith({filters:{state:["open","held"]},offset:0});
 fireEvent.click(screen.getByRole("button",{name:"Clear filters"}));
 expect(change).toHaveBeenLastCalledWith({filters:{},offset:0});
});

test("saved enum filters are never silently discarded when a field becomes unavailable",async()=>{
 const choiceInfo={...entity,fields:[{name:"state",title:"State",type:"choice" as const,choices:["open","held"]}]};
 const list=vi.fn(async()=>({records:[record("A")],total:1}));
 const source:RecordSource={scope:"member",entity:()=>choiceInfo,list,get:vi.fn()};
 const onSave=vi.fn();
 render(<RecordList source={source} type={choiceInfo.type} initial={{filters:{state:["open"],gone:["x"]}}} onSave={onSave}/>);
 expect(screen.getByRole("alert").textContent).toContain("unavailable");
 expect(list).not.toHaveBeenCalled();
 expect((screen.getByRole("button",{name:"Save view…"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"Clear filters"}));
 await waitFor(()=>expect(list).toHaveBeenCalled());
 fireEvent.click(screen.getByRole("button",{name:"Save view…"}));
 expect((onSave.mock.calls[0] as unknown[])[0]).toMatchObject({filters:undefined});
});

test("the ID column header sorts the query, not the current page",async()=>{
 const textInfo={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]};
 const change=vi.fn(),list=vi.fn(async()=>({records:[record("A")],total:1}));
 const source:RecordSource={scope:"member",entity:()=>textInfo,list,get:vi.fn()};
 const {rerender}=render(<RecordList source={source} type={textInfo.type} window={{query:{sort:["id"],limit:1},page:{records:[record("A")],total:1},maxOffset:10,onChange:change}}/>);
 fireEvent.click(screen.getByRole("button",{name:"ID"}));
 expect(change).toHaveBeenLastCalledWith({sort:["-id"],offset:0});
 rerender(<RecordList source={source} type={textInfo.type} window={{query:{sort:["-id"],limit:1},page:{records:[record("A")],total:1},maxOffset:10,onChange:change}}/>);
 fireEvent.click(screen.getByRole("button",{name:"ID"}));
 expect(change).toHaveBeenLastCalledWith({sort:["id"],offset:0});
 // The full local table keeps its own ordering semantics: the header toggles
 // its local model (none → ascending), while the query reload follows.
 cleanup();
 render(<RecordList source={source} type={textInfo.type}/>);
 await waitFor(()=>expect(list).toHaveBeenCalled());
 fireEvent.click(screen.getByRole("button",{name:"ID"}));
 await waitFor(()=>expect(JSON.stringify((list.mock.calls.at(-1) as unknown[])[1])).toContain("\"sort\":[\"id\"]"));
 expect(screen.getByRole("columnheader",{name:"ID"}).getAttribute("aria-sort")).toBe("ascending");
 cleanup();
 render(<RecordList source={source} type={textInfo.type} window={{query:{sort:["id"],limit:1},page:{records:[],total:0},sortLocked:true,maxOffset:10,onChange:change}}/>);
 expect(screen.queryByRole("button",{name:"ID"})).toBeNull();
});

test("saving and reopening a view retains its original predicates, read shape and personal filters",async()=>{
 const info={...entity,fields:[{name:"state",title:"State",type:"choice" as const,choices:["open","held"]}]};
 const baseQuery={domain:[["state","in",["open","held"]]],sort:["id","-changed"],limit:12,archived:true,set:{op:"union",inputs:[{domain:[["id","=","A"]]}]}};
 const list=vi.fn(async()=>({records:[record("A")],total:1})),save=vi.fn(),source:RecordSource={scope:"member",entity:()=>info,list,get:vi.fn()};
 render(<RecordList source={source} type={info.type} onSave={save} window={{query:{...baseQuery,domain:[...baseQuery.domain,["state","in",["open"]]],search:"term"},baseQuery,sortLocked:true,filters:{state:["open"]},page:{records:[record("A")],total:1},maxOffset:100,onChange:vi.fn()}}/>);
 fireEvent.click(screen.getByRole("button",{name:"Save view…"}));
 const saved=save.mock.calls[0]![0];expect(saved.baseQuery).toMatchObject(baseQuery);expect(saved.filters).toEqual({state:["open"]});
 cleanup();render(<RecordList source={source} type={info.type} initial={saved}/>);
 await waitFor(()=>expect(list).toHaveBeenCalled());
 expect((list.mock.calls[0] as unknown[])[1]).toMatchObject({...baseQuery,domain:[...baseQuery.domain,["state","in",["open"]]],search:"term",offset:0});
 expect((screen.getByRole("combobox",{name:"Sort"}) as HTMLSelectElement).disabled).toBe(true);
});

test("facet reads follow the current scope and watch without applying late counts",async()=>{
 const info={...entity,fields:[{name:"state",title:"State",type:"choice" as const,choices:["open","held"]}]},first=pending<never>(),second=pending<never>(),stop=vi.fn();let changed!:()=>void;
 const aggregate=vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise).mockResolvedValue({rows:[{state:"held",count:7}]}),watchAggregate=vi.fn((_type:string,_query:unknown,next:()=>void)=>{changed=next;return stop;});
 const source:RecordSource={scope:"first",entity:()=>info,list:async()=>({records:[],total:0}),get:vi.fn(),aggregate,watchAggregate};
 const {rerender}=render(<RecordList source={source} type={info.type}/>);
 fireEvent.click(screen.getByRole("button",{name:"Filters"}));await waitFor(()=>expect(aggregate).toHaveBeenCalledTimes(1));
 rerender(<RecordList source={{...source,scope:"second"}} type={info.type}/>);await waitFor(()=>expect(aggregate).toHaveBeenCalledTimes(2));
 await act(async()=>second.resolve({rows:[{state:"held",count:3}]} as never));
 expect(screen.getByRole("checkbox",{name:"held"}).parentElement?.parentElement?.textContent).toContain("3");
 await act(async()=>first.resolve({rows:[{state:"open",count:99}]} as never));
 expect(screen.queryByRole("checkbox",{name:"open"})).toBeNull();
 await act(async()=>changed());expect(screen.getByRole("checkbox",{name:"held"}).parentElement?.parentElement?.textContent).toContain("7");
 expect(stop).toHaveBeenCalledTimes(1);
});

test("ordinary metadata refresh preserves staged rows and submits with the current port",async()=>{
 const info={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]},submit=vi.fn(async()=>({accepted:true})),updated=vi.fn(async()=>({accepted:true}));
 const source:RecordSource={scope:"actor/gen1",ruleIdentity:{me:"actor",timezone:"UTC"},entity:()=>info,list:vi.fn(),get:vi.fn()},port={schema:"sample.item.edit",scope:"gen1",fields:["title"],maxRows:64,submit};
 const props={source,type:info.type,inlineEdit:port,window:{query:{limit:10},page:{records:[{...record("A"),title:"Old"}],total:1},maxOffset:100,onChange:vi.fn()}};
 const {rerender}=render(<RecordList {...props}/>);fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));
 const cell=screen.getByRole("cell",{name:"Old"});fireEvent.doubleClick(cell);const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value:"Staged"}});fireEvent.keyDown(input,{key:"Enter"});
 rerender(<RecordList {...props} source={{...source,scope:"actor/gen2",entity:()=>({...info})}} inlineEdit={{...port,scope:"gen2",submit:updated}}/>);
 expect(screen.getByRole("cell",{name:"Staged"})).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));
 await waitFor(()=>expect(updated).toHaveBeenCalledTimes(1));expect(submit).not.toHaveBeenCalled();
});

test("replacing an unscoped record source cannot carry staged values into its matching record ID",()=>{
 const info={...entity,fields:[{name:"title",title:"Title",type:"text" as const}]},source:RecordSource={entity:()=>info,list:vi.fn(),get:vi.fn()},submit=vi.fn();
 const props={source,type:info.type,inlineEdit:{schema:"sample.item.edit",scope:"binding",fields:["title"],maxRows:64,submit},window:{query:{limit:10},page:{records:[{...record("A"),title:"First source"}],total:1},maxOffset:100,onChange:vi.fn()}};
 const {rerender}=render(<RecordList {...props}/>);fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));
 const cell=screen.getByRole("cell",{name:"First source"});fireEvent.doubleClick(cell);const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value:"Private draft"}});fireEvent.keyDown(input,{key:"Enter"});
 rerender(<RecordList {...props} source={{...source}} window={{...props.window,page:{records:[{...record("A"),title:"Second source"}],total:1}}}/>);
 expect(screen.queryByRole("cell",{name:"Private draft"})).toBeNull();expect(screen.getByRole("cell",{name:"Second source"})).toBeTruthy();expect(screen.queryByRole("button",{name:"Submit cell edits"})).toBeNull();expect(submit).not.toHaveBeenCalled();
});

test("enterprise references retain closed labels through their own source and require an explicit navigation port",async()=>{
 const next=pending<never>(),source:RecordSource={scope:"one",entity:()=>entity,list:vi.fn(),get:vi.fn(),enterpriseResolve:vi.fn(async()=>({elements:[{id:"A",name:"Closed unit",closed:true}]} as never))},open=vi.fn();
 const {rerender}=render(<EnterpriseReference source={source} value="A" onOpen={open}/>);
 await waitFor(()=>expect(screen.getByRole("button").textContent).toBe("Closed unit · Inactive"));
 fireEvent.click(screen.getByRole("button"));expect(open).toHaveBeenCalledWith("enterprise.element","A");
 rerender(<EnterpriseReference source={{...source,scope:"two",enterpriseResolve:()=>next.promise}} value="A"/>);
 expect(screen.getByRole("button").textContent).toBe("A");expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(true);
 await act(async()=>next.resolve({elements:[]} as never));expect(screen.getByRole("button").textContent).toBe("A");
});

test("staged cell edits survive a window filter change and keep their opened revision",async()=>{
 const textInfo={...entity,fields:[{name:"title",title:"Title",type:"text" as const},{name:"state",title:"State",type:"choice" as const,choices:["open"]}]};
 const submit=vi.fn(async()=>({accepted:true})),source:RecordSource={scope:"actor",entity:()=>textInfo,list:vi.fn(),get:vi.fn()};
 const data=[{...record("A"),title:"A old"},{...record("B"),title:"B old"}];
 const props=(filters?:Record<string,string[]>)=>({source,type:textInfo.type,inlineEdit:{schema:"sample.item.edit",scope:"binding",fields:["title"],maxRows:64,submit},
   window:{query:{limit:100,...(filters?{domain:[["state","in",Object.values(filters).flat()]]}:{})},page:{records:data,total:2},maxOffset:100,filters,onChange:vi.fn()}});
 const {rerender}=render(<RecordList {...props()}/>);
 fireEvent.click(screen.getByRole("button",{name:"Edit cells"}));
 const cell=screen.getByRole("cell",{name:"B old"});fireEvent.doubleClick(cell);
 const input=cell.querySelector("input")!;fireEvent.change(input,{target:{value:"B staged"}});fireEvent.keyDown(input,{key:"Enter"});
 // A filter change re-reads the window; the staged row and its baseline stay.
 rerender(<RecordList {...props({state:["open"]})}/>);
 expect(screen.getByRole("cell",{name:"B staged"})).toBeTruthy();
 fireEvent.click(screen.getByRole("button",{name:"Submit cell edits"}));
 await waitFor(()=>expect(submit).toHaveBeenCalledTimes(1));
 const [staged,patch]=submit.mock.calls[0] as unknown as [EntityRecord,Record<string,unknown>];expect(staged.revision).toBe(1);expect(patch).toEqual({title:"B staged"});
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

test("scoped list wrappers do not refetch and background revisions retain rows while stale responses or changed scopes cannot leak", async () => {
 const requests:ReturnType<typeof pending<RecordPageData>>[]=[],list=vi.fn(()=>{const request=pending<RecordPageData>();requests.push(request);return request.promise;});
 const source=(revision=0,scope="member"):RecordSource=>({scope,revision,entity:()=>({...entity}),list,get:vi.fn()});
 const {rerender}=render(<RecordList source={source()} type={entity.type}/>);
 await waitFor(()=>expect(requests).toHaveLength(1));await act(async()=>requests[0]!.resolve({records:[record("OLD")],total:1}));
 rerender(<RecordList source={source()} type={entity.type}/>);
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,250));});expect(list).toHaveBeenCalledTimes(1);
 rerender(<RecordList source={source(1)} type={entity.type}/>);expect(screen.getByRole("cell",{name:"OLD"})).toBeTruthy();await waitFor(()=>expect(requests).toHaveLength(2));
 rerender(<RecordList source={source(2)} type={entity.type}/>);await waitFor(()=>expect(requests).toHaveLength(3));await act(async()=>requests[1]!.resolve({records:[record("STALE")],total:1}));expect(screen.queryByRole("cell",{name:"STALE"})).toBeNull();expect(screen.getByRole("cell",{name:"OLD"})).toBeTruthy();
 await act(async()=>requests[2]!.resolve({records:[record("NEW")],total:1}));expect(screen.getByRole("cell",{name:"NEW"})).toBeTruthy();
 rerender(<RecordList source={source(2,"other-member")} type={entity.type}/>);expect(screen.queryByRole("cell",{name:"NEW"})).toBeNull();
 await waitFor(()=>expect(requests).toHaveLength(4));await act(async()=>requests[3]!.resolve({records:[],total:0}));
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

test("table toolbar changes keep window paging and density changes do not issue a new read",async()=>{
 const source:RecordSource={scope:"member",entity:()=>entity,list:vi.fn(),get:vi.fn()},change=vi.fn(),window={query:{sort:["id"],limit:1},page:{records:[record("A")],total:2} as RecordPageData,maxOffset:100,onChange:change};
 const {rerender}=render(<RecordList source={source} type={entity.type} window={window} tablePresentation={{density:"compact",showToolbar:true}}/>);await waitFor(()=>expect(screen.getByRole("cell",{name:"A"})).toBeTruthy());fireEvent.change(screen.getByRole("combobox",{name:"Record density"}),{target:{value:"normal"}});expect((screen.getByRole("combobox",{name:"Record density"}) as HTMLSelectElement).value).toBe("normal");expect(source.list).not.toHaveBeenCalled();
 rerender(<RecordList source={source} type={entity.type} window={window} tablePresentation={{density:"compact",showToolbar:false}}/>);expect(screen.queryByRole("combobox",{name:"Record density"})).toBeNull();expect(screen.queryByRole("textbox",{name:"Search"})).toBeNull();expect(screen.queryByRole("combobox",{name:"Sort"})).toBeNull();fireEvent.click(screen.getByRole("button",{name:"Next page"}));expect(change).toHaveBeenCalledWith({offset:1});expect(source.list).not.toHaveBeenCalled();
});

test("hidden record windows release their live lease, preserve search and register again when shown", async () => {
 const release=vi.fn(),watch=vi.fn(()=>release),list=vi.fn(async()=>({records:[record("VISIBLE")],total:1}));
 const source:RecordSource={scope:"member",entity:()=>entity,list,get:vi.fn(),watchList:watch};
 const view=(visible:boolean)=><ViewVisibilityContext.Provider value={visible}><RecordList source={source} type={entity.type}/></ViewVisibilityContext.Provider>;
 const {rerender}=render(view(true));await waitFor(()=>expect(screen.getByText("VISIBLE")).toBeTruthy());
 const search=screen.getByPlaceholderText(/Search/);fireEvent.change(search,{target:{value:"retained search"}});
 await waitFor(()=>expect(watch).toHaveBeenCalledTimes(2));
 rerender(view(false));expect(release).toHaveBeenCalledTimes(2);const count=list.mock.calls.length;
 rerender(view(false));await new Promise(resolve=>setTimeout(resolve,180));expect(list).toHaveBeenCalledTimes(count);
 rerender(view(true));expect((screen.getByPlaceholderText(/Search/) as HTMLInputElement).value).toBe("retained search");await waitFor(()=>expect(watch).toHaveBeenCalledTimes(3));
});

test("RecordList does not guess a lifecycle from an unrelated choice field", async () => {
  const info = { ...entity, fields: [{ name: "priority", title: "Priority", type: "choice" as const, choices: ["low", "high"] }] };
  const source: RecordSource = { entity: () => info, list: async () => ({ records: [{ ...record("A"), priority: "low" }], total: 1 }), get: vi.fn() };
  const props = { source, type: info.type, initial: { view: "kanban" as const } };
  const { rerender } = render(<RecordList {...props} />);
  await waitFor(() => expect(screen.getByRole("cell", { name: "A" })).toBeTruthy());
  expect(screen.queryByRole("button", { name: "Board" })).toBeNull();
  rerender(<RecordList {...props} kanban={{ stateField: "priority" }} />);
  fireEvent.click(screen.getByRole("button", { name: "Board" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "A" })).toBeTruthy());
  expect(screen.getByRole("region", { name: "Items" })).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: "Move A with action" })).toBeNull();
});

test("RecordList uses only caller-offered board commands and keeps empty or absent moves read-only", async () => {
  const lifecycle = { field: "state", initial: "open", states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }, { name: "cancelled", title: "Cancelled" }], transitions: [
    { name: "finish", schema: "sample.item.finish", title: "Finish", from: ["open"], to: ["done"] },
    { name: "cancel", schema: "sample.item.cancel", title: "Cancel item", from: ["open"], to: ["cancelled"] },
  ] };
  const info = { ...entity, fields: [{ name: "state", title: "State", type: "choice" as const }], lifecycle };
  const row = { ...record("A"), state: "open" }, moved = vi.fn();
  const source: RecordSource = { entity: () => info, list: async () => ({ records: [row], total: 1 }), get: vi.fn() };
  const props = { source, type: info.type, onMove: moved };
  const { rerender } = render(<RecordList {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Board" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "A" })).toBeTruthy());
  expect(screen.queryByRole("combobox", { name: "Move A with action" })).toBeNull();
  rerender(<RecordList {...props} moves={[{ schema: "sample.item.finish", title: "Finish", from: ["open"], to: "done" }]} />);
  const command = screen.getByRole("combobox", { name: "Move A with action" });
  expect(screen.queryByRole("option", { name: "Cancel item → Cancelled" })).toBeNull();
  fireEvent.change(command, { target: { value: "sample.item.finish" } });
  expect(moved).toHaveBeenCalledExactlyOnceWith(row, "sample.item.finish");
  rerender(<RecordList {...props} moves={[]} />);
  expect(screen.queryByRole("combobox", { name: "Move A with action" })).toBeNull();
  expect(screen.getByRole("button", { name: "A" }).closest("[draggable]")?.getAttribute("draggable")).toBe("false");
});


test("enterprise picker updates names live and retains an inactive existing reference", async () => {
  let changed: (() => void) | undefined;
  let name = "Operations", inactive = false;
  const selected = () => ({id: "ORG", name, stereotype: "ActualOrganization", closed: inactive});
  const stop = vi.fn();
  const source: RecordSource = {
    scope: "enterprise-reader", entity: () => undefined, list: vi.fn(), get: vi.fn(),
    enterpriseQuery: vi.fn(async () => ({elements: inactive ? [] : [selected()]})),
    enterpriseResolve: vi.fn(async () => ({elements: [selected()]})),
    watchEnterprise: listener => { changed = listener; return stop; },
  };
  const onChange = vi.fn();
  const view = render(<EnterpriseElementLookup source={source} value="ORG" stereotypes={["ActualOrganization"]} onChange={onChange} ariaLabel="Organisation" />);
  await screen.findByRole("option", {name: "Operations"});
  name = "Customer operations";
  await act(async () => changed?.());
  await screen.findByRole("option", {name});
  inactive = true;
  await act(async () => changed?.());
  await screen.findByRole("option", {name: "Customer operations · Inactive"});
  expect((screen.getByRole("combobox", {name: "Organisation"}) as HTMLSelectElement).value).toBe("ORG");
  expect(onChange).not.toHaveBeenCalled();
  view.unmount();
  expect(stop).toHaveBeenCalledOnce();
});

test("record actions wait for the refreshed original revision after an accepted change",async()=>{
 const next=pending<RecordView>();let changed=()=>{};
 const view={record:record("A"),history:[],related:[],linked:[],activity:[],processes:[],approvals:[],tasks:[],files:[],comments:[],following:false} as RecordView;
 const source:RecordSource={scope:"same-member",entity:()=>entity,list:vi.fn(),get:vi.fn().mockResolvedValueOnce(view).mockImplementation(()=>next.promise),watchRecord:(_type,_id,notify)=>{changed=notify;return()=>{};}};
 render(<RecordPage source={source} type={entity.type} id="A" actions={r=><button>{`Act on revision ${r.revision}`}</button>}/>);
 await waitFor(()=>expect((screen.getByRole("button",{name:"Act on revision 1"}) as HTMLButtonElement).disabled).toBe(false));
 act(()=>changed());
 expect(screen.getByText("Refreshing record…")).toBeTruthy();
 expect(screen.getByRole("button",{name:"Act on revision 1"}).closest("fieldset")?.disabled).toBe(true);
 await act(async()=>next.resolve({...view,record:{...view.record,revision:2}}));
 expect(screen.getByRole("button",{name:"Act on revision 2"}).closest("fieldset")?.disabled).toBe(false);
});

test("structured line inputs keep their drafts on their own row and field", async () => {
  const linesEntity = entityFrom({ ...entity, fields: [
    { name: "qty", title: "Qty", type: "integer" },
    { name: "items", title: "Items", type: "lines", fields: [{ name: "qty", title: "Qty", type: "integer" }, { name: "note", title: "Note", type: "text" }] },
  ] } as never);
  const editor = linesEntity.fields.items!.editor!;
  const change = vi.fn();
  const view = render(editor({ id: "record:items", value: [{ qty: 1, note: "first" }, { qty: 2, note: "second" }] as never, onChange: change }));
  const spins = screen.getAllByRole("spinbutton");
  expect(spins).toHaveLength(2);
  // An invalid exponent in row one stays in row one; row two keeps its value.
  fireEvent.change(spins[0]!, { target: { value: "1e-" } });
  expect(change).not.toHaveBeenCalled();
  fireEvent.blur(spins[0]!);
  await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
  expect((spins[1] as HTMLInputElement).value).toBe("2");
  // Rows carry their own draft path, visible on the element.
  const paths = screen.getAllByRole("spinbutton").map((node) => node.getAttribute("data-draft-key"));
  expect(paths[0]).not.toEqual(paths[1]);
  expect(paths[0]).toContain("line:");
  // Deleting the first row removes its draft and moves the remaining row's
  // input with its object, not its index.
  const before = screen.getAllByRole("spinbutton")[1]!.getAttribute("data-draft-key");
  fireEvent.click(screen.getAllByRole("button", { name: "Remove line" })[0]!);
  expect(change).toHaveBeenCalledTimes(1);
  const next = change.mock.calls[0]![0] as unknown[];
  expect(next).toHaveLength(1);
  // The surviving row is the same object, so its inputs move with it.
  view.rerender(editor({ id: "record:items", value: next as never, onChange: change }));
  const remaining = screen.getAllByRole("spinbutton");
  expect(remaining).toHaveLength(1);
  expect(remaining[0]!.getAttribute("data-draft-key")).toBe(before);
  view.unmount();
});
