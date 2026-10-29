// @vitest-environment jsdom
import {afterEach,expect,test,vi} from "vitest";
import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {useState} from "react";
import type {EntityInfo} from "@platform/ui";
import {FormRulePreview} from "./FormRulePreview";
const engine=vi.hoisted(()=>({evaluateRule:vi.fn(),ruleDependencies:vi.fn()}));
vi.mock("./rule-engine",()=>({...engine,initializeRules:()=>Promise.reject(new Error("Rule module is unavailable."))}));
afterEach(()=>{cleanup();});

test("a failed local rule module leaves the original editable form usable",async()=>{
 const info={type:"sample.item",fields:[{name:"qty",title:"Quantity",type:"integer"},{name:"amount",title:"Amount",type:"decimal",readOnly:true}],localRules:{amount:{kind:"decimal",ref:"qty"}}}as unknown as EntityInfo;
 const submitted=vi.fn();
 function Form(){const [qty,setQty]=useState("2");return <form onSubmit={event=>{event.preventDefault();submitted({qty:Number(qty)});}}><label>Quantity<input value={qty} onChange={event=>setQty(event.target.value)}/></label><FormRulePreview info={info} values={{qty:Number(qty)}} clock={{me:"member",tenant:"tenant",now:"2026-10-28T00:00:00Z",today:"2026-10-27"}}/><button>Save</button></form>;}
 render(<Form/>);
 await screen.findByRole("alert");
 fireEvent.change(screen.getByLabelText("Quantity"),{target:{value:"3"}});
 fireEvent.click(screen.getByRole("button",{name:"Save"}));
 expect(submitted).toHaveBeenCalledWith({qty:3});
 expect(engine.evaluateRule).not.toHaveBeenCalled();
});
