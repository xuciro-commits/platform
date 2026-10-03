import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {StepSelector,TabSelector} from "./IndexedChoices";
import {ChoiceInput} from "./ChoiceInput";
import {validChoiceInput} from "./choice";
afterEach(cleanup);
const options=[{value:"0",label:"Review"},{value:"1",label:"Review"},{value:"2",label:""}];
test("steps preserve duplicate labels with distinct original values and remain controlled",()=>{
 const writes:string[]=[],props={options,label:"Build steps",onChange:(value:string)=>writes.push(value)},view=render(<StepSelector {...props} value="1"/>);
 const steps=screen.getAllByRole("button");expect(steps[0]!.getAttribute("aria-pressed")).toBe("false");expect(steps[1]!.getAttribute("aria-current")).toBe("step");expect(screen.getAllByRole("button",{name:"Review"})).toHaveLength(2);
 fireEvent.click(steps[0]!);expect(writes).toEqual(["0"]);expect(steps[1]!.getAttribute("aria-current")).toBe("step");
 view.rerender(<StepSelector {...props} value="0"/>);expect(steps[0]!.getAttribute("aria-current")).toBe("step");expect(steps[1]!.hasAttribute("aria-current")).toBe(false);
 fireEvent.click(screen.getByRole("button",{name:"Step 3"}));expect(writes.at(-1)).toBe("2");
});
test("empty or unmatched steps never fabricate the first current choice and require an explicit permitted repair",()=>{
 const writes:string[]=[],props={options,label:"Steps",onChange:(value:string)=>writes.push(value)},view=render(<StepSelector {...props} value=""/>);
 expect(screen.getByRole("status").textContent).toBe("No current choice is selected.");expect(screen.getAllByRole("button").every(button=>!button.hasAttribute("aria-current"))).toBe(true);expect(writes).toEqual([]);
 for(const value of ["retired","9","01","-1"]){view.rerender(<StepSelector {...props} value={value}/>);expect(screen.getByRole("status").textContent).toContain(value);expect(screen.getAllByRole("button").every(button=>button.getAttribute("aria-pressed")==="false")).toBe(true);}
 fireEvent.click(screen.getAllByRole("button",{name:"Review"})[1]!);expect(writes).toEqual(["1"]);
});
test("steps support native keyboard focus traversal and disabled or read-only state cannot write",()=>{
 const writes:string[]=[],props={options,label:"Steps",value:"1",onChange:(value:string)=>writes.push(value)},view=render(<StepSelector {...props}/>);
 const steps=screen.getAllByRole("button");steps[0]!.focus();fireEvent.keyDown(steps[0]!,{key:"ArrowRight"});expect(document.activeElement).toBe(steps[1]);fireEvent.keyDown(steps[1]!,{key:"End"});expect(document.activeElement).toBe(steps[2]);fireEvent.keyDown(steps[2]!,{key:"Home"});expect(document.activeElement).toBe(steps[0]);expect(writes).toEqual([]);
 view.rerender(<StepSelector {...props} enabled={false}/>);fireEvent.click(steps[0]!);expect(writes).toEqual([]);expect(steps.every(button=>(button as HTMLButtonElement).disabled)).toBe(true);
 view.rerender(<StepSelector options={options} label="Steps" value="1"/>);expect(steps[1]!.getAttribute("aria-current")).toBe("step");expect(steps.every(button=>(button as HTMLButtonElement).disabled)).toBe(true);
});
test("semantic tabs retain distinct identities and roving focus without inventing panel content",()=>{
 const writes:string[]=[],props={options,label:"Review tabs",onChange:(value:string)=>writes.push(value)},view=render(<TabSelector {...props} value="1"/>);
 const group=screen.getByRole("tablist",{name:"Review tabs"}),tabs=within(group).getAllByRole("tab");expect(tabs[1]!.getAttribute("aria-selected")).toBe("true");expect(tabs.map(tab=>tab.tabIndex)).toEqual([-1,0,-1]);expect(screen.queryByRole("tabpanel")).toBeNull();expect(screen.getAllByRole("tab",{name:"Review"})).toHaveLength(2);
 tabs[1]!.focus();fireEvent.keyDown(tabs[1]!,{key:"ArrowRight"});expect(document.activeElement).toBe(tabs[2]);expect(writes).toEqual([]);expect(tabs[1]!.getAttribute("aria-selected")).toBe("true");
 fireEvent.click(tabs[2]!);expect(writes).toEqual(["2"]);view.rerender(<TabSelector {...props} value="2"/>);expect(tabs[2]!.getAttribute("aria-selected")).toBe("true");expect(screen.getByRole("tab",{name:"Tab 3"})).toBe(tabs[2]);
});
test("tabs keep empty and unknown current values unselected and respect read-only state",()=>{
 const props={options,label:"Tabs"},view=render(<TabSelector {...props} value="" onChange={()=>{}}/>);expect(screen.getByRole("status").textContent).toBe("No current choice is selected.");expect(screen.getAllByRole("tab").every(tab=>tab.getAttribute("aria-selected")==="false")).toBe(true);expect(screen.getAllByRole("tab").map(tab=>tab.tabIndex)).toEqual([0,-1,-1]);
 view.rerender(<TabSelector {...props} value="out of range"/>);expect(screen.getByRole("status").textContent).toContain("out of range");expect(screen.getAllByRole("tab").every(tab=>(tab as HTMLButtonElement).disabled)).toBe(true);
});
test("generic ordered choices reject duplicate identities and label budgets but permit literal duplicate labels",()=>{
 const view=render(<StepSelector options={[{value:"A",label:"<b>Same</b>"},{value:"B",label:"<b>Same</b>"}]} value="B" label="Steps"/>);expect(screen.getAllByText("<b>Same</b>")).toHaveLength(2);expect(document.querySelector("b")).toBeNull();
 for(const options of [[],[{value:"A",label:"One"},{value:"A",label:"Two"}],[{value:"",label:"Empty"}],[{value:"A",label:"汉".repeat(86)}],Array.from({length:65},(_,index)=>({value:String(index),label:"Label"}))]){view.rerender(<StepSelector options={options} value="A" label="Steps"/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByRole("list")).toBeNull();}
});
test("native choices validate canonical index strings and exact duplicate label lists",()=>{
 expect(validChoiceInput({variant:"steps",options:["0","1"],optionLabels:["Same","Same"]})).toBe(true);expect(validChoiceInput({variant:"tabs",options:["0"],optionLabels:[""]})).toBe(true);
 for(const config of [{variant:"steps",options:[],optionLabels:[]},{variant:"steps",options:["1"],optionLabels:["One"]},{variant:"tabs",options:["00"],optionLabels:["One"]},{variant:"steps",options:["0","1"],optionLabels:["One"]},{variant:"tabs",options:["0"]},{variant:"select",options:["A"],optionLabels:["A"]},{variant:"steps",options:["0"],optionLabels:["汉".repeat(86)]}])expect(validChoiceInput(config)).toBe(false);
});
test("ChoiceInput delegates indexed variants using native values and labels with readonly optional writes",()=>{
 const writes:string[]=[],props={options:["0","1"],optionLabels:["Same","Same"],title:"Native choice",onChange:(value:string)=>writes.push(value)},view=render(<ChoiceInput {...props} variant="steps" value="1"/>);fireEvent.click(screen.getAllByRole("button",{name:"Same"})[0]!);expect(writes).toEqual(["0"]);
 view.rerender(<ChoiceInput {...props} variant="tabs" value="0"/>);expect(screen.getByRole("tablist",{name:"Native choice"})).toBeTruthy();expect(screen.getAllByRole("tab")[0]!.getAttribute("aria-selected")).toBe("true");
 view.rerender(<ChoiceInput options={["0","1"]} optionLabels={["Same","Same"]} title="Readonly choice" variant="tabs" value="0"/>);expect(screen.getAllByRole("tab").every(tab=>(tab as HTMLButtonElement).disabled)).toBe(true);
});
