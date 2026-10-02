import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {RecordList,type EntityRecord,type RecordSource} from "./Records";
afterEach(cleanup);
test("card layouts share the caller window, keep record identity and show only permission-filtered summaries",()=>{
 const info={app:"sample",type:"sample.item",title:"Item",plural:"Items",display:"name",fields:[{name:"name",title:"Name",type:"text" as const},{name:"qty",title:"Quantity",type:"integer" as const}],standard:[]},record={id:"A",revision:1,created:{},changed:{},name:"Card A",qty:7,secret:"Private"} as EntityRecord,list=vi.fn(),get=vi.fn(),select=vi.fn(),source:RecordSource={scope:"member1",entity:()=>info,list,get},props={source,type:info.type,fields:["qty","secret"],cards:{layout:"grid" as const,labelField:"name"},selectedId:"A",onOpen:select,window:{query:{limit:1},page:{records:[record],total:42},maxOffset:100,onChange:vi.fn()}};
 const {rerender}=render(<RecordList {...props}/>);expect(screen.getByRole("button",{name:"Card A"}).getAttribute("aria-pressed")).toBe("true");expect(screen.queryByText("Private")).toBeNull();expect(screen.getByText("7")).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"Card A"}));expect(select).toHaveBeenCalledWith(record);fireEvent.click(screen.getByRole("button",{name:"List"}));expect(screen.getByRole("button",{name:"List"}).getAttribute("aria-pressed")).toBe("true");expect(list).not.toHaveBeenCalled();expect(get).not.toHaveBeenCalled();
 rerender(<RecordList {...props} source={{...source,scope:"member2"}}/>);expect(screen.getByRole("button",{name:"Grid"}).getAttribute("aria-pressed")).toBe("true");
 rerender(<RecordList {...props} window={{...props.window,page:undefined,error:"Denied"}}/>);expect(screen.queryByRole("button",{name:"Card A"})).toBeNull();expect(screen.getByRole("alert").textContent).toContain("Denied");
});
