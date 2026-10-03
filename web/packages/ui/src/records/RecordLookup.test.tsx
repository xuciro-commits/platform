import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {RecordLookup} from "./RecordLookup";
afterEach(cleanup);
test("lookup consumes the original window without another read, emits original IDs and removes obsolete candidates",()=>{
 const reads=vi.fn(),select=vi.fn(),change=vi.fn(),source={scope:"member",entity:()=>({type:"sample.note",display:"title",fields:[]}),list:reads,get:vi.fn()} as any,records=[{id:"A",revision:1,title:"Original A"},{id:"B",revision:1,title:"Original B"}] as any,window={query:{limit:20,sort:["id"]},page:{records,total:100},maxOffset:0,onChange:change};
  const {rerender}=render(<RecordLookup source={source} type="sample.note" window={window} ariaLabel="Picker" labelField="title" onChange={select}/>);fireEvent.focus(screen.getByRole("combobox",{name:"Picker"}));expect(reads).not.toHaveBeenCalled();expect(screen.getAllByRole("option")).toHaveLength(2);fireEvent.click(screen.getByRole("option",{name:"B · Original B"}));expect(select).toHaveBeenCalledWith("B");
 fireEvent.focus(screen.getByRole("combobox",{name:"Picker"}));fireEvent.keyDown(screen.getByRole("option",{name:"A · Original A"}),{key:"Escape"});expect(screen.queryAllByRole("option")).toHaveLength(0);
 fireEvent.focus(screen.getByRole("combobox",{name:"Picker"}));fireEvent.change(screen.getByRole("combobox",{name:"Picker"}),{target:{value:"other"}});expect(change).toHaveBeenCalledWith({search:"other",offset:0});rerender(<RecordLookup source={source} type="sample.note" window={{...window,query:{...window.query,search:"other"},page:undefined}} ariaLabel="Picker" labelField="title" onChange={select}/>);expect(screen.queryAllByRole("option")).toHaveLength(0);
 rerender(<RecordLookup source={source} type="sample.note" window={{...window,page:undefined,error:"Denied"}} ariaLabel="Picker" onChange={select}/>);expect(screen.getByRole("alert")).toBeTruthy();expect(reads).not.toHaveBeenCalled();
});
