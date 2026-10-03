import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {AssetDirectory,type AssetDirectoryItem} from "./AssetDirectory";
afterEach(cleanup);
const item=(id:string,kind="page"):AssetDirectoryItem=>({id,label:"Operations / <b>Same path</b>",title:"Actual original title",asset:{ref:{app:"sample",kind,name:id},sourceVersion:"retained-v3"}});
test("directory entries show literal original paths and exact retained bindings, and only explicitly bound pages open",()=>{
 const items=[item("assets"),item("query","query")],open=vi.fn(),view=render(<AssetDirectory items={items} onOpen={open}/>);expect(screen.getAllByText("Operations / <b>Same path</b>")).toHaveLength(2);expect(screen.getByText("sample/page/assets · retained-v3")).toBeTruthy();expect(screen.getByText("sample/query/query · retained-v3")).toBeTruthy();expect(view.container.querySelector("a")).toBeNull();expect(view.container.querySelector("b")).toBeNull();expect(screen.getAllByRole("button")).toHaveLength(1);fireEvent.click(screen.getByRole("button"));expect(open).toHaveBeenCalledWith(items[0]);
 view.rerender(<AssetDirectory items={items} onOpen={open} enabled={false}/>);fireEvent.click(screen.getByRole("button"));expect(open).toHaveBeenCalledTimes(1);view.rerender(<AssetDirectory items={items}/>);expect(screen.queryByRole("button")).toBeNull();
});
test("unmapped assets remain empty instead of becoming source placeholders and duplicate or missing bindings refuse",()=>{
 const original=item("page"),view=render(<AssetDirectory items={[]}/>);expect(screen.getByText("No mapped assets are available.")).toBeTruthy();expect(screen.queryByText("Datasets / telemetry_q3")).toBeNull();
 for(const items of [[original,original],Array.from({length:5},(_,index)=>item(String(index))),[{...original,asset:{...original.asset,sourceVersion:""}}],[{...original,asset:{...original.asset,ref:{...original.asset.ref,kind:"uri"}}}]]){view.rerender(<AssetDirectory items={items}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByRole("list")).toBeNull();}
});
test("each page keeps its own explicit navigation binding without making other original entries clickable",()=>{
 const items=[item("bound"),item("unbound")],open=vi.fn(),canOpen=(entry:AssetDirectoryItem)=>entry===items[0],view=render(<AssetDirectory items={items} onOpen={open} canOpen={canOpen}/>);
 expect(screen.getAllByRole("button")).toHaveLength(1);expect(screen.getAllByRole("listitem")).toHaveLength(2);expect(screen.getByText("sample/page/unbound · retained-v3").closest("button")).toBeNull();fireEvent.click(screen.getByRole("button"));expect(open).toHaveBeenCalledTimes(1);expect(open).toHaveBeenCalledWith(items[0]);
 view.rerender(<AssetDirectory items={items} onOpen={open} canOpen={canOpen} enabled={false}/>);expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(true);fireEvent.click(screen.getByRole("button"));expect(open).toHaveBeenCalledTimes(1);
 view.rerender(<AssetDirectory items={items} onOpen={open}/>);expect(screen.getAllByRole("button")).toHaveLength(2);view.rerender(<AssetDirectory items={items} canOpen={canOpen}/>);expect(screen.queryByRole("button")).toBeNull();
});
