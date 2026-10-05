import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,expect,it} from "vitest";
import {ApplicationHeader} from "./ApplicationHeader";
import {useTheme} from "../theme";

afterEach(()=>{cleanup();delete document.documentElement.dataset.theme;});
it("keeps declared page order, invokes native callbacks and preserves vertical collapse",()=>{
 const calls:string[]=[];
 render(<ApplicationHeader header={{variant:"vertical",title:"Original",collapsed:true,items:[{kind:"tabs",pages:["second","first","private"]},{kind:"button",label:"Refresh",action:"refresh"},{kind:"button",label:"Theme",action:"theme"}]}} pages={[{name:"first",title:"First"},{name:"second",title:"Second"}]} currentPage="first" onPage={name=>calls.push(name)} onRefresh={()=>calls.push("refresh")} onTheme={()=>calls.push("theme")}><p>Original content</p></ApplicationHeader>);
 expect(screen.queryByText("private")).toBeNull();fireEvent.click(screen.getByRole("button",{name:"Expand application navigation"}));
 const nav=within(screen.getByRole("navigation"));expect(nav.getAllByRole("button").map(b=>b.textContent)).toEqual(["Second","First"]);
 fireEvent.click(nav.getByRole("button",{name:"Second"}));fireEvent.click(screen.getByRole("button",{name:"Refresh"}));fireEvent.click(screen.getByRole("button",{name:"Theme"}));expect(calls).toEqual(["second","refresh","theme"]);expect(screen.getByText("Original content")).toBeTruthy();
});
it("theme controls share the existing UI token theme without modifying application definitions",()=>{
 function Theme(){const {theme,toggle}=useTheme();return <button onClick={toggle}>{theme}</button>;}
 render(<><Theme/><Theme/></>);fireEvent.click(screen.getAllByRole("button",{name:"light"})[0]!);expect(document.documentElement.dataset.theme).toBe("dark");expect(screen.getAllByRole("button",{name:"dark"})).toHaveLength(2);
});
