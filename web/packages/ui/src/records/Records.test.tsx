import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { RecordList, RecordPage, type EntityRecord, type RecordPageData, type RecordSource, type RecordView } from "./Records";

afterEach(cleanup);
Element.prototype.getBoundingClientRect = () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON: () => ({}) });
for (const [key, value] of [["offsetWidth", 800], ["offsetHeight", 280]] as const) Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
const pending = <T,>() => { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; };
const entity = { app: "sample", type: "sample.item", title: "Item", plural: "Items", display: "id", fields: [], standard: [] };
const record = (id: string) => ({ id, revision: 1, created: {}, changed: {} } as EntityRecord);

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
