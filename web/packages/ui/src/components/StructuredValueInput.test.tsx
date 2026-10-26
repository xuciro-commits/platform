import {useState} from "react";
import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {StructuredValueInput} from "../fields/StructuredValueInput";
import {InputDraftProvider,useInputDrafts} from "../fields/draft";

afterEach(cleanup);
function DraftStatus(){
 const drafts=useInputDrafts();
 return <button disabled={drafts?.invalid}>Save draft</button>;
}
function EditableValue({initial,onChange}:{initial:unknown;onChange:(value:unknown)=>void}){
 const [value,setValue]=useState(initial);
 return <InputDraftProvider scope="structured-value"><StructuredValueInput label="Configuration" value={value} onChange={next=>{setValue(next);onChange(next);}}/><output data-testid="value">{JSON.stringify(value)}</output><DraftStatus/></InputDraftProvider>;
}

test("structured visual values preserve nested types and array order without changing the supplied value",()=>{
 const initial={query:{limit:0,enabled:false,empty:null},order:["b","a"],empty:[]},original=structuredClone(initial),change=vi.fn();
 render(<EditableValue initial={initial} onChange={change}/>);
 expect(change).not.toHaveBeenCalled();
 const arrayType=screen.getByRole("combobox",{name:"Configuration.order[0] type"});
 expect(within(arrayType).queryByRole("option",{name:"Not set"})).toBeNull();
 expect(within(arrayType).getByRole("option",{name:"Null"})).toBeTruthy();
 fireEvent.change(screen.getByLabelText("Configuration.query.limit"),{target:{value:"3"}});
 const expected={...initial,query:{...initial.query,limit:3}};
 expect(change).toHaveBeenCalledExactlyOnceWith(expected);
 expect(JSON.parse(screen.getByTestId("value").textContent!)).toEqual(expected);
 expect(initial).toEqual(original);
});

test("invalid visual numbers and duplicate object names retain the last valid value and block submission",()=>{
 const initial={count:0},change=vi.fn();
 render(<EditableValue initial={initial} onChange={change}/>);
 const count=screen.getByLabelText("Configuration.count") as HTMLInputElement;
 fireEvent.change(count,{target:{value:"-"}});
 expect(count.value).toBe("-");
 expect((screen.getByRole("button",{name:"Save draft"}) as HTMLButtonElement).disabled).toBe(true);
 expect(JSON.parse(screen.getByTestId("value").textContent!)).toEqual(initial);
 expect(change).not.toHaveBeenCalled();
 fireEvent.change(count,{target:{value:"1.5"}});
 expect((screen.getByRole("button",{name:"Save draft"}) as HTMLButtonElement).disabled).toBe(false);
 expect(change).toHaveBeenCalledExactlyOnceWith({count:1.5});
 const region=screen.getByRole("group",{name:"Configuration"});
 const name=within(region).getByRole("textbox",{name:"Property name"}) as HTMLInputElement;
 fireEvent.change(name,{target:{value:"count"}});
 fireEvent.click(within(region).getByRole("button",{name:"Add property"}));
 expect(name.value).toBe("count");
 expect(change).toHaveBeenCalledTimes(1);
 expect(JSON.parse(screen.getByTestId("value").textContent!)).toEqual({count:1.5});
 expect((screen.getByRole("button",{name:"Save draft"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.change(name,{target:{value:"newProperty"}});
 fireEvent.click(within(region).getByRole("button",{name:"Add property"}));
 expect(change).toHaveBeenLastCalledWith({count:1.5,newProperty:null});
 expect((screen.getByRole("button",{name:"Save draft"}) as HTMLButtonElement).disabled).toBe(false);
});
