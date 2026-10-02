import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {entityFrom,type EntityInfo} from "./Records";
import {RecordForm} from "../components/EntityForm";
afterEach(cleanup);
test("generic line edits keep owned structured metadata without exposing a text editor for it",async()=>{
 const info:EntityInfo={app:"sample",type:"sample.definition",title:"Definition",plural:"Definitions",display:"id",standard:[],fields:[{name:"fields",title:"Fields",type:"lines",fields:[{name:"name",title:"Name",type:"text"},{name:"property",title:"Property",type:"text",aside:true}]}]};
 const property={ref:{app:"sample",kind:"property-type",name:"quantity"},sourceVersion:"one"},submit=vi.fn();
 render(<RecordForm entity={entityFrom(info)} defaultValues={{fields:[{name:"before",property}]}} onSubmit={submit}/>);
 expect(screen.queryByText("Property")).toBeNull();expect(screen.getByRole("button",{name:"Add line"})).toBeTruthy();
 fireEvent.change(screen.getByRole("textbox"),{target:{value:"after"}});fireEvent.click(screen.getByRole("button",{name:"Save"}));
 await waitFor(()=>expect(submit).toHaveBeenCalled());expect(submit.mock.calls[0]![0].fields[0]).toEqual({name:"after",property});
});
